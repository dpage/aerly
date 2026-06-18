package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func TestNominatimReversePlace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("zoom") != "10" {
			t.Errorf("expected city zoom=10, got %q", r.URL.RawQuery)
		}
		// /reverse returns a single object (not an array).
		_, _ = w.Write([]byte(`{"address":{"village":"Droupt-Saint-Basle","county":"Aube","country":"France","country_code":"FR"}}`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1)

	place, code, ok, err := n.ReversePlace(context.Background(), 48.46, 1.57)
	if err != nil || !ok {
		t.Fatalf("ReversePlace: ok=%v err=%v", ok, err)
	}
	if place != "Droupt-Saint-Basle, France" {
		t.Errorf("place = %q, want %q", place, "Droupt-Saint-Basle, France")
	}
	if code != "fr" {
		t.Errorf("code = %q, want lowercase fr", code)
	}
}

func TestNominatimReversePlace_CountryOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No city/town/etc — only a country resolves.
		_, _ = w.Write([]byte(`{"address":{"country":"France","country_code":"FR"}}`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1)

	place, code, ok, err := n.ReversePlace(context.Background(), 0, 0)
	if err != nil || !ok {
		t.Fatalf("ReversePlace: ok=%v err=%v", ok, err)
	}
	if place != "France" {
		t.Errorf("place = %q, want just the country", place)
	}
	if code != "fr" {
		t.Errorf("code = %q, want fr", code)
	}
}

func TestNominatimReversePlace_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"address":{}}`))
	}))
	defer srv.Close()

	n := NewNominatim("aerly-test")
	n.BaseURL = srv.URL
	n.limiter = rate.NewLimiter(rate.Inf, 1)

	if place, _, ok, err := n.ReversePlace(context.Background(), 0, 0); ok || place != "" || err != nil {
		t.Errorf("want empty/not-ok, got place=%q ok=%v err=%v", place, ok, err)
	}
}
