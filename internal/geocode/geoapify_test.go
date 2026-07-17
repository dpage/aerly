package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const searchFixture = `{"results":[
  {"lat":51.5,"lon":-0.14,"formatted":"Test Hotel, Example Street, London",
   "country_code":"gb","rank":{"confidence":0.95,"match_type":"full_match"},
   "datasource":{"sourcename":"openstreetmap"}},
  {"lat":52.4,"lon":-1.9,"formatted":"Test Hotel, Example Road, Birmingham",
   "country_code":"gb","rank":{"confidence":0.42,"match_type":"inner_part"},
   "datasource":{"sourcename":"openstreetmap"}}]}`

func newTestGeoapify(t *testing.T, body string, status int) (*Geoapify, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RawQuery)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	g := NewGeoapify("test-key")
	g.BaseURL = srv.URL
	return g, &seen
}

func TestCandidatesParsesRanked(t *testing.T) {
	g, _ := newTestGeoapify(t, searchFixture, http.StatusOK)
	got, err := g.Candidates(context.Background(), Query{Text: "Test Hotel, London"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(got))
	}
	if got[0].Confidence != 0.95 || got[0].MatchType != "full_match" {
		t.Errorf("bad first candidate: %+v", got[0])
	}
	if got[0].Formatted != "Test Hotel, Example Street, London" {
		t.Errorf("bad formatted: %q", got[0].Formatted)
	}
	if got[0].SourceName != "openstreetmap" {
		t.Errorf("bad sourcename: %q", got[0].SourceName)
	}
}

