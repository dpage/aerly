// Package geocode turns free-text addresses into coordinates so addressed plan
// parts (hotels, taxis, dining, …) can be plotted on the map.
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

// maxResponseBytes caps how much of a Nominatim response we read before
// decoding, so a misbehaving/compromised endpoint can't stream an unbounded
// body into memory. Real search/reverse responses are a few KiB.
const maxResponseBytes = 1 << 20

// maxCacheEntries bounds each in-memory cache. Geocode keys derive from
// user-supplied free text / coordinates, so without a bound a user could grow
// the maps for the process lifetime. When the cap is hit the cache is cleared
// (a coarse but simple eviction for a best-effort cache).
const maxCacheEntries = 8192

// Geocoder turns a free-text address into coordinates, and a place into its
// ISO country. ok is false when the query simply couldn't be located (not an
// error).
type Geocoder interface {
	Geocode(ctx context.Context, query string) (lat, lon float64, ok bool, err error)
	// GeocodeCountry resolves a place to its lowercase ISO 3166-1 alpha-2
	// country code (e.g. "es"). ok is false when no country was found.
	GeocodeCountry(ctx context.Context, query string) (iso2 string, ok bool, err error)
	// ReverseCountry resolves a coordinate to its lowercase ISO 3166-1 alpha-2
	// country code. ok is false when the point isn't in any country (e.g. open
	// ocean). More reliable than GeocodeCountry for plan endpoints, whose
	// coordinates are already known but whose labels can be ambiguous.
	ReverseCountry(ctx context.Context, lat, lon float64) (iso2 string, ok bool, err error)
}

// Nominatim geocodes via an OpenStreetMap Nominatim server. It honours the
// public-server usage policy: a descriptive User-Agent, at most one request per
// second, and an in-memory result cache so repeat lookups don't hit the network.
type Nominatim struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client

	limiter *rate.Limiter

	cacheMu sync.RWMutex
	cache   map[string]cached

	countryMu    sync.RWMutex
	countryCache map[string]string // query → iso2 ("" = looked up, none found)
}

type cached struct {
	lat, lon float64
	ok       bool
}

// NewNominatim returns a geocoder pointed at the public OSM Nominatim service.
// userAgent must identify the application (policy requirement).
func NewNominatim(userAgent string) *Nominatim {
	return &Nominatim{
		BaseURL:   "https://nominatim.openstreetmap.org",
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		// One request/second (policy), burst 1. Wait(ctx) respects cancellation
		// and, unlike a mutex+sleep, never serialises callers behind a held lock.
		limiter:      rate.NewLimiter(rate.Every(time.Second), 1),
		cache:        map[string]cached{},
		countryCache: map[string]string{},
	}
}

func (n *Nominatim) Geocode(ctx context.Context, query string) (float64, float64, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, 0, false, nil
	}
	if c, hit := n.cached(query); hit {
		return c.lat, c.lon, c.ok, nil
	}

	if err := n.throttle(ctx); err != nil {
		return 0, 0, false, err
	}

	endpoint := n.BaseURL + "/search?" + url.Values{
		"q": {query}, "format": {"json"}, "limit": {"1"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, false, err
	}
	req.Header.Set("User-Agent", n.UserAgent)
	resp, err := n.HTTP.Do(req)
	if err != nil {
		return 0, 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, false, fmt.Errorf("nominatim: status %d", resp.StatusCode)
	}
	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&results); err != nil {
		return 0, 0, false, err
	}
	var c cached
	if len(results) > 0 {
		lat, errLat := strconv.ParseFloat(results[0].Lat, 64)
		lon, errLon := strconv.ParseFloat(results[0].Lon, 64)
		if errLat == nil && errLon == nil {
			c = cached{lat: lat, lon: lon, ok: true}
		}
	}
	n.store(query, c)
	return c.lat, c.lon, c.ok, nil
}

func (n *Nominatim) GeocodeCountry(ctx context.Context, query string) (string, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", false, nil
	}
	n.countryMu.RLock()
	c, hit := n.countryCache[query]
	n.countryMu.RUnlock()
	if hit {
		return c, c != "", nil
	}

	if err := n.throttle(ctx); err != nil {
		return "", false, err
	}

	endpoint := n.BaseURL + "/search?" + url.Values{
		"q": {query}, "format": {"json"}, "limit": {"1"}, "addressdetails": {"1"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", n.UserAgent)
	resp, err := n.HTTP.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("nominatim: status %d", resp.StatusCode)
	}
	var results []struct {
		Address struct {
			CountryCode string `json:"country_code"`
		} `json:"address"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&results); err != nil {
		return "", false, err
	}
	code := ""
	if len(results) > 0 {
		code = strings.ToLower(strings.TrimSpace(results[0].Address.CountryCode))
	}
	n.storeCountry(query, code)
	return code, code != "", nil
}

// ReverseCountry resolves a coordinate to its lowercase ISO country code via
// Nominatim's /reverse endpoint. Cached (alongside the forward country cache,
// keyed by the coordinate) and rate-limited like the other lookups.
func (n *Nominatim) ReverseCountry(ctx context.Context, lat, lon float64) (string, bool, error) {
	key := fmt.Sprintf("rev:%.5f,%.5f", lat, lon)
	n.countryMu.RLock()
	c, hit := n.countryCache[key]
	n.countryMu.RUnlock()
	if hit {
		return c, c != "", nil
	}

	if err := n.throttle(ctx); err != nil {
		return "", false, err
	}

	endpoint := n.BaseURL + "/reverse?" + url.Values{
		"lat":            {strconv.FormatFloat(lat, 'f', -1, 64)},
		"lon":            {strconv.FormatFloat(lon, 'f', -1, 64)},
		"format":         {"json"},
		"zoom":           {"3"}, // country level — we only want the country.
		"addressdetails": {"1"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", n.UserAgent)
	resp, err := n.HTTP.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("nominatim: status %d", resp.StatusCode)
	}
	// /reverse returns a single object (not an array like /search).
	var result struct {
		Address struct {
			CountryCode string `json:"country_code"`
		} `json:"address"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&result); err != nil {
		return "", false, err
	}
	code := strings.ToLower(strings.TrimSpace(result.Address.CountryCode))
	n.storeCountry(key, code)
	return code, code != "", nil
}

func (n *Nominatim) cached(q string) (cached, bool) {
	n.cacheMu.RLock()
	defer n.cacheMu.RUnlock()
	c, ok := n.cache[q]
	return c, ok
}

func (n *Nominatim) store(q string, c cached) {
	n.cacheMu.Lock()
	if len(n.cache) >= maxCacheEntries {
		n.cache = map[string]cached{}
	}
	n.cache[q] = c
	n.cacheMu.Unlock()
}

func (n *Nominatim) storeCountry(key, code string) {
	n.countryMu.Lock()
	if len(n.countryCache) >= maxCacheEntries {
		n.countryCache = map[string]string{}
	}
	n.countryCache[key] = code
	n.countryMu.Unlock()
}

// throttle waits for the rate limiter (one request/second policy), respecting
// context cancellation. It does not hold any lock while waiting.
func (n *Nominatim) throttle(ctx context.Context) error {
	return n.limiter.Wait(ctx)
}
