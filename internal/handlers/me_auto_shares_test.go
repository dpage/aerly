package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestAutoSharesCRUD(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "asme", false)
	wife := e.user(t, "aswife", false)

	// Empty to start.
	w := e.req(t, "GET", "/api/me/auto-shares", nil, me)
	if w.Code != http.StatusOK {
		t.Fatalf("list code = %d; body=%s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("expected empty list, got %s", w.Body.String())
	}

	// Add wife as a viewer default.
	path := "/api/me/auto-shares/" + strconv.FormatInt(wife, 10)
	w = e.req(t, "PUT", path, map[string]any{"role": "viewer"}, me)
	if w.Code != http.StatusOK {
		t.Fatalf("put code = %d; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"role":"viewer"`) ||
		!strings.Contains(w.Body.String(), `"user_id":`+strconv.FormatInt(wife, 10)) {
		t.Errorf("response missing entry: %s", w.Body.String())
	}

	// Re-PUT updates the role.
	w = e.req(t, "PUT", path, map[string]any{"role": "editor"}, me)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"role":"editor"`) {
		t.Fatalf("update code = %d; body=%s", w.Code, w.Body.String())
	}

	// Remove it.
	w = e.req(t, "DELETE", path, nil, me)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete code = %d; body=%s", w.Code, w.Body.String())
	}
	w = e.req(t, "GET", "/api/me/auto-shares", nil, me)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("expected empty list after delete, got %s", w.Body.String())
	}
}

func TestAutoShareValidation(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "asvme", false)
	friend := e.user(t, "asvfriend", false)

	// Invalid role.
	w := e.req(t, "PUT", "/api/me/auto-shares/"+strconv.FormatInt(friend, 10),
		map[string]any{"role": "bogus"}, me)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad role code = %d, want 400", w.Code)
	}

	// Can't auto-share with yourself.
	w = e.req(t, "PUT", "/api/me/auto-shares/"+strconv.FormatInt(me, 10),
		map[string]any{"role": "viewer"}, me)
	if w.Code != http.StatusBadRequest {
		t.Errorf("self-share code = %d, want 400", w.Code)
	}

	// Unknown target user.
	w = e.req(t, "PUT", "/api/me/auto-shares/999999",
		map[string]any{"role": "viewer"}, me)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown user code = %d, want 404", w.Code)
	}

	// Delete of a non-existent default is a 404.
	w = e.req(t, "DELETE", "/api/me/auto-shares/"+strconv.FormatInt(friend, 10), nil, me)
	if w.Code != http.StatusNotFound {
		t.Errorf("delete missing code = %d, want 404", w.Code)
	}
}
