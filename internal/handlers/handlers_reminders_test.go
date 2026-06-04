package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
)

// tripReminderState reads the trip detail as viewer and returns the trip-level
// reminder opt-in fields.
func tripReminderState(t *testing.T, e *testEnv, tripID, viewer int64) (bool, int) {
	t.Helper()
	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tripID), nil, viewer)
	if w.Code != http.StatusOK {
		t.Fatalf("get trip = %d %s", w.Code, w.Body.String())
	}
	var body struct {
		api.TripDTO
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trip: %v", err)
	}
	return body.ReminderOptedIn, body.ReminderLeadHours
}

// planReminderOverride reads the trip detail as viewer and returns the named
// plan's reminder_override.
func planReminderOverride(t *testing.T, e *testEnv, tripID, planID, viewer int64) string {
	t.Helper()
	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tripID), nil, viewer)
	if w.Code != http.StatusOK {
		t.Fatalf("get trip = %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Plans []api.PlanDTO `json:"plans"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trip: %v", err)
	}
	for _, p := range body.Plans {
		if p.ID == planID {
			return p.ReminderOverride
		}
	}
	t.Fatalf("plan %d not found in trip %d", planID, tripID)
	return ""
}

func TestTripReminder_RoundTrip(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")

	// Default: not opted in.
	if optedIn, _ := tripReminderState(t, e, tid, owner); optedIn {
		t.Fatal("default reminder_opted_in = true, want false")
	}
	// PUT opts in with a lead.
	if w := e.req(t, "PUT", fmt.Sprintf("/api/trips/%d/reminder", tid), map[string]any{"lead_hours": 12}, owner); w.Code != http.StatusNoContent {
		t.Fatalf("PUT reminder = %d %s", w.Code, w.Body.String())
	}
	optedIn, lead := tripReminderState(t, e, tid, owner)
	if !optedIn || lead != 12 {
		t.Fatalf("after PUT = (%v,%d), want (true,12)", optedIn, lead)
	}
	// DELETE opts out.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d/reminder", tid), nil, owner); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE reminder = %d %s", w.Code, w.Body.String())
	}
	if optedIn, _ := tripReminderState(t, e, tid, owner); optedIn {
		t.Fatal("after DELETE reminder_opted_in = true, want false")
	}
}

func TestTripReminder_ClampsLead(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	// Non-positive lead clamps to the 24h default.
	if w := e.req(t, "PUT", fmt.Sprintf("/api/trips/%d/reminder", tid), map[string]any{"lead_hours": 0}, owner); w.Code != http.StatusNoContent {
		t.Fatalf("PUT reminder = %d %s", w.Code, w.Body.String())
	}
	if _, lead := tripReminderState(t, e, tid, owner); lead != 24 {
		t.Fatalf("lead = %d, want clamped to 24", lead)
	}
}

func TestPlanReminder_Override(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), flightPlanBody(), owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", w.Code, w.Body.String())
	}
	pid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	if ov := planReminderOverride(t, e, tid, pid, owner); ov != "inherit" {
		t.Fatalf("default override = %q, want inherit", ov)
	}
	// Force off.
	if w := e.req(t, "PUT", fmt.Sprintf("/api/plans/%d/reminder", pid), map[string]any{"enabled": false, "lead_hours": 24}, owner); w.Code != http.StatusNoContent {
		t.Fatalf("PUT plan reminder = %d %s", w.Code, w.Body.String())
	}
	if ov := planReminderOverride(t, e, tid, pid, owner); ov != "off" {
		t.Fatalf("override = %q, want off", ov)
	}
	// Clear → inherit.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/plans/%d/reminder", pid), nil, owner); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE plan reminder = %d %s", w.Code, w.Body.String())
	}
	if ov := planReminderOverride(t, e, tid, pid, owner); ov != "inherit" {
		t.Fatalf("override = %q, want inherit", ov)
	}
}

func TestSetTripReminder_NonMemberHidden(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	stranger := e.user(t, "stranger", false)
	tid := newTrip(t, e, owner, "Trip")
	if w := e.req(t, "PUT", fmt.Sprintf("/api/trips/%d/reminder", tid), map[string]any{"lead_hours": 24}, stranger); w.Code != http.StatusNotFound {
		t.Fatalf("stranger PUT reminder = %d, want 404", w.Code)
	}
}
