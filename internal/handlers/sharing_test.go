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
