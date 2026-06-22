package handlers

import (
	"net/http"
	"testing"
)

// g2tripWithFlight creates a trip owned by a superuser with one flight plan and
// returns the env, admin id, trip id, plan id. Used to drive planDTO/partDTO
// error branches via GET /api/trips/{id} (the superuser bypasses canViewTrip and
// CanViewPlan stays clean as long as plan_visibility survives).
func g2tripWithFlight(t *testing.T, name string) (*testEnv, int64, int64, int64) {
	t.Helper()
	e := setup(t, nil, nil)
	admin := e.user(t, name, true)
	tid := newTrip(t, e, admin, "Trip")
	pid := newFlightPlan(t, e, tid, admin, "BA286", g2planOut)
	return e, admin, tid, pid
}

// getTripExpect500 GETs the trip as admin and asserts a 500 (a planDTO/partDTO
// sub-query failed). Used after dropping a table that only the DTO assembly
// reads.
func g2getTripExpect500(t *testing.T, e *testEnv, tid, admin int64, what string) {
	t.Helper()
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("%s = %d, want 500; body=%s", what, w.Code, w.Body.String())
	}
}

func TestPlanDTOPassengersErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2dtopax")
	g1dropTable(t, e, "plan_passengers")
	g2getTripExpect500(t, e, tid, admin, "PassengersByPlan err")
}

func TestPlanDTOPartsErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2dtoparts")
	// Drop a plan_parts column PartsByPlan selects but CanViewPlan does not need.
	g1dropColumn(t, e, "plan_parts", "starts_at")
	g2getTripExpect500(t, e, tid, admin, "PartsByPlan err")
}

func TestPlanDTOPositionsErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2dtopos")
	// LatestPartPositions / PartTracks read positions; dropping it errors them
	// (the plan has a flight part, so flightIDs is non-empty).
	g1dropTable(t, e, "positions")
	g2getTripExpect500(t, e, tid, admin, "LatestPartPositions err")
}

func TestPlanDTOAlertOptedInErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2dtoalert")
	g1dropTable(t, e, "plan_alert_optin")
	g2getTripExpect500(t, e, tid, admin, "PlanAlertOptedIn err")
}

func TestPlanDTOReminderErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2dtorem")
	g1dropTable(t, e, "plan_reminder_optin")
	g2getTripExpect500(t, e, tid, admin, "PlanReminderFor err")
}

func TestPlanDTOTripOwnersErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2dtoowners")
	// TripOwnersByPlan joins plans→trips and reads trips.created_by; dropping that
	// column errors the lookup while planDTO's earlier queries (which don't read
	// it) still work. Drive planDTO directly.
	pid := newFlightPlan(t, e, tid, admin, "AY9", g2planOut)
	g1dropColumn(t, e, "trips", "created_by")
	if _, err := e.api.planDTO(t.Context(), pid, admin); err == nil {
		t.Error("planDTO should error when TripOwnersByPlan fails")
	}
}

func TestPlanDTOVisibilityErrG2(t *testing.T) {
	e, admin, _, pid := g2tripWithFlight(t, "g2dtovis")
	// PlanVisibilityFor reads plan_visibility; a dropped table is a real error
	// (not the tolerated NotFound). Drive planDTO directly so CanViewPlan in
	// visiblePlanDTOs doesn't pre-empt it.
	g1dropTable(t, e, "plan_visibility")
	if _, err := e.api.planDTO(t.Context(), pid, admin); err == nil {
		t.Error("planDTO should error when PlanVisibilityFor fails")
	}
}

func TestPlanDTOPlanByIDErrG2(t *testing.T) {
	e, admin, _, pid := g2tripWithFlight(t, "g2dtobyid")
	g1dropColumn(t, e, "plans", "share_all_friends")
	if _, err := e.api.planDTO(t.Context(), pid, admin); err == nil {
		t.Error("planDTO should error when PlanByID fails")
	}
}

