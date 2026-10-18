package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// createTZPlan posts a one-part dining plan and returns the plan and part ids
// plus the response status and body, so a case can assert on failures too.
func createTZPlan(t *testing.T, e *testEnv, trip, owner int64, part map[string]any) (int64, int64, *httptest.ResponseRecorder) {
	t.Helper()
	part["type"] = "dining"
	if _, ok := part["starts_at"]; !ok {
		part["starts_at"] = time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
	}
	w := e.req(t, "POST", "/api/trips/"+itoa(trip)+"/plans", map[string]any{
		"type": "dining", "title": "Test Bistro", "parts": []map[string]any{part},
	}, owner)
	if w.Code != http.StatusCreated {
		return 0, 0, w
	}
	plan := decodeBody[map[string]any](t, w)
	parts, _ := plan["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	p, _ := parts[0].(map[string]any)
	return int64(plan["id"].(float64)), int64(p["id"].(float64)), w
}

func tripPartField(t *testing.T, e *testEnv, trip, owner int64, field string) any {
	t.Helper()
	w := e.req(t, "GET", "/api/trips/"+itoa(trip), nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("get trip = %d: %s", w.Code, w.Body.String())
	}
	trp := decodeBody[map[string]any](t, w)
	plans, _ := trp["plans"].([]any)
	pl, _ := plans[0].(map[string]any)
	parts, _ := pl["parts"].([]any)
	p, _ := parts[0].(map[string]any)
	return p[field]
}

func TestCreatePlanAcceptsTZSource(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrccreate", false)
	trip := newTrip(t, e, owner, "Trip")

	_, _, w := createTZPlan(t, e, trip, owner, map[string]any{
		"start_tz": "Europe/Paris", "start_tz_source": "inferred",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	if got := tripPartField(t, e, trip, owner, "start_tz_source"); got != "inferred" {
		t.Errorf("start_tz_source = %v, want inferred", got)
	}
}

func TestCreatePlanDefaultsTZSourceToFloating(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrcfloat", false)
	trip := newTrip(t, e, owner, "Trip")

	_, _, w := createTZPlan(t, e, trip, owner, map[string]any{})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	if got := tripPartField(t, e, trip, owner, "start_tz_source"); got != "floating" {
		t.Errorf("start_tz_source = %v, want floating", got)
	}
}

func TestCreatePlanRejectsInvalidZone(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrcbad", false)
	trip := newTrip(t, e, owner, "Trip")

	_, _, w := createTZPlan(t, e, trip, owner, map[string]any{"start_tz": "Nope/Nope"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid time zone.") {
		t.Errorf("body = %s, want Invalid time zone.", w.Body.String())
	}
}

func TestUpdatePlanPartTZSource(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrcpatch", false)
	trip := newTrip(t, e, owner, "Trip")

	// A part with no zone: confirming a source for it must be refused.
	_, floatingID, w := createTZPlan(t, e, trip, owner, map[string]any{})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	w = e.req(t, "PATCH", fmt.Sprintf("/api/plan-parts/%d", floatingID),
		map[string]any{"start_tz_source": "confirmed"}, owner)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Invalid time zone.") {
		t.Fatalf("confirmed on empty zone = %d, want 400 Invalid time zone.: %s", w.Code, w.Body.String())
	}

	// Clearing a zone with an explicit unknown source is accepted and echoed.
	trip2 := newTrip(t, e, owner, "Trip2")
	_, zonedID, w := createTZPlan(t, e, trip2, owner, map[string]any{
		"start_tz": "Europe/Paris", "start_tz_source": "confirmed",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	w = e.req(t, "PATCH", fmt.Sprintf("/api/plan-parts/%d", zonedID),
		map[string]any{"start_tz": "", "start_tz_source": "unknown"}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("clear zone = %d: %s", w.Code, w.Body.String())
	}
	if got := decodeBody[map[string]any](t, w)["start_tz_source"]; got != "unknown" {
		t.Errorf("start_tz_source = %v, want unknown", got)
	}
}

func TestIngestConfirmAcceptsTZSource(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrcconfirm", false)
	trip := newTrip(t, e, owner, "Trip")

	confirm := map[string]any{
		"plans": []map[string]any{{
			"type": "dining", "title": "Test Bistro", "source": "paste",
			"parts": []map[string]any{{
				"type": "dining", "starts_at": time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
				"start_tz": "Europe/Paris", "start_tz_source": "inferred",
				"end_tz": "", "end_tz_source": "unknown",
			}},
		}},
	}
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", trip), confirm, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := tripPartField(t, e, trip, owner, "start_tz_source"); got != "inferred" {
		t.Errorf("start_tz_source = %v, want inferred", got)
	}
	if got := tripPartField(t, e, trip, owner, "end_tz_source"); got != "unknown" {
		t.Errorf("end_tz_source = %v, want unknown", got)
	}
}

// A PATCH carrying only a zone for a floating part anchors the stored
// wall-clock digits in that zone instead of relabelling them as an instant.
func TestUpdatePlanPartZoneOnlyAnchorsFloating(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrcanchor", false)
	trip := newTrip(t, e, owner, "Trip")

	_, partID, w := createTZPlan(t, e, trip, owner, map[string]any{
		"ends_at": time.Date(2026, 7, 1, 17, 0, 0, 0, time.UTC),
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	w = e.req(t, "PATCH", fmt.Sprintf("/api/plan-parts/%d", partID),
		map[string]any{"start_tz": "Europe/Paris", "end_tz": "Europe/Paris"}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("patch = %d: %s", w.Code, w.Body.String())
	}
	got := decodeBody[map[string]any](t, w)
	// 15:00 and 17:00 wall clock in Paris (CEST, UTC+2) are 13:00 and 15:00 UTC.
	for field, want := range map[string]time.Time{
		"starts_at": time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC),
		"ends_at":   time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC),
	} {
		s, _ := got[field].(string)
		at, err := time.Parse(time.RFC3339, s)
		if err != nil || !at.Equal(want) {
			t.Errorf("%s = %v, want %v", field, got[field], want)
		}
	}
	if got["start_tz_source"] != "confirmed" || got["end_tz_source"] != "confirmed" {
		t.Errorf("sources = %v / %v, want confirmed", got["start_tz_source"], got["end_tz_source"])
	}
}
