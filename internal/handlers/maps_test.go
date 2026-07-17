package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/geocode"
	aerlymaps "github.com/dpage/aerly/internal/maps"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// stubHintGeocoder is a minimal Geocoder double for the Suggest path, built to
// an exact ranked candidate list per query text (Formatted included, since the
// confirmation response surfaces it as Label). geocode_backfill_test.go's
// fakeGeocoder doesn't carry per-text Formatted/Confidence, hence this small
// second double rather than reusing it.
type stubHintGeocoder struct {
	byText map[string][]geocode.Candidate
}

func (s *stubHintGeocoder) Candidates(_ context.Context, q geocode.Query) ([]geocode.Candidate, error) {
	return s.byText[q.Text], nil
}
func (s *stubHintGeocoder) Geocode(context.Context, string, string) (float64, float64, bool, error) {
	return 0, 0, false, nil
}
func (s *stubHintGeocoder) GeocodeCountry(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (s *stubHintGeocoder) ReverseCountry(context.Context, float64, float64) (string, bool, error) {
	return "", false, nil
}
func (s *stubHintGeocoder) ReversePlace(context.Context, float64, float64) (string, string, bool, error) {
	return "", "", false, nil
}

func TestResolveMapsURL_FullURL(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "alice", false)
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://www.google.com/maps/place/X/data=!3d40.5!4d-70.25"}, u)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	}](t, w)
	if got.Lat != 40.5 || got.Lon != -70.25 {
		t.Fatalf("got %+v, want 40.5,-70.25", got)
	}
}

func TestResolveMapsURL_BadHost(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "alice", false)
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://evil.example.com/maps"}, u)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestResolveMapsURL_NoCoords(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "alice", false)
	e.api.Maps = aerlymaps.NewResolver()
	e.api.Maps.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	// e.api.GeoResolver is nil (never wired up by setup), so even though this
	// URL carries a place name, there is no geocoder to try it against: the
	// nil-safety fallthrough to 422 must hold, not a panic.
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://www.google.com/maps/place/NoCoordsCafe"}, u)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422; body=%s", w.Code, w.Body.String())
	}
}

// TestResolveMapsURL_HintGeocodedNeedsConfirmation exercises a coordinate-less
// link (the iOS Share-link shape: q= carries a name, no lat/lon) with a
// GeoResolver wired up: the handler must geocode the hint and return it with
// needs_confirmation=true rather than plotting it silently.
func TestResolveMapsURL_HintGeocodedNeedsConfirmation(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "alice", false)
	e.api.Maps = aerlymaps.NewResolver()
	e.api.Maps.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	e.api.GeoResolver = geoResolver(&stubHintGeocoder{byText: map[string][]geocode.Candidate{
		"Test Hotel, London": {{Lat: 51.5, Lon: -0.14, Confidence: 0.95, Formatted: "Test Hotel, London, UK"}},
	}})
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://www.google.com/maps/search/?api=1&q=Test+Hotel%2C+London&ftid=0x1:0x2"}, u)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[api.ResolvedLocationDTO](t, w)
	if got.Lat != 51.5 || got.Lon != -0.14 {
		t.Fatalf("got %+v, want 51.5,-0.14", got)
	}
	if !got.NeedsConfirmation {
		t.Error("a geocoded hint must be returned with needs_confirmation=true")
	}
	if got.Label != "Test Hotel, London, UK" {
		t.Errorf("label = %q, want the formatted candidate", got.Label)
	}
}

// TestResolveMapsURL_HintGeocodeMissFallsThrough422 checks that a hint the
// geocoder can't confidently place still falls back to the long-press message,
// exactly as the coordinate-less case with no GeoResolver does: a missing pin
// is acceptable, a wrong one is not.
func TestResolveMapsURL_HintGeocodeMissFallsThrough422(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "alice", false)
	e.api.Maps = aerlymaps.NewResolver()
	e.api.Maps.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	e.api.GeoResolver = geoResolver(&stubHintGeocoder{byText: map[string][]geocode.Candidate{}})
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://www.google.com/maps/search/?api=1&q=Test+Hotel%2C+London&ftid=0x1:0x2"}, u)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422; body=%s", w.Code, w.Body.String())
	}
}

func TestResolveMapsURL_BadBody(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "alice", false)
	w := e.req(t, "POST", "/api/maps/resolve", map[string]any{"url": ""}, u)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestResolveMapsURL_Unauthenticated(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://www.google.com/maps/@1,2,3z"}, 0)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}