// TestPartDTOSatelliteErrsG2 covers partDTOWithPositions' satellite-load error
// branches for each part type by dropping the detail table and reading the part
// via partDTO directly.
func TestPartDTOSatelliteErrsG2(t *testing.T) {
	cases := []struct {
		planType, table string
	}{
		{"flight", "flight_details"},
		{"hotel", "hotel_details"},
		{"train", "train_details"},
		{"ground", "ground_details"},
		{"dining", "dining_details"},
		{"excursion", "excursion_details"},
		{"ice_cream", "ice_cream_details"},
		{"meeting", "meeting_details"},
		{"event", "event_details"},
	}
	for _, c := range cases {
		t.Run(c.planType, func(t *testing.T) {
			e := setup(t, nil, nil)
			admin := e.user(t, "g2sat"+c.planType, true)
			_, _, partID := g2makePlanOfType(t, e, admin, c.planType, nil)
			part, err := e.store.PlanPartByID(t.Context(), partID)
			if err != nil {
				t.Fatalf("load part: %v", err)
			}
			g1dropTable(t, e, c.table)
			if _, err := e.api.partDTO(t.Context(), part); err == nil {
				t.Errorf("partDTO(%s) should error when %s is gone", c.planType, c.table)
			}
		})
	}
}

// TestPartDTOPlanByIDErrG2 covers partDTO's PlanByID error branch.
func TestPartDTOPlanByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2partbyid", true)
	_, _, partID := g2makePlanOfType(t, e, admin, "dining", nil)
	part, err := e.store.PlanPartByID(t.Context(), partID)
	if err != nil {
		t.Fatalf("load part: %v", err)
	}
	g1dropColumn(t, e, "plans", "share_all_friends")
	if _, err := e.api.partDTO(t.Context(), part); err == nil {
		t.Error("partDTO should error when PlanByID fails")
	}
}

// TestTripFlightPartsErrG2 covers tripFlightParts' PlansByTrip and PartsByPlan
// error branches via planDTO of a hotel plan (which calls tripFlightParts).
func TestTripFlightPartsErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2tfp", true)
	tid, pid, _ := g2makePlanOfType(t, e, admin, "hotel", map[string]any{"property_name": "H"})
	// Also add a flight plan so tripFlightParts iterates a flight (PartsByPlan).
	newFlightPlan(t, e, tid, admin, "AY1", g2planOut)
	// Drop a plan_parts column PartsByPlan reads → tripFlightParts errors while
	// iterating the flight plan.
	g1dropColumn(t, e, "plan_parts", "ends_at")
	if _, err := e.api.planDTO(t.Context(), pid, admin); err == nil {
		t.Error("planDTO of a hotel should error when tripFlightParts fails")
	}
}

// TestVisiblePlanDTOsCanViewErrG2 covers visiblePlanDTOs' CanViewPlan error
// branch via getTrip with plan_visibility dropped (a non-superuser viewer so
// canViewTrip... actually superuser bypasses canViewTrip but visiblePlanDTOs
// still calls CanViewPlan, which errors).
func TestVisiblePlanDTOsCanViewErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2vpdcv")
	g1dropTable(t, e, "plan_visibility")
	g2getTripExpect500(t, e, tid, admin, "visiblePlanDTOs CanViewPlan err")
}

// TestVisiblePlanDTOsPlansByTripErrG2 covers visiblePlanDTOs' PlansByTrip error
// branch. tripDTO builds first (untouched), then PlansByTrip errors.
func TestVisiblePlanDTOsPlansByTripErrG2(t *testing.T) {
	e, admin, tid, _ := g2tripWithFlight(t, "g2vpdpbt")
	// PlansByTrip reads plans; drop a column it selects that tripDTO does not.
	g1dropColumn(t, e, "plans", "type")
	g2getTripExpect500(t, e, tid, admin, "visiblePlanDTOs PlansByTrip err")
}
