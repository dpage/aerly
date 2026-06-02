package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// fakeGeocoder resolves every address to a fixed coordinate.
type fakeGeocoder struct{ lat, lon float64 }

func (f fakeGeocoder) Geocode(context.Context, string) (float64, float64, bool, error) {
	return f.lat, f.lon, true, nil
}

// TestBackfillPartCoordinates verifies the startup backfill geocodes an
// addressed part that has no coordinates (a plan ingested before address
// geocoding existed), so it can finally plot on the map.
func TestBackfillPartCoordinates(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Geocoder = fakeGeocoder{lat: 50.8489, lon: 4.3491}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Brussels"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	checkin := time.Date(2026, 2, 1, 15, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 2, 3, 11, 0, 0, 0, time.UTC)
	plan, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Brussels Marriott",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel:   "Brussels Marriott Hotel Grand Place",
			StartAddress: "Rue Auguste Orts 3-7, Brussels 1000, Belgium",
			Hotel:        &store.HotelDetail{PropertyName: "Brussels Marriott"},
		}},
	}, uid)
	if err != nil {
		t.Fatal(err)
	}

	// Precondition: no coordinates yet.
	parts, _ := e.store.PartsByPlan(ctx, plan.ID)
	if len(parts) != 1 || parts[0].StartLat != nil {
		t.Fatalf("expected 1 part with no coords, got %+v", parts)
	}
	// The plan is a backfill candidate.
	ids, err := e.store.PlanIDsNeedingGeocode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != plan.ID {
		t.Fatalf("PlanIDsNeedingGeocode = %v, want [%d]", ids, plan.ID)
	}

	e.api.BackfillPartCoordinates(ctx)

	parts, _ = e.store.PartsByPlan(ctx, plan.ID)
	if parts[0].StartLat == nil || parts[0].StartLon == nil {
		t.Fatalf("part not geocoded by backfill: %+v", parts[0])
	}
	if *parts[0].StartLat != 50.8489 || *parts[0].StartLon != 4.3491 {
		t.Errorf("coords = (%v, %v), want (50.8489, 4.3491)", *parts[0].StartLat, *parts[0].StartLon)
	}
	// Idempotent: once geocoded, the plan is no longer a candidate.
	ids, _ = e.store.PlanIDsNeedingGeocode(ctx)
	if len(ids) != 0 {
		t.Errorf("plan still a geocode candidate after backfill: %v", ids)
	}
}
