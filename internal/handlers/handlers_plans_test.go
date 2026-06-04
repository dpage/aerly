package handlers

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// newTrip creates a trip owned by uid and returns its id.
func newTrip(t *testing.T, e *testEnv, uid int64, name string) int64 {
	t.Helper()
	w := e.req(t, "POST", "/api/trips", map[string]any{"name": name}, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create trip: %d %s", w.Code, w.Body.String())
	}
	return int64(decodeBody[map[string]any](t, w)["id"].(float64))
}

func TestPlanCRUDEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	stranger := e.user(t, "stranger", false)
	tid := newTrip(t, e, owner, "Trip")
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	body := map[string]any{
		"type": "flight", "title": "BA286", "confirmation_ref": "X1",
		"parts": []map[string]any{{
			"type": "flight", "starts_at": out, "ends_at": in,
			"start_label": "LHR", "end_label": "SFO",
			"flight": map[string]any{
				"ident": "BA286", "scheduled_out": out, "scheduled_in": in,
				"origin_iata": "LHR", "dest_iata": "SFO",
			},
		}},
	}
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), body, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", w.Code, w.Body.String())
	}
	plan := decodeBody[map[string]any](t, w)
	pid := int64(plan["id"].(float64))
	if plan["type"] != "flight" {
		t.Errorf("type = %v", plan["type"])
	}
	parts := plan["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	part0 := parts[0].(map[string]any)
	if part0["flight"] == nil {
		t.Error("flight detail missing on part")
	}
	if part0["start_tz"] != "Europe/London" {
		t.Errorf("start_tz = %v, want Europe/London (TZ lookup)", part0["start_tz"])
	}

	// Stranger can't create in the trip.
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), body, stranger); w.Code != 403 {
		t.Errorf("stranger create plan = %d, want 403", w.Code)
	}

	// Invalid type.
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid),
		map[string]any{"type": "spaceship", "parts": []any{}}, owner); w.Code != 400 {
		t.Errorf("bad type = %d, want 400", w.Code)
	}

	// Edit plan.
	w = e.req(t, "PATCH", fmt.Sprintf("/api/plans/%d", pid), map[string]any{"notes": "window seat"}, owner)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["notes"] != "window seat" {
		t.Errorf("patch plan = %d %s", w.Code, w.Body.String())
	}

	// Edit the part.
	partID := int64(part0["id"].(float64))
	w = e.req(t, "PATCH", fmt.Sprintf("/api/plan-parts/%d", partID), map[string]any{"status": "confirmed"}, owner)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["status"] != "confirmed" {
		t.Errorf("patch part = %d %s", w.Code, w.Body.String())
	}

	// Dismiss the part.
	if w := e.req(t, "POST", fmt.Sprintf("/api/plan-parts/%d/dismiss", partID), nil, owner); w.Code != 204 {
		t.Errorf("dismiss = %d, want 204", w.Code)
	}

	// Delete the plan.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/plans/%d", pid), nil, owner); w.Code != 204 {
		t.Errorf("delete plan = %d, want 204", w.Code)
	}
}

func TestPlanPassengerEndpointMakesViewer(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	pax := e.user(t, "pax", false)
	e.befriend(t, owner, pax)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), map[string]any{
		"type": "dining", "title": "Dinner",
		"parts": []map[string]any{{"type": "dining", "starts_at": time.Now()}},
	}, owner)
	pid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// Add passenger; response is the plan with passenger_ids.
	w = e.req(t, "POST", fmt.Sprintf("/api/plans/%d/passengers", pid), map[string]any{"user_id": pax}, owner)
	if w.Code != 200 {
		t.Fatalf("add passenger = %d %s", w.Code, w.Body.String())
	}
	if len(decodeBody[map[string]any](t, w)["passenger_ids"].([]any)) != 1 {
		t.Error("expected 1 passenger_id")
	}

	// The trigger made pax a trip viewer, so they can now GET the trip.
	if w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, pax); w.Code != 200 {
		t.Errorf("passenger should now see the trip (viewer via trigger): %d", w.Code)
	}

	// Remove passenger.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/plans/%d/passengers/%d", pid, pax), nil, owner); w.Code != 204 {
		t.Errorf("remove passenger = %d, want 204", w.Code)
	}
}

