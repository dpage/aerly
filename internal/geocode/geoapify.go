package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// geoapifyRPS is Geoapify's documented free-tier ceiling. We self-limit rather
// than rely on being told off with a 429.
const geoapifyRPS = 5

const defaultGeoapifyBase = "https://api.geoapify.com"

// Geoapify geocodes via the Geoapify API. It is transport only: it ranks
// nothing and decides nothing, so all confidence policy lives in the resolver.
type Geoapify struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client

	limiter *rate.Limiter

	cacheMu sync.RWMutex
	cache   map[string]cached

	candMu    sync.RWMutex
	candCache map[string][]Candidate

	countryMu    sync.RWMutex
	countryCache map[string]string
}

// NewGeoapify builds a Geoapify geocoder. A blank key yields a client that will
// fail every request; callers must not construct one without a key (main.go
// leaves the Geocoder nil instead, which every handler already guards for).
func NewGeoapify(apiKey string) *Geoapify {
	return &Geoapify{
		APIKey:       apiKey,
		BaseURL:      defaultGeoapifyBase,
		HTTP:         &http.Client{Timeout: 10 * time.Second},
		limiter:      rate.NewLimiter(rate.Every(time.Second/geoapifyRPS), 1),
		cache:        map[string]cached{},
		candCache:    map[string][]Candidate{},
		countryCache: map[string]string{},
	}
}

func (g *Geoapify) throttle(ctx context.Context) error { return g.limiter.Wait(ctx) }

