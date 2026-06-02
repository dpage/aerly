package geocode

import (
	"context"
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
	n.minGap = 0

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
	n.minGap = 0

	code, ok, err := n.GeocodeCountry(context.Background(), "Nowhere-at-all")
	if err != nil {
		t.Fatal(err)
	}
	if ok || code != "" {
		t.Errorf("got (%q, %v), want (\"\", false)", code, ok)
	}
}