// TestPlanVisibilityFilteringEndpoint exercises the §4 predicate through the
// trip-detail read: hidden_from / only_visible_to / passenger-always-sees /
// owner-always-sees.
func TestPlanVisibilityFilteringEndpoint(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	memberA := e.user(t, "memberA", false)
	memberB := e.user(t, "memberB", false)
	e.befriend(t, owner, memberA)
	e.befriend(t, owner, memberB)
	tid := newTrip(t, e, owner, "Trip")

	// Both members are viewers on the trip.
	for _, m := range []int64{memberA, memberB} {
		if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
			map[string]any{"user_id": m, "role": "viewer"}, owner); w.Code != 200 {
			t.Fatalf("add member: %d", w.Code)
		}
	}

	mkPlan := func(title string) int64 {
		w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), map[string]any{
			"type": "dining", "title": title,
			"parts": []map[string]any{{"type": "dining", "starts_at": time.Now()}},
		}, owner)
		if w.Code != http.StatusCreated {
			t.Fatalf("create plan %s: %d %s", title, w.Code, w.Body.String())
		}
		return int64(decodeBody[map[string]any](t, w)["id"].(float64))
	}

	hidden := mkPlan("hidden-from-A")
	only := mkPlan("only-B")
	def := mkPlan("default")

	// hidden_from memberA.
	if w := e.req(t, "PUT", fmt.Sprintf("/api/plans/%d/visibility", hidden),
		map[string]any{"mode": "hidden_from", "user_ids": []int64{memberA}}, owner); w.Code != 200 {
		t.Fatalf("set hidden_from: %d %s", w.Code, w.Body.String())
	}
	// only_visible_to memberB.
	if w := e.req(t, "PUT", fmt.Sprintf("/api/plans/%d/visibility", only),
		map[string]any{"mode": "only_visible_to", "user_ids": []int64{memberB}}, owner); w.Code != 200 {
		t.Fatalf("set only_visible_to: %d", w.Code)
	}

	titlesFor := func(uid int64) map[string]bool {
		w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, uid)
		if w.Code != 200 {
			t.Fatalf("get trip as %d: %d", uid, w.Code)
		}
		out := map[string]bool{}
		for _, p := range decodeBody[map[string]any](t, w)["plans"].([]any) {
			out[p.(map[string]any)["title"].(string)] = true
		}
		return out
	}

	// Owner sees all three (owner-always-sees).
	if o := titlesFor(owner); !o["hidden-from-A"] || !o["only-B"] || !o["default"] {
		t.Errorf("owner should see all plans, got %v", o)
	}
	// memberA: hidden plan invisible; only-B invisible (not named); default visible.
	a := titlesFor(memberA)
	if a["hidden-from-A"] {
		t.Error("memberA must not see the hidden_from plan")
	}
	if a["only-B"] {
		t.Error("memberA must not see the only_visible_to(B) plan")
	}
	if !a["default"] {
		t.Error("memberA should see the default plan")
	}
	// memberB: sees hidden (not named), only-B (named), default.
	b := titlesFor(memberB)
	if !b["hidden-from-A"] || !b["only-B"] || !b["default"] {
		t.Errorf("memberB should see all three, got %v", b)
	}

	_ = def

	// passenger-always-sees: add memberA as a passenger on the only-B plan,
	// which doesn't name them; they should still see it.
	if w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/passengers", only),
		map[string]any{"user_id": memberA}, owner); w.Code != 200 {
		t.Fatalf("add passenger: %d", w.Code)
	}
	if a := titlesFor(memberA); !a["only-B"] {
		t.Error("a passenger must always see their plan, even under only_visible_to that omits them")
	}
}

