package geocode

import (
	"context"
	"golang.org/x/time/rate"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNominatimGeocodeAndCache(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		_, _ = w.Write([]byte(`[{"lat":"51.4706","lon":"-0.461941"}]`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1) // don't throttle the test

	lat, lon, ok, err := n.Geocode(context.Background(), "Heathrow Terminal 5", "")
	if err != nil || !ok {
		t.Fatalf("Geocode: ok=%v err=%v", ok, err)
	}
	if lat != 51.4706 || lon != -0.461941 {
		t.Errorf("coords = %v, %v", lat, lon)
	}
	// Second lookup is served from cache (no extra HTTP call).
	if _, _, _, err := n.Geocode(context.Background(), "Heathrow Terminal 5", ""); err != nil {
		t.Fatalf("cached Geocode: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("HTTP calls = %d, want 1 (second should hit cache)", got)
	}
}

func TestNominatimGeocodeCountryBias(t *testing.T) {
	var lastCountry string
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		lastCountry = r.URL.Query().Get("countrycodes")
		_, _ = w.Write([]byte(`[{"lat":"16.7388","lon":"-22.9511"}]`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1)

	if _, _, ok, err := n.Geocode(context.Background(), "Sal Airport", "cv"); err != nil || !ok {
		t.Fatalf("Geocode: ok=%v err=%v", ok, err)
	}
	if lastCountry != "cv" {
		t.Errorf("countrycodes = %q, want \"cv\"", lastCountry)
	}
	// The same query with a different (or no) country must not be served from the
	// country-biased cache entry — it's a distinct lookup.
	if _, _, _, err := n.Geocode(context.Background(), "Sal Airport", ""); err != nil {
		t.Fatalf("unbiased Geocode: %v", err)
	}
	if lastCountry != "" {
		t.Errorf("unbiased countrycodes = %q, want empty", lastCountry)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("HTTP calls = %d, want 2 (biased and unbiased are separate cache keys)", got)
	}
}

func TestNominatimEmptyQuery(t *testing.T) {
	n := NewNominatim("aerly-test")
	_, _, ok, err := n.Geocode(context.Background(), "   ", "")
	if err != nil || ok {
		t.Errorf("empty query: ok=%v err=%v", ok, err)
	}
}

func TestNominatimNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1) // don't throttle the test
	_, _, ok, err := n.Geocode(context.Background(), "nowhere at all", "")
	if err != nil || ok {
		t.Errorf("no-match: ok=%v err=%v", ok, err)
	}
}
