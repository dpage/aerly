package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func osFlight(icao string) *store.Flight {
	f := &store.Flight{ID: 1}
	if icao != "" {
		f.ICAO24 = &icao
	}
	return f
}

func newOpenSky(t *testing.T, h http.HandlerFunc) *OpenSky {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	o := NewOpenSky("", "")
	o.BaseURL = srv.URL
	return o
}

func TestOpenSkyNoICAO(t *testing.T) {
	o := NewOpenSky("", "")
	if p, err := o.Track(context.Background(), osFlight(""), time.Now()); p != nil || err != nil {
		t.Errorf("no icao24 → (nil,nil), got %v %v", p, err)
	}
}

func TestOpenSkyRateLimited(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected rate-limit error")
	}
}

// A 429 fires the OnRateLimit hook so the operator can be alerted even though
// the wrapping DeadReckoner swallows the error.
func TestOpenSkyRateLimitFiresHook(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var gotProvider, gotDetail string
	o.OnRateLimit = func(provider, detail string) { gotProvider, gotDetail = provider, detail }
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected rate-limit error")
	}
	if gotProvider != "OpenSky" || gotDetail == "" {
		t.Errorf("hook got (%q,%q), want provider OpenSky + non-empty detail", gotProvider, gotDetail)
	}
}

func TestOpenSkyNon200(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected error for non-200")
	}
}

func TestOpenSkyBadJSON(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestOpenSkyEmptyStates(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	})
	if p, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); p != nil || err != nil {
		t.Errorf("empty states → (nil,nil), got %v %v", p, err)
	}
}

func TestOpenSkyPartialStateNoLatLon(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		// lon (idx5) / lat (idx6) are null.
		_, _ = w.Write([]byte(`{"time":100,"states":[["abc123","CALL","UK",100,100,null,null,null,false]]}`))
	})
	if p, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); p != nil || err != nil {
		t.Errorf("missing lat/lon → (nil,nil), got %v %v", p, err)
	}
}

func TestOpenSkyFullState(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("icao24") != "abc123" {
			t.Errorf("expected lowercased icao24 query, got %q", r.URL.RawQuery)
		}
		// idx: 0 icao,1 call,2 country,3 time_position,4 last_contact,
		// 5 lon,6 lat,7 baro_alt(m),8 on_ground,9 velocity(m/s),10 track
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[["abc123","BA286 ","UK",1700000111,1700000111,-30.5,45.2,10668,false,231.5,87.0]]}`))
	})
	p, err := o.Track(context.Background(), osFlight("ABC123"), time.Now())
	if err != nil || p == nil {
		t.Fatalf("expected a position, got %v %v", p, err)
	}
	if p.Lat != 45.2 || p.Lon != -30.5 {
		t.Errorf("lat/lon = (%v,%v)", p.Lat, p.Lon)
	}
	// time_position present → ts from that field.
	if p.Ts.Unix() != 1700000111 {
		t.Errorf("ts = %v, want time_position", p.Ts.Unix())
	}
	if p.AltitudeFt == nil || *p.AltitudeFt < 30000 {
		t.Errorf("altitude conversion wrong: %v", p.AltitudeFt)
	}
	if p.GroundspeedKt == nil || *p.GroundspeedKt < 400 {
		t.Errorf("groundspeed conversion wrong: %v", p.GroundspeedKt)
	}
	if p.HeadingDeg == nil || *p.HeadingDeg != 87 {
		t.Errorf("heading = %v, want 87", p.HeadingDeg)
	}
	if p.IsEstimated {
		t.Error("OpenSky fixes are real, not estimated")
	}
}

func TestOpenSkyNoTimePositionUsesGlobalTime(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		// time_position (idx3) null → fall back to top-level "time".
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[["abc123","C","UK",null,1,10.0,20.0]]}`))
	})
	p, err := o.Track(context.Background(), osFlight("abc123"), time.Now())
	if err != nil || p == nil {
		t.Fatalf("got %v %v", p, err)
	}
	if p.Ts.Unix() != 1700000000 {
		t.Errorf("ts = %d, want global time 1700000000", p.Ts.Unix())
	}
}

// newAuthedOpenSky wires an OpenSky against a fake API handler plus a fake
// OAuth2 token endpoint, and reports how many times a token was minted so the
// caching behaviour can be asserted.
func newAuthedOpenSky(t *testing.T, h http.HandlerFunc) (*OpenSky, *int) {
	t.Helper()
	var tokens int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("token request body unparseable: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "client_credentials" {
			t.Errorf("grant_type = %q, want client_credentials", got)
		}
		if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "csec" {
			t.Errorf("credentials not posted: %v", r.Form)
		}
		tokens++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-` + strconv.Itoa(tokens) + `","expires_in":1800}`))
	}))
	t.Cleanup(tokenSrv.Close)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	o := NewOpenSky("cid", "csec")
	o.BaseURL = srv.URL
	o.TokenURL = tokenSrv.URL
	return o, &tokens
}