// TestMovePlanEndpointRecomputesVisibility moves a plan to a trip the named
// viewer isn't on; the plan then drops out of their trip view of neither trip.
func TestMovePlanEndpointRecomputesVisibility(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	named := e.user(t, "named", false)
	e.befriend(t, owner, named)
	src := newTrip(t, e, owner, "Source")
	dst := newTrip(t, e, owner, "Dest")

	// named is a viewer on the source trip only.
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", src),
		map[string]any{"user_id": named, "role": "viewer"}, owner); w.Code != 200 {
		t.Fatalf("add member: %d", w.Code)
	}

	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", src), map[string]any{
		"type": "dining", "title": "Secret dinner",
		"parts": []map[string]any{{"type": "dining", "starts_at": time.Now()}},
	}, owner)
	pid := int64(decodeBody[map[string]any](t, w)["id"].(float64))
	if w := e.req(t, "PUT", fmt.Sprintf("/api/plans/%d/visibility", pid),
		map[string]any{"mode": "only_visible_to", "user_ids": []int64{named}}, owner); w.Code != 200 {
		t.Fatalf("set visibility: %d", w.Code)
	}

	// Before the move, named sees it in the source trip.
	seesIn := func(trip int64) bool {
		w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", trip), nil, named)
		if w.Code != 200 {
			return false
		}
		for _, p := range decodeBody[map[string]any](t, w)["plans"].([]any) {
			if p.(map[string]any)["title"] == "Secret dinner" {
				return true
			}
		}
		return false
	}
	if !seesIn(src) {
		t.Fatal("named should see the plan in the source trip before the move")
	}

	// Move to the destination trip (owner is editor on both).
	if w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/move", pid),
		map[string]any{"trip_id": dst}, owner); w.Code != 200 {
		t.Fatalf("move = %d %s", w.Code, w.Body.String())
	}

	// named is not on the destination trip, so they can't even see that trip.
	if w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", dst), nil, named); w.Code != 404 {
		t.Errorf("named GET dest trip = %d, want 404 (not a member)", w.Code)
	}
	// And the plan no longer shows under the source trip (it moved away).
	if seesIn(src) {
		t.Error("plan should no longer appear under the source trip after the move")
	}
}

func TestMovePlanRequiresEditorOnDestination(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	other := e.user(t, "other", false)
	src := newTrip(t, e, owner, "Source")
	dst := newTrip(t, e, other, "OthersDest") // owner is NOT on dst

	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", src), map[string]any{
		"type": "dining", "title": "D",
		"parts": []map[string]any{{"type": "dining", "starts_at": time.Now()}},
	}, owner)
	pid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// owner has editor on src but not dst → move forbidden.
	if w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/move", pid),
		map[string]any{"trip_id": dst}, owner); w.Code != 403 {
		t.Errorf("move to a trip owner can't edit = %d, want 403", w.Code)
	}
}

// newFlightPlan creates a single-leg flight plan in tid and returns its id.
func newFlightPlan(t *testing.T, e *testEnv, tid, uid int64, ident string, out time.Time) int64 {
	t.Helper()
	in := out.Add(2 * time.Hour)
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), map[string]any{
		"type": "flight", "title": ident, "confirmation_ref": "PNR",
		"parts": []map[string]any{{
			"type": "flight", "starts_at": out, "ends_at": in,
			"start_label": "LHR", "end_label": "HEL",
			"flight": map[string]any{
				"ident": ident, "scheduled_out": out, "scheduled_in": in,
				"origin_iata": "LHR", "dest_iata": "HEL",
			},
		}},
	}, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create flight plan %s: %d %s", ident, w.Code, w.Body.String())
	}
	return int64(decodeBody[map[string]any](t, w)["id"].(float64))
}

