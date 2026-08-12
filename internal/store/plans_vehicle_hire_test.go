package store

import (
	"testing"
	"time"
)

// seedVehicleHirePart creates a vehicle_hire plan with one part and returns
// the part id. Used by tests that need an existing satellite row to update.
func seedVehicleHirePart(t *testing.T, s *Store, tripID int64, owner int64) int64 {
	t.Helper()
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: tripID, Type: "vehicle_hire", Title: "Test Rentals Ltd",
		Parts: []CreatePlanPartPayload{{
			StartsAt: time.Date(2026, 9, 9, 15, 30, 0, 0, time.UTC),
			EndsAt:   g1ptr(time.Date(2026, 9, 11, 14, 0, 0, 0, time.UTC)),
			VehicleHire: &VehicleHireDetail{
				Category: "Standard SUV", Vehicle: "Test Hatchback or similar",
				Transmission: "Automatic", FuelPolicy: "Same to same",
				Mileage: "Unlimited",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("seedVehicleHirePart: CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("seedVehicleHirePart: PartsByPlan = %d, %v", len(parts), err)
	}
	return parts[0].ID
}

func TestCreatePlanWritesVehicleHireDetail(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	excess := 1400.0
	deposit := 300.0
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "vehicle_hire", Title: "Test Rentals Ltd",
		Parts: []CreatePlanPartPayload{{
			StartsAt: time.Date(2026, 9, 9, 15, 30, 0, 0, time.UTC),
			EndsAt:   g1ptr(time.Date(2026, 9, 11, 14, 0, 0, 0, time.UTC)),
			VehicleHire: &VehicleHireDetail{
				Category: "Standard SUV", Vehicle: "Test Hatchback or similar",
				Transmission: "Automatic", FuelPolicy: "Same to same",
				Mileage: "Unlimited", ExcessAmount: &excess, ExcessCurrency: "EUR",
				DepositAmount: &deposit, DepositCurrency: "EUR",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("PartsByPlan: %v", err)
	}
	d, err := s.VehicleHireDetailFor(ctx, parts[0].ID)
	if err != nil {
		t.Fatalf("VehicleHireDetailFor: %v", err)
	}
	if d == nil || d.Category != "Standard SUV" || d.Transmission != "Automatic" {
		t.Fatalf("detail = %+v, want the seeded hire", d)
	}
	if d.ExcessAmount == nil || *d.ExcessAmount != 1400 || d.ExcessCurrency != "EUR" {
		t.Fatalf("excess = %v %q, want 1400 EUR", d.ExcessAmount, d.ExcessCurrency)
	}
	if d.DepositAmount == nil || *d.DepositAmount != 300 || d.DepositCurrency != "EUR" {
		t.Fatalf("deposit = %v %q, want 300 EUR", d.DepositAmount, d.DepositCurrency)
	}
	// No other satellite leaked.
	if hd, _ := s.HotelDetailFor(ctx, parts[0].ID); hd != nil {
		t.Error("vehicle_hire part should not have a hotel satellite")
	}
}

func TestCreatePlanWritesVehicleHireDetailNilDefaults(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "vehicle_hire", Title: "Test Rentals Ltd",
		Parts: []CreatePlanPartPayload{{
			StartsAt: time.Date(2026, 9, 9, 15, 30, 0, 0, time.UTC),
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("PartsByPlan: %v", err)
	}
	d, err := s.VehicleHireDetailFor(ctx, parts[0].ID)
	if err != nil || d == nil {
		t.Fatalf("VehicleHireDetailFor: %v, %v", d, err)
	}
	if d.Category != "" || d.ExcessAmount != nil || d.DepositAmount != nil {
		t.Fatalf("nil detail should default to zero values, got %+v", d)
	}
}

func TestVehicleHireDetailForMissingReturnsNilNil(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	d, err := s.VehicleHireDetailFor(ctx, 999999)
	if err != nil || d != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", d, err)
	}
}

func TestUpdateVehicleHireDetailUpsertsAndLeavesNilFields(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	partID := seedVehicleHirePart(t, s, trip, owner)

	cat := "Compact"
	if err := s.UpdateVehicleHireDetail(ctx, partID,
		VehicleHireDetailUpdate{Category: &cat}); err != nil {
		t.Fatalf("update: %v", err)
	}
	d, err := s.VehicleHireDetailFor(ctx, partID)
	if err != nil || d == nil {
		t.Fatalf("VehicleHireDetailFor: %v, %v", d, err)
	}
	if d.Category != "Compact" {
		t.Fatalf("category = %q, want Compact", d.Category)
	}
	if d.Transmission != "Automatic" {
		t.Fatalf("transmission = %q, want the original Automatic (nil field must not clear)", d.Transmission)
	}
}

func TestUpdateVehicleHireDetailUpsertsWithoutExistingRow(t *testing.T) {
	// UpdateVehicleHireDetail must upsert: a part predating its detail row
	// (or one created with a nil VehicleHire) still takes the edit.
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "vehicle_hire", Title: "Test Rentals Ltd",
		Parts: []CreatePlanPartPayload{{StartsAt: time.Now()}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("PartsByPlan: %v", err)
	}
	partID := parts[0].ID

	excess := 250.5
	if err := s.UpdateVehicleHireDetail(ctx, partID,
		VehicleHireDetailUpdate{ExcessAmount: &excess, ExcessCurrency: g1ptr("GBP")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	d, err := s.VehicleHireDetailFor(ctx, partID)
	if err != nil || d == nil {
		t.Fatalf("VehicleHireDetailFor: %v, %v", d, err)
	}
	if d.ExcessAmount == nil || *d.ExcessAmount != 250.5 || d.ExcessCurrency != "GBP" {
		t.Fatalf("excess = %v %q, want 250.5 GBP", d.ExcessAmount, d.ExcessCurrency)
	}

	// A second update that leaves ExcessAmount nil must not clear it back to
	// NULL: numeric fields can be set/corrected but not cleared.
	if err := s.UpdateVehicleHireDetail(ctx, partID,
		VehicleHireDetailUpdate{Mileage: g1ptr("100 miles/day")}); err != nil {
		t.Fatalf("second update: %v", err)
	}
	d2, err := s.VehicleHireDetailFor(ctx, partID)
	if err != nil || d2 == nil {
		t.Fatalf("VehicleHireDetailFor: %v, %v", d2, err)
	}
	if d2.ExcessAmount == nil || *d2.ExcessAmount != 250.5 {
		t.Fatalf("excess after unrelated update = %v, want unchanged 250.5", d2.ExcessAmount)
	}
	if d2.Mileage != "100 miles/day" {
		t.Fatalf("mileage = %q, want 100 miles/day", d2.Mileage)
	}
}

func TestVehicleHireIsNotLinkable(t *testing.T) {
	if LinkableType("vehicle_hire") {
		t.Fatal("vehicle_hire must not be link/split eligible: a hire is one continuous possession of one vehicle, not a multi-leg journey")
	}
}