// geoapifyResult mirrors the API's format=json response. Verified against a
// live response on 17 July 2026; do not change these tags without re-checking.
type geoapifyResult struct {
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Formatted   string  `json:"formatted"`
	CountryCode string  `json:"country_code"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	Rank        struct {
		Confidence float64 `json:"confidence"`
		MatchType  string  `json:"match_type"`
	} `json:"rank"`
	Datasource struct {
		SourceName string `json:"sourcename"`
	} `json:"datasource"`
}

type geoapifyResponse struct {
	Results []geoapifyResult `json:"results"`
}

func (q Query) cacheKey() string {
	b := &strings.Builder{}
	b.WriteString(q.Text)
	b.WriteByte(0)
	b.WriteString(q.CountryCode)
	b.WriteByte(0)
	b.WriteString(q.Type)
	b.WriteByte(0)
	if q.Bias != nil {
		fmt.Fprintf(b, "%.4f,%.4f", q.Bias.Lat, q.Bias.Lon)
	}
	b.WriteByte(0)
	b.WriteString(strconv.Itoa(q.Limit))
	return b.String()
}

// Candidates returns Geoapify's ranked results for a query. An empty slice
// means nothing matched (not an error); a non-nil error means the lookup
// failed and the caller must not treat it as a miss.
func (g *Geoapify) Candidates(ctx context.Context, q Query) ([]Candidate, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" {
		return nil, nil
	}
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	key := q.cacheKey()
	g.candMu.RLock()
	hit, found := g.candCache[key]
	g.candMu.RUnlock()
	if found {
		return hit, nil
	}
	if err := g.throttle(ctx); err != nil {
		return nil, err
	}

	vals := url.Values{
		"text":   {q.Text},
		"format": {"json"},
		"limit":  {strconv.Itoa(q.Limit)},
		"apiKey": {g.APIKey},
	}
	if q.CountryCode != "" {
		vals.Set("filter", "countrycode:"+strings.ToLower(q.CountryCode))
	}
	if q.Bias != nil {
		vals.Set("bias", fmt.Sprintf("proximity:%v,%v", q.Bias.Lon, q.Bias.Lat))
	}
	if q.Type != "" {
		vals.Set("type", q.Type)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		g.BaseURL+"/v1/geocode/search?"+vals.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geoapify: status %d", resp.StatusCode)
	}
	var out geoapifyResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return nil, err
	}
	cands := make([]Candidate, 0, len(out.Results))
	for _, r := range out.Results {
		cands = append(cands, Candidate{
			Lat: r.Lat, Lon: r.Lon,
			Confidence:  r.Rank.Confidence,
			MatchType:   r.Rank.MatchType,
			Formatted:   r.Formatted,
			CountryCode: strings.ToLower(strings.TrimSpace(r.CountryCode)),
			SourceName:  r.Datasource.SourceName,
		})
	}
	g.candMu.Lock()
	if len(g.candCache) >= maxCacheEntries {
		g.candCache = map[string][]Candidate{}
	}
	g.candCache[key] = cands
	g.candMu.Unlock()
	return cands, nil
}

// Geocode returns the top-ranked result for a query, applying no confidence
// policy. Callers wanting a confidence decision use Candidates + Choose.
func (g *Geoapify) Geocode(ctx context.Context, query, countryCode string) (float64, float64, bool, error) {
	cands, err := g.Candidates(ctx, Query{Text: query, CountryCode: countryCode, Limit: 1})
	if err != nil || len(cands) == 0 {
		return 0, 0, false, err
	}
	return cands[0].Lat, cands[0].Lon, true, nil
}

// reverse fetches the first reverse-geocoding result for a coordinate.
func (g *Geoapify) reverse(ctx context.Context, lat, lon float64) (geoapifyResult, bool, error) {
	if err := g.throttle(ctx); err != nil {
		return geoapifyResult{}, false, err
	}
	vals := url.Values{
		"lat":    {strconv.FormatFloat(lat, 'f', -1, 64)},
		"lon":    {strconv.FormatFloat(lon, 'f', -1, 64)},
		"format": {"json"},
		"apiKey": {g.APIKey},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		g.BaseURL+"/v1/geocode/reverse?"+vals.Encode(), nil)
	if err != nil {
		return geoapifyResult{}, false, err
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return geoapifyResult{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return geoapifyResult{}, false, fmt.Errorf("geoapify: status %d", resp.StatusCode)
	}
	var out geoapifyResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return geoapifyResult{}, false, err
	}
	if len(out.Results) == 0 {
		return geoapifyResult{}, false, nil
	}
	return out.Results[0], true, nil
}

// GeocodeCountry resolves a place to its lowercase ISO country code.
func (g *Geoapify) GeocodeCountry(ctx context.Context, query string) (string, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", false, nil
	}
	g.countryMu.RLock()
	c, hit := g.countryCache[query]
	g.countryMu.RUnlock()
	if hit {
		return c, c != "", nil
	}
	cands, err := g.Candidates(ctx, Query{Text: query, Limit: 1})
	if err != nil {
		return "", false, err
	}
	code := ""
	if len(cands) > 0 {
		code = cands[0].CountryCode
	}
	g.storeCountry(query, code)
	return code, code != "", nil
}

// ReverseCountry resolves a coordinate to its lowercase ISO country code. ok is
// false when the point isn't in any country (e.g. open ocean).
func (g *Geoapify) ReverseCountry(ctx context.Context, lat, lon float64) (string, bool, error) {
	key := fmt.Sprintf("rev:%.5f,%.5f", lat, lon)
	g.countryMu.RLock()
	c, hit := g.countryCache[key]
	g.countryMu.RUnlock()
	if hit {
		return c, c != "", nil
	}
	r, ok, err := g.reverse(ctx, lat, lon)
	if err != nil {
		return "", false, err
	}
	code := ""
	if ok {
		code = strings.ToLower(strings.TrimSpace(r.CountryCode))
	}
	g.storeCountry(key, code)
	return code, code != "", nil
}

// ReversePlace resolves a coordinate to a "City, Country" label and its
// lowercase ISO country code. Not cached: called at most once per trip
// derivation, which is rare.
func (g *Geoapify) ReversePlace(ctx context.Context, lat, lon float64) (string, string, bool, error) {
	r, ok, err := g.reverse(ctx, lat, lon)
	if err != nil || !ok {
		return "", "", false, err
	}
	code := strings.ToLower(strings.TrimSpace(r.CountryCode))
	place := joinPlace(strings.TrimSpace(r.City), strings.TrimSpace(r.Country))
	return place, code, place != "", nil
}

func (g *Geoapify) storeCountry(key, code string) {
	g.countryMu.Lock()
	if len(g.countryCache) >= maxCacheEntries {
		g.countryCache = map[string]string{}
	}
	g.countryCache[key] = code
	g.countryMu.Unlock()
}
