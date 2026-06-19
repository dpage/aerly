package handlers

import (
	"net/http"
	"testing"
)

// TestUpdateMePaperSize: the page-size preference round-trips through
// PATCH /api/me, defaults to "a4", and rejects an unknown value with a 400.
func TestUpdateMePaperSize(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "prefs", false)

	// New users default to A4.
	w := e.req(t, "GET", "/api/me", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/me = %d", w.Code)
	}
	if me := decodeBody[map[string]any](t, w); me["paper_size"] != "a4" {
		t.Errorf("default paper_size = %v, want a4", me["paper_size"])
	}

	// Switch to US Letter.
	w = e.req(t, "PATCH", "/api/me", map[string]string{"paper_size": "letter"}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("set letter = %d %s", w.Code, w.Body.String())
	}
	if me := decodeBody[map[string]any](t, w); me["paper_size"] != "letter" {
		t.Errorf("paper_size = %v, want letter", me["paper_size"])
	}

	// An unknown value is rejected without touching the stored preference.
	w = e.req(t, "PATCH", "/api/me", map[string]string{"paper_size": "tabloid"}, uid)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad paper_size = %d, want 400", w.Code)
	}
	w = e.req(t, "GET", "/api/me", nil, uid)
	if me := decodeBody[map[string]any](t, w); me["paper_size"] != "letter" {
		t.Errorf("rejected update changed stored value to %v", me["paper_size"])
	}

	// A home-address-only patch leaves paper_size unchanged (COALESCE).
	w = e.req(t, "PATCH", "/api/me", map[string]string{"home_address": "1 Main St"}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("set home = %d %s", w.Code, w.Body.String())
	}
	if me := decodeBody[map[string]any](t, w); me["paper_size"] != "letter" || me["home_address"] != "1 Main St" {
		t.Errorf("partial patch wrong: %v", me)
	}
}