func TestCandidatesSendsFilters(t *testing.T) {
	g, seen := newTestGeoapify(t, searchFixture, http.StatusOK)
	_, err := g.Candidates(context.Background(), Query{
		Text: "Test Hotel", CountryCode: "gb",
		Bias: &LatLon{Lat: 51.5, Lon: -0.14}, Type: "amenity", Limit: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q := (*seen)[0]
	for _, want := range []string{
		"filter=countrycode%3Agb", "bias=proximity%3A-0.14%2C51.5",
		"type=amenity", "limit=5", "apiKey=test-key", "format=json",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q missing %q", q, want)
		}
	}
}

func TestCandidatesErrorsOnBadStatus(t *testing.T) {
	g, _ := newTestGeoapify(t, `{}`, http.StatusTooManyRequests)
	if _, err := g.Candidates(context.Background(), Query{Text: "x"}); err == nil {
		t.Fatal("want an error on 429, got nil: a rate-limited provider must never look like a miss")
	}
}

func TestGeocodeReturnsTopCandidate(t *testing.T) {
	g, _ := newTestGeoapify(t, searchFixture, http.StatusOK)
	lat, lon, ok, err := g.Geocode(context.Background(), "Test Hotel, London", "")
	if err != nil || !ok {
		t.Fatalf("want a hit, got ok=%v err=%v", ok, err)
	}
	if lat != 51.5 || lon != -0.14 {
		t.Errorf("want the top candidate, got %v,%v", lat, lon)
	}
}

func TestGeocodeEmptyQueryIsMiss(t *testing.T) {
	g, _ := newTestGeoapify(t, searchFixture, http.StatusOK)
	if _, _, ok, err := g.Geocode(context.Background(), "   ", ""); ok || err != nil {
		t.Fatalf("blank query must miss without a request: ok=%v err=%v", ok, err)
	}
}

const reverseFixture = `{"results":[{"lat":51.5,"lon":-0.14,
  "city":"London","country":"United Kingdom","country_code":"gb",
  "formatted":"Example Street, London","rank":{"confidence":1,"match_type":"full_match"},
  "datasource":{"sourcename":"openstreetmap"}}]}`

func TestReversePlace(t *testing.T) {
	g, _ := newTestGeoapify(t, reverseFixture, http.StatusOK)
	place, iso, ok, err := g.ReversePlace(context.Background(), 51.5, -0.14)
	if err != nil || !ok {
		t.Fatalf("want a hit: ok=%v err=%v", ok, err)
	}
	if place != "London, United Kingdom" || iso != "gb" {
		t.Errorf("got %q / %q", place, iso)
	}
}

func TestReverseCountry(t *testing.T) {
	g, _ := newTestGeoapify(t, reverseFixture, http.StatusOK)
	iso, ok, err := g.ReverseCountry(context.Background(), 51.5, -0.14)
	if err != nil || !ok || iso != "gb" {
		t.Fatalf("got %q ok=%v err=%v", iso, ok, err)
	}
}

func TestGeocodeCountry(t *testing.T) {
	g, _ := newTestGeoapify(t, searchFixture, http.StatusOK)
	iso, ok, err := g.GeocodeCountry(context.Background(), "Test Hotel, London")
	if err != nil || !ok || iso != "gb" {
		t.Fatalf("got %q ok=%v err=%v", iso, ok, err)
	}
}

func TestReversePlaceOceanIsMiss(t *testing.T) {
	g, _ := newTestGeoapify(t, `{"results":[]}`, http.StatusOK)
	if _, _, ok, err := g.ReversePlace(context.Background(), 0, 0); ok || err != nil {
		t.Fatalf("open ocean must miss cleanly: ok=%v err=%v", ok, err)
	}
}

// TestReverseCountryOceanIsMiss mirrors TestReversePlaceOceanIsMiss for
// ReverseCountry: a coordinate with no reverse result (open ocean) must miss
// cleanly rather than error. Ported from the deleted Nominatim test
// TestNominatimReverseCountry_NoResult so the provider-agnostic guarantee
// stays covered.
func TestReverseCountryOceanIsMiss(t *testing.T) {
	g, _ := newTestGeoapify(t, `{"results":[]}`, http.StatusOK)
	if iso, ok, err := g.ReverseCountry(context.Background(), 0, 0); ok || err != nil || iso != "" {
		t.Fatalf("open ocean must miss cleanly: iso=%q ok=%v err=%v", iso, ok, err)
	}
}

// --- Cache-hit coverage ---
//
// The deleted Nominatim tests (TestNominatimGeocodeAndCache,
// TestNominatimGeocodeCountry, TestNominatimReverseCountry) each asserted that
// a repeat lookup is served from candCache/countryCache without a second HTTP
// call. Geoapify self-limits to geoapifyRPS rather than relying on a 429 to
// tell it off, so an untested cache is exactly the kind of thing that
// regresses silently and re-introduces the rate-limit risk the cache exists
// to avoid.

func TestCandidatesCachesRepeatLookup(t *testing.T) {
	g, seen := newTestGeoapify(t, searchFixture, http.StatusOK)
	q := Query{Text: "Test Hotel, London"}
	if _, err := g.Candidates(context.Background(), q); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	if _, err := g.Candidates(context.Background(), q); err != nil {
		t.Fatalf("second (cached) lookup: %v", err)
	}
	if got := len(*seen); got != 1 {
		t.Errorf("HTTP requests = %d, want 1 (second lookup should be served from candCache)", got)
	}
}

// TestCandidatesCachesMissWithoutRefetch pins down why the cache stores a
// negative result at all: a query with no matches must be remembered as a
// miss and replayed from cache, not re-fetched on every repeat.
func TestCandidatesCachesMissWithoutRefetch(t *testing.T) {
	g, seen := newTestGeoapify(t, `{"results":[]}`, http.StatusOK)
	q := Query{Text: "Nowhere At All"}
	got1, err := g.Candidates(context.Background(), q)
	if err != nil || len(got1) != 0 {
		t.Fatalf("first lookup: got %d candidates, err=%v, want an empty miss", len(got1), err)
	}
	got2, err := g.Candidates(context.Background(), q)
	if err != nil || len(got2) != 0 {
		t.Fatalf("second lookup: got %d candidates, err=%v, want the cached empty miss", len(got2), err)
	}
	if got := len(*seen); got != 1 {
		t.Errorf("HTTP requests = %d, want 1 (the miss should be cached, not re-fetched)", got)
	}
}

func TestGeocodeCountryCachesRepeatLookup(t *testing.T) {
	g, seen := newTestGeoapify(t, searchFixture, http.StatusOK)
	if _, ok, err := g.GeocodeCountry(context.Background(), "Test Hotel, London"); err != nil || !ok {
		t.Fatalf("first lookup: ok=%v err=%v", ok, err)
	}
	if _, ok, err := g.GeocodeCountry(context.Background(), "Test Hotel, London"); err != nil || !ok {
		t.Fatalf("second (cached) lookup: ok=%v err=%v", ok, err)
	}
	if got := len(*seen); got != 1 {
		t.Errorf("HTTP requests = %d, want 1 (second lookup should be served from countryCache)", got)
	}
}

// TestGeocodeCountryCachesMissWithoutRefetch mirrors the Candidates miss-cache
// test for the country cache: a place with no country (or no match at all)
// must be remembered as a miss.
func TestGeocodeCountryCachesMissWithoutRefetch(t *testing.T) {
	g, seen := newTestGeoapify(t, `{"results":[]}`, http.StatusOK)
	if code, ok, err := g.GeocodeCountry(context.Background(), "Nowhere At All"); err != nil || ok || code != "" {
		t.Fatalf("first lookup: code=%q ok=%v err=%v, want a clean miss", code, ok, err)
	}
	if code, ok, err := g.GeocodeCountry(context.Background(), "Nowhere At All"); err != nil || ok || code != "" {
		t.Fatalf("second lookup: code=%q ok=%v err=%v, want the cached miss", code, ok, err)
	}
	if got := len(*seen); got != 1 {
		t.Errorf("HTTP requests = %d, want 1 (the miss should be cached, not re-fetched)", got)
	}
}

func TestReverseCountryCachesRepeatLookup(t *testing.T) {
	g, seen := newTestGeoapify(t, reverseFixture, http.StatusOK)
	if _, ok, err := g.ReverseCountry(context.Background(), 51.5, -0.14); err != nil || !ok {
		t.Fatalf("first lookup: ok=%v err=%v", ok, err)
	}
	if _, ok, err := g.ReverseCountry(context.Background(), 51.5, -0.14); err != nil || !ok {
		t.Fatalf("second (cached) lookup: ok=%v err=%v", ok, err)
	}
	if got := len(*seen); got != 1 {
		t.Errorf("HTTP requests = %d, want 1 (second lookup should be served from countryCache)", got)
	}
}

// TestReverseCountryCachesMissWithoutRefetch mirrors
// TestNominatimReverseCountry_NoResult's cache assertion: an open-ocean
// coordinate must be remembered as a miss and replayed from cache.
func TestReverseCountryCachesMissWithoutRefetch(t *testing.T) {
	g, seen := newTestGeoapify(t, `{"results":[]}`, http.StatusOK)
	if code, ok, err := g.ReverseCountry(context.Background(), 0, 0); err != nil || ok || code != "" {
		t.Fatalf("first lookup: code=%q ok=%v err=%v, want a clean miss", code, ok, err)
	}
	if code, ok, err := g.ReverseCountry(context.Background(), 0, 0); err != nil || ok || code != "" {
		t.Fatalf("second lookup: code=%q ok=%v err=%v, want the cached miss", code, ok, err)
	}
	if got := len(*seen); got != 1 {
		t.Errorf("HTTP requests = %d, want 1 (the miss should be cached, not re-fetched)", got)
	}
}

// --- reverse() error-path coverage ---
//
// The deleted TestReverseCountry_Errors and TestReversePlace_Errors covered
// the /reverse endpoint's non-200 status, malformed JSON, and a transport-
// level Do() failure. Only the /search path (TestCandidatesErrorsOnBadStatus)
// had an equivalent test; nothing exercised reverse()'s error handling. A
// failed lookup must surface as a non-nil error and must NOT be reported as
// an ok=false miss: a miss means "not in any country" (open ocean), whereas
// an error means "we couldn't find out" (e.g. rate-limited). Conflating the
// two would let a rate-limited API masquerade as a nonexistent place, which
// is the exact bug class this migration exists to eliminate.

// newErrorTestGeoapify returns a Geoapify that answers every request with the
// given status and body.
func newErrorTestGeoapify(t *testing.T, status int, body string) *Geoapify {
	t.Helper()
	g, _ := newTestGeoapify(t, body, status)
	return g
}

// newDoErrorTestGeoapify returns a Geoapify pointed at an already-closed
// server, so HTTP.Do fails at the transport level rather than returning a
// response.
func newDoErrorTestGeoapify(t *testing.T) *Geoapify {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	g := NewGeoapify("test-key")
	g.BaseURL = url
	return g
}

func TestReverseCountryErrors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		g := newErrorTestGeoapify(t, http.StatusTooManyRequests, `{}`)
		if code, ok, err := g.ReverseCountry(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got code=%q ok=%v err=%v, want an error on a bad status, not a miss", code, ok, err)
		}
	})
	t.Run("bad json", func(t *testing.T) {
		g := newErrorTestGeoapify(t, http.StatusOK, "not json")
		if code, ok, err := g.ReverseCountry(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got code=%q ok=%v err=%v, want a decode error, not a miss", code, ok, err)
		}
	})
	t.Run("do error", func(t *testing.T) {
		g := newDoErrorTestGeoapify(t)
		if code, ok, err := g.ReverseCountry(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got code=%q ok=%v err=%v, want a transport error, not a miss", code, ok, err)
		}
	})
}

func TestReversePlaceErrors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		g := newErrorTestGeoapify(t, http.StatusInternalServerError, `{}`)
		if place, code, ok, err := g.ReversePlace(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got place=%q code=%q ok=%v err=%v, want an error on a bad status, not a miss", place, code, ok, err)
		}
	})
	t.Run("bad json", func(t *testing.T) {
		g := newErrorTestGeoapify(t, http.StatusOK, "nope")
		if place, code, ok, err := g.ReversePlace(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got place=%q code=%q ok=%v err=%v, want a decode error, not a miss", place, code, ok, err)
		}
	})
	t.Run("do error", func(t *testing.T) {
		g := newDoErrorTestGeoapify(t)
		if place, code, ok, err := g.ReversePlace(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got place=%q code=%q ok=%v err=%v, want a transport error, not a miss", place, code, ok, err)
		}
	})
}
