package handlers

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestEditAddressUsesFallbackChain proves the PATCH edit path geocodes a changed
// address through the shared fallback chain (geocode.Endpoint), not a single raw
// lookup: the full messy address doesn't resolve, but the property name + country
// tail does, so the part ends up with the fallback coordinates.
func TestEditAddressUsesFallbackChain(t *testing.T) {
	e := setup(t, nil, nil)
	// Only the name+country query resolves — the raw full address does not.
	geo := fakeGeocoder{resolves: map[string][2]float64{
		"Ukino Palmeiras Village, Portugal": {37.1, -8.38},
	}}
	e.api.Geocoder = geo
	e.api.GeoResolver = geoResolver(geo)
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Algarve"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	checkin := time.Date(2026, 6, 8, 15, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)
	plan, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Ukino Palmeiras Village",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel: "Ukino Palmeiras Village",
			Hotel:      &store.HotelDetail{PropertyName: "Ukino Palmeiras Village"},
		}},
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := e.store.PartsByPlan(ctx, plan.ID)
	partID := parts[0].ID

	body := map[string]any{
		"start_address": "Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal",
	}
	w := e.req(t, "PATCH", "/api/plan-parts/"+strconv.FormatInt(partID, 10), body, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
	}

	got, _ := e.store.PlanPartByID(ctx, partID)
	if got.StartLat == nil || got.StartLon == nil {
		t.Fatalf("part not geocoded via fallback chain: %+v", got)
	}
	if *got.StartLat != 37.1 || *got.StartLon != -8.38 {
		t.Errorf("coords = (%v, %v), want (37.1, -8.38)", *got.StartLat, *got.StartLon)
	}
}
