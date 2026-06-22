package handlers

import (
	"context"
	"net/http"
	"testing"
)

// TestG4UpdateMe covers updateMe: a bad body (400), an invalid paper size
// (400), a happy-path update of the self-editable profile fields, and the
// store-error 500 path.
func TestG4UpdateMe(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g4me", false)

	// Bad body: an unknown field trips DisallowUnknownFields → 400.
	if w := e.req(t, "PATCH", "/api/me", map[string]any{"bogus": 1}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}

	// Invalid paper size → 400.
	bad := "foolscap"
	if w := e.req(t, "PATCH", "/api/me", map[string]any{"paper_size": bad}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad paper size = %d, want 400; body=%s", w.Code, w.Body.String())
	}

	// Happy path: set home address and a valid paper size.
	addr := "1 Test Street, Test Town"
	size := "letter"
	w := e.req(t, "PATCH", "/api/me", map[string]any{"home_address": addr, "paper_size": size}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[map[string]any](t, w)
	if got["home_address"] != addr {
		t.Errorf("home_address = %v, want %q", got["home_address"], addr)
	}
}

// TestG4UpdateMeStoreErr covers the store-error 500 branch of updateMe. The
// users table can't simply be dropped (the auth middleware reads it to load the
// session user, which would 401 before the handler runs), so instead a CHECK
// constraint is added that the UPDATE will violate: the SELECT-based session
// load still succeeds, but UpdateUser's write fails → 500.
func TestG4UpdateMeStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g4mestore", false)
	if _, err := e.pool.Exec(context.Background(),
		`ALTER TABLE users ADD CONSTRAINT g4_no_home CHECK (home_address IS NULL OR home_address = '')`); err != nil {
		t.Fatalf("add constraint: %v", err)
	}
	addr := "2 Test Street"
	if w := e.req(t, "PATCH", "/api/me", map[string]any{"home_address": addr}, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
