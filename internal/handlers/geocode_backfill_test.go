package handlers

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// fakeGeocoder resolves every address to a fixed coordinate and (optionally) a
// fixed country code. byCoord, when set, drives ReverseCountry per-coordinate so
// part-based country derivation can be exercised.
type fakeGeocoder struct {
	lat, lon float64
	country  string
	byCoord  map[[2]float64]string
	resolves map[string][2]float64 // when set, Geocode answers per exact query
}

func (f fakeGeocoder) Geocode(_ context.Context, q string) (float64, float64, bool, error) {
	if f.resolves != nil {
		if c, ok := f.resolves[q]; ok {
			return c[0], c[1], true, nil
		}
		return 0, 0, false, nil
	}
	return f.lat, f.lon, true, nil
}

func (f fakeGeocoder) GeocodeCountry(context.Context, string) (string, bool, error) {
	return f.country, f.country != "", nil
}

func (f fakeGeocoder) ReverseCountry(_ context.Context, lat, lon float64) (string, bool, error) {
	if f.byCoord != nil {
		c := f.byCoord[[2]float64{lat, lon}]
		return c, c != "", nil
	}
	return f.country, f.country != "", nil
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

// TestBackfillTripCountries verifies the startup backfill derives + caches a
// trip's ISO country code, and that an unresolved destination is marked with the
// "zz" sentinel so it isn't re-queried forever.
func TestBackfillTripCountries(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Geocoder = fakeGeocoder{country: "pt"}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Lisbon trip", Destination: "Lisbon"}, uid)
	if err != nil {
		t.Fatal(err)
	}

	e.api.BackfillTripCountries(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "pt" {
		t.Fatalf("country = %q, want pt", got.CountryCode)
	}
	// And it surfaces in the trip DTO.
	dto, err := e.api.tripDTO(httptest.NewRequest("GET", "/", nil).WithContext(ctx), got, uid)
	if err != nil {
		t.Fatal(err)
	}
	if dto.CountryCode != "pt" {
		t.Errorf("dto country = %q, want pt", dto.CountryCode)
	}

	// A geocoder that finds no country marks the trip "zz" (won't re-query).
	e.api.Geocoder = fakeGeocoder{} // country: "" → ok=false
	trip2, _ := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Mystery", Destination: "Atlantis"}, uid)
	e.api.BackfillTripCountries(ctx)
	got2, _ := e.store.TripByID(ctx, trip2.ID)
	if got2.CountryCode != "zz" {
		t.Errorf("unresolved trip country = %q, want zz sentinel", got2.CountryCode)
	}
}

// TestDeriveTripCountryFromParts covers the flag fix: a trip with no destination
// must take its country from where its plans actually are (reverse-geocoded
// endpoints, majority wins) and must NEVER geocode the freeform trip name — the
// "50's, Hopefully" → Oregon → US bug. Here a Folkestone↔Calais round trip plus
// a French hotel votes France even though the name would (mis)resolve to "us".
func TestDeriveTripCountryFromParts(t *testing.T) {
	e := setup(t, nil, nil)
	const folkLat, folkLon = 51.08169, 1.16734       // GB
	const calaisLat, calaisLon = 50.95194, 1.85635   // FR
	const hotelLat, hotelLon = 48.4618739, 1.5714336 // FR
	// country:"us" stands in for what geocoding the *name* would return — proving
	// we don't fall back to it. byCoord drives the reliable reverse lookups.
	e.api.Geocoder = fakeGeocoder{
		country: "us",
		byCoord: map[[2]float64]string{
			{folkLat, folkLon}:     "gb",
			{calaisLat, calaisLon}: "fr",
			{hotelLat, hotelLon}:   "fr",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "50's, Hopefully"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	at := time.Date(2025, 9, 12, 9, 0, 0, 0, time.UTC)
	end := at.Add(time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "train", Title: "Eurotunnel - Folkestone to Calais",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &end,
			StartLabel: "Folkestone", StartLat: p(folkLat), StartLon: p(folkLon),
			EndLabel: "Calais", EndLat: p(calaisLat), EndLon: p(calaisLon),
			Train: &store.TrainDetail{Operator: "Eurotunnel"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	checkout := at.Add(48 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Ablacus Naufrage",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &checkout,
			StartLabel: "Ablacus Naufrage", StartLat: p(hotelLat), StartLon: p(hotelLon),
			Hotel: &store.HotelDetail{PropertyName: "Ablacus Naufrage"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	e.api.BackfillTripCountries(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "fr" {
		t.Errorf("country = %q, want fr (from the French plans, not 'us' from the name)", got.CountryCode)
	}
}
