package handlers

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/store"
)

// fakeGeocoder resolves every address to a fixed coordinate and (optionally) a
// fixed country code. byCoord, when set, drives ReverseCountry per-coordinate so
// part-based country derivation can be exercised.
type fakeGeocoder struct {
	lat, lon       float64
	country        string
	byCoord        map[[2]float64]string
	placeByCoord   map[[2]float64]string // ReversePlace label per coordinate
	resolves       map[string][2]float64 // when set, Geocode answers per exact query
	countryByQuery map[string]string     // when set, GeocodeCountry answers per exact query
}

// Candidates is unused by these tests: they exercise callers of Geocode only.
func (f fakeGeocoder) Candidates(_ context.Context, _ geocode.Query) ([]geocode.Candidate, error) {
	return nil, nil
}

func (f fakeGeocoder) Geocode(_ context.Context, q string, _ string) (float64, float64, bool, error) {
	if f.resolves != nil {
		if c, ok := f.resolves[q]; ok {
			return c[0], c[1], true, nil
		}
		return 0, 0, false, nil
	}
	return f.lat, f.lon, true, nil
}

func (f fakeGeocoder) GeocodeCountry(_ context.Context, q string) (string, bool, error) {
	if f.countryByQuery != nil {
		c := f.countryByQuery[q]
		return c, c != "", nil
	}
	return f.country, f.country != "", nil
}

func (f fakeGeocoder) ReverseCountry(_ context.Context, lat, lon float64) (string, bool, error) {
	if f.byCoord != nil {
		c := f.byCoord[[2]float64{lat, lon}]
		return c, c != "", nil
	}
	return f.country, f.country != "", nil
}

