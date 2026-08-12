package handlers

import (
	"net/http"
	"strings"
	"testing"
)

// TestCreateVehicleHirePlanAccepted covers the vehicle_hire plan type being
// accepted by createPlan (validPlanTypes) and its satellite persisted via
// toCreatePartPayload's vehicle_hire branch.
func TestCreateVehicleHirePlanAccepted(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2vhcreate", false)
	tid, pid, partID := g2makePlanOfType(t, e, owner, "vehicle_hire", map[string]any{
		"category":         "Compact",
		"vehicle":          "Test Motors Corsa",
		"transmission":     "manual",
		"fuel_policy":      "full-to-full",
		"mileage":          "unlimited",
		"excess_amount":    750.0,
		"excess_currency":  "GBP",
		"deposit_amount":   200.0,
		"deposit_currency": "GBP",
	})
	if tid == 0 || pid == 0 || partID == 0 {
		t.Fatalf("expected non-zero ids, got tid=%d pid=%d partID=%d", tid, pid, partID)
	}
}

// TestGetPlanReturnsVehicleHireDetail covers the detailFor switch's vehicle_hire
// branch and its projection into the DTO via ToPlanPartDTO's new argument.
func TestGetPlanReturnsVehicleHireDetail(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2vhget", false)
	tid, _, _ := g2makePlanOfType(t, e, owner, "vehicle_hire", map[string]any{
		"category": "Compact",
		"vehicle":  "Test Motors Corsa",
	})
	w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("get trip = %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"vehicle_hire"`) || !strings.Contains(body, `"category":"Compact"`) {
		t.Errorf("expected body to contain vehicle_hire detail with category Compact; body=%s", body)
	}
}

// TestPatchVehicleHirePartUpdatesDetail covers the per-type update block's
// vehicle_hire branch (UpdateVehicleHireDetail).
func TestPatchVehicleHirePartUpdatesDetail(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2vhpatch", false)
	_, _, partID := g2makePlanOfType(t, e, owner, "vehicle_hire", map[string]any{
		"category": "Compact",
	})
	w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{
		"vehicle_hire": map[string]any{"category": "Estate"},
	}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("vehicle_hire edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := decodeBody[map[string]any](t, w)
	vh, ok := body["vehicle_hire"].(map[string]any)
	if !ok {
		t.Fatalf("expected vehicle_hire object in response, got %v", body)
	}
	if vh["category"] != "Estate" {
		t.Errorf("category = %v, want Estate", vh["category"])
	}
}

// TestPatchVehicleHireIgnoredOnWrongPartType guards the part.Type == "vehicle_hire"
// check: a vehicle_hire edit posted against a hotel part must be silently
// ignored, exactly as every sibling per-type edit is.
func TestPatchVehicleHireIgnoredOnWrongPartType(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2vhwrong", false)
	_, _, hPart := g2makePlanOfType(t, e, owner, "hotel", map[string]any{
		"property_name": "Test Hotel",
	})
	w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(hPart), map[string]any{
		"vehicle_hire": map[string]any{"category": "Estate"},
	}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("hotel edit with vehicle_hire payload = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := decodeBody[map[string]any](t, w)
	if _, ok := body["vehicle_hire"]; ok {
		t.Errorf("expected no vehicle_hire detail on a hotel part, got %v", body)
	}
	if hotel, ok := body["hotel"].(map[string]any); !ok || hotel["property_name"] != "Test Hotel" {
		t.Errorf("expected hotel detail untouched, got %v", body)
	}
}
