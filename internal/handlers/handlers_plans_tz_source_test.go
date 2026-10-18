package handlers

import (
	"context"
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

// A manually added flight's floating times are anchored in its airports'
// zones before flight_details is seeded, so the poller never reads wall-clock
// digits as instants; with no airport zone the part stays floating.
func TestCreateFlightAnchorsFloatingSchedule(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrcflight", false)
	trip := newTrip(t, e, owner, "Trip")
	create := func(flight map[string]any) int64 {
		t.Helper()
		w := e.req(t, "POST", "/api/trips/"+itoa(trip)+"/plans", map[string]any{
			"type": "flight", "title": "XX100", "parts": []map[string]any{{
				"type":      "flight",
				"starts_at": time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC), "start_tz_source": "floating",
				"ends_at": time.Date(2026, 7, 1, 17, 0, 0, 0, time.UTC), "end_tz_source": "floating",
				"flight": flight,
			}},
		}, owner)
		if w.Code != http.StatusCreated {
			t.Fatalf("create = %d: %s", w.Code, w.Body.String())
		}
		plan := decodeBody[map[string]any](t, w)
		parts, _ := plan["parts"].([]any)
		p, _ := parts[0].(map[string]any)
		return int64(p["id"].(float64))
	}
	ctx := context.Background()

	t.Run("airport zones anchor the part and the schedule", func(t *testing.T) {
		id := create(map[string]any{"ident": "XX100", "origin_iata": "LHR", "dest_iata": "CDG"})
		fd, err := e.store.FlightDetailFor(ctx, id)
		if err != nil || fd == nil {
			t.Fatalf("flight detail: %v", err)
		}
		// 15:00 BST is 14:00Z; 17:00 CEST is 15:00Z.
		wantOut := time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC)
		wantIn := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
		if !fd.ScheduledOut.Equal(wantOut) || !fd.ScheduledIn.Equal(wantIn) {
			t.Errorf("schedule = %v / %v, want %v / %v", fd.ScheduledOut.UTC(), fd.ScheduledIn.UTC(), wantOut, wantIn)
		}
		part, _ := e.store.PlanPartByID(ctx, id)
		if part.StartTZ != "Europe/London" || part.StartTZSource != "confirmed" || !part.StartsAt.Equal(wantOut) {
			t.Errorf("start = %q/%q %v", part.StartTZ, part.StartTZSource, part.StartsAt.UTC())
		}
		if part.EndTZ != "Europe/Paris" || part.EndTZSource != "confirmed" || part.EndsAt == nil || !part.EndsAt.Equal(wantIn) {
			t.Errorf("end = %q/%q %v", part.EndTZ, part.EndTZSource, part.EndsAt)
		}
	})

	t.Run("no airport zone leaves the part floating", func(t *testing.T) {
		id := create(map[string]any{"ident": "XX100"})
		part, _ := e.store.PlanPartByID(ctx, id)
		if part.StartTZ != "" || part.StartTZSource != "floating" || part.EndTZSource != "floating" {
			t.Errorf("sources = %q/%q, %q, want floating", part.StartTZ, part.StartTZSource, part.EndTZSource)
		}
	})
}

func TestAnchorFlightEndSkipsZonedEnds(t *testing.T) {
	at := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
	if _, ok := anchorFlightEnd(at, "Europe/Paris", "confirmed", "LHR", nil, nil); ok {
		t.Error("a zoned end was re-anchored")
	}
	if _, ok := anchorFlightEnd(at, "", "unknown", "LHR", nil, nil); ok {
		t.Error("an unknown end (a true instant) was re-anchored")
	}
}

