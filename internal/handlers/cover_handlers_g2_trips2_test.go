package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// g2FlightTime is a fixed departure used when a test just needs a valid flight
// plan to exist (the exact instant is irrelevant).
var g2FlightTime = time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

// TestListTripsIncludeFriendsG2 covers listTrips' superuser ?include=friends
// scope (the friends branch of the switch).
func TestListTripsIncludeFriendsG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2lfadmin", true)
	w := e.req(t, "GET", "/api/trips?include=friends", nil, admin)
	if w.Code != http.StatusOK {
		t.Errorf("include=friends = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestListTripsDTOErrG2 covers listTrips' per-trip tripDTO error branch: the
// list query succeeds, then building a trip's DTO fails (trip_tags gone).
func TestListTripsDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2listdto", false)
	newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "trip_tags")
	if w := e.req(t, "GET", "/api/trips", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("list DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripErrPathsG2 covers getTrip's TripByID, tripDTO and visiblePlanDTOs
// error branches. A superuser viewer bypasses canViewTrip so we can drop tables
// the DTO/plans path reads.
func TestGetTripTripByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2gettbid", true)
	tid := newTrip(t, e, admin, "Trip")
	// TripByID selects country_code; dropping it errors the lookup (canViewTrip is
	// bypassed for the superuser).
	g1dropColumn(t, e, "trips", "country_code")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("getTrip TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripDTOErrG2 covers getTrip's tripDTO error branch: TripByID succeeds,
// then tripDTO's TripPassengers errors (trip_passengers gone).
func TestGetTripDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2getdto", true)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "trip_passengers")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("getTrip DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripVisiblePlansErrG2 covers getTrip's visiblePlanDTOs error branch:
// the trip + DTO succeed, then listing visible plans fails (plans gone).
func TestGetTripVisiblePlansErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2getplans", true)
	tid := newTrip(t, e, admin, "Trip")
	// Seed a plan so visiblePlanDTOs reaches planDTO. tripDTO builds first and
	// must succeed, so we drop a table only planDTO reads: plan_parts (PartsByPlan).
	// tripDTO never touches plan_parts, so it builds; then planDTO errors → the
	// visiblePlanDTOs error branch in getTrip.
	newFlightPlan(t, e, tid, admin, "BA1", g2FlightTime)
	g1dropTable(t, e, "plan_parts")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("getTrip visiblePlans err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdateTripDTOErrG2 covers updateTrip's tripDTO error branch: the update
// lands, then the DTO build fails (trip_passengers gone). Superuser owner.
func TestUpdateTripDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2upddto", true)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "trip_passengers")
	if w := e.req(t, "PATCH", "/api/trips/"+itoa(tid), map[string]any{"name": "Z"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("update DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestDeleteTripStoreErrG2 covers deleteTrip's DeleteTrip store-error branch:
// dropping a child column that the cascade delete cannot satisfy. A superuser
// owner passes requireTripOwner without reading the dropped artifact.
func TestDeleteTripStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2delserr", true)
	tid := newTrip(t, e, admin, "Trip")
	// DeleteTrip removes the trips row (cascading to children). Dropping the
	// trips table outright makes the DELETE error.
	g1dropTable(t, e, "trips")
	if w := e.req(t, "DELETE", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("delete store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddMemberStoreErrG2 covers addTripMember's AddTripMember store-error and
// its TripByID error branch. Superuser owner bypasses the friend check.
func TestAddMemberStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2admemserr", true)
	target := e.user(t, "g2admemtarget", false)
	tid := newTrip(t, e, admin, "Trip")
	// AddTripMember writes trip_members; dropping it errors the add (the
	// superuser owner check already passed for the route).
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/members",
		map[string]any{"user_id": target, "role": "viewer"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("add member store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddMemberTripByIDErrG2 covers addTripMember's TripByID error branch after
// a successful add: drop a trips column TripByID reads.
func TestAddMemberTripByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2admemtbid", true)
	target := e.user(t, "g2admemtbidtgt", false)
	tid := newTrip(t, e, admin, "Trip")
	g1dropColumn(t, e, "trips", "country_code")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/members",
		map[string]any{"user_id": target, "role": "viewer"}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("add member TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddPassengerCanViewErrG2 covers addTripPassenger's canViewTrip error
// branch (dropping plan_visibility for a non-superuser actor).
func TestAddPassengerCanViewErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2adpaxcv", false)
	tid := newTrip(t, e, owner, "Trip")
	g1dropTable(t, e, "plan_visibility")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/passengers",
		map[string]any{"user_id": 1}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("add pax canView err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddPassengerStoreErrG2 covers addTripPassenger's AddTripPassenger
// store-error and TripByID error branches. Superuser actor bypasses the friend
// check; canViewTrip is bypassed for the superuser.
func TestAddPassengerStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2adpaxserr", true)
	target := e.user(t, "g2adpaxtgt", false)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "trip_passengers")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/passengers",
		map[string]any{"user_id": target}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("add pax store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestAddPassengerTripByIDErrG2 covers addTripPassenger's TripByID error branch
// after a successful add.
func TestAddPassengerTripByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2adpaxtbid", true)
	target := e.user(t, "g2adpaxtbidtgt", false)
	tid := newTrip(t, e, admin, "Trip")
	g1dropColumn(t, e, "trips", "country_code")
	if w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/passengers",
		map[string]any{"user_id": target}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("add pax TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestSetTagsDTOErrG2 covers setTripTags' tripDTO error branch: SetTripTags and
// TripByID succeed, then tripDTO's TripPassengers errors (trip_passengers gone).
func TestSetTagsDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2settagsdto", true)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "trip_passengers")
	if w := e.req(t, "PUT", "/api/trips/"+itoa(tid)+"/tags",
		map[string]any{"labels": []string{"a"}}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("set tags DTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestTripDTOSubqueryErrsG2 covers tripDTO's TripRole-non-NotFound (451),
// IsTripPassenger (468) and TripReminderFor (476) error branches, each isolated
// by dropping the table that sub-query reads while the earlier ones still work.
// Driven via getTrip as a superuser (canViewTrip + TripByID bypass/succeed).
func TestTripDTOTripRoleErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2tdrole", true)
	tid := newTrip(t, e, admin, "Trip")
	// TripRole reads trip_members; dropping it errors with a real (non-NotFound)
	// error, hitting tripDTO's else branch.
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("tripDTO TripRole err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestTripDTOIsPassengerErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2tdpax", true)
	tid := newTrip(t, e, admin, "Trip")
	// plan_passengers feeds IsTripPassenger; TripRole/TripMembers/TagsByTrip all
	// run first and succeed (trip_members + trip_tags intact).
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("tripDTO IsTripPassenger err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestTripDTOReminderErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2tdrem", true)
	tid := newTrip(t, e, admin, "Trip")
	g1dropTable(t, e, "trip_reminder_optin")
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("tripDTO TripReminderFor err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestTripAuthHelpersNilUserG2 covers the defensive nil-user/nil-actor guards in
// canViewTrip, requireTripEdit, requireTripOwner and requireFriendTarget. These
// branches are unreachable through the authenticated mux (the middleware always
// supplies a user), so they are exercised here by calling the helpers directly.
func TestTripAuthHelpersNilUserG2(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	r := httptest.NewRequest("GET", "/", nil)

	if ok, err := e.api.canViewTrip(ctx, 1, nil); ok || err != nil {
		t.Errorf("canViewTrip(nil) = (%v,%v), want (false,nil)", ok, err)
	}

	w := httptest.NewRecorder()
	if err := e.api.requireTripEdit(r.Context(), 1, nil, w); err == nil || w.Code != http.StatusUnauthorized {
		t.Errorf("requireTripEdit(nil) = (%v, code %d), want (err, 401)", err, w.Code)
	}
	w = httptest.NewRecorder()
	if err := e.api.requireTripOwner(r.Context(), 1, nil, w); err == nil || w.Code != http.StatusUnauthorized {
		t.Errorf("requireTripOwner(nil) = (%v, code %d), want (err, 401)", err, w.Code)
	}
	w = httptest.NewRecorder()
	if err := e.api.requireFriendTarget(r.Context(), nil, 1, w); err == nil || w.Code != http.StatusUnauthorized {
		t.Errorf("requireFriendTarget(nil) = (%v, code %d), want (err, 401)", err, w.Code)
	}
}

// silence unused import if store ever drops out.
var _ = store.ErrNotFound
