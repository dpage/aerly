package handlers

import (
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
