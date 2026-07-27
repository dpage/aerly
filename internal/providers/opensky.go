package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// OpenSky is a Tracker backed by the OpenSky Network's public state-vectors
// API. Free for non-commercial use, and metered in daily "credits": 400/day
// anonymously (bucketed by source IP, so the whole instance shares one
// allowance), 4000/day for a registered API client, and 8000/day for accounts
// that feed data back to the network.
//
// Tracking is keyed on the aircraft's ICAO 24-bit address ("icao24"), six
// lowercase hex characters such as "a1b2c3" — this is the airframe ID, not
// the flight number. Flights with no icao24 yield (nil, nil) so the caller
// (typically a DeadReckoner) can decide whether to extrapolate.
//
// It implements BatchTracker: Prefetch pulls every tracked airframe in one
// request per tick and Track then serves from that cache. A /states/all
// filtered by icao24 costs one credit however many addresses it names, so the
// batched shape keeps the daily burn flat at one credit per tick instead of
// one per flight per tick.
//
// Authentication is OAuth2 client credentials. OpenSky retired HTTP Basic auth
// and now rejects username/password outright, demoting such requests to the
// 400/day anonymous bucket, so credentials here mean a client ID and secret
// minted on the OpenSky account page.
type OpenSky struct {
	BaseURL      string
	ClientID     string // OAuth2 client credentials from the OpenSky account page
	ClientSecret string
	// TokenURL is the OpenSky Keycloak token endpoint. Overridable for tests.
	TokenURL string
	HTTP     *http.Client
	// OnRateLimit, when set, is invoked on a 429 from OpenSky so the operator
	// can be alerted (the DeadReckoner above us hides the error otherwise).
	OnRateLimit RateLimitReporter

	// CacheTTL bounds how long a Prefetch result is served to Track. It only
	// has to outlive one tick's per-flight loop; beyond it, Track falls back to
	// a single-airframe request so a caller that never batches still works.
	CacheTTL time.Duration

	// MaxBatch caps the icao24 addresses named in one request, so a very busy
	// instance splits into a handful of requests rather than building a URL no
	// proxy will forward. Each chunk costs one credit.
	MaxBatch int

	mu sync.Mutex
	// token / tokenExp cache the bearer token between requests; OpenSky's
	// tokens last 30 minutes.
	token    string
	tokenExp time.Time
	// cache holds the most recent batch, keyed by lowercase icao24, alongside
	// the server timestamp that came with it. batchAt is when the batch was
	// attempted — set on failure too, so a failed batch suppresses the
	// per-flight fallback for that tick instead of fanning out N requests.
	cache      map[string][]any
	cacheTime  int64
	batchAt    time.Time
	backoffFor time.Duration
	// backoffUntil suppresses all upstream traffic after a 429. Hammering a
	// provider that has just told us to stop only burns the recovery window;
	// the DeadReckoner covers the gap in the meantime.
	backoffUntil time.Time
	// now is time.Now, overridable in tests to drive the backoff and cache
	// clocks deterministically.
	now func() time.Time
}

// defaultTokenURL is OpenSky's Keycloak client-credentials endpoint.
const defaultTokenURL = "https://auth.opensky-network.org/auth/realms/opensky-network/protocol/openid-connect/token"

// Backoff bounds after a 429. OpenSky's daily credits refill on a rolling
// basis, so there is no point retrying every minute; we start at five minutes
// and double up to an hour, resetting on the first successful call.
const (
	minRateLimitBackoff = 5 * time.Minute
	maxRateLimitBackoff = time.Hour
)

func NewOpenSky(clientID, clientSecret string) *OpenSky {
	return &OpenSky{
		BaseURL:      "https://opensky-network.org/api",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     defaultTokenURL,
		HTTP:         &http.Client{Timeout: 15 * time.Second},
		CacheTTL:     2 * time.Minute,
		MaxBatch:     200,
		now:          time.Now,
	}
}

// openSkyStates is the response shape of /api/states/all. The `states` field
// is an array of arrays — each inner array is a fixed-position vector. We
// unpack only the fields we care about; nil indicates "field absent".
type openSkyStates struct {
	Time   int64           `json:"time"`
	States [][]interface{} `json:"states"`
}