func (f fakeGeocoder) ReversePlace(_ context.Context, lat, lon float64) (string, string, bool, error) {
	code := f.country
	if f.byCoord != nil {
		code = f.byCoord[[2]float64{lat, lon}]
	}
	place := ""
	if f.placeByCoord != nil {
		place = f.placeByCoord[[2]float64{lat, lon}]
	}
	// Mirror the production country-only fallback: when no city label is mapped
	// but a country code is, the real geocoder still returns the country name.
	if place == "" && code != "" {
		place = code
	}
	return place, code, place != "", nil
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
// endpoints, weighted by dwell time) and must NEVER geocode the freeform trip
// name — the "50's, Hopefully" → Oregon → US bug. Here a Folkestone↔Calais round
// trip plus a two-day French hotel picks France even though the name would
// (mis)resolve to "us".
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

// TestBackfillTripDestinationsFromDwell covers the import fix: a trip with no
// destination (a calendar import) gets one from where it spends the most time —
// the multi-day French hotel, not the brief UK→FR transfer — reverse-geocoded to
// a "City, Country" label, and the flag follows.
func TestBackfillTripDestinationsFromDwell(t *testing.T) {
	e := setup(t, nil, nil)
	const folkLat, folkLon = 51.08169, 1.16734
	const calaisLat, calaisLon = 50.95194, 1.85635
	const hotelLat, hotelLon = 48.4618739, 1.5714336
	e.api.Geocoder = fakeGeocoder{
		byCoord: map[[2]float64]string{
			{folkLat, folkLon}:     "gb",
			{calaisLat, calaisLon}: "fr",
			{hotelLat, hotelLon}:   "fr",
		},
		placeByCoord: map[[2]float64]string{
			{hotelLat, hotelLon}: "Drouot-Saint-Basle, France",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "France Fishing"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	at := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	end := at.Add(time.Hour)
	// A brief UK→FR transfer.
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "train", Title: "LeShuttle Folkestone to Calais",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &end,
			StartLabel: "Folkestone Terminal", StartLat: p(folkLat), StartLon: p(folkLon),
			EndLabel: "Calais Terminal", EndLat: p(calaisLat), EndLon: p(calaisLon),
			Train: &store.TrainDetail{Operator: "LeShuttle"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	// A week-long French hotel — the longest dwell, so the destination.
	checkout := at.Add(7 * 24 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Fishing at Belenos Lake",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &checkout,
			StartLabel: "Belenos Lake", StartLat: p(hotelLat), StartLon: p(hotelLon),
			Hotel: &store.HotelDetail{PropertyName: "Belenos Lake"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	e.api.BackfillTripDestinations(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.Destination != "Drouot-Saint-Basle, France" {
		t.Errorf("destination = %q, want the French hotel's city,country", got.Destination)
	}
	if got.CountryCode != "fr" {
		t.Errorf("country = %q, want fr (follows the destination)", got.CountryCode)
	}
}

// TestDeriveTripCountryByDwellTime covers the there-and-back flag bug: a trip to
// "Tallinn, Estonia" whose endpoints are mostly in the UK (the home↔airport cab
// out and back) must fly the Estonian flag because that's where the week is spent.
// A plain endpoint count would pick "gb" (four UK endpoints vs one EE), so the
// derivation must weight by dwell time — the week-long hotel stay wins.
func TestDeriveTripCountryByDwellTime(t *testing.T) {
	e := setup(t, nil, nil)
	const homeLat, homeLon = 51.5, -0.5         // GB
	const lhrLat, lhrLon = 51.4700, -0.4543     // GB
	const hotelLat, hotelLon = 59.4370, 24.7536 // EE (Tallinn)
	// country:"ee" stands in for geocoding the destination "Tallinn, Estonia".
	// byCoord drives the reverse lookups: four GB endpoints (cab out + back) vs a
	// single EE one, so a pure endpoint vote would (wrongly) pick "gb".
	e.api.Geocoder = fakeGeocoder{
		country: "ee",
		byCoord: map[[2]float64]string{
			{homeLat, homeLon}:   "gb",
			{lhrLat, lhrLon}:     "gb",
			{hotelLat, hotelLon}: "ee",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{
		Name: "Tallinn break", Destination: "Tallinn, Estonia",
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	at := time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC)
	end := at.Add(time.Hour)
	// Outbound cab home → LHR.
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "ground", Title: "Cab to LHR",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &end,
			StartLabel: "Home", StartLat: p(homeLat), StartLon: p(homeLon),
			EndLabel: "LHR T3", EndLat: p(lhrLat), EndLon: p(lhrLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	// Return cab LHR → home.
	rb := at.Add(7 * 24 * time.Hour)
	rbEnd := rb.Add(time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "ground", Title: "Cab home",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: rb, EndsAt: &rbEnd,
			StartLabel: "LHR T3", StartLat: p(lhrLat), StartLon: p(lhrLon),
			EndLabel: "Home", EndLat: p(homeLat), EndLon: p(homeLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	// A week at the Radisson Tallinn.
	checkout := at.Add(7 * 24 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Radisson Tallinn",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: at, EndsAt: &checkout,
			StartLabel: "Radisson Tallinn", StartLat: p(hotelLat), StartLon: p(hotelLon),
			Hotel: &store.HotelDetail{PropertyName: "Radisson Tallinn"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	e.api.BackfillTripCountries(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "ee" {
		t.Errorf("country = %q, want ee (the stated destination, not 'gb' from the UK endpoints)", got.CountryCode)
	}
}

// TestDeriveTripDestinationAwayEndUnplottedHotel covers the addressless-hotel
// bug: a there-and-back LHR↔DUB trip whose week-long Dublin hotel was imported
// without an address (so it has no coordinate and casts no dwell vote) must
// still fly the Irish flag and read "Dublin, Ireland". With only the two flights
// plotted, a plain dwell tally ties GB/IE and breaks toward the London origin,
// and a "longest plotted part" rule picks the slightly longer return flight's
// London arrival; the away-end correction pins it to the non-origin end instead.
func TestDeriveTripDestinationAwayEndUnplottedHotel(t *testing.T) {
	e := setup(t, nil, nil)
	const lhrLat, lhrLon = 51.4775, -0.4614 // GB
	const dubLat, dubLon = 53.4264, -6.2499 // IE
	e.api.Geocoder = fakeGeocoder{
		byCoord: map[[2]float64]string{
			{lhrLat, lhrLon}: "gb",
			{dubLat, dubLon}: "ie",
		},
		placeByCoord: map[[2]float64]string{
			{dubLat, dubLon}: "Dublin, Ireland",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "PGConf Dublin"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	out := time.Date(2025, 5, 1, 8, 0, 0, 0, time.UTC)
	outEnd := out.Add(time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA836 LHR to DUB",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &outEnd,
			StartLabel: "LHR", StartLat: p(lhrLat), StartLon: p(lhrLon),
			EndLabel: "DUB", EndLat: p(dubLat), EndLon: p(dubLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	// Return is slightly longer, so a "longest plotted part" rule would wrongly
	// pick its London arrival.
	ret := out.Add(7 * 24 * time.Hour)
	retEnd := ret.Add(90 * time.Minute)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA837 DUB to LHR",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: ret, EndsAt: &retEnd,
			StartLabel: "DUB", StartLat: p(dubLat), StartLon: p(dubLon),
			EndLabel: "LHR", EndLat: p(lhrLat), EndLon: p(lhrLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	// The week where the time is actually spent, imported without an address —
	// no coordinate, so it casts no dwell vote.
	checkin := out.Add(2 * time.Hour)
	checkout := ret.Add(-2 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Conrad Hotels & Resorts",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel: "Conrad Hotels & Resorts",
			Hotel:      &store.HotelDetail{PropertyName: "Conrad Hotels & Resorts"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	e.api.BackfillTripDestinations(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "ie" {
		t.Errorf("country = %q, want ie (the away end, not the gb origin)", got.CountryCode)
	}
	if got.Destination != "Dublin, Ireland" {
		t.Errorf("destination = %q, want Dublin, Ireland", got.Destination)
	}
}

// TestDeriveTripCountryHotelBeforeOutboundFlight guards the FOSDEM/Brussels
// regression: the destination hotel's nominal check-in (14:00) sorts ahead of
// the same-day outbound flight (14:50), so the earliest plotted *endpoint* is
// Brussels, not home. The journey origin must come from the first flight's
// departure (LHR/GB) so the heavily-dwelt Brussels hotel keeps the flag — an
// earlier "origin = first endpoint" rule mistook Brussels for home and handed
// the flag to the London transit endpoints.
func TestDeriveTripCountryHotelBeforeOutboundFlight(t *testing.T) {
	e := setup(t, nil, nil)
	const lhrLat, lhrLon = 51.4700, -0.4543 // GB
	const bruLat, bruLon = 50.9014, 4.4844  // BE (airport)
	const hotLat, hotLon = 50.8505, 4.3488  // BE (Brussels city)
	e.api.Geocoder = fakeGeocoder{
		byCoord: map[[2]float64]string{
			{lhrLat, lhrLon}: "gb",
			{bruLat, bruLon}: "be",
			{hotLat, hotLon}: "be",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "FOSDEM/PGDay"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	// Hotel check-in sorts first (14:00), before the outbound flight (14:50);
	// start and end coordinates are the same Brussels point, so it's a stay, not
	// a movement, and must not define the origin.
	checkin := time.Date(2026, 1, 28, 14, 0, 0, 0, time.UTC)
	checkout := checkin.Add(114 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Brussels Marriott",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel: "Brussels Marriott", StartLat: p(hotLat), StartLon: p(hotLon),
			EndLabel: "Brussels Marriott", EndLat: p(hotLat), EndLon: p(hotLon),
			Hotel: &store.HotelDetail{PropertyName: "Brussels Marriott"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	out := time.Date(2026, 1, 28, 14, 50, 0, 0, time.UTC)
	outEnd := out.Add(80 * time.Minute)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA392 LHR to BRU",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &outEnd,
			StartLabel: "LHR", StartLat: p(lhrLat), StartLon: p(lhrLon),
			EndLabel: "BRU", EndLat: p(bruLat), EndLon: p(bruLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	ret := checkout.Add(time.Hour)
	retEnd := ret.Add(80 * time.Minute)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA392 BRU to LHR",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: ret, EndsAt: &retEnd,
			StartLabel: "BRU", StartLat: p(bruLat), StartLon: p(bruLon),
			EndLabel: "LHR", EndLat: p(lhrLat), EndLon: p(lhrLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	e.api.BackfillTripCountries(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "be" {
		t.Errorf("country = %q, want be (the dwelt Brussels hotel, not the gb transit endpoints)", got.CountryCode)
	}
}

// TestReconcileTripPlacesUnfreezesFlag covers the frozen-flag bug: a trip whose
// New York hotel is fully plotted, but whose country_code was derived early
// (against half-geocoded plans) and frozen to the gb origin, must be re-derived
// to us. Its destination is already correct (resolves to us, not the origin), so
// it must be left untouched, and the trip must drop out of the candidate set.
func TestReconcileTripPlacesUnfreezesFlag(t *testing.T) {
	e := setup(t, nil, nil)
	const lhrLat, lhrLon = 51.4775, -0.4614  // GB
	const ewrLat, ewrLon = 40.6895, -74.1745 // US
	const nycLat, nycLon = 40.7128, -74.0060 // US
	e.api.Geocoder = fakeGeocoder{
		byCoord: map[[2]float64]string{
			{lhrLat, lhrLon}: "gb",
			{ewrLat, ewrLon}: "us",
			{nycLat, nycLon}: "us",
		},
		placeByCoord: map[[2]float64]string{
			{nycLat, nycLon}: "New York, United States",
		},
		countryByQuery: map[string]string{
			"New York, United States": "us",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{
		Name: "NYC PGConf", Destination: "New York, United States",
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	// A wrong flag frozen before the hotel geocoded.
	if err := e.store.SetTripCountry(ctx, trip.ID, "gb"); err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	out := time.Date(2014, 9, 1, 8, 0, 0, 0, time.UTC)
	outEnd := out.Add(8 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA185 LHR to EWR",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &outEnd,
			StartLabel: "LHR", StartLat: p(lhrLat), StartLon: p(lhrLon),
			EndLabel: "EWR", EndLat: p(ewrLat), EndLon: p(ewrLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	checkin := out.Add(10 * time.Hour)
	checkout := checkin.Add(96 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "New York Marriott Downtown",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel: "Marriott", StartLat: p(nycLat), StartLon: p(nycLon),
			Hotel: &store.HotelDetail{PropertyName: "New York Marriott Downtown"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	e.api.ReconcileTripPlaces(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "us" {
		t.Errorf("country = %q, want us (re-derived from the plotted NY hotel)", got.CountryCode)
	}
	if got.Destination != "New York, United States" {
		t.Errorf("destination = %q, want it left untouched (already correct)", got.Destination)
	}
	// One-shot: no longer a reconcile candidate.
	cand, _ := e.store.TripsNeedingPlaceReconcile(ctx)
	for _, c := range cand {
		if c.ID == trip.ID {
			t.Errorf("trip still a reconcile candidate after the pass")
		}
	}
}

// TestReconcileTripPlacesRepairsOriginBiasedDestination covers the targeted
// destination repair: a Dublin trip with an addressless hotel whose destination
// was auto-filled to its London origin ("Greater London, United Kingdom") gets
// both the flag and the misleading label corrected to the away end. A
// destination that does NOT resolve to the origin is never touched (covered by
// TestReconcileTripPlacesUnfreezesFlag).
func TestReconcileTripPlacesRepairsOriginBiasedDestination(t *testing.T) {
	e := setup(t, nil, nil)
	const lhrLat, lhrLon = 51.4775, -0.4614 // GB
	const dubLat, dubLon = 53.4264, -6.2499 // IE
	e.api.Geocoder = fakeGeocoder{
		byCoord: map[[2]float64]string{
			{lhrLat, lhrLon}: "gb",
			{dubLat, dubLon}: "ie",
		},
		placeByCoord: map[[2]float64]string{
			{dubLat, dubLon}: "Dublin, Ireland",
		},
		countryByQuery: map[string]string{
			"Greater London, United Kingdom": "gb",
		},
	}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{
		Name: "PGConf EU 2013", Destination: "Greater London, United Kingdom",
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetTripCountry(ctx, trip.ID, "gb"); err != nil {
		t.Fatal(err)
	}
	p := func(f float64) *float64 { return &f }
	out := time.Date(2013, 10, 28, 8, 0, 0, 0, time.UTC)
	outEnd := out.Add(time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA836 LHR to DUB",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &outEnd,
			StartLabel: "LHR", StartLat: p(lhrLat), StartLon: p(lhrLon),
			EndLabel: "DUB", EndLat: p(dubLat), EndLon: p(dubLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	ret := out.Add(5 * 24 * time.Hour)
	retEnd := ret.Add(time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "flight", Title: "BA837 DUB to LHR",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: ret, EndsAt: &retEnd,
			StartLabel: "DUB", StartLat: p(dubLat), StartLon: p(dubLon),
			EndLabel: "LHR", EndLat: p(lhrLat), EndLon: p(lhrLon),
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}
	checkin := out.Add(2 * time.Hour)
	checkout := ret.Add(-2 * time.Hour)
	if _, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Conrad Hotels & Resorts",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel: "Conrad Hotels & Resorts",
			Hotel:      &store.HotelDetail{PropertyName: "Conrad Hotels & Resorts"},
		}},
	}, uid); err != nil {
		t.Fatal(err)
	}

	e.api.ReconcileTripPlaces(ctx)

	got, _ := e.store.TripByID(ctx, trip.ID)
	if got.CountryCode != "ie" {
		t.Errorf("country = %q, want ie", got.CountryCode)
	}
	if got.Destination != "Dublin, Ireland" {
		t.Errorf("destination = %q, want Dublin, Ireland (origin-biased label repaired)", got.Destination)
	}
}
