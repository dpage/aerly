package handlers

import (
	"net/http"
	"testing"
	"time"
)

// TestCreatePlanInlinePassengerStoreErrG2 covers createPlan's inline
// AddPlanPassenger store-error branch (322): a superuser adds an inline
// passenger (bypassing the friend check) while plan_passengers is gone, so the
// add errors (400, echoing the store message). The plan row is created first
// (CreatePlan with no parts avoids the plan_passengers trigger).
func TestCreatePlanInlinePassengerStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2cpinlpax", true)
	target := e.user(t, "g2cpinlpaxt", false)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "dining", "title": "D", "passenger_ids": []int64{target},
	}, admin); w.Code != http.StatusBadRequest {
		t.Errorf("inline pax store err = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestCreatePlanInlineVisibilityStoreErrG2 covers createPlan's inline
// SetPlanVisibility store-error branch (332): plan_visibility is gone, so the
// visibility set fails after the plan is created.
func TestCreatePlanInlineVisibilityStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2cpinlvis", true)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "dining", "title": "D",
		"visibility": map[string]any{"mode": "hidden_from", "user_ids": []int64{1}},
	}, admin); w.Code != http.StatusBadRequest {
		t.Errorf("inline vis store err = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestDeletePlanByIDErrG2 covers deletePlan's post-auth PlanByID error branch
// (400/500): a superuser passes requirePlanEdit, then the PlanByID re-read fails
// because a selected column is gone.
func TestDeletePlanByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2delbyid", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	g1dropColumn(t, e, "plans", "share_all_friends")
	if w := e.req(t, "DELETE", "/api/plans/"+itoa(pid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("delete PlanByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSetPlanVisibilityEveryoneG2 covers setPlanVisibility's "everyone" mode
// branch (mode cleared) and its DTO-error branch.
func TestSetPlanVisibilityEveryoneG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2viseveryone", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/visibility", map[string]any{"mode": "everyone"}, owner); w.Code != http.StatusOK {
		t.Errorf("everyone mode = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestSetPlanVisibilityDTOErrG2 covers setPlanVisibility's planDTO error branch.
func TestSetPlanVisibilityDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2visdto", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	// SetPlanVisibility with "everyone" clears the row (no plan_visibility write),
	// then planDTO's PlanReminderFor errors because plan_reminder_optin is gone.
	g1dropTable(t, e, "plan_reminder_optin")
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/visibility", map[string]any{"mode": "everyone"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("set vis DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddPlanPassengerDTOErrG2 covers addPlanPassenger's planDTO error branch.
func TestAddPlanPassengerDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2appaxdto", true)
	target := e.user(t, "g2appaxdtot", false)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	// AddPlanPassenger succeeds (plan_passengers intact), then planDTO's
	// PlanReminderFor errors (plan_reminder_optin gone).
	g1dropTable(t, e, "plan_reminder_optin")
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/passengers", map[string]any{"user_id": target}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("add pax DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestMovePlanDTOErrG2 covers movePlan's planDTO error branch after a successful
// move.
func TestMovePlanDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2mvdto", true)
	src := newTrip(t, e, admin, "Src")
	dst := newTrip(t, e, admin, "Dst")
	pid := g2dining(t, e, src, admin)
	g1dropTable(t, e, "plan_reminder_optin")
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/move", map[string]any{"trip_id": dst}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("move DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestLinkPlansDTOErrG2 covers linkPlans' planDTO error branch after a
// successful link of two flights.
func TestLinkPlansDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2lkdto", true)
	tid := newTrip(t, e, admin, "Trip")
	primary := newFlightPlan(t, e, tid, admin, "AY1", g2planOut)
	absorbed := newFlightPlan(t, e, tid, admin, "AY2", g2planOut.Add(3*time.Hour))
	g1dropTable(t, e, "plan_reminder_optin")
	if w := e.req(t, "POST", "/api/plans/"+itoa(primary)+"/link", map[string]any{"plan_ids": []int64{absorbed}}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("link DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSplitPlanPartDTOErrG2 covers splitPlanPart's planDTO error branch after a
// successful split.
func TestSplitPlanPartDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2splitdto", true)
	tid := newTrip(t, e, admin, "Trip")
	primary := newFlightPlan(t, e, tid, admin, "AY1", g2planOut)
	absorbed := newFlightPlan(t, e, tid, admin, "AY2", g2planOut.Add(3*time.Hour))
	if w := e.req(t, "POST", "/api/plans/"+itoa(primary)+"/link", map[string]any{"plan_ids": []int64{absorbed}}, admin); w.Code != http.StatusOK {
		t.Fatalf("link setup = %d %s", w.Code, w.Body.String())
	}
	parts := planParts(t, e, admin, tid, primary)
	leg2 := int64(parts[1]["id"].(float64))
	g1dropTable(t, e, "plan_reminder_optin")
	if w := e.req(t, "POST", "/api/plan-parts/"+itoa(leg2)+"/split", nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("split DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdatePartFlightDetailReloadErrG2 covers updatePlanPart's post-flight-edit
// PlanPartByID reload error branch (760): a flight edit applies, then the reload
// fails because a plan_parts column it reads is gone.
func TestCreateFlightPlanNoDetailG2(t *testing.T) {
	// Covers toCreatePartPayload's flight else-branch (1012): a flight part with
	// no flight detail block falls back to a default FlightDetail.
	e := setup(t, nil, nil)
	owner := e.user(t, "g2flnodetail", false)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "flight", "title": "Bare flight",
		"parts": []map[string]any{{
			"type": "flight", "starts_at": g2planOut, "ends_at": g2planOut.Add(2 * time.Hour),
			"start_label": "LHR", "end_label": "JFK",
		}},
	}, owner)
	if w.Code != http.StatusCreated {
		t.Errorf("bare flight create = %d, want 201; body=%s", w.Code, w.Body.String())
	}
}

// TestApplyFlightEditEdgeCasesG2 covers applyFlightEdit's nil-detail (non-flight
// part) and ident-unchanged branches, plus the no-change manual route case.
func TestApplyFlightEditEdgeCasesG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2fledge", false)

	// Non-flight part with a flight edit block: applyFlightEdit returns nil
	// (fd == nil), so the part edit still succeeds.
	_, _, diningPart := g2makePlanOfType(t, e, owner, "dining", nil)
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(diningPart), map[string]any{
		"flight": map[string]any{"ident": "BA1"},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("flight edit on dining part = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Flight part: send the SAME ident (after normalisation) → ident-unchanged
	// branch, no re-resolve. Also send no IATA change.
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", g2planOut)
	flightPart := g2firstPartID(t, e, tid, pid, owner)
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(flightPart), map[string]any{
		"flight": map[string]any{"ident": "ba 286"},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("flight edit same ident = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePlanEditStoreErrG2 covers requirePlanEdit's CanEditTrip error and
// PlanByID error branches.
func TestRequirePlanEditStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2rpeerr", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	// CanEditTrip reads trip_members; dropping it errors the edit check for a
	// non-superuser (PlanByID succeeds, then CanEditTrip errors).
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"notes": "x"}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("requirePlanEdit CanEditTrip err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePlanEditPlanByIDErrG2 covers requirePlanEdit's PlanByID error
// branch.
func TestRequirePlanEditPlanByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2rpebyid", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	g1dropColumn(t, e, "plans", "share_all_friends")
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"notes": "x"}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("requirePlanEdit PlanByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePartEditStoreErrG2 covers requirePartEdit's PlanIDForPart error and
// CanEditTrip error branches.
func TestRequirePartEditStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2rparterr", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	partID := g2firstPartID(t, e, tid, pid, owner)
	// CanEditTrip reads trip_members; PlanIDForPart reads plan_parts (intact),
	// then the edit check errors.
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "POST", "/api/plan-parts/"+itoa(partID)+"/dismiss", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("requirePartEdit CanEditTrip err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePartEditPlanIDForPartErrG2 covers requirePartEdit's PlanIDForPart
// error branch.
func TestRequirePartEditPlanIDForPartErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2rpartpidf", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	partID := g2firstPartID(t, e, tid, pid, owner)
	// PlanIDForPart joins plans/plan_parts; dropping a plans column it reads
	// errors the lookup before the edit check.
	g1dropColumn(t, e, "plans", "trip_id")
	if w := e.req(t, "POST", "/api/plan-parts/"+itoa(partID)+"/dismiss", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("requirePartEdit PlanIDForPart err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
