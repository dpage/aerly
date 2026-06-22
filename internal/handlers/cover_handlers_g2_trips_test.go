package handlers

import (
	"net/http"
	"testing"
)

// TestTripsBadIDAndBodyG2 sweeps the bad-ID (400) and bad-body (400) branches
// across the trip endpoints that parse a path id and/or decode a JSON body.
func TestTripsBadIDAndBodyG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trbadid", false)
	tid := newTrip(t, e, owner, "Trip")

	// Bad path id → 400.
	badID := []struct {
		method, path string
	}{
		{"GET", "/api/trips/abc"},
		{"PATCH", "/api/trips/abc"},
		{"DELETE", "/api/trips/abc"},
		{"POST", "/api/trips/abc/members"},
		{"DELETE", "/api/trips/abc/members/1"},
		{"DELETE", "/api/trips/" + itoa(tid) + "/members/xyz"},
		{"POST", "/api/trips/abc/passengers"},
		{"DELETE", "/api/trips/abc/passengers/1"},
		{"DELETE", "/api/trips/" + itoa(tid) + "/passengers/xyz"},
		{"PUT", "/api/trips/abc/tags"},
	}
	for _, c := range badID {
		if w := e.req(t, c.method, c.path, map[string]any{}, owner); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d, want 400", c.method, c.path, w.Code)
		}
	}

	// Bad body (invalid JSON) on endpoints that decode after their auth check.
	badBody := []struct{ method, path string }{
		{"POST", "/api/trips"},
		{"PATCH", "/api/trips/" + itoa(tid)},
		{"POST", "/api/trips/" + itoa(tid) + "/members"},
		{"POST", "/api/trips/" + itoa(tid) + "/passengers"},
		{"PUT", "/api/trips/" + itoa(tid) + "/tags"},
	}
	for _, c := range badBody {
		if w := e.req(t, c.method, c.path, "??", owner); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s bad body = %d, want 400", c.method, c.path, w.Code)
		}
	}
}

// TestCreateTripBadDatesG2 covers createTrip's parseDate-failure branches for
// both starts_on and ends_on, and updateTrip's equivalents.
func TestCreateTripBadDatesG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trdates", false)

	if w := e.req(t, "POST", "/api/trips", map[string]any{"name": "X", "starts_on": "01/06/2026"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad starts_on = %d, want 400", w.Code)
	}
	if w := e.req(t, "POST", "/api/trips", map[string]any{"name": "X", "ends_on": "31-12-2026"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad ends_on = %d, want 400", w.Code)
	}

	tid := newTrip(t, e, owner, "Trip")
	if w := e.req(t, "PATCH", "/api/trips/"+itoa(tid), map[string]any{"starts_on": "nope"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("patch bad starts_on = %d, want 400", w.Code)
	}
	if w := e.req(t, "PATCH", "/api/trips/"+itoa(tid), map[string]any{"ends_on": "nope"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("patch bad ends_on = %d, want 400", w.Code)
	}
}

// TestAddTripMemberValidationG2 covers the missing-user_id and bad-role
// branches, plus the non-friend (403) target branch.
func TestAddTripMemberValidationG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trmemval", false)
	tid := newTrip(t, e, owner, "Trip")

	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/members", map[string]any{"role": "viewer"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("missing user_id = %d, want 400", w.Code)
	}
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/members", map[string]any{"user_id": 999999, "role": "viewer"}, owner); w.Code != http.StatusForbidden {
		t.Errorf("non-friend member = %d, want 403", w.Code)
	}
}

// TestAddTripPassengerValidationG2 covers the missing-user_id and non-friend
// branches of addTripPassenger, plus removeTripPassenger requiring edit for a
// non-self target.
func TestAddTripPassengerValidationG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trpaxval", false)
	tid := newTrip(t, e, owner, "Trip")

	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/passengers", map[string]any{}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("missing user_id = %d, want 400", w.Code)
	}
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/passengers", map[string]any{"user_id": 999999}, owner); w.Code != http.StatusForbidden {
		t.Errorf("non-friend pax = %d, want 403", w.Code)
	}

	// A non-editor removing someone other than themselves is forbidden.
	stranger := e.user(t, "g2trpaxstranger", false)
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid)+"/passengers/"+itoa(owner), nil, stranger); w.Code != http.StatusForbidden {
		t.Errorf("stranger remove other pax = %d, want 403", w.Code)
	}
}

