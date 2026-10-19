package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// postLocalPart creates a one-part dining plan from local fields only (no
// starts_at), which createTZPlan cannot do because it fills starts_at in.
func postLocalPart(t *testing.T, e *testEnv, trip, owner int64, part map[string]any) int {
	t.Helper()
	part["type"] = "dining"
	w := e.req(t, "POST", "/api/trips/"+itoa(trip)+"/plans", map[string]any{
		"type": "dining", "title": "Test Bistro", "parts": []map[string]any{part},
	}, owner)
	if w.Code != http.StatusCreated && w.Code != http.StatusBadRequest {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	if w.Code == http.StatusBadRequest {
		t.Logf("400 body: %s", w.Body.String())
	}
	return w.Code
}

// partInstant reads the first part's starts_at as a UTC RFC 3339 string; the
// API echoes the instant in the server's local offset, which varies by host.
func partInstant(t *testing.T, e *testEnv, trip, owner int64) string {
	t.Helper()
	s, _ := tripPartField(t, e, trip, owner, "starts_at").(string)
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("starts_at %q: %v", s, err)
	}
	return at.UTC().Format(time.RFC3339)
}

func TestCreatePlanStartLocalInZone(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "localzone", false)
	trip := newTrip(t, e, owner, "Trip")
	if code := postLocalPart(t, e, trip, owner, map[string]any{
		"start_local": "2026-10-18T14:00", "start_tz": "Asia/Tashkent", "start_tz_source": "confirmed",
	}); code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	if got := partInstant(t, e, trip, owner); got != "2026-10-18T09:00:00Z" {
		t.Errorf("starts_at = %v", got)
	}
	if got := tripPartField(t, e, trip, owner, "start_tz_source"); got != "confirmed" {
		t.Errorf("start_tz_source = %v", got)
	}
}

func TestCreatePlanStartLocalNoZoneFloats(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "localfloat", false)
	trip := newTrip(t, e, owner, "Trip")
	postLocalPart(t, e, trip, owner, map[string]any{"start_local": "2026-10-18T14:00"})
	if got := partInstant(t, e, trip, owner); got != "2026-10-18T14:00:00Z" {
		t.Errorf("starts_at = %v", got)
	}
	if got := tripPartField(t, e, trip, owner, "start_tz_source"); got != "floating" {
		t.Errorf("start_tz_source = %v", got)
	}
}

func TestCreatePlanStartLocalRejects(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "localreject", false)
	trip := newTrip(t, e, owner, "Trip")
	for name, c := range map[string]struct {
		part map[string]any
		want string
	}{
		"gap":  {map[string]any{"start_local": "2026-03-29T01:30", "start_tz": "Europe/London"}, "That time doesn't exist in Europe/London (clocks go forward)."},
		"both": {map[string]any{"start_local": "2026-10-18T14:00", "starts_at": "2026-10-18T09:00:00Z"}, "Send starts_at or start_local, not both."},
	} {
		c.part["type"] = "dining"
		w := e.req(t, "POST", "/api/trips/"+itoa(trip)+"/plans", map[string]any{
			"type": "dining", "title": "Test Bistro", "parts": []map[string]any{c.part},
		}, owner)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestUpdatePlanPartStartLocal(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "localpatch", false)
	trip := newTrip(t, e, owner, "Trip")
	_, part, w := createTZPlan(t, e, trip, owner, map[string]any{"start_tz": "Asia/Tashkent"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	w = e.req(t, "PATCH", "/api/plan-parts/"+itoa(part), map[string]any{"start_local": "2026-10-18T15:00"}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("patch = %d: %s", w.Code, w.Body.String())
	}
	if got := partInstant(t, e, trip, owner); got != "2026-10-18T10:00:00Z" {
		t.Errorf("starts_at = %v", got)
	}
	w = e.req(t, "PATCH", "/api/plan-parts/"+itoa(part), map[string]any{
		"start_local": "2026-03-29T01:30", "start_tz": "Europe/London",
	}, owner)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "clocks go forward") {
		t.Errorf("gap patch = %d %s", w.Code, w.Body.String())
	}
}

func TestIngestConfirmStartLocal(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "localconfirm", false)
	trip := newTrip(t, e, owner, "Trip")
	body := map[string]any{"plans": []map[string]any{{
		"type": "dining", "title": "Test Bistro",
		"parts": []map[string]any{{"type": "dining", "start_local": "2026-03-29T01:30", "start_tz": "Europe/London"}},
	}}}
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", trip), body, owner)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "clocks go forward") {
		t.Errorf("confirm = %d %s", w.Code, w.Body.String())
	}
}
