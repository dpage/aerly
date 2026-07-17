package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// pollUntil retries fn until it returns true or the deadline passes. Used to
// observe the result of a fire-and-forget background goroutine.
func g3pollUntil(t *testing.T, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestG3DeriveTripCountryAsync covers deriveTripCountryAsync's full goroutine
// body: it loads the trip, derives the country (from the stated destination via
// the geocoder), caches it, and republishes. A second call no-ops once the code
// is set.
func TestG3DeriveTripCountryAsync(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Geocoder = fakeGeocoder{country: "pt"}
	ctx := context.Background()
	uid := e.user(t, "g3dtca", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Lisbon", Destination: "Lisbon"}, uid)
	if err != nil {
		t.Fatal(err)
	}

	e.api.deriveTripCountryAsync(trip.ID)
	if !g3pollUntil(t, func() bool {
		got, _ := e.store.TripByID(ctx, trip.ID)
		return got.CountryCode == "pt"
	}) {
		got, _ := e.store.TripByID(ctx, trip.ID)
		t.Fatalf("country not derived async: %q", got.CountryCode)
	}

	// Already set → the async derive no-ops (returns early after TripByID).
	e.api.deriveTripCountryAsync(trip.ID)
	time.Sleep(50 * time.Millisecond)
	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "pt" {
		t.Errorf("country changed on no-op pass: %q", got.CountryCode)
	}
}

// TestG3DeriveTripCountryAsyncUnknownSentinel covers the sentinel branch: a
// destination the geocoder can't resolve is cached as "zz".
func TestG3DeriveTripCountryAsyncUnknownSentinel(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Geocoder = fakeGeocoder{} // resolves nothing
	ctx := context.Background()
	uid := e.user(t, "g3dtcaz", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Mystery", Destination: "Atlantis"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	e.api.deriveTripCountryAsync(trip.ID)
	if !g3pollUntil(t, func() bool {
		got, _ := e.store.TripByID(ctx, trip.ID)
		return got.CountryCode == "zz"
	}) {
		got, _ := e.store.TripByID(ctx, trip.ID)
		t.Fatalf("unknown destination not marked zz: %q", got.CountryCode)
	}
}

// TestG3DeriveTripCountryAsyncNoGeocoder covers the nil-geocoder early return.
func TestG3DeriveTripCountryAsyncNoGeocoder(t *testing.T) {
	e := setup(t, nil, nil) // no geocoder wired
	ctx := context.Background()
	uid := e.user(t, "g3dtcanil", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "X", Destination: "Y"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	e.api.deriveTripCountryAsync(trip.ID) // must not panic; returns immediately
	time.Sleep(20 * time.Millisecond)
	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "" {
		t.Errorf("country set without geocoder: %q", got.CountryCode)
	}
}

// TestG3BackfillPartTimezones covers BackfillPartTimezones: a part with real
// coordinates but no timezone is anchored to its zone (resolved from the
// coordinate via the tz database, independent of the geocoder).
func TestG3BackfillPartTimezones(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	uid := e.user(t, "g3bptz", false)
	p := func(f float64) *float64 { return &f }
	// London coordinates → Europe/London.
	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	end := at.Add(2 * time.Hour)
	plan, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: mustTrip(t, e, ctx, uid), Type: "ground", Title: "Cab",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &end,
			StartLabel: "London", StartLat: p(51.5074), StartLon: p(-0.1278),
			EndLabel: "London", EndLat: p(51.5074), EndLon: p(-0.1278),
		}},
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	// Precondition: the part qualifies for the tz backfill.
	need, err := e.store.PartsNeedingTZ(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, np := range need {
		if np.PlanID == plan.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("part not a tz-backfill candidate")
	}

	e.api.BackfillPartTimezones(ctx)

	parts, _ := e.store.PartsByPlan(ctx, plan.ID)
	if len(parts) != 1 || parts[0].StartTZ == "" {
		t.Fatalf("tz not anchored by backfill: %+v", parts[0])
	}
}

// TestG3BackfillPartTimezonesQueryErr covers the query-error branch (dropping
// plan_parts makes PartsNeedingTZ fail).
func TestG3BackfillPartTimezonesQueryErr(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	g1dropTable(t, e, "plan_parts")
	e.api.BackfillPartTimezones(ctx) // logs + returns; must not panic
}

// TestG3GeocodePlanAsyncNoGeocoder covers geocodePlanAsync's nil-geocoder early
// return.
func TestG3GeocodePlanAsyncNoGeocoder(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.geocodePlanAsync(1, 1) // no geocoder → returns immediately, no panic
	time.Sleep(10 * time.Millisecond)
}

// TestG3BackfillQueryErrs covers the query-error branches of the four startup
// backfills (each logs a warning and returns). Dropping the table each one
// queries first makes it fail.
func TestG3BackfillQueryErrs(t *testing.T) {
	geo := fakeGeocoder{country: "pt"}

	// BackfillTripDestinations → TripsNeedingDestination.
	e := setup(t, nil, nil)
	e.api.Geocoder = geo
	g1dropTable(t, e, "trips")
	e.api.BackfillTripDestinations(context.Background())

	// ReconcileTripPlaces → TripsNeedingPlaceReconcile.
	e2 := setup(t, nil, nil)
	e2.api.Geocoder = geo
	g1dropTable(t, e2, "trips")
	e2.api.ReconcileTripPlaces(context.Background())

	// BackfillTripCountries → TripsNeedingCountry.
	e3 := setup(t, nil, nil)
	e3.api.Geocoder = geo
	g1dropTable(t, e3, "trips")
	e3.api.BackfillTripCountries(context.Background())

	// BackfillPartCoordinates → PlanIDsNeedingGeocode.
	e4 := setup(t, nil, nil)
	e4.api.Geocoder = geo
	e4.api.GeoResolver = geoResolver(geo)
	g1dropTable(t, e4, "plan_parts")
	e4.api.BackfillPartCoordinates(context.Background())
}

// mustTrip creates a trip and returns its id, failing the test on error.
func mustTrip(t *testing.T, e *testEnv, ctx context.Context, uid int64) int64 {
	t.Helper()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "TZ trip"}, uid)
	if err != nil {
		t.Fatalf("create trip: %v", err)
	}
	return trip.ID
}
