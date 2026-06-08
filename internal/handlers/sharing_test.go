package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSetTripShareAllFriends(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "safowner", false)
	tripID := newTrip(t, e, owner, "Share trip")

	w := e.req(t, "PUT", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/share-all-friends",
		map[string]any{"role": "viewer"}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"share_all_friends_role":"viewer"`) {
		t.Errorf("response DTO missing flag: %s", w.Body.String())
	}

	// Clearing with empty role.
	w = e.req(t, "PUT", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/share-all-friends",
		map[string]any{"role": ""}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("clear code = %d; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"share_all_friends_role"`) {
		t.Errorf("cleared role should be omitted: %s", w.Body.String())
	}

	// Invalid role rejected.
	w = e.req(t, "PUT", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/share-all-friends",
		map[string]any{"role": "bogus"}, owner)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid role code = %d, want 400", w.Code)
	}

	// Non-owner forbidden.
	other := e.user(t, "safother", false)
	w = e.req(t, "PUT", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/share-all-friends",
		map[string]any{"role": "viewer"}, other)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-owner code = %d, want 403", w.Code)
	}
}

func TestSetPlanShareAllFriends(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "psafowner", false)
	tripID := newTrip(t, e, owner, "Plan share trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	w := e.req(t, "PUT", "/api/plans/"+strconv.FormatInt(planID, 10)+"/share-all-friends",
		map[string]any{"enabled": true}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"share_all_friends":true`) {
		t.Errorf("response DTO missing flag: %s", w.Body.String())
	}

	// Turning it back off.
	w = e.req(t, "PUT", "/api/plans/"+strconv.FormatInt(planID, 10)+"/share-all-friends",
		map[string]any{"enabled": false}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("disable code = %d; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"share_all_friends":false`) {
		t.Errorf("flag should be false: %s", w.Body.String())
	}

	// Non-editor forbidden.
	other := e.user(t, "psafother", false)
	w = e.req(t, "PUT", "/api/plans/"+strconv.FormatInt(planID, 10)+"/share-all-friends",
		map[string]any{"enabled": true}, other)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-editor code = %d, want 403", w.Code)
	}
}

func TestShareTripByEmailNewAddress(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "stbeowner", false)
	tripID := newTrip(t, e, owner, "Share-by-email trip")
	ctx := context.Background()

	const email = "newbie@example.com"
	w := e.req(t, "POST", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/share-by-email",
		map[string]any{"email": email, "role": "viewer"}, owner)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}

	var n int
	if err := e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_shares WHERE email_lower=$1 AND kind='trip' AND target_id=$2`,
		email, tripID).Scan(&n); err != nil {
		t.Fatalf("count pending_shares: %v", err)
	}
	if n != 1 {
		t.Errorf("pending_shares rows = %d, want 1", n)
	}
}

func TestShareTripByEmailExistingUser(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "stbeexowner", false)
	bob := e.user(t, "stbeexbob", false)
	const bobEmail = "bob@example.com"
	seedVerifiedEmail(t, e, bob, bobEmail)
	tripID := newTrip(t, e, owner, "Existing-user share trip")
	ctx := context.Background()

	w := e.req(t, "POST", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/share-by-email",
		map[string]any{"email": bobEmail, "role": "editor"}, owner)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}

	// (a) a pending friendship owner<->bob now exists.
	fs, err := e.store.ListFriendships(ctx, bob)
	if err != nil {
		t.Fatalf("ListFriendships: %v", err)
	}
	foundPending := false
	for _, f := range fs {
		if f.Status == "pending" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Errorf("expected a pending friendship for bob, got %+v", fs)
	}

	// (b) a trip_members row for bob with role editor exists.
	var role string
	if err := e.pool.QueryRow(ctx,
		`SELECT role FROM trip_members WHERE trip_id=$1 AND user_id=$2`,
		tripID, bob).Scan(&role); err != nil {
		t.Fatalf("query trip_members: %v", err)
	}
	if role != "editor" {
		t.Errorf("trip_members role = %q, want editor", role)
	}

	// (c) dormant until accepted: CanViewTrip is false.
	can, err := e.store.CanViewTrip(ctx, tripID, bob)
	if err != nil {
		t.Fatalf("CanViewTrip: %v", err)
	}
	if can {
		t.Errorf("CanViewTrip = true before acceptance, want false (dormant)")
	}

	// Accept the friendship (bob accepts owner's request) -> lights up.
	if _, err := e.store.AcceptFriendship(ctx, bob, owner); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	can, err = e.store.CanViewTrip(ctx, tripID, bob)
	if err != nil {
		t.Fatalf("CanViewTrip after accept: %v", err)
	}
	if !can {
		t.Errorf("CanViewTrip = false after acceptance, want true")
	}
}

func TestSharePlanByEmailNewAddress(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "spbeowner", false)
	tripID := newTrip(t, e, owner, "Plan share-by-email trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()

	const email = "planmate@example.com"
	w := e.req(t, "POST", "/api/plans/"+strconv.FormatInt(planID, 10)+"/share-by-email",
		map[string]any{"email": email}, owner)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}

	var n int
	if err := e.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_shares WHERE email_lower=$1 AND kind='plan' AND target_id=$2`,
		email, planID).Scan(&n); err != nil {
		t.Fatalf("count pending_shares: %v", err)
	}
	if n != 1 {
		t.Errorf("pending_shares rows = %d, want 1", n)
	}
}

func TestShareByEmailValidation(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "sbevowner", false)
	tripID := newTrip(t, e, owner, "Validation trip")
	tpath := "/api/trips/" + strconv.FormatInt(tripID, 10) + "/share-by-email"

	// Invalid email -> 400.
	w := e.req(t, "POST", tpath, map[string]any{"email": "not-an-email", "role": "viewer"}, owner)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid email code = %d, want 400; body=%s", w.Code, w.Body.String())
	}

	// Bad role -> 400.
	w = e.req(t, "POST", tpath, map[string]any{"email": "ok@example.com", "role": "bogus"}, owner)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad role code = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestNotifyTripShares(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "ntowner", false)
	bob := e.user(t, "ntbob", false)
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "Trip")
	// bob is a member
	e.req(t, "POST", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner)

	w := e.req(t, "POST", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{}}, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	n, err := e.store.CountUnreadNotifications(context.Background(), bob)
	if err != nil {
		t.Fatalf("CountUnreadNotifications: %v", err)
	}
	if n != 1 {
		t.Errorf("bob unread notifications = %d, want 1", n)
	}
}

func TestNotifyPlanShares(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "npowner", false)
	bob := e.user(t, "npbob", false)
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "Trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	e.req(t, "POST", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner)

	w := e.req(t, "POST", "/api/plans/"+strconv.FormatInt(planID, 10)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{}}, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	n, err := e.store.CountUnreadNotifications(context.Background(), bob)
	if err != nil {
		t.Fatalf("CountUnreadNotifications: %v", err)
	}
	if n != 1 {
		t.Errorf("bob unread notifications = %d, want 1", n)
	}
}
