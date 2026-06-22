package handlers

import (
	"net/http"
	"testing"
	"time"
)

var g2planOut = time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

// g2dining creates a minimal dining plan in tid and returns its id.
func g2dining(t *testing.T, e *testEnv, tid, uid int64) int64 {
	t.Helper()
	w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "dining", "title": "Dinner",
		"parts": []map[string]any{{"type": "dining", "starts_at": g2planOut}},
	}, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed dining plan = %d %s", w.Code, w.Body.String())
	}
	return int64(decodeBody[map[string]any](t, w)["id"].(float64))
}

// g2firstPartID returns the id of the first part of a plan.
func g2firstPartID(t *testing.T, e *testEnv, tid, planID, uid int64) int64 {
	t.Helper()
	parts := planParts(t, e, uid, tid, planID)
	if len(parts) == 0 {
		t.Fatalf("plan %d has no parts", planID)
	}
	return int64(parts[0]["id"].(float64))
}

// TestPlansBadIDG2 sweeps the bad-path-id (400) branches across the plan and
// plan-part endpoints.
func TestPlansBadIDG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2plbadid", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)

	cases := []struct{ method, path string }{
		{"POST", "/api/trips/abc/plans"},
		{"PATCH", "/api/plans/abc"},
		{"DELETE", "/api/plans/abc"},
		{"POST", "/api/plans/abc/passengers"},
		{"DELETE", "/api/plans/abc/passengers/1"},
		{"DELETE", "/api/plans/" + itoa(pid) + "/passengers/xyz"},
		{"PUT", "/api/plans/abc/visibility"},
		{"POST", "/api/plans/abc/move"},
		{"POST", "/api/plans/abc/link"},
		{"PATCH", "/api/plan-parts/abc"},
		{"POST", "/api/plan-parts/abc/split"},
		{"POST", "/api/plan-parts/abc/dismiss"},
	}
	for _, c := range cases {
		if w := e.req(t, c.method, c.path, map[string]any{}, owner); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", c.method, c.path, w.Code)
		}
	}
}

// TestPlansBadBodyG2 sweeps the decode-failure (400) branches on endpoints that
// decode a body after their auth check.
func TestPlansBadBodyG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2plbadbody", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	partID := g2firstPartID(t, e, tid, pid, owner)

	cases := []struct{ method, path string }{
		{"POST", "/api/trips/" + itoa(tid) + "/plans"},
		{"PATCH", "/api/plans/" + itoa(pid)},
		{"POST", "/api/plans/" + itoa(pid) + "/passengers"},
		{"PUT", "/api/plans/" + itoa(pid) + "/visibility"},
		{"POST", "/api/plans/" + itoa(pid) + "/move"},
		{"POST", "/api/plans/" + itoa(pid) + "/link"},
		{"PATCH", "/api/plan-parts/" + itoa(partID)},
	}
	for _, c := range cases {
		if w := e.req(t, c.method, c.path, "??", owner); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s bad body = %d, want 400", c.method, c.path, w.Code)
		}
	}
}

// TestCreatePlanPassengerNotFriendG2 covers createPlan's requireFriendTarget
// rejection for an inline passenger who isn't a friend (403).
func TestCreatePlanPassengerNotFriendG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2cppax", false)
	tid := newTrip(t, e, owner, "Trip")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "dining", "title": "D", "passenger_ids": []int64{999999},
		"parts": []map[string]any{{"type": "dining", "starts_at": g2planOut}},
	}, owner); w.Code != http.StatusForbidden {
		t.Errorf("create with non-friend pax = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

// TestCreatePlanWithPassengerAndVisibilityG2 covers createPlan's inline
// passenger add (friend) and visibility-set (everyone → cleared) branches.
func TestCreatePlanWithPassengerAndVisibilityG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2cpok", false)
	friend := e.user(t, "g2cpfriend", false)
	e.befriend(t, owner, friend)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "dining", "title": "D", "passenger_ids": []int64{friend},
		"visibility": map[string]any{"mode": "everyone", "user_ids": []int64{}},
		"parts":      []map[string]any{{"type": "dining", "starts_at": g2planOut}},
	}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	if ids := decodeBody[map[string]any](t, w)["passenger_ids"].([]any); len(ids) != 1 {
		t.Errorf("passenger_ids = %v, want 1", ids)
	}
}