func TestLinkPlansEndpoint(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	stranger := e.user(t, "stranger", false)
	tid := newTrip(t, e, owner, "Trip")
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	primary := newFlightPlan(t, e, tid, owner, "AY1", out)
	absorbed := newFlightPlan(t, e, tid, owner, "AY2", out.Add(3*time.Hour))

	// A non-editor can't link.
	if w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/link", primary),
		map[string]any{"plan_ids": []int64{absorbed}}, stranger); w.Code != 403 {
		t.Fatalf("stranger link = %d, want 403", w.Code)
	}

	w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/link", primary),
		map[string]any{"plan_ids": []int64{absorbed}}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("link = %d %s", w.Code, w.Body.String())
	}
	dto := decodeBody[map[string]any](t, w)
	if parts := dto["parts"].([]any); len(parts) != 2 {
		t.Fatalf("primary should have 2 parts after link, got %d", len(parts))
	}
	// The absorbed plan is gone.
	if w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, owner); w.Code == 200 {
		for _, p := range decodeBody[map[string]any](t, w)["plans"].([]any) {
			if int64(p.(map[string]any)["id"].(float64)) == absorbed {
				t.Fatal("absorbed plan should no longer exist")
			}
		}
	}

	// Empty plan_ids is a 400.
	if w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/link", primary),
		map[string]any{"plan_ids": []int64{}}, owner); w.Code != 400 {
		t.Errorf("empty link = %d, want 400", w.Code)
	}
}

func TestSplitPlanPartEndpoint(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	// Build a 2-leg booking by linking two flights.
	primary := newFlightPlan(t, e, tid, owner, "AY1", out)
	absorbed := newFlightPlan(t, e, tid, owner, "AY2", out.Add(3*time.Hour))
	if w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/link", primary),
		map[string]any{"plan_ids": []int64{absorbed}}, owner); w.Code != 200 {
		t.Fatalf("link setup = %d %s", w.Code, w.Body.String())
	}
	parts := planParts(t, e, owner, tid, primary)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts before split, got %d", len(parts))
	}
	leg2 := int64(parts[1]["id"].(float64))

	sw := e.req(t, "POST", fmt.Sprintf("/api/plan-parts/%d/split", leg2), nil, owner)
	if sw.Code != http.StatusCreated {
		t.Fatalf("split = %d %s", sw.Code, sw.Body.String())
	}
	newPlan := decodeBody[map[string]any](t, sw)
	if np := newPlan["parts"].([]any); len(np) != 1 {
		t.Fatalf("split plan should have 1 part, got %d", len(np))
	}
	// The primary is back to a single leg.
	if got := planParts(t, e, owner, tid, primary); len(got) != 1 {
		t.Fatalf("primary should have 1 part after split, got %d", len(got))
	}

	// Splitting the now-single-part primary leg is a 400.
	rem := planParts(t, e, owner, tid, primary)
	if w := e.req(t, "POST", fmt.Sprintf("/api/plan-parts/%d/split", int64(rem[0]["id"].(float64))), nil, owner); w.Code != 400 {
		t.Errorf("split single-part = %d, want 400", w.Code)
	}
}

// planParts reads a plan's parts via the trip detail endpoint.
func planParts(t *testing.T, e *testEnv, uid, tid, planID int64) []map[string]any {
	t.Helper()
	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, uid)
	if w.Code != 200 {
		t.Fatalf("get trip = %d", w.Code)
	}
	for _, p := range decodeBody[map[string]any](t, w)["plans"].([]any) {
		pm := p.(map[string]any)
		if int64(pm["id"].(float64)) == planID {
			out := []map[string]any{}
			for _, pt := range pm["parts"].([]any) {
				out = append(out, pt.(map[string]any))
			}
			return out
		}
	}
	t.Fatalf("plan %d not found in trip %d", planID, tid)
	return nil
}