// State-vector positions defined by OpenSky:
//
//	[0] icao24            string
//	[1] callsign          string|nil
//	[2] origin_country    string
//	[3] time_position     int|nil   (seconds since epoch of last position update)
//	[4] last_contact      int
//	[5] longitude         float|nil
//	[6] latitude          float|nil
//	[7] baro_altitude     float|nil (metres)
//	[8] on_ground         bool
//	[9] velocity          float|nil (m/s)
//	[10] true_track       float|nil (degrees clockwise from north)
//	[11] vertical_rate    float|nil
//	[12] sensors          int[]|nil
//	[13] geo_altitude     float|nil
//	[14] squawk           string|nil
//	[15] spi              bool
//	[16] position_source  int

func (o *OpenSky) clock() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

// authed reports whether we hold OAuth2 client credentials. Without them every
// request lands in the 400/day anonymous bucket shared across the source IP.
func (o *OpenSky) authed() bool { return o.ClientID != "" }

// Prefetch fetches every distinct airframe among flights in as few requests as
// the batch cap allows, caching the result for the Track calls that follow.
// Best-effort: on any failure the cache is left empty but stamped, so Track
// reports "no fix" for this tick rather than reverting to one request each.
func (o *OpenSky) Prefetch(ctx context.Context, flights []*store.Flight) {
	icaos := make([]string, 0, len(flights))
	seen := make(map[string]struct{}, len(flights))
	for _, f := range flights {
		if f == nil || f.ICAO24 == nil {
			continue
		}
		icao := strings.ToLower(strings.TrimSpace(*f.ICAO24))
		if icao == "" {
			continue
		}
		if _, dup := seen[icao]; dup {
			continue
		}
		seen[icao] = struct{}{}
		icaos = append(icaos, icao)
	}
	if len(icaos) == 0 {
		return
	}
	if until, backing := o.backingOff(); backing {
		slog.Debug("opensky: batch skipped, backing off after rate limit",
			"until", until, "airframes", len(icaos))
		return
	}

	cache := make(map[string][]any, len(icaos))
	var serverTime int64
	var failed bool
	for _, chunk := range chunks(icaos, o.batchSize()) {
		out, err := o.fetchStates(ctx, chunk)
		if err != nil {
			slog.Warn("opensky: batch fetch failed", "airframes", len(chunk), "err", err)
			failed = true
			break
		}
		if out.Time > serverTime {
			serverTime = out.Time
		}
		for _, st := range out.States {
			id, _ := stateStr(st, 0)
			id = strings.ToLower(strings.TrimSpace(id))
			if id == "" {
				continue
			}
			// Prefer a state vector that actually carries a fix; OpenSky can
			// return a positionless vector for an airframe it has only just
			// heard from.
			if _, dup := cache[id]; dup && !hasFix(st) {
				continue
			}
			cache[id] = st
		}
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	o.batchAt = o.clock()
	o.cacheTime = serverTime
	if failed {
		o.cache = nil
		return
	}
	o.cache = cache
}

func (o *OpenSky) batchSize() int {
	if o.MaxBatch > 0 {
		return o.MaxBatch
	}
	return 200
}

func (o *OpenSky) cacheTTL() time.Duration {
	if o.CacheTTL > 0 {
		return o.CacheTTL
	}
	return 2 * time.Minute
}

// chunks splits s into consecutive slices of at most n elements.
func chunks(s []string, n int) [][]string {
	if n <= 0 {
		n = len(s)
	}
	var out [][]string
	for i := 0; i < len(s); i += n {
		out = append(out, s[i:min(i+n, len(s))])
	}
	return out
}

func hasFix(state []any) bool {
	if _, ok := stateFloat(state, 6); !ok {
		return false
	}
	_, ok := stateFloat(state, 5)
	return ok
}

// backingOff reports whether a previous 429 still suppresses upstream traffic.
func (o *OpenSky) backingOff() (time.Time, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.backoffUntil, o.clock().Before(o.backoffUntil)
}

func (o *OpenSky) Track(ctx context.Context, f *store.Flight, _ time.Time) (*store.Position, error) {
	if f.ICAO24 == nil || *f.ICAO24 == "" {
		return nil, nil //nolint:nilnil // no aircraft id to query
	}
	icao := strings.ToLower(strings.TrimSpace(*f.ICAO24))

	// A recent batch answers for every airframe it was asked about, so a miss
	// there is genuine ADS-B silence rather than a reason to ask again.
	if state, cached := o.fromCache(icao); cached {
		if state == nil {
			return nil, nil //nolint:nilnil // covered by the batch, simply not heard
		}
		return o.position(f, state), nil
	}
	if _, backing := o.backingOff(); backing {
		return nil, nil //nolint:nilnil // suppressed after a 429; DeadReckoner covers it
	}

	out, err := o.fetchStates(ctx, []string{icao})
	if err != nil {
		return nil, err
	}
	// We filtered by icao24, but don't blindly trust states[0]: verify the
	// returned state vector is actually for the requested airframe (index 0 is
	// the icao24), and pick the first matching one that carries a usable fix —
	// guarding against an icao24 collision or a partial leading state.
	for _, st := range out.States {
		id, _ := stateStr(st, 0)
		if !strings.EqualFold(strings.TrimSpace(id), icao) || !hasFix(st) {
			continue
		}
		return o.positionAt(f, st, out.Time), nil
	}
	return nil, nil //nolint:nilnil // no matching state with a usable fix
}

// fromCache returns the cached state vector for icao and whether the cache is
// fresh enough to be authoritative. A fresh cache with no entry yields
// (nil, true): the batch asked and heard nothing.
func (o *OpenSky) fromCache(icao string) ([]any, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.batchAt.IsZero() || o.clock().Sub(o.batchAt) >= o.cacheTTL() {
		return nil, false
	}
	if o.cache == nil {
		// The batch ran but failed; suppress the per-flight fallback so one
		// upstream blip doesn't fan out into a request per tracked flight.
		return nil, true
	}
	st, ok := o.cache[icao]
	if !ok || !hasFix(st) {
		return nil, true
	}
	return st, true
}

func (o *OpenSky) position(f *store.Flight, state []any) *store.Position {
	o.mu.Lock()
	ts := o.cacheTime
	o.mu.Unlock()
	return o.positionAt(f, state, ts)
}

// positionAt converts a state vector into a store.Position, preferring the
// vector's own time_position over the response-level server time.
func (o *OpenSky) positionAt(f *store.Flight, state []any, serverTime int64) *store.Position {
	lat, _ := stateFloat(state, 6)
	lon, _ := stateFloat(state, 5)
	ts := time.Unix(serverTime, 0).UTC()
	if tp, ok := stateInt(state, 3); ok {
		ts = time.Unix(tp, 0).UTC()
	}
	pos := &store.Position{
		FlightID:    f.ID,
		Ts:          ts,
		Lat:         lat,
		Lon:         lon,
		IsEstimated: false,
	}
	if v, ok := stateFloat(state, 7); ok {
		a := int32(v * 3.28084) // metres → feet
		pos.AltitudeFt = &a
	}
	if v, ok := stateFloat(state, 9); ok {
		g := int32(v * 1.94384) // m/s → knots
		pos.GroundspeedKt = &g
	}
	if v, ok := stateFloat(state, 10); ok {
		h := int16(v)
		pos.HeadingDeg = &h
	}
	return pos
}

// fetchStates issues one /states/all filtered by the given icao24 addresses.
// OpenSky charges a single credit for a serial-filtered query regardless of how
// many addresses it names, which is what makes batching worthwhile.
func (o *OpenSky) fetchStates(ctx context.Context, icaos []string) (*openSkyStates, error) {
	out, err := o.doStates(ctx, icaos, true)
	if err == nil {
		o.clearBackoff()
	}
	return out, err
}

// doStates performs the request, refreshing the bearer token once if OpenSky
// rejects the one we hold (tokens last 30 minutes and can expire mid-tick).
func (o *OpenSky) doStates(ctx context.Context, icaos []string, mayRetry bool) (*openSkyStates, error) {
	q := url.Values{}
	for _, icao := range icaos {
		q.Add("icao24", icao)
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		o.BaseURL+"/states/all?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if o.authed() {
		tok, tokErr := o.bearer(ctx)
		if tokErr != nil {
			return nil, tokErr
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aerly (https://github.com/dpage/aerly)")

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		o.enterBackoff()
		if o.OnRateLimit != nil {
			o.OnRateLimit("OpenSky", o.rateLimitHint())
		}
		return nil, errors.New("opensky rate limit; daily credit allowance exhausted")
	case http.StatusUnauthorized:
		// Either the cached token expired mid-flight, or the credentials are
		// wrong. Drop the token and try once more; a second 401 is terminal.
		o.clearToken()
		if o.authed() && mayRetry {
			return o.doStates(ctx, icaos, false)
		}
		return nil, errors.New("opensky rejected our credentials (401); check OPENSKY_CLIENT_ID/OPENSKY_CLIENT_SECRET")
	case http.StatusOK:
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("opensky /states/all -> %d: %s", resp.StatusCode, body)
	}
	var out openSkyStates
	// Cap the decoded body so a misbehaving/compromised upstream can't make us
	// buffer an unbounded response into memory (matches the AeroDataBox cap).
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// rateLimitHint describes what the operator can actually do about a 429. The
// remedy differs sharply by tier: anonymous access shares a 400/day bucket
// across the whole source IP, whereas an authenticated client gets 4000/day
// and is far more likely to be suffering from too tight a poll interval.
func (o *OpenSky) rateLimitHint() string {
	if !o.authed() {
		return "anonymous daily credit allowance (400/day, shared per source IP) exhausted; " +
			"set OPENSKY_CLIENT_ID/OPENSKY_CLIENT_SECRET for the 4000/day authenticated allowance"
	}
	return "authenticated daily credit allowance (4000/day) exhausted; widen POLL_INTERVAL, " +
		"or feed data back to OpenSky to earn the 8000/day contributor allowance"
}

// enterBackoff suppresses upstream traffic for a spell after a 429, doubling
// the quiet period on each consecutive rejection up to the cap.
func (o *OpenSky) enterBackoff() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.backoffFor <= 0 {
		o.backoffFor = minRateLimitBackoff
	} else {
		o.backoffFor = min(o.backoffFor*2, maxRateLimitBackoff)
	}
	o.backoffUntil = o.clock().Add(o.backoffFor)
	slog.Warn("opensky: rate limited, pausing upstream calls",
		"until", o.backoffUntil, "backoff", o.backoffFor, "authed", o.authed())
}

func (o *OpenSky) clearBackoff() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.backoffFor = 0
	o.backoffUntil = time.Time{}
}

func (o *OpenSky) clearToken() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.token = ""
	o.tokenExp = time.Time{}
}

// tokenSkew renews a little before nominal expiry so a token doesn't lapse
// between the check and the request landing.
const tokenSkew = time.Minute

// bearer returns a valid access token, fetching a fresh one when the cached
// token is missing or close to expiry. OpenSky issues 30-minute tokens via the
// OAuth2 client-credentials grant.
func (o *OpenSky) bearer(ctx context.Context) (string, error) {
	o.mu.Lock()
	if o.token != "" && o.clock().Before(o.tokenExp.Add(-tokenSkew)) {
		tok := o.token
		o.mu.Unlock()
		return tok, nil
	}
	o.mu.Unlock()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", o.ClientID)
	form.Set("client_secret", o.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, "POST", o.tokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("opensky token -> %d: %s", resp.StatusCode, body)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", errors.New("opensky token response carried no access_token")
	}
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	o.mu.Lock()
	o.token = tr.AccessToken
	o.tokenExp = o.clock().Add(ttl)
	o.mu.Unlock()
	return tr.AccessToken, nil
}

func (o *OpenSky) tokenURL() string {
	if o.TokenURL != "" {
		return o.TokenURL
	}
	return defaultTokenURL
}

func stateFloat(state []interface{}, i int) (float64, bool) {
	if i >= len(state) || state[i] == nil {
		return 0, false
	}
	v, ok := state[i].(float64)
	return v, ok
}

func stateInt(state []interface{}, i int) (int64, bool) {
	if i >= len(state) || state[i] == nil {
		return 0, false
	}
	if v, ok := state[i].(float64); ok {
		return int64(v), true
	}
	return 0, false
}

func stateStr(state []interface{}, i int) (string, bool) {
	if i >= len(state) || state[i] == nil {
		return "", false
	}
	v, ok := state[i].(string)
	return v, ok
}
