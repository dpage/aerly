package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestHotelEffectiveAtUsesSmartCheckin verifies a hotel part is ordered by its
// §10 smart check-in (after the inbound flight's arrival) rather than its raw
// default check-in, so it doesn't sort ahead of the flight that gets you there
// on the timeline / map (the BA-holiday "shuttle, hotel, flight" regression).
func TestHotelEffectiveAtUsesSmartCheckin(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Benidorm"}, uid)
	if err != nil {
		t.Fatal(err)
	}

	// Inbound flight arriving 18:00 on the check-in day.
	dep := time.Date(2026, 3, 6, 15, 30, 0, 0, time.UTC)
	arr := time.Date(2026, 3, 6, 18, 0, 0, 0, time.UTC)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA2656",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: dep, EndsAt: &arr,
			Flight: &store.FlightDetail{Ident: "BA2656", ScheduledOut: dep, ScheduledIn: arr, OriginIATA: "LGW", DestIATA: "ALC"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	// Hotel with the default 15:00 check-in — earlier than the flight's 18:00
	// arrival, which is exactly what mis-ordered the timeline.
	checkin := time.Date(2026, 3, 6, 15, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC)
	hotelPlan, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Melia Benidorm",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			Hotel: &store.HotelDetail{PropertyName: "Melia Benidorm"},
		}},
	}, uid)
	if err != nil {
		t.Fatal(err)
	}

	parts, err := e.store.PartsByPlan(ctx, hotelPlan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	flights, err := e.api.tripFlightParts(ctx, trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	dto, err := e.api.partDTOWithFlights(ctx, parts[0], flights)
	if err != nil {
		t.Fatal(err)
	}

	// effective_at should be the smart check-in (>= the 18:00 arrival), not the
	// raw 15:00 default — so the hotel sorts after the inbound flight.
	if !dto.EffectiveAt.After(arr) && !dto.EffectiveAt.Equal(arr) {
		t.Errorf("hotel effective_at = %s, want >= flight arrival %s", dto.EffectiveAt, arr)
	}
	if dto.EffectiveAt.Equal(checkin) {
		t.Errorf("hotel effective_at still the raw 15:00 default; smart check-in not applied")
	}
	if dto.Hotel == nil || dto.Hotel.CheckinSuggested == nil {
		t.Fatal("expected a suggested check-in on the hotel DTO")
	}
	if !dto.EffectiveAt.Equal(*dto.Hotel.CheckinSuggested) {
		t.Errorf("effective_at %s != suggested check-in %s", dto.EffectiveAt, *dto.Hotel.CheckinSuggested)
	}
}
