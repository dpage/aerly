package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

// newTestNominatim returns a Nominatim pointed at srv with throttling disabled,
// so the error-path tests don't wait on the one-request/second limiter.
func newTestNominatim(baseURL string) *Nominatim {
	n := NewNominatim("aerly-test")
	n.BaseURL = baseURL
	n.limiter = rate.NewLimiter(rate.Inf, 1)
	return n
}

// errServer serves a fixed status code and body for every request.
func errServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGeocode_Non200(t *testing.T) {
	n := newTestNominatim(errServer(t, http.StatusInternalServerError, "boom").URL)
	if _, _, ok, err := n.Geocode(context.Background(), "somewhere", ""); err == nil || ok {
		t.Errorf("got ok=%v err=%v, want an error on status 500", ok, err)
	}
}

func TestGeocode_BadJSON(t *testing.T) {
	n := newTestNominatim(errServer(t, http.StatusOK, "not json").URL)
	if _, _, ok, err := n.Geocode(context.Background(), "somewhere", ""); err == nil || ok {
		t.Errorf("got ok=%v err=%v, want a decode error", ok, err)
	}
}

// A result whose lat/lon aren't parseable floats is treated as "no match"
// (ok=false) rather than an error — the cache stores the empty result.
func TestGeocode_UnparseableCoords(t *testing.T) {
	n := newTestNominatim(errServer(t, http.StatusOK, `[{"lat":"north","lon":"west"}]`).URL)
	lat, lon, ok, err := n.Geocode(context.Background(), "somewhere", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || lat != 0 || lon != 0 {
		t.Errorf("got (%v,%v,%v), want (0,0,false) for unparseable coords", lat, lon, ok)
	}
}

func TestGeocode_DoError(t *testing.T) {
	// A closed server's URL makes HTTP.Do fail outright.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	n := newTestNominatim(url)
	if _, _, ok, err := n.Geocode(context.Background(), "somewhere", ""); err == nil || ok {
		t.Errorf("got ok=%v err=%v, want a transport error", ok, err)
	}
}

func TestGeocodeCountry_Errors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		n := newTestNominatim(errServer(t, http.StatusBadGateway, "").URL)
		if _, ok, err := n.GeocodeCountry(context.Background(), "Benidorm"); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want an error", ok, err)
		}
	})
	t.Run("bad json", func(t *testing.T) {
		n := newTestNominatim(errServer(t, http.StatusOK, "{").URL)
		if _, ok, err := n.GeocodeCountry(context.Background(), "Benidorm"); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want a decode error", ok, err)
		}
	})
	t.Run("do error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		n := newTestNominatim(url)
		if _, ok, err := n.GeocodeCountry(context.Background(), "Benidorm"); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want a transport error", ok, err)
		}
	})
	t.Run("empty query", func(t *testing.T) {
		n := newTestNominatim("http://192.0.2.1") // never contacted
		if code, ok, err := n.GeocodeCountry(context.Background(), "  "); err != nil || ok || code != "" {
			t.Errorf("empty query: got (%q,%v,%v), want (\"\",false,nil)", code, ok, err)
		}
	})
}

func TestReverseCountry_Errors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		n := newTestNominatim(errServer(t, http.StatusServiceUnavailable, "").URL)
		if _, ok, err := n.ReverseCountry(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want an error", ok, err)
		}
	})
	t.Run("bad json", func(t *testing.T) {
		n := newTestNominatim(errServer(t, http.StatusOK, "}{").URL)
		if _, ok, err := n.ReverseCountry(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want a decode error", ok, err)
		}
	})
	t.Run("do error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		n := newTestNominatim(url)
		if _, ok, err := n.ReverseCountry(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want a transport error", ok, err)
		}
	})
}

func TestReversePlace_Errors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		n := newTestNominatim(errServer(t, http.StatusInternalServerError, "").URL)
		if _, _, ok, err := n.ReversePlace(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want an error", ok, err)
		}
	})
	t.Run("bad json", func(t *testing.T) {
		n := newTestNominatim(errServer(t, http.StatusOK, "nope").URL)
		if _, _, ok, err := n.ReversePlace(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want a decode error", ok, err)
		}
	})
	t.Run("do error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		n := newTestNominatim(url)
		if _, _, ok, err := n.ReversePlace(context.Background(), 51.5, -0.1); err == nil || ok {
			t.Errorf("got ok=%v err=%v, want a transport error", ok, err)
		}
	})
}
