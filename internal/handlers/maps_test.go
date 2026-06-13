package handlers

import (
	"net/http"
	"testing"

	aerlymaps "github.com/dpage/aerly/internal/maps"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://www.google.com/maps/place/NoCoordsCafe"}, u)
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