// Editing an unresolved, floating flight's times anchors them in the airports'
// zones before flight_details is synced, and writes the anchoring back to the
// part; an end with no airport zone keeps its wall-clock digits.
func TestUpdateFlightAnchorsFloatingSchedule(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "tzsrcsync", false)
	trip := newTrip(t, e, owner, "Trip")
	ctx := context.Background()
	// A floating flight with no airports yet, as a manual add without IATA
	// codes produces; the codes are set directly to simulate the table hit.
	create := func(origin, dest string) int64 {
		t.Helper()
		w := e.req(t, "POST", "/api/trips/"+itoa(trip)+"/plans", map[string]any{
			"type": "flight", "title": "XX100", "parts": []map[string]any{{
				"type":      "flight",
				"starts_at": time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC), "start_tz_source": "floating",
				"ends_at": time.Date(2026, 7, 1, 17, 0, 0, 0, time.UTC), "end_tz_source": "floating",
				"flight": map[string]any{"ident": "XX100"},
			}},
		}, owner)
		if w.Code != http.StatusCreated {
			t.Fatalf("create = %d: %s", w.Code, w.Body.String())
		}
		plan := decodeBody[map[string]any](t, w)
		parts, _ := plan["parts"].([]any)
		p, _ := parts[0].(map[string]any)
		id := int64(p["id"].(float64))
		if _, err := e.store.Pool().Exec(ctx,
			`UPDATE flight_details SET origin_iata = $2, dest_iata = $3 WHERE plan_part_id = $1`,
			id, origin, dest); err != nil {
			t.Fatalf("set airports: %v", err)
		}
		return id
	}
	patch := func(id int64, body map[string]any) {
		t.Helper()
		if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(id), body, owner); w.Code != http.StatusOK {
			t.Fatalf("patch = %d: %s", w.Code, w.Body.String())
		}
	}

	t.Run("table airports anchor the schedule and the part", func(t *testing.T) {
		id := create("LHR", "CDG")
		patch(id, map[string]any{"starts_at": time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
			"ends_at": time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)})
		// 09:00 BST is 08:00Z; 11:00 CEST is 09:00Z.
		wantOut := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
		wantIn := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
		fd, _ := e.store.FlightDetailFor(ctx, id)
		if !fd.ScheduledOut.Equal(wantOut) || !fd.ScheduledIn.Equal(wantIn) {
			t.Errorf("schedule = %v / %v, want %v / %v", fd.ScheduledOut.UTC(), fd.ScheduledIn.UTC(), wantOut, wantIn)
		}
		part, _ := e.store.PlanPartByID(ctx, id)
		if part.StartTZ != "Europe/London" || part.StartTZSource != "confirmed" || !part.StartsAt.Equal(wantOut) {
			t.Errorf("start = %q/%q %v", part.StartTZ, part.StartTZSource, part.StartsAt.UTC())
		}
		if part.EndTZ != "Europe/Paris" || part.EndTZSource != "confirmed" || part.EndsAt == nil || !part.EndsAt.Equal(wantIn) {
			t.Errorf("end = %q/%q %v", part.EndTZ, part.EndTZSource, part.EndsAt)
		}
	})

	t.Run("a cleared arrival stays equal to the departure", func(t *testing.T) {
		id := create("LHR", "CDG")
		d := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
		patch(id, map[string]any{"starts_at": d, "ends_at": d})
		fd, _ := e.store.FlightDetailFor(ctx, id)
		want := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
		if !fd.ScheduledOut.Equal(want) || !fd.ScheduledIn.Equal(want) {
			t.Errorf("schedule = %v / %v, want both %v", fd.ScheduledOut.UTC(), fd.ScheduledIn.UTC(), want)
		}
	})

	t.Run("a departure-only flight with an unzoned origin stays departure-only", func(t *testing.T) {
		id := create("", "CDG")
		d := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
		patch(id, map[string]any{"starts_at": d, "ends_at": d})
		fd, _ := e.store.FlightDetailFor(ctx, id)
		if !fd.ScheduledOut.Equal(d) || !fd.ScheduledIn.Equal(d) {
			t.Errorf("schedule = %v / %v, want both %v", fd.ScheduledOut.UTC(), fd.ScheduledIn.UTC(), d)
		}
		part, _ := e.store.PlanPartByID(ctx, id)
		if part.EndsAt == nil || !part.EndsAt.Equal(part.StartsAt) || part.EndTZSource != "floating" {
			t.Errorf("part = %v / %v (%q), want departure-only, floating", part.StartsAt, part.EndsAt, part.EndTZSource)
		}
	})

	t.Run("a confirmed flight is left untouched", func(t *testing.T) {
		id := create("LHR", "CDG")
		patch(id, map[string]any{"starts_at": time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)})
		before, _ := e.store.PlanPartByID(ctx, id)
		fdBefore, _ := e.store.FlightDetailFor(ctx, id)
		patch(id, map[string]any{"start_label": "Heathrow"})
		patch(id, map[string]any{"starts_at": before.StartsAt})
		after, _ := e.store.PlanPartByID(ctx, id)
		fdAfter, _ := e.store.FlightDetailFor(ctx, id)
		if !after.StartsAt.Equal(before.StartsAt) || after.StartTZ != before.StartTZ || after.StartTZSource != "confirmed" {
			t.Errorf("part changed: %v %q -> %v %q", before.StartsAt, before.StartTZ, after.StartsAt, after.StartTZ)
		}
		if !fdAfter.ScheduledOut.Equal(fdBefore.ScheduledOut) || !fdAfter.ScheduledIn.Equal(fdBefore.ScheduledIn) {
			t.Errorf("schedule changed: %v -> %v", fdBefore.ScheduledOut, fdAfter.ScheduledOut)
		}
	})

	t.Run("an airport with no known zone keeps the digits", func(t *testing.T) {
		id := create("", "")
		patch(id, map[string]any{"starts_at": time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
			"ends_at": time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)})
		fd, _ := e.store.FlightDetailFor(ctx, id)
		if !fd.ScheduledOut.Equal(time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)) ||
			!fd.ScheduledIn.Equal(time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)) {
			t.Errorf("schedule = %v / %v, want the typed digits", fd.ScheduledOut.UTC(), fd.ScheduledIn.UTC())
		}
		part, _ := e.store.PlanPartByID(ctx, id)
		if part.StartTZ != "" || part.StartTZSource != "floating" || part.EndTZSource != "floating" {
			t.Errorf("part = %q/%q, %q, want left floating", part.StartTZ, part.StartTZSource, part.EndTZSource)
		}
	})
}
