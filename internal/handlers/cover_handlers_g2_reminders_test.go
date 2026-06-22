package handlers

import (
	"net/http"
	"testing"
	"time"
)

// TestClampLeadUpperBoundG2 covers clampLead's upper-bound clamp (>8760 → 8760),
// which the round-trip tests didn't reach.
func TestClampLeadUpperBoundG2(t *testing.T) {
	if got := clampLead(99999); got != 8760 {
		t.Errorf("clampLead(99999) = %d, want 8760", got)
	}
	if got := clampLead(100); got != 100 {
		t.Errorf("clampLead(100) = %d, want 100 (unchanged)", got)
	}
}

// TestTripReminderBadInputsG2 covers the bad-ID and bad-body branches of the two
// trip-reminder handlers.
func TestTripReminderBadInputsG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trbad", false)
	tid := newTrip(t, e, owner, "Trip")

	// Bad trip ID → 400.
	if w := e.req(t, "PUT", "/api/trips/abc/reminder", map[string]any{"lead_hours": 1}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("PUT bad id = %d, want 400", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/trips/abc/reminder", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("DELETE bad id = %d, want 400", w.Code)
	}
	// Bad body on a viewable trip → 400 (decode failure after the view check).
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tid)+"/reminder", "??", owner); w.Code != http.StatusBadRequest {
		t.Errorf("PUT bad body = %d, want 400", w.Code)
	}
}

// TestTripReminderStoreErrG2 covers the store-error (500) branches: a viewable
// trip whose reminder set/delete fails because the backing table is gone, plus
// the canViewTrip error path.
func TestTripReminderStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trserr", false)
	tid := newTrip(t, e, owner, "Trip")

	// SetTripReminder writes trip_reminder_optin; dropping it makes the set error
	// after the (still working) canViewTrip check.
	g1dropTable(t, e, "trip_reminder_optin")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tid)+"/reminder", map[string]any{"lead_hours": 4}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("set reminder store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid)+"/reminder", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("delete reminder store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestTripReminderCanViewErrG2 covers setTripReminder's canViewTrip error
// branch: dropping plan_visibility (used by CanViewTrip but not auth) makes the
// view check error for a non-owner viewer.
func TestTripReminderCanViewErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trcvowner", false)
	viewer := e.user(t, "g2trcvviewer", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tid)+"/reminder", map[string]any{"lead_hours": 4}, viewer); w.Code != http.StatusInternalServerError {
		t.Errorf("set reminder canView err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestPlanReminderBadInputsG2 covers the bad-ID, bad-body and not-found (404)
// branches of the two plan-reminder handlers.
func TestPlanReminderBadInputsG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2prbad", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	if w := e.req(t, "PUT", "/api/plans/abc/reminder", map[string]any{"enabled": true}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("PUT bad id = %d, want 400", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/plans/abc/reminder", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("DELETE bad id = %d, want 400", w.Code)
	}
	// A viewer who can't see the plan → 404 (existence not leaked).
	outsider := e.user(t, "g2proutsider", false)
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/reminder", map[string]any{"enabled": true}, outsider); w.Code != http.StatusNotFound {
		t.Errorf("outsider PUT = %d, want 404", w.Code)
	}
	// Bad body on a viewable plan → 400.
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/reminder", "??", owner); w.Code != http.StatusBadRequest {
		t.Errorf("PUT bad body = %d, want 400", w.Code)
	}
}

// TestPlanReminderStoreErrG2 covers the store-error branches of the plan
// reminder set/delete and the CanViewPlan error path.
func TestPlanReminderStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2prserr", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	// SetPlanReminder writes plan_reminder_optin; dropping it makes set/delete
	// error (CanViewPlan still works for the owner).
	g1dropTable(t, e, "plan_reminder_optin")
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/reminder", map[string]any{"enabled": true, "lead_hours": 6}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("set plan reminder store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if w := e.req(t, "DELETE", "/api/plans/"+itoa(pid)+"/reminder", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("delete plan reminder store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestPlanReminderCanViewErrG2 covers setPlanReminder's CanViewPlan error branch
// by dropping plan_visibility for a non-owner viewer.
func TestPlanReminderCanViewErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2prcvowner", false)
	viewer := e.user(t, "g2prcvviewer", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/reminder", map[string]any{"enabled": true}, viewer); w.Code != http.StatusInternalServerError {
		t.Errorf("plan reminder canView err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
