package handlers

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestG3DeriveAndStoreTripPlaceCountryFromDestination covers
// deriveAndStoreTripPlace's "no away-end, derive country from destination"
// branch (242-254, dok=true): a trip with a stated destination but no plotted
// parts gets its flag from geocoding the destination.
func TestG3DeriveAndStoreTripPlaceCountryFromDestination(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Geocoder = fakeGeocoder{country: "fr"} // GeocodeCountry(dest) → fr
	ctx := context.Background()
	uid := e.user(t, "g3dasp", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Paris", Destination: "Paris"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := e.store.TripByID(ctx, trip.ID)
	if !e.api.deriveAndStoreTripPlace(ctx, tr) {
		t.Fatalf("expected a change (flag derived from destination)")
	}
	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "fr" {
		t.Errorf("country = %q, want fr", got.CountryCode)
	}
}

// TestG3DeriveAndStoreTripPlaceUnknownSentinel covers the c=="" → "zz" sentinel
// branch (247-248): a trip with an unresolvable destination and no plotted parts
// is marked "zz".
func TestG3DeriveAndStoreTripPlaceUnknownSentinel(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Geocoder = fakeGeocoder{} // resolves nothing
	ctx := context.Background()
	uid := e.user(t, "g3daspz", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Nowhere", Destination: "Atlantis"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := e.store.TripByID(ctx, trip.ID)
	if !e.api.deriveAndStoreTripPlace(ctx, tr) {
		t.Fatalf("expected a change (zz sentinel written)")
	}
	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "zz" {
		t.Errorf("country = %q, want zz", got.CountryCode)
	}
}

// TestG3TripAwayNoGeocoder covers tripAway's nil-geocoder early return via
// deriveTripCountry (which calls tripAway).
func TestG3TripAwayNoGeocoder(t *testing.T) {
	e := setup(t, nil, nil) // no geocoder
	ctx := context.Background()
	away, ok := e.api.tripAway(ctx, 1)
	if ok || away.code != "" {
		t.Errorf("tripAway with no geocoder = (%+v, %v), want zero/false", away, ok)
	}
}

// TestG3TripAwayZeroDurationFloor covers the w<=0 one-second floor (114) and the
// order-empty guard is exercised elsewhere; here two zero-duration plotted
// endpoints in different countries degrade to a simple majority, with the floor
// giving each presence. With one part in each country and a tie, the earliest
// wins.
func TestG3TripAwayZeroDurationFloor(t *testing.T) {
	e := setup(t, nil, nil)
	const aLat, aLon = 48.8566, 2.3522  // FR
	const bLat, bLon = 52.5200, 13.4050 // DE
	e.api.Geocoder = fakeGeocoder{
		byCoord: map[[2]float64]string{
			{aLat, aLon}: "fr",
			{bLat, bLon}: "de",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "g3tazero", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Zero dwell"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	// Two instantaneous markers (StartsAt == EndsAt → zero duration → 1s floor).
	for i, c := range [][2]float64{{aLat, aLon}, {bLat, bLon}} {
		ts := at.Add(time.Duration(i) * time.Hour)
		if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
			TripID: trip.ID, Type: "meeting", Title: "Marker",
			Parts: []store.CreatePlanPartPayload{{
				StartsAt: ts, EndsAt: &ts,
				StartLabel: "Marker", StartLat: p(c[0]), StartLon: p(c[1]),
			}},
		}, uid); err != nil {
			t.Fatal(err)
		}
	}
	away, ok := e.api.tripAway(ctx, trip.ID)
	if !ok || away.code == "" {
		t.Fatalf("tripAway = (%+v, %v), want a resolved country", away, ok)
	}
}

// TestG3TripAwayUnresolvableEndpoints covers the order-empty guard (147): a part
// whose coordinate the geocoder can't reverse (returns ok=false), so no country
// votes and tripAway reports ok=false.
func TestG3TripAwayUnresolvableEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	// byCoord is set but the part's coordinate is absent from it → ReverseCountry
	// returns "" / ok=false, so the country loop "continues" and order stays empty.
	e.api.Geocoder = fakeGeocoder{byCoord: map[[2]float64]string{{0, 0}: "zz"}}
	ctx := context.Background()
	uid := e.user(t, "g3taunres", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Unmapped"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	at := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	end := at.Add(time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "meeting", Title: "Marker",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &end,
			StartLabel: "Marker", StartLat: p(12.34), StartLon: p(56.78),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	away, ok := e.api.tripAway(ctx, trip.ID)
	if ok {
		t.Errorf("tripAway with unresolvable endpoints = (%+v, %v), want ok=false", away, ok)
	}
}

// TestG3BackfillsNoGeocoder covers the nil-geocoder early returns of the four
// startup backfills plus geocodePlanAsync.
func TestG3BackfillsNoGeocoder(t *testing.T) {
	e := setup(t, nil, nil) // no geocoder
	ctx := context.Background()
	e.api.BackfillTripDestinations(ctx)
	e.api.ReconcileTripPlaces(ctx)
	e.api.BackfillTripCountries(ctx)
	e.api.BackfillPartCoordinates(ctx)
}