// OpenSky replaced Basic auth with the OAuth2 client-credentials grant, so an
// authenticated request must carry a bearer token, and the token must be
// cached across calls rather than re-minted per request.
func TestOpenSkyBearerTokenAndCaching(t *testing.T) {
	var auths []string
	o, tokens := newAuthedOpenSky(t, func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		if _, _, ok := r.BasicAuth(); ok {
			t.Error("Basic auth must not be sent; OpenSky no longer accepts it")
		}
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	})
	for range 3 {
		if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err != nil {
			t.Fatalf("authed request failed: %v", err)
		}
	}
	if *tokens != 1 {
		t.Errorf("minted %d tokens across 3 requests, want 1 (cached)", *tokens)
	}
	for _, a := range auths {
		if a != "Bearer tok-1" {
			t.Errorf("Authorization = %q, want Bearer tok-1", a)
		}
	}
}

// A token can lapse mid-tick. A 401 should drop the cached token, mint a fresh
// one and retry once, transparently to the caller.
func TestOpenSkyRefreshesTokenOn401(t *testing.T) {
	var calls int
	o, tokens := newAuthedOpenSky(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-2" {
			t.Errorf("retry Authorization = %q, want the refreshed token", got)
		}
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[["abc123","C","UK",null,1,10.0,20.0]]}`))
	})
	p, err := o.Track(context.Background(), osFlight("abc123"), time.Now())
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if p == nil || p.Lat != 20.0 {
		t.Fatalf("expected the retried fix, got %v", p)
	}
	if calls != 2 || *tokens != 2 {
		t.Errorf("calls=%d tokens=%d, want 2 and 2", calls, *tokens)
	}
}

// A second consecutive 401 is credentials being wrong, not a lapsed token, so
// it must surface as an error rather than retrying forever.
func TestOpenSkyPersistent401Fails(t *testing.T) {
	var calls int
	o, _ := newAuthedOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected an error after a repeated 401")
	}
	if calls != 2 {
		t.Errorf("made %d attempts, want exactly 2 (one retry)", calls)
	}
}

// An unauthenticated tracker has no token to refresh, so a 401 is terminal
// immediately rather than triggering the retry path.
func TestOpenSkyAnonymous401NoRetry(t *testing.T) {
	var calls int
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected a 401 error")
	}
	if calls != 1 {
		t.Errorf("made %d attempts, want 1 (nothing to refresh when anonymous)", calls)
	}
}

// A failure to mint a token must surface rather than falling back to an
// anonymous request, which would silently drop us into the 400/day bucket.
func TestOpenSkyTokenEndpointFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the API must not be called when no token could be obtained")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(tokenSrv.Close)
	o := NewOpenSky("cid", "csec")
	o.BaseURL, o.TokenURL = srv.URL, tokenSrv.URL
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
		t.Error("expected the token failure to surface")
	}
}

func TestOpenSkyTokenResponseProblems(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"malformed json", `{`},
		{"no access_token", `{"expires_in":1800}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(tokenSrv.Close)
			o := NewOpenSky("cid", "csec")
			o.BaseURL, o.TokenURL = "http://127.0.0.1:1", tokenSrv.URL
			if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err == nil {
				t.Error("expected an error from an unusable token response")
			}
		})
	}
}

// A token response without a usable expires_in should still be cached, on the
// documented 30-minute default, rather than re-minted on every request.
func TestOpenSkyTokenDefaultTTL(t *testing.T) {
	var tokens int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokens++
		_, _ = w.Write([]byte(`{"access_token":"tok"}`))
	}))
	t.Cleanup(tokenSrv.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	}))
	t.Cleanup(srv.Close)
	o := NewOpenSky("cid", "csec")
	o.BaseURL, o.TokenURL = srv.URL, tokenSrv.URL
	for range 2 {
		if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err != nil {
			t.Fatalf("track failed: %v", err)
		}
	}
	if tokens != 1 {
		t.Errorf("minted %d tokens, want 1 (default TTL should still cache)", tokens)
	}
}