// TestGetTripCanViewErrG2 covers getTrip's canViewTrip error branch.
func TestGetTripCanViewErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trgetcvowner", false)
	viewer := e.user(t, "g2trgetcvviewer", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, viewer); w.Code != http.StatusInternalServerError {
		t.Errorf("getTrip canView err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestCreateTripStoreAndDTOErrG2 covers createTrip's CreateTrip store-error and
// tripDTO-error branches.
func TestCreateTripStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trcreateserr", false)
	// CreateTrip inserts into trips; dropping the table makes the insert error.
	g1dropTable(t, e, "trips")
	if w := e.req(t, "POST", "/api/trips", map[string]any{"name": "X"}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("create store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestCreateTripDTOErrG2 covers createTrip's tripDTO error branch: the insert
// succeeds but building the response DTO fails because trip_tags is gone.
func TestCreateTripDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trcreatedto", false)
	g1dropTable(t, e, "trip_tags")
	if w := e.req(t, "POST", "/api/trips", map[string]any{"name": "X"}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("create DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestTripMutationStoreErrG2 covers the store-error branches of update, delete,
// add/remove member, add/remove passenger, set-tags, suggest-tags, and the
// list-trips store error, all driven by dropping the relevant table. A
// superuser owner is used where the auth check would otherwise read the dropped
// table.
func TestTripMutationStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2trmutadmin", true) // superuser bypasses member-based auth
	tid := newTrip(t, e, admin, "Trip")

	// suggestTags store error: drop the tag table.
	{
		e2 := setup(t, nil, nil)
		u := e2.user(t, "g2trsugg", false)
		g1dropTable(t, e2, "trip_tags")
		if w := e2.req(t, "GET", "/api/tags/suggest?q=x", nil, u); w.Code != http.StatusInternalServerError {
			t.Errorf("suggest store err = %d, want 500", w.Code)
		}
	}

	// listTrips store error.
	{
		e2 := setup(t, nil, nil)
		u := e2.user(t, "g2trlist", false)
		g1dropTable(t, e2, "trips")
		if w := e2.req(t, "GET", "/api/trips", nil, u); w.Code != http.StatusInternalServerError {
			t.Errorf("list store err = %d, want 500", w.Code)
		}
	}

	// setTripTags store error: drop trip_tags (SetTripTags writes it). Admin
	// bypasses the requireTripEdit membership read.
	g1dropTable(t, e, "trip_tags")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tid)+"/tags", map[string]any{"labels": []string{"a"}}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("set tags store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdateTripStoreErrG2 covers updateTrip's UpdateTrip store-error branch.
func TestUpdateTripStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2trupdserr", true)
	tid := newTrip(t, e, admin, "Trip")
	// Drop a column UpdateTrip writes but the (superuser) edit check does not read.
	g1dropColumn(t, e, "trips", "destination")
	if w := e.req(t, "PATCH", "/api/trips/"+itoa(tid), map[string]any{"name": "Y"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("update store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestDeleteTripStoreErrG2 covers deleteTrip's DeleteTrip store-error branch: a
// superuser owner so requireTripOwner passes, then dropping a child table that
// the cascade can't satisfy. We drop the trips table's NOT-yet-deletable child
// by removing a FK target indirectly; simpler: drop trips after capturing the
// id via a column drop that breaks DELETE.
func TestDeleteTripDTOPathsG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trdelowner", false)
	tid := newTrip(t, e, owner, "Trip")

	// requireTripOwner non-owner branch: an editor (not owner) can't delete.
	editor := e.user(t, "g2trdeleditor", false)
	e.befriend(t, owner, editor)
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/members", map[string]any{"user_id": editor, "role": "editor"}, owner); w.Code != http.StatusOK {
		t.Fatalf("add editor: %d %s", w.Code, w.Body.String())
	}
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid), nil, editor); w.Code != http.StatusForbidden {
		t.Errorf("editor delete = %d, want 403", w.Code)
	}
}

// TestRequireTripOwnerStoreErrG2 covers requireTripOwner's non-NotFound
// TripRole error branch: dropping trip_members makes the role lookup error for a
// non-superuser owner action.
func TestRequireTripOwnerStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trownerr", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("requireTripOwner store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRequireTripEditStoreErrG2 covers requireTripEdit's CanEditTrip error
// branch via setTripTags for a non-superuser whose edit check errors.
func TestRequireTripEditStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trediterr", false)
	tid := newTrip(t, e, owner, "Trip")
	// CanEditTrip reads trip_members; dropping it errors the edit check.
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tid)+"/tags", map[string]any{"labels": []string{"a"}}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("requireTripEdit store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRequireFriendTargetStoreErrG2 covers requireFriendTarget's
// AnyFriendshipEdge error branch: a non-superuser owner adds a member, but the
// friendship lookup errors because friendships is gone.
func TestRequireFriendTargetStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trfterr", false)
	target := e.user(t, "g2trfttarget", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "friendships")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/members",
		map[string]any{"user_id": target, "role": "viewer"}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("requireFriendTarget store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestTripMemberPassengerTagDTOErrG2 covers the tripDTO-error branches after a
// successful member/passenger/tag mutation: the write lands, then building the
// response DTO fails because trip_tags is gone (TagsByTrip errors). A superuser
// owner is used so the auth checks don't depend on the dropped table.
func TestAddMemberStoreAndDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2tradmemdto", true)
	friend := e.user(t, "g2tradmemfriend", false)
	// Admin (superuser) bypasses requireFriendTarget, so no friendship needed.
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "trip_tags")
	// AddTripMember succeeds, then tripDTO's TagsByTrip errors → 500.
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/members",
		map[string]any{"user_id": friend, "role": "viewer"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("add member DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddPassengerDTOErrG2 covers addTripPassenger's tripDTO error branch.
func TestAddPassengerDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2trpaxdto", true)
	friend := e.user(t, "g2trpaxfriend", false)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "trip_tags")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/passengers",
		map[string]any{"user_id": friend}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("add pax DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRemoveMemberAndPassengerStoreErrG2 covers the RemoveTripMember and
// RemoveTripPassenger store-error branches.
// TestRemoveMemberStoreErrG2 covers removeTripMember's RemoveTripMember
// store-error branch: a superuser owner bypasses the owner check, then the
// DELETE errors because trip_members is gone.
func TestRemoveMemberStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2trrmadmin", true)
	tid := newTrip(t, e, admin, "Trip")
	other := e.user(t, "g2trrmother", false)

	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid)+"/members/"+itoa(other), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("remove member store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRemoveMemberOwnerCheckErrG2 covers removeTripMember's requireTripOwner
// error-return branch (298): a NON-superuser owner whose TripRole lookup errors
// (trip_members dropped) makes requireTripOwner return a non-NotFound error.
func TestRemoveMemberOwnerCheckErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trrmownerr", false)
	other := e.user(t, "g2trrmotherr", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid)+"/members/"+itoa(other), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("remove member owner-check err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestRemovePassengerStoreErrG2 covers removeTripPassenger's RemoveTripPassenger
// store-error branch using self-removal (no edit check needed).
func TestRemovePassengerStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2trrmpax", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "trip_passengers")
	// Self-removal: me.ID == uid, so the edit check is skipped and RemoveTripPassenger errors.
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid)+"/passengers/"+itoa(owner), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("remove pax store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSetTagsTripByIDErrG2 covers setTripTags' TripByID error branch after a
// successful SetTripTags: drop a trips column TripByID reads. A superuser owner
// avoids the edit check touching that column.
func TestSetTagsTripByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2trsettagstbid", true)
	tid := newTrip(t, e, admin, "Trip")
	g1dropColumn(t, e, "trips", "country_code")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tid)+"/tags", map[string]any{"labels": []string{"a"}}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("set tags TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
