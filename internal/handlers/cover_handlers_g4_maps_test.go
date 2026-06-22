package handlers

import (
	"errors"
	"net/http"
	"testing"

	aerlymaps "github.com/dpage/aerly/internal/maps"
)

// TestG4ResolveMapsURL_BadBody covers the decode-error branch (a malformed body
// with an unknown field trips DisallowUnknownFields → 400).
func TestG4ResolveMapsURL_BadBody(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "g4maps", false)
	w := e.req(t, "POST", "/api/maps/resolve", map[string]any{"bogus": "x"}, u)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ResolveMapsURL_ResolverError covers the generic serverError (500)
// branch: an allowlisted short link whose redirect fetch errors out is neither
// ErrNotAllowed nor a clean "no coords" result.
func TestG4ResolveMapsURL_ResolverError(t *testing.T) {
	e := setup(t, nil, nil)
	u := e.user(t, "g4mapserr", false)
	e.api.Maps = aerlymaps.NewResolver()
	e.api.Maps.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	w := e.req(t, "POST", "/api/maps/resolve",
		map[string]any{"url": "https://goo.gl/maps/abc123"}, u)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