// A cached token nearing expiry is renewed before it lapses.
func TestOpenSkyTokenRenewedNearExpiry(t *testing.T) {
	clock := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	o, tokens := newAuthedOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	})
	o.now = func() time.Time { return clock }
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err != nil {
		t.Fatalf("first track: %v", err)
	}
	// 29 minutes on, the 30-minute token is inside the renewal skew.
	clock = clock.Add(29 * time.Minute)
	if _, err := o.Track(context.Background(), osFlight("abc123"), time.Now()); err != nil {
		t.Fatalf("second track: %v", err)
	}
	if *tokens != 2 {
		t.Errorf("minted %d tokens, want 2 (renewal near expiry)", *tokens)
	}
}

// The whole point of the batch: one request covering every tracked airframe,
// with the per-flight Track calls served from that single response.
func TestOpenSkyPrefetchBatchesIntoOneRequest(t *testing.T) {
	var requests int
	var asked []string
	o := newOpenSky(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		asked = r.URL.Query()["icao24"]
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[` +
			`["aaa111","BA1","UK",1700000111,1700000111,-1.0,51.0,10000,false,200.0,90.0],` +
			`["bbb222","BA2","UK",1700000112,1700000112,-2.0,52.0,11000,false,210.0,180.0]` +
			`]}`))
	})
	flights := []*store.Flight{osFlight("AAA111"), osFlight("bbb222"), osFlight("ccc333"), osFlight("")}
	flights[1].ID, flights[2].ID = 2, 3
	o.Prefetch(context.Background(), flights)

	if requests != 1 {
		t.Fatalf("made %d requests, want exactly 1 for the whole batch", requests)
	}
	if len(asked) != 3 {
		t.Errorf("asked for %v, want the three airframes with an icao24", asked)
	}

	// Each Track is now free: served from the batch, no further traffic.
	p1, err := o.Track(context.Background(), flights[0], time.Now())
	if err != nil || p1 == nil || p1.Lat != 51.0 {
		t.Errorf("first flight: %v %v", p1, err)
	}
	p2, err := o.Track(context.Background(), flights[1], time.Now())
	if err != nil || p2 == nil || p2.Lat != 52.0 {
		t.Errorf("second flight: %v %v", p2, err)
	}
	// ccc333 was in the batch but OpenSky didn't hear it: genuine silence, so
	// (nil, nil) and — crucially — no extra request.
	p3, err := o.Track(context.Background(), flights[2], time.Now())
	if p3 != nil || err != nil {
		t.Errorf("silent airframe → (nil,nil), got %v %v", p3, err)
	}
	if requests != 1 {
		t.Errorf("Track issued %d extra requests; the batch should have covered them", requests-1)
	}
}

// Duplicate airframes (the same aircraft on two tracked legs) are asked for
// once, and an empty flight set costs nothing at all.
func TestOpenSkyPrefetchDedupesAndSkipsEmpty(t *testing.T) {
	var requests int
	var asked []string
	o := newOpenSky(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		asked = r.URL.Query()["icao24"]
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	})
	o.Prefetch(context.Background(), nil)
	o.Prefetch(context.Background(), []*store.Flight{osFlight(""), nil})
	if requests != 0 {
		t.Errorf("made %d requests for a batch with no airframes, want 0", requests)
	}
	o.Prefetch(context.Background(), []*store.Flight{osFlight("aaa111"), osFlight(" AAA111 "), osFlight("aaa111")})
	if requests != 1 || len(asked) != 1 || asked[0] != "aaa111" {
		t.Errorf("requests=%d asked=%v, want one request for one deduped address", requests, asked)
	}
}

// Above the batch cap the addresses are split across requests, each of which
// costs a credit, and the results are merged.
func TestOpenSkyPrefetchChunks(t *testing.T) {
	var requests int
	o := newOpenSky(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := len(r.URL.Query()["icao24"]); got > 2 {
			t.Errorf("chunk carried %d addresses, want at most 2", got)
		}
		_, _ = w.Write([]byte(`{"time":1,"states":[]}`))
	})
	o.MaxBatch = 2
	o.Prefetch(context.Background(), []*store.Flight{
		osFlight("aaa111"), osFlight("bbb222"), osFlight("ccc333"), osFlight("ddd444"), osFlight("eee555"),
	})
	if requests != 3 {
		t.Errorf("made %d requests for 5 addresses at a cap of 2, want 3", requests)
	}
}

func TestChunks(t *testing.T) {
	if got := chunks([]string{"a", "b", "c"}, 0); len(got) != 1 || len(got[0]) != 3 {
		t.Errorf("a non-positive size should yield one chunk, got %v", got)
	}
	if got := chunks(nil, 5); got != nil {
		t.Errorf("no input → no chunks, got %v", got)
	}
}

// A failed batch must not degrade into one request per flight — that fan-out
// is exactly what the batching exists to prevent. Dead-reckoning covers the
// tick instead.
func TestOpenSkyFailedBatchSuppressesPerFlightFallback(t *testing.T) {
	var requests int
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	})
	flights := []*store.Flight{osFlight("aaa111"), osFlight("bbb222")}
	o.Prefetch(context.Background(), flights)
	if requests != 1 {
		t.Fatalf("batch made %d requests, want 1", requests)
	}
	for _, f := range flights {
		if p, err := o.Track(context.Background(), f, time.Now()); p != nil || err != nil {
			t.Errorf("after a failed batch → (nil,nil), got %v %v", p, err)
		}
	}
	if requests != 1 {
		t.Errorf("Track fanned out %d extra requests after a failed batch", requests-1)
	}
}

// Once the cache ages past its TTL, Track falls back to a single-airframe
// request so a caller that never batches still works.
func TestOpenSkyCacheExpiryFallsBackToSingleFetch(t *testing.T) {
	clock := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	var requests int
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[["aaa111","C","UK",null,1,10.0,20.0]]}`))
	})
	o.now = func() time.Time { return clock }
	f := osFlight("aaa111")
	o.Prefetch(context.Background(), []*store.Flight{f})
	if _, err := o.Track(context.Background(), f, time.Now()); err != nil {
		t.Fatalf("cached track: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d, want the batch to have served the Track", requests)
	}
	clock = clock.Add(3 * time.Minute) // past the 2-minute default TTL
	if _, err := o.Track(context.Background(), f, time.Now()); err != nil {
		t.Fatalf("post-expiry track: %v", err)
	}
	if requests != 2 {
		t.Errorf("requests=%d, want a fresh fetch once the cache expired", requests)
	}
}

