// Package geocode turns free-text addresses into coordinates so addressed plan
// parts (hotels, taxis, dining, …) can be plotted on the map.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Geocoder turns a free-text address into coordinates, and a place into its
// ISO country. ok is false when the query simply couldn't be located (not an
// error).
type Geocoder interface {
	Geocode(ctx context.Context, query string) (lat, lon float64, ok bool, err error)
	// GeocodeCountry resolves a place to its lowercase ISO 3166-1 alpha-2
	// country code (e.g. "es"). ok is false when no country was found.
	GeocodeCountry(ctx context.Context, query string) (iso2 string, ok bool, err error)
}

// Nominatim geocodes via an OpenStreetMap Nominatim server. It honours the
// public-server usage policy: a descriptive User-Agent, at most one request per
// second, and an in-memory result cache so repeat lookups don't hit the network.
type Nominatim struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client

	rateMu sync.Mutex
	last   time.Time
	minGap time.Duration

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
		BaseURL:      "https://nominatim.openstreetmap.org",
		UserAgent:    userAgent,
		HTTP:         &http.Client{Timeout: 10 * time.Second},
		minGap:       time.Second,
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

	n.throttle()

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
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
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

	n.throttle()

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
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return "", false, err
	}
	code := ""
	if len(results) > 0 {
		code = strings.ToLower(strings.TrimSpace(results[0].Address.CountryCode))
	}
	n.countryMu.Lock()
	n.countryCache[query] = code
	n.countryMu.Unlock()
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
	n.cache[q] = c
	n.cacheMu.Unlock()
}

// throttle blocks until at least minGap has elapsed since the last request,
// keeping us within the one-request-per-second policy under concurrency.
func (n *Nominatim) throttle() {
	n.rateMu.Lock()
	defer n.rateMu.Unlock()
	if gap := time.Since(n.last); gap < n.minGap {
		time.Sleep(n.minGap - gap)
	}
	n.last = time.Now()
}
