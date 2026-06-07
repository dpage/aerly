package handlers

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestPinnedCoordsSurviveAddressEdit proves a manually-pinned coordinate is a
// hard override: once pinned, a later address edit does NOT re-geocode over it,
// and clearing the pin reverts the endpoint to address-derived coordinates.
func TestPinnedCoordsSurviveAddressEdit(t *testing.T) {
	e := setup(t, nil, nil)
	// The geocoder resolves every address to this (deliberately "wrong") spot.
	e.api.Geocoder = fakeGeocoder{lat: 50.0, lon: 4.0}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "France"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	checkin := time.Date(2026, 8, 1, 15, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC)
	plan, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Lake hire",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel:   "Lake",
			StartAddress: "10170 Droupt-Saint-Basle, France",
		}},
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := e.store.PartsByPlan(ctx, plan.ID)
	partID := parts[0].ID
	path := "/api/plan-parts/" + strconv.FormatInt(partID, 10)

	// Pin an exact lat/lon (a Google Maps pin), distinct from what geocoding gives.
	pin := map[string]any{
		"start_lat":           48.21,
		"start_lon":           4.08,
		"start_coords_pinned": true,
	}
	if w := e.req(t, "PATCH", path, pin, uid); w.Code != http.StatusOK {
		t.Fatalf("pin PATCH = %d, body %s", w.Code, w.Body.String())
	}
	got, _ := e.store.PlanPartByID(ctx, partID)
	if got.StartLat == nil || *got.StartLat != 48.21 || !got.StartCoordsPinned {
		t.Fatalf("after pin: lat=%v pinned=%v, want 48.21 pinned", got.StartLat, got.StartCoordsPinned)
	}

	// Editing the address must NOT re-geocode over the pinned coordinates.
	if w := e.req(t, "PATCH", path, map[string]any{"start_address": "Somewhere else, France"}, uid); w.Code != http.StatusOK {
		t.Fatalf("address PATCH = %d, body %s", w.Code, w.Body.String())
	}
	got, _ = e.store.PlanPartByID(ctx, partID)
	if got.StartLat == nil || *got.StartLat != 48.21 {
		t.Errorf("pinned coords clobbered by address edit: lat=%v, want 48.21", got.StartLat)
	}

	// Clearing the pin reverts to address-derived coordinates (the geocoder).
	if w := e.req(t, "PATCH", path, map[string]any{"start_coords_pinned": false}, uid); w.Code != http.StatusOK {
		t.Fatalf("unpin PATCH = %d, body %s", w.Code, w.Body.String())
	}
	got, _ = e.store.PlanPartByID(ctx, partID)
	if got.StartCoordsPinned {
		t.Errorf("still pinned after unpin")
	}
	if got.StartLat == nil || *got.StartLat != 50.0 {
		t.Errorf("after unpin: lat=%v, want geocoded 50.0", got.StartLat)
	}
}