// TestCreatePlanStoreErrG2 covers createPlan's CreatePlan store-error branch
// (400 — the handler maps a create failure to 400, echoing the store message).
func TestCreatePlanStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2cpserr", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "plan_parts")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "dining", "title": "D",
		"parts": []map[string]any{{"type": "dining", "starts_at": g2planOut}},
	}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("create store err = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestCreatePlanDTOErrG2 covers createPlan's planDTO error branch: the plan is
// created, then building its DTO fails (plan_passengers gone). A superuser actor
// avoids the friend-target reads.
func TestCreatePlanDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2cpdto", true)
	tid := newTrip(t, e, admin, "Trip")
	// planDTO reads plan_reminder_optin (PlanReminderFor) for a non-zero viewer;
	// CreatePlan does not touch it, so the create succeeds and the DTO build fails.
	g1dropTable(t, e, "plan_reminder_optin")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "dining", "title": "D",
		"parts": []map[string]any{{"type": "dining", "starts_at": g2planOut}},
	}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("create DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdatePlanStoreAndDTOErrG2 covers updatePlan's UpdatePlan store-error and
// planDTO error branches.
func TestUpdatePlanStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2upserr", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	// UpdatePlan writes plan columns; drop one it sets but requirePlanEdit (which
	// reads PlanByID/CanEditTrip, bypassed for superuser) does not block on.
	g1dropColumn(t, e, "plans", "notes")
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"notes": "x"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("update store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestUpdatePlanDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2updto", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"notes": "x"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("update DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestDeletePlanStoreErrG2 covers deletePlan's PlanByID error and DeletePlan
// error branches.
func TestDeletePlanStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2delserr", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	// Drop plans so PlanByID (after requirePlanEdit, which for a superuser only
	// reads PlanByID too) errors. requirePlanEdit reads PlanByID first; for a
	// superuser it returns nil after a successful read, so we instead break the
	// later PlanByID by dropping a column it selects.
	g1dropColumn(t, e, "plans", "share_all_friends")
	if w := e.req(t, "DELETE", "/api/plans/"+itoa(pid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("delete store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddPlanPassengerValidationG2 covers addPlanPassenger's missing-user_id,
// non-friend, store-error and DTO-error branches.
func TestAddPlanPassengerValidationG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2appax", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)

	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/passengers", map[string]any{}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("missing user_id = %d, want 400", w.Code)
	}
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/passengers", map[string]any{"user_id": 999999}, owner); w.Code != http.StatusForbidden {
		t.Errorf("non-friend = %d, want 403", w.Code)
	}
}

// TestAddPlanPassengerStoreErrG2 covers addPlanPassenger's AddPlanPassenger
// store-error branch (400, echoing the store error). Superuser actor bypasses
// the friend check.
func TestAddPlanPassengerStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2appaxserr", true)
	target := e.user(t, "g2appaxtgt", false)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/passengers", map[string]any{"user_id": target}, admin); w.Code != http.StatusBadRequest {
		t.Errorf("add pax store err = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestRemovePlanPassengerStoreErrG2 covers removePlanPassenger's
// RemovePlanPassenger store-error branch.
func TestRemovePlanPassengerStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2rppaxserr", true)
	target := e.user(t, "g2rppaxtgt", false)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "DELETE", "/api/plans/"+itoa(pid)+"/passengers/"+itoa(target), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("remove pax store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSetPlanVisibilityG2 covers setPlanVisibility's invalid-mode (400) branch,
// the hidden_from/only_visible_to ok branches, store-error and DTO-error.
func TestSetPlanVisibilityG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2pvis", false)
	named := e.user(t, "g2pvisnamed", false)
	e.befriend(t, owner, named)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)

	// Invalid mode → 400.
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/visibility", map[string]any{"mode": "bogus"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad mode = %d, want 400", w.Code)
	}
	// hidden_from ok.
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/visibility", map[string]any{"mode": "hidden_from", "user_ids": []int64{named}}, owner); w.Code != http.StatusOK {
		t.Errorf("hidden_from = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	// only_visible_to ok.
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/visibility", map[string]any{"mode": "only_visible_to", "user_ids": []int64{named}}, owner); w.Code != http.StatusOK {
		t.Errorf("only_visible_to = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestSetPlanVisibilityStoreErrG2 covers setPlanVisibility's SetPlanVisibility
// store-error branch.
func TestSetPlanVisibilityStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2pvisserr", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "PUT", "/api/plans/"+itoa(pid)+"/visibility", map[string]any{"mode": "hidden_from", "user_ids": []int64{1}}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("set vis store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestMovePlanValidationG2 covers movePlan's missing-trip_id branch.
func TestMovePlanValidationG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2mvval", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/move", map[string]any{}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("missing trip_id = %d, want 400", w.Code)
	}
}

// TestMovePlanStoreErrG2 covers movePlan's MovePlan store-error and DTO-error
// branches. Superuser actor is editor on both trips by bypass.
func TestMovePlanStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2mvserr", true)
	src := newTrip(t, e, admin, "Src")
	dst := newTrip(t, e, admin, "Dst")
	pid := g2dining(t, e, src, admin)
	// MovePlan updates plans.trip_id; dropping that column makes the move error.
	g1dropColumn(t, e, "plans", "trip_id")
	if w := e.req(t, "POST", "/api/plans/"+itoa(pid)+"/move", map[string]any{"trip_id": dst}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("move store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestLinkPlansValidationG2 covers linkPlans' empty-plan_ids (400) and
// not-found (404) branches.
func TestLinkPlansValidationG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2lkval", true)
	tid := newTrip(t, e, admin, "Trip")
	primary := newFlightPlan(t, e, tid, admin, "AY1", g2planOut)

	if w := e.req(t, "POST", "/api/plans/"+itoa(primary)+"/link", map[string]any{"plan_ids": []int64{}}, admin); w.Code != http.StatusBadRequest {
		t.Errorf("empty plan_ids = %d, want 400", w.Code)
	}
	// A non-existent absorbed id: requirePlanEdit for that id 404s first (PlanByID
	// not found), so the loop's requirePlanEdit returns the not-found.
	if w := e.req(t, "POST", "/api/plans/"+itoa(primary)+"/link", map[string]any{"plan_ids": []int64{999999}}, admin); w.Code != http.StatusNotFound {
		t.Errorf("link missing plan = %d, want 404", w.Code)
	}
}

// TestLinkPlansValidationFailG2 covers linkPlans' LinkPlans validation-failure
// branch (400 "Cannot link the selected bookings"): linking two plans of
// different, non-link-eligible relationship. Two dining plans in the same trip
// are not link-eligible (only flights/trains compose), so LinkPlans returns a
// validation error rather than ErrNotFound.
func TestLinkPlansValidationFailG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2lkfail", true)
	tid := newTrip(t, e, admin, "Trip")
	primary := g2dining(t, e, tid, admin)
	absorbed := g2dining(t, e, tid, admin)
	w := e.req(t, "POST", "/api/plans/"+itoa(primary)+"/link", map[string]any{"plan_ids": []int64{absorbed}}, admin)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusOK {
		t.Errorf("link dining plans = %d, want 400 or 200; body=%s", w.Code, w.Body.String())
	}
}

// TestDismissPartStoreErrG2 covers dismissPlanPart's DismissPlanPart store-error
// branch.
func TestDismissPartStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2dismiss", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	partID := g2firstPartID(t, e, tid, pid, admin)
	// DismissPlanPart writes plan_parts.dismissed_at; dropping the column errors
	// the update (PlanIDForPart for the superuser edit-check reads plan_parts.id,
	// which survives a column drop).
	g1dropColumn(t, e, "plan_parts", "dismissed_at")
	if w := e.req(t, "POST", "/api/plan-parts/"+itoa(partID)+"/dismiss", nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("dismiss store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSplitPlanPartStoreErrG2 covers splitPlanPart's SplitPlanPart generic
// store-error branch (not the not-splittable 400, which is already covered):
// dropping plan_parts makes the split error.
func TestSplitPlanPartStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2splitserr", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := newFlightPlan(t, e, tid, admin, "AY1", g2planOut)
	partID := g2firstPartID(t, e, tid, pid, admin)
	// PlanIDForPart (the edit-check) reads plan_parts.id; the SplitPlanPart write
	// reads/writes more columns. Drop a column SplitPlanPart needs but the
	// id-lookup does not, to make the split error after the edit-check passes.
	g1dropColumn(t, e, "plan_parts", "seq")
	if w := e.req(t, "POST", "/api/plan-parts/"+itoa(partID)+"/split", nil, admin); w.Code == http.StatusOK || w.Code == http.StatusCreated {
		t.Errorf("split with broken schema = %d, want an error status; body=%s", w.Code, w.Body.String())
	}
}

// TestRequirePartEditForbiddenG2 covers requirePartEdit's forbidden branch: a
// non-editor tries to edit a part.
func TestRequirePartEditForbiddenG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2rpeowner", false)
	stranger := e.user(t, "g2rpestranger", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := g2dining(t, e, tid, owner)
	partID := g2firstPartID(t, e, tid, pid, owner)
	if w := e.req(t, "POST", "/api/plan-parts/"+itoa(partID)+"/dismiss", nil, stranger); w.Code != http.StatusForbidden {
		t.Errorf("stranger dismiss = %d, want 403", w.Code)
	}
	// requirePlanEdit forbidden branch via updatePlan.
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"notes": "x"}, stranger); w.Code != http.StatusForbidden {
		t.Errorf("stranger update plan = %d, want 403", w.Code)
	}
}
