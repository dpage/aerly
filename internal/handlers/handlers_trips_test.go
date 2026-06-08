package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dpage/aerly/internal/store"
)

func TestTripCRUDEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	stranger := e.user(t, "stranger", false)

	// Create requires a name.
	if w := e.req(t, "POST", "/api/trips", map[string]any{}, owner); w.Code != 400 {
		t.Errorf("create without name = %d, want 400", w.Code)
	}
	w := e.req(t, "POST", "/api/trips", map[string]any{
		"name": "Italy", "destination": "Rome", "starts_on": "2026-06-01",
	}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	trip := decodeBody[map[string]any](t, w)
	tid := int64(trip["id"].(float64))
	if trip["my_role"] != "owner" {
		t.Errorf("my_role = %v, want owner", trip["my_role"])
	}
	if trip["starts_on"] != "2026-06-01" {
		t.Errorf("starts_on = %v", trip["starts_on"])
	}

	// List shows it to the owner, not the stranger.
	if w := e.req(t, "GET", "/api/trips", nil, owner); len(decodeBody[[]map[string]any](t, w)) != 1 {
		t.Error("owner should list 1 trip")
	}
	if w := e.req(t, "GET", "/api/trips", nil, stranger); len(decodeBody[[]map[string]any](t, w)) != 0 {
		t.Error("stranger should list 0 trips")
	}

	// Get embeds a plans array (FE contract: Trip & { plans: Plan[] }).
	w = e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, owner)
	if w.Code != 200 {
		t.Fatalf("get = %d %s", w.Code, w.Body.String())
	}
	full := decodeBody[map[string]any](t, w)
	if _, ok := full["plans"]; !ok {
		t.Error("trip detail must embed a plans array")
	}

	// Stranger gets 404 (existence not leaked).
	if w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, stranger); w.Code != 404 {
		t.Errorf("stranger get = %d, want 404", w.Code)
	}

	// Patch (owner).
	w = e.req(t, "PATCH", fmt.Sprintf("/api/trips/%d", tid), map[string]any{"name": "Italy 26"}, owner)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["name"] != "Italy 26" {
		t.Errorf("patch = %d %s", w.Code, w.Body.String())
	}
	// Stranger can't patch.
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/trips/%d", tid), map[string]any{"name": "x"}, stranger); w.Code != 403 {
		t.Errorf("stranger patch = %d, want 403", w.Code)
	}

	// Delete (stranger 403, owner 204).
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d", tid), nil, stranger); w.Code != 403 {
		t.Errorf("stranger delete = %d, want 403", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d", tid), nil, owner); w.Code != 204 {
		t.Errorf("owner delete = %d, want 204", w.Code)
	}
}

