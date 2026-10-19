package handlers

import (
	"net/http"
	"testing"
)

// TestUpdateMeHomeCoords: the pinned home coordinates round-trip through
// PATCH /api/me via the home_coords object, can be cleared, and reject a lone
// coordinate or out-of-range values.
func TestUpdateMeHomeCoords(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "homepin", false)

	// Unset by default.
	w := e.req(t, "GET", "/api/me", nil, uid)
	if me := decodeBody[map[string]any](t, w); me["home_lat"] != nil {
		t.Errorf("default home_lat = %v, want absent", me["home_lat"])
	}

	// Pin a location.
	w = e.req(t, "PATCH", "/api/me", map[string]any{"home_coords": map[string]any{"lat": 51.5, "lon": -0.12}}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("pin home = %d %s", w.Code, w.Body.String())
	}
	if me := decodeBody[map[string]any](t, w); me["home_lat"] != 51.5 || me["home_lon"] != -0.12 {
		t.Errorf("pinned coords = (%v,%v), want (51.5,-0.12)", me["home_lat"], me["home_lon"])
	}

	// A patch that omits home_coords leaves the pin untouched.
	w = e.req(t, "PATCH", "/api/me", map[string]any{"home_address": "Home"}, uid)
	if me := decodeBody[map[string]any](t, w); me["home_lat"] != 51.5 {
		t.Errorf("unrelated patch cleared the pin: %v", me["home_lat"])
	}

	// A lone coordinate is rejected.
	if w := e.req(t, "PATCH", "/api/me", map[string]any{"home_coords": map[string]any{"lat": 51.5}}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("lone lat = %d, want 400", w.Code)
	}
	// Out of range is rejected.
	if w := e.req(t, "PATCH", "/api/me", map[string]any{"home_coords": map[string]any{"lat": 200.0, "lon": 0.0}}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("out-of-range lat = %d, want 400", w.Code)
	}

	// Clearing: present home_coords with null lat/lon nulls the pin.
	w = e.req(t, "PATCH", "/api/me", map[string]any{"home_coords": map[string]any{"lat": nil, "lon": nil}}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("clear home = %d %s", w.Code, w.Body.String())
	}
	if me := decodeBody[map[string]any](t, w); me["home_lat"] != nil {
		t.Errorf("cleared home_lat = %v, want absent", me["home_lat"])
	}
}

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

// TestUpdateMeHiddenFeatures: the hide_explore/hide_maps preferences round-trip
// through PATCH /api/me, are independent, and don't disturb other fields.
func TestUpdateMeHiddenFeatures(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "hideprefs", false)

	// Hide Explore only.
	w := e.req(t, "PATCH", "/api/me", map[string]bool{"hide_explore": true}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("set hide_explore = %d %s", w.Code, w.Body.String())
	}
	me := decodeBody[map[string]any](t, w)
	if me["hide_explore"] != true {
		t.Errorf("hide_explore = %v, want true", me["hide_explore"])
	}
	// hide_maps stays false; with omitempty it's simply absent.
	if me["hide_maps"] == true {
		t.Errorf("hide_maps should still be false, got %v", me["hide_maps"])
	}

	// Toggling hide_maps leaves hide_explore set (independent, COALESCE).
	w = e.req(t, "PATCH", "/api/me", map[string]bool{"hide_maps": true}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("set hide_maps = %d %s", w.Code, w.Body.String())
	}
	me = decodeBody[map[string]any](t, w)
	if me["hide_maps"] != true || me["hide_explore"] != true {
		t.Errorf("both flags should be true now: %v", me)
	}

	// Clearing hide_explore back to false works too.
	w = e.req(t, "PATCH", "/api/me", map[string]bool{"hide_explore": false}, uid)
	me = decodeBody[map[string]any](t, w)
	if me["hide_explore"] == true {
		t.Errorf("hide_explore should be cleared: %v", me)
	}
}

// TestUpdateMeHomeTZ: the home zone round-trips through PATCH /api/me, an
// unknown zone is a 400, "" clears it, an absent field leaves it alone, and
// other users never see it.
func TestUpdateMeHomeTZ(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "tzprefs", false)
	other := e.user(t, "tzother", false)

	w := e.req(t, "PATCH", "/api/me", map[string]string{"home_tz": "Asia/Tashkent"}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("set home_tz = %d %s", w.Code, w.Body.String())
	}
	w = e.req(t, "GET", "/api/me", nil, uid)
	if me := decodeBody[map[string]any](t, w); me["home_tz"] != "Asia/Tashkent" {
		t.Errorf("home_tz = %v, want Asia/Tashkent", me["home_tz"])
	}

	// An unknown zone is rejected and the stored value survives.
	w = e.req(t, "PATCH", "/api/me", map[string]string{"home_tz": "Mars/Olympus"}, uid)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad home_tz = %d, want 400", w.Code)
	}
	if body := decodeBody[map[string]string](t, w); body["error"] != "Home time zone is not a recognised time zone." {
		t.Errorf("bad home_tz error = %q", body["error"])
	}
	w = e.req(t, "GET", "/api/me", nil, uid)
	if me := decodeBody[map[string]any](t, w); me["home_tz"] != "Asia/Tashkent" {
		t.Errorf("rejected update changed home_tz to %v", me["home_tz"])
	}

	// A patch without the field leaves it unchanged.
	w = e.req(t, "PATCH", "/api/me", map[string]string{"home_address": "1 Main St"}, uid)
	if me := decodeBody[map[string]any](t, w); me["home_tz"] != "Asia/Tashkent" {
		t.Errorf("partial patch changed home_tz to %v", me["home_tz"])
	}

	// Another user sees no home_tz for this one.
	w = e.req(t, "GET", "/api/users", nil, other)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/users = %d", w.Code)
	}
	for _, u := range decodeBody[[]map[string]any](t, w) {
		if _, ok := u["home_tz"]; ok {
			t.Errorf("user directory leaked home_tz: %v", u)
		}
	}

	// "" clears it.
	w = e.req(t, "PATCH", "/api/me", map[string]string{"home_tz": ""}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("clear home_tz = %d %s", w.Code, w.Body.String())
	}
	if me := decodeBody[map[string]any](t, w); me["home_tz"] != nil {
		t.Errorf("cleared home_tz = %v, want absent", me["home_tz"])
	}
}
