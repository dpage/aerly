package geocode

import (
	"context"
	"golang.org/x/time/rate"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNominatimGeocodeCountry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("addressdetails") != "1" {
			t.Errorf("expected addressdetails=1, got %q", r.URL.RawQuery)
		}
		// Nominatim returns an uppercase-or-lowercase country_code; we lowercase it.
		_, _ = w.Write([]byte(`[{"address":{"country_code":"ES"}}]`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1) // don't throttle the test

	code, ok, err := n.GeocodeCountry(context.Background(), "Benidorm")
	if err != nil || !ok {
		t.Fatalf("GeocodeCountry: ok=%v err=%v", ok, err)
	}
	if code != "es" {
		t.Errorf("code = %q, want lowercase es", code)
	}
	// Second lookup is cached (no extra HTTP call).
	if _, _, err := n.GeocodeCountry(context.Background(), "Benidorm"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("HTTP calls = %d, want 1 (second cached)", calls)
	}
}

func TestNominatimGeocodeCountry_NoResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1) // don't throttle the test

	code, ok, err := n.GeocodeCountry(context.Background(), "Nowhere-at-all")
	if err != nil {
		t.Fatal(err)
	}
	if ok || code != "" {
		t.Errorf("got (%q, %v), want (\"\", false)", code, ok)
	}
}

func TestNominatimReverseCountry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("lat") != "50.95194" || r.URL.Query().Get("lon") != "1.85635" {
			t.Errorf("unexpected reverse query: %q", r.URL.RawQuery)
		}
		// /reverse returns a single object, not an array.
		_, _ = w.Write([]byte(`{"address":{"country_code":"FR"}}`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1) // don't throttle the test

	code, ok, err := n.ReverseCountry(context.Background(), 50.95194, 1.85635)
	if err != nil || !ok {
		t.Fatalf("ReverseCountry: ok=%v err=%v", ok, err)
	}
	if code != "fr" {
		t.Errorf("code = %q, want fr (lowercased)", code)
	}
	// The result is cached by coordinate — a repeat doesn't hit the network.
	if _, _, err := n.ReverseCountry(context.Background(), 50.95194, 1.85635); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("made %d HTTP calls, want 1 (second served from cache)", calls)
	}
}

func TestNominatimReverseCountry_NoResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Open ocean: a result with no country.
		_, _ = w.Write([]byte(`{"address":{}}`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1) // don't throttle the test

	code, ok, err := n.ReverseCountry(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok || code != "" {
		t.Errorf("got (%q, %v), want (\"\", false)", code, ok)
	}
}