// TestListTripsViewerIsPassenger: a passenger on a plan sees the trip in their
// list flagged viewer_is_passenger (so the FE files it under "My trips" and
// badges it), while a shared-only viewer and the owner are not flagged (#19).
func TestListTripsViewerIsPassenger(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	owner := e.user(t, "owner", false)
	pax := e.user(t, "pax", false)
	shared := e.user(t, "shared", false)

	var tid, pid int64
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, owner).Scan(&tid); err != nil {
		t.Fatalf("trip: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, tid, owner); err != nil {
		t.Fatalf("owner member: %v", err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`, tid, owner).Scan(&pid); err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Passenger on a plan (the trigger also makes them a trip viewer).
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2)`, pid, pax); err != nil {
		t.Fatalf("passenger: %v", err)
	}
	// A plain shared viewer (not a passenger).
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'viewer')`, tid, shared); err != nil {
		t.Fatalf("shared member: %v", err)
	}
	// The trip-list visibility gate now requires an accepted friendship with the
	// owner before either grantee can see the trip via ListTrips.
	e.befriend(t, owner, pax)
	e.befriend(t, owner, shared)

	flagFor := func(uid int64) (string, bool) {
		t.Helper()
		w := e.req(t, "GET", "/api/trips", nil, uid)
		trips := decodeBody[[]map[string]any](t, w)
		if len(trips) != 1 {
			t.Fatalf("user %d sees %d trips, want 1", uid, len(trips))
		}
		role, _ := trips[0]["my_role"].(string)
		pass, _ := trips[0]["viewer_is_passenger"].(bool)
		return role, pass
	}

	if role, pass := flagFor(pax); !pass || role != "viewer" {
		t.Errorf("passenger: role=%q viewer_is_passenger=%v, want viewer/true", role, pass)
	}
	if _, pass := flagFor(shared); pass {
		t.Error("shared-only viewer should not be flagged as a passenger")
	}
	if role, pass := flagFor(owner); pass || role != "owner" {
		t.Errorf("owner: role=%q viewer_is_passenger=%v, want owner/false", role, pass)
	}
}

func TestTripMemberEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	editor := e.user(t, "editor", false)
	viewer := e.user(t, "viewer", false)
	e.befriend(t, owner, editor)
	e.befriend(t, owner, viewer)

	w := e.req(t, "POST", "/api/trips", map[string]any{"name": "T"}, owner)
	tid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// Owner adds an editor; response is the trip with the new member.
	w = e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
		map[string]any{"user_id": editor, "role": "editor"}, owner)
	if w.Code != 200 {
		t.Fatalf("add editor = %d %s", w.Code, w.Body.String())
	}
	if len(decodeBody[map[string]any](t, w)["members"].([]any)) != 2 {
		t.Error("expected 2 members after add")
	}

	// Bad role rejected.
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
		map[string]any{"user_id": viewer, "role": "boss"}, owner); w.Code != 400 {
		t.Errorf("bad role = %d, want 400", w.Code)
	}

	// An editor is NOT allowed to manage members (owner-only).
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
		map[string]any{"user_id": viewer, "role": "viewer"}, editor); w.Code != 403 {
		t.Errorf("editor add member = %d, want 403", w.Code)
	}

	// Owner removes the editor.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d/members/%d", tid, editor), nil, owner); w.Code != 204 {
		t.Errorf("remove member = %d, want 204", w.Code)
	}

	// A non-friend cannot be added (would silently grant them trip access).
	stranger := e.user(t, "stranger", false)
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
		map[string]any{"user_id": stranger, "role": "viewer"}, owner); w.Code != 403 {
		t.Errorf("add non-friend member = %d, want 403", w.Code)
	}
}

func TestTagEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	w := e.req(t, "POST", "/api/trips", map[string]any{"name": "Beach trip"}, owner)
	tid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// PUT tags (FE sends { labels: [...] }).
	w = e.req(t, "PUT", fmt.Sprintf("/api/trips/%d/tags", tid),
		map[string]any{"labels": []string{"Beach", "Summer"}}, owner)
	if w.Code != 200 {
		t.Fatalf("set tags = %d %s", w.Code, w.Body.String())
	}
	tags := decodeBody[map[string]any](t, w)["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2", tags)
	}

	// Suggest autocompletes over visible tags.
	w = e.req(t, "GET", "/api/tags/suggest?q=bea", nil, owner)
	if w.Code != 200 {
		t.Fatalf("suggest = %d", w.Code)
	}
	sug := decodeBody[[]map[string]any](t, w)
	if len(sug) != 1 || sug[0]["label"] != "Beach" {
		t.Errorf("suggest = %v, want [Beach]", sug)
	}
}

func TestListTripsSuperuserIncludeScopes(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "admin", true) // superuser
	stranger := e.user(t, "stranger2", false)
	ctx := context.Background()

	if _, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Admin trip"}, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Stranger trip"}, stranger); err != nil {
		t.Fatal(err)
	}

	// Superuser ?include=all sees every trip (both).
	all := decodeBody[[]map[string]any](t, e.req(t, "GET", "/api/trips?include=all", nil, admin))
	if len(all) < 2 {
		t.Errorf("superuser include=all = %d trips, want >= 2", len(all))
	}

	// Non-superuser passing include=all is ignored — only their own trip.
	mine := decodeBody[[]map[string]any](t, e.req(t, "GET", "/api/trips?include=all", nil, stranger))
	if len(mine) != 1 {
		t.Errorf("non-superuser include=all = %d trips, want 1 (ignored)", len(mine))
	}
}

// TestTripPassengerEndpoints covers adding/removing a trip-level passenger via
// the API: any trip member may add an accepted friend (a non-owner can bring
// their partner), the passenger then sees the trip flagged, non-friends and
// non-members are refused, and removal un-shares them (#20).
func TestTripPassengerEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	owner := e.user(t, "owner", false)
	partner := e.user(t, "partner", false)
	stranger := e.user(t, "stranger", false)
	outsider := e.user(t, "outsider", false)
	e.befriend(t, owner, partner)

	var tid, pid int64
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('Holiday', $1) RETURNING id`, owner).Scan(&tid); err != nil {
		t.Fatalf("trip: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, tid, owner); err != nil {
		t.Fatalf("owner member: %v", err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`, tid, owner).Scan(&pid); err != nil {
		t.Fatalf("plan: %v", err)
	}

	add := func(actor, target int64) *httptest.ResponseRecorder {
		return e.req(t, "POST", fmt.Sprintf("/api/trips/%d/passengers", tid),
			map[string]any{"user_id": target}, actor)
	}

	// A non-member can't add passengers.
	if w := add(outsider, partner); w.Code != http.StatusForbidden {
		t.Errorf("non-member add = %d, want 403", w.Code)
	}
	// A non-friend target is refused.
	if w := add(owner, stranger); w.Code != http.StatusForbidden {
		t.Errorf("non-friend target = %d, want 403", w.Code)
	}
	// Owner adds their friend as a trip passenger.
	w := add(owner, partner)
	if w.Code != http.StatusOK {
		t.Fatalf("add passenger = %d %s", w.Code, w.Body.String())
	}
	dto := decodeBody[map[string]any](t, w)
	if ids, _ := dto["passenger_ids"].([]any); len(ids) != 1 || int64(ids[0].(float64)) != partner {
		t.Errorf("passenger_ids = %v, want [%d]", dto["passenger_ids"], partner)
	}

	// The partner now sees the trip, flagged as a passenger, and is a passenger
	// on the existing plan.
	trips := decodeBody[[]map[string]any](t, e.req(t, "GET", "/api/trips", nil, partner))
	if len(trips) != 1 || trips[0]["viewer_is_passenger"] != true {
		t.Fatalf("partner trips = %v", trips)
	}
	var onPlan bool
	if err := e.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM plan_passengers WHERE plan_id=$1 AND user_id=$2)`, pid, partner).Scan(&onPlan); err != nil {
		t.Fatalf("check plan passenger: %v", err)
	}
	if !onPlan {
		t.Error("trip passenger was not materialised onto the existing plan")
	}

	// A non-owner member may add their own friend (the couple scenario): make
	// the partner befriend a third user and add them.
	third := e.user(t, "third", false)
	e.befriend(t, partner, third)
	if w := add(partner, third); w.Code != http.StatusOK {
		t.Errorf("member (non-owner) add = %d %s, want 200", w.Code, w.Body.String())
	}

	// Removal: a passenger may remove themselves; afterwards they no longer see
	// the trip.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d/passengers/%d", tid, third), nil, third); w.Code != http.StatusNoContent {
		t.Errorf("self-remove = %d, want 204", w.Code)
	}
	// Owner removes the partner.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d/passengers/%d", tid, partner), nil, owner); w.Code != http.StatusNoContent {
		t.Errorf("owner remove = %d, want 204", w.Code)
	}
	if seen := decodeBody[[]map[string]any](t, e.req(t, "GET", "/api/trips", nil, partner)); len(seen) != 0 {
		t.Errorf("partner still sees %d trips after removal, want 0", len(seen))
	}
	// Removing a non-passenger is 404.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d/passengers/%d", tid, stranger), nil, owner); w.Code != http.StatusNotFound {
		t.Errorf("remove non-passenger = %d, want 404", w.Code)
	}
}
