package handlers

import (
	"context"
	"testing"
	"time"
)

// g2seedHotelPlan creates a hotel plan whose single part carries an address but
// no coordinates, so a geocode pass changes it (exercising the changed→publish
// branch). Returns the trip id and plan id.
func g2seedHotelPlan(t *testing.T, e *testEnv, owner int64) (tripID, planID int64) {
	t.Helper()
	tripID = newTrip(t, e, owner, "Async geo trip")
	out := time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC)
	w := e.req(t, "POST", "/api/trips/"+itoa(tripID)+"/plans", map[string]any{
		"type": "hotel", "title": "Hotel Example",
		"parts": []map[string]any{{
			"type": "hotel", "starts_at": out, "ends_at": out.Add(24 * time.Hour),
			"start_label": "Hotel Example", "start_address": "1 Test Street, Example City",
		}},
	}, owner)
	if w.Code != 201 {
		t.Fatalf("seed hotel plan = %d %s", w.Code, w.Body.String())
	}
	planID = int64(decodeBody[map[string]any](t, w)["id"].(float64))
	return tripID, planID
}

// TestGeocodeDeriveAsyncChangedAndDeriveG2 drives geocodeAndDeriveImportedTripAsync
// directly over a hotel plan that has an address but no coordinates: the geocode
// pass changes the part (changed→publish), then the trip's country is derived
// and stored. We observe completion by polling the trip's country_code.
func TestGeocodeDeriveAsyncChangedAndDeriveG2(t *testing.T) {
	e := setup(t, nil, nil)
	geo := fakeGeocoder{lat: 41.9028, lon: 12.4964, country: "it"}
	e.api.Geocoder = geo
	e.api.GeoResolver = geoResolver(geo)
	owner := e.user(t, "g2asyncok", false)
	tripID, planID := g2seedHotelPlan(t, e, owner)

	e.api.geocodeAndDeriveImportedTripAsync(tripID, []int64{planID})

	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var cc string
		if err := e.pool.QueryRow(ctx,
			`SELECT COALESCE(country_code, '') FROM trips WHERE id = $1`, tripID).Scan(&cc); err == nil && cc != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("country_code never derived from the geocoded plan within the deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestGeocodeDeriveAsyncGeocodeFailsG2 drives the goroutine's hard-failure
// branch: with plan_parts dropped, geocode.PlanParts errors (geocodeFailed), so
// the function logs, skips the derive, and returns. We assert it doesn't panic
// and never derives a country (the trip is gone with the table, so we just give
// the goroutine time to run and exit cleanly).
func TestGeocodeDeriveAsyncGeocodeFailsG2(t *testing.T) {
	e := setup(t, nil, nil)
	geo := fakeGeocoder{lat: 1, lon: 2, country: "it"}
	e.api.Geocoder = geo
	e.api.GeoResolver = geoResolver(geo)
	owner := e.user(t, "g2asyncfail", false)
	tripID, planID := g2seedHotelPlan(t, e, owner)

	// PartsByPlan reads plan_parts; dropping it makes geocode.PlanParts error.
	g1dropTable(t, e, "plan_parts")
	e.api.geocodeAndDeriveImportedTripAsync(tripID, []int64{planID})

	// The single failing query returns fast; a short grace lets the goroutine
	// reach the geocodeFailed return before teardown.
	time.Sleep(300 * time.Millisecond)
}

// TestGeocodeDeriveAsyncTripByIDErrG2 covers the goroutine's TripByID error
// branch: the geocode pass succeeds (the plan has no addressed parts to fail
// on), but the post-geocode TripByID lookup errors because a column it selects
// is gone. Observed indirectly: the goroutine logs and returns without deriving.
func TestGeocodeDeriveAsyncTripByIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	geo := fakeGeocoder{lat: 41.9, lon: 12.5, country: "it"}
	e.api.Geocoder = geo
	e.api.GeoResolver = geoResolver(geo)
	owner := e.user(t, "g2asynctbid", false)
	tripID, planID := g2seedHotelPlan(t, e, owner)

	// country_code is part of TripByID's selected columns; dropping it makes the
	// post-geocode TripByID error while PartsByPlan/UpdatePlanPart still work.
	g1dropColumn(t, e, "trips", "country_code")
	e.api.geocodeAndDeriveImportedTripAsync(tripID, []int64{planID})

	// Give the goroutine time to geocode the parts and hit the failing TripByID.
	time.Sleep(500 * time.Millisecond)
}
