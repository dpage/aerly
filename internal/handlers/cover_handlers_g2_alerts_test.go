package handlers

import (
	"net/http"
	"testing"
	"time"
)

// TestSetAlertPrefsBadBodyG2 covers setAlertPrefs' decode-failure branch.
func TestSetAlertPrefsBadBodyG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2apbad", false)
	if w := e.req(t, "PUT", "/api/alert-prefs", "??", uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400", w.Code)
	}
}

// TestSetAlertPrefsClampsNegativeG2 covers the min_delay_min < 0 floor.
func TestSetAlertPrefsClampsNegativeG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2apclamp", false)
	w := e.req(t, "PUT", "/api/alert-prefs", map[string]any{"min_delay_min": -10}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := decodeBody[alertPrefsDTO](t, w); got.MinDelayMin != 0 {
		t.Errorf("min_delay_min = %d, want clamped to 0", got.MinDelayMin)
	}
}

// TestAlertPrefsStoreErrG2 covers the AlertPrefsFor / SetAlertPrefs store-error
// branches by dropping the backing table.
func TestAlertPrefsStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2apserr", false)
	g1dropTable(t, e, "alert_prefs")
	if w := e.req(t, "GET", "/api/alert-prefs", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("get prefs store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if w := e.req(t, "PUT", "/api/alert-prefs", map[string]any{"in_app": false}, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("set prefs store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestPlanAlertOptinBadInputsG2 covers the bad-ID branches of both opt-in
// handlers.
func TestPlanAlertOptinBadInputsG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2optbad", false)
	if w := e.req(t, "POST", "/api/plans/abc/alerts/optin", nil, uid); w.Code != http.StatusBadRequest {
		t.Errorf("optin bad id = %d, want 400", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/plans/abc/alerts/optin", nil, uid); w.Code != http.StatusBadRequest {
		t.Errorf("optout bad id = %d, want 400", w.Code)
	}
}

// TestPlanAlertOptinStoreErrG2 covers addPlanAlertOptin's CanViewPlan error
// branch and AddPlanAlertOptin's insert error, plus removePlanAlertOptin's
// delete error.
func TestPlanAlertOptinStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2optserr", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	// Drop plan_alert_optin: the owner's CanViewPlan still works, but the insert
	// (add) and delete (remove) error.
	g1dropTable(t, e, "plan_alert_optin")
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/alerts/optin", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("add optin store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if w := e.req(t, "DELETE", "/api/plans/"+itoa(pid)+"/alerts/optin", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("remove optin store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestPlanAlertOptinCanViewErrG2 covers addPlanAlertOptin's CanViewPlan error
// branch (dropping plan_visibility for a non-owner viewer).
func TestPlanAlertOptinCanViewErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2optcvowner", false)
	viewer := e.user(t, "g2optcvviewer", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/alerts/optin", nil, viewer); w.Code != http.StatusInternalServerError {
		t.Errorf("optin canView err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