// A state vector in the batch that carries no fix is treated as silence, and a
// vector with a fix wins over a positionless duplicate for the same airframe.
func TestOpenSkyBatchPrefersVectorWithFix(t *testing.T) {
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"time":1700000000,"states":[` +
			`["aaa111","C","UK",null,1,null,null],` +
			`["aaa111","C","UK",null,1,10.0,20.0],` +
			`["","C","UK",null,1,10.0,20.0],` +
			`["bbb222","C","UK",null,1,null,null]` +
			`]}`))
	})
	a, b := osFlight("aaa111"), osFlight("bbb222")
	o.Prefetch(context.Background(), []*store.Flight{a, b})
	p, err := o.Track(context.Background(), a, time.Now())
	if err != nil || p == nil || p.Lat != 20.0 {
		t.Errorf("expected the vector carrying a fix, got %v %v", p, err)
	}
	if p, err := o.Track(context.Background(), b, time.Now()); p != nil || err != nil {
		t.Errorf("positionless vector → (nil,nil), got %v %v", p, err)
	}
}

// After a 429 we stop calling OpenSky for a spell rather than burning the
// recovery window, and the quiet period doubles on repeated rejections.
func TestOpenSkyBacksOffAfterRateLimit(t *testing.T) {
	clock := time.Date(2026, 7, 24, 14, 55, 0, 0, time.UTC)
	var requests int
	o := newOpenSky(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusTooManyRequests)
	})
	o.now = func() time.Time { return clock }
	f := osFlight("aaa111")

	o.Prefetch(context.Background(), []*store.Flight{f})
	if requests != 1 {
		t.Fatalf("requests=%d, want the first batch to have gone out", requests)
	}
	// Still inside the five-minute backoff: no traffic from either path.
	clock = clock.Add(4 * time.Minute)
	o.Prefetch(context.Background(), []*store.Flight{f})
	if p, err := o.Track(context.Background(), f, time.Now()); p != nil || err != nil {
		t.Errorf("whilst backing off → (nil,nil), got %v %v", p, err)
	}
	if requests != 1 {
		t.Errorf("made %d requests whilst backing off, want none beyond the first", requests-1)
	}
	// Past it, we try again — and the second 429 doubles the quiet period.
	clock = clock.Add(2 * time.Minute)
	o.Prefetch(context.Background(), []*store.Flight{f})
	if requests != 2 {
		t.Fatalf("requests=%d, want a retry once the backoff elapsed", requests)
	}
	clock = clock.Add(6 * time.Minute)
	o.Prefetch(context.Background(), []*store.Flight{f})
	if requests != 2 {
		t.Errorf("backoff did not double: made a request %v after the second 429", 6*time.Minute)
	}
	clock = clock.Add(5 * time.Minute) // 11 min > the doubled 10-minute period
	o.Prefetch(context.Background(), []*store.Flight{f})
	if requests != 3 {
		t.Errorf("requests=%d, want a retry after the doubled backoff elapsed", requests)
	}
}

// The backoff is capped, and a success clears it so tracking resumes at full
// cadence rather than staying throttled for the rest of the day.
func TestOpenSkyBackoffCapAndReset(t *testing.T) {
	o := NewOpenSky("", "")
	o.backoffFor = maxRateLimitBackoff
	o.enterBackoff()
	if o.backoffFor != maxRateLimitBackoff {
		t.Errorf("backoff = %v, want it capped at %v", o.backoffFor, maxRateLimitBackoff)
	}
	o.clearBackoff()
	if _, backing := o.backingOff(); backing {
		t.Error("a success should clear the backoff")
	}
	if o.backoffFor != 0 {
		t.Errorf("backoffFor = %v, want reset to zero", o.backoffFor)
	}
}

// The operator-facing hint has to name the remedy that matches the tier: the
// anonymous 400/day bucket needs credentials, an authenticated 4000/day one
// needs a wider interval.
func TestOpenSkyRateLimitHintByTier(t *testing.T) {
	anon := NewOpenSky("", "")
	if h := anon.rateLimitHint(); !strings.Contains(h, "400/day") || !strings.Contains(h, "OPENSKY_CLIENT_ID") {
		t.Errorf("anonymous hint should point at credentials, got %q", h)
	}
	authed := NewOpenSky("cid", "csec")
	h := authed.rateLimitHint()
	if !strings.Contains(h, "4000/day") || !strings.Contains(h, "POLL_INTERVAL") {
		t.Errorf("authenticated hint should point at the poll interval, got %q", h)
	}
	if strings.Contains(h, "OPENSKY_CLIENT_ID") {
		t.Error("authenticated hint must not tell the operator to set credentials they already have")
	}
}

// Defaults are used when the tunables are left at their zero values.
func TestOpenSkyZeroValueDefaults(t *testing.T) {
	o := &OpenSky{}
	if o.batchSize() != 200 {
		t.Errorf("batchSize = %d, want the 200 default", o.batchSize())
	}
	if o.cacheTTL() != 2*time.Minute {
		t.Errorf("cacheTTL = %v, want the 2m default", o.cacheTTL())
	}
	if o.tokenURL() != defaultTokenURL {
		t.Errorf("tokenURL = %q, want the OpenSky default", o.tokenURL())
	}
	if o.clock().IsZero() {
		t.Error("clock should fall back to time.Now")
	}
}

// The wrappers must forward Prefetch, or composing OpenSky into the real chain
// would quietly lose the batching.
func TestPrefetchForwardsThroughWrappers(t *testing.T) {
	inner := &fakeBatchTracker{}
	chain := NewDeadReckoner(NewSpeedGate(inner, nil), nil)
	flights := []*store.Flight{osFlight("aaa111")}
	Prefetch(context.Background(), chain, flights)
	if inner.calls != 1 || len(inner.got) != 1 {
		t.Errorf("inner tracker saw calls=%d flights=%d, want 1 and 1", inner.calls, len(inner.got))
	}
	// A tracker that can't batch is simply skipped, not an error.
	Prefetch(context.Background(), NewDeadReckoner(NewStub(), nil), flights)
}

type fakeBatchTracker struct {
	calls int
	got   []*store.Flight
}

func (f *fakeBatchTracker) Track(context.Context, *store.Flight, time.Time) (*store.Position, error) {
	return nil, nil //nolint:nilnil // not exercised by the prefetch test
}

func (f *fakeBatchTracker) Prefetch(_ context.Context, flights []*store.Flight) {
	f.calls++
	f.got = flights
}

func TestStateHelpers(t *testing.T) {
	st := []interface{}{"a", nil, 3.0}
	if _, ok := stateFloat(st, 99); ok {
		t.Error("out-of-range index should be !ok")
	}
	if _, ok := stateFloat(st, 1); ok {
		t.Error("nil element should be !ok")
	}
	if _, ok := stateFloat(st, 0); ok {
		t.Error("non-float element should be !ok")
	}
	v, ok := stateFloat(st, 2)
	if !ok || v != 3.0 {
		t.Errorf("stateFloat = %v,%v", v, ok)
	}
	if _, ok := stateInt(st, 99); ok {
		t.Error("out-of-range int should be !ok")
	}
	if _, ok := stateInt(st, 1); ok {
		t.Error("nil int should be !ok")
	}
	if _, ok := stateInt(st, 0); ok {
		t.Error("non-numeric int should be !ok")
	}
	n, ok := stateInt(st, 2)
	if !ok || n != 3 {
		t.Errorf("stateInt = %v,%v", n, ok)
	}
}
