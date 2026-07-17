package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

// stubPOIs is a fake providers.POIResolver returning a fixed POI set,
// regardless of the query, for handler tests. It records the radius it was
// last called with so tests can assert the handler's clamping.
type stubPOIs struct {
	pois      []providers.POI
	gotRadius int
	gotCats   []string
	err       error
}

func (s *stubPOIs) Nearby(ctx context.Context, lat, lon float64, r int, cats []string) ([]providers.POI, error) {
	s.gotRadius = r
	s.gotCats = cats
	return s.pois, s.err
}

// TestGetTripPOIsByCoords covers the lat/lon query-param branch of
// resolvePOICenter: the request supplies coordinates directly, so no
// geocoder call is needed, and the stub Overpass resolver's POIs come back
// verbatim in the response body.
func TestGetTripPOIsByCoords(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poiuser", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}

	e.api.POIs = &stubPOIs{pois: []providers.POI{{
		ID:        "node/1",
		Name:      "Example Tower",
		Category:  "sights",
		Lat:       51.5,
		Lon:       -0.12,
		DistanceM: 40,
	}}}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12&radius=2000&cats=sights", trip.ID), nil, uid)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Example Tower") {
		t.Errorf("body missing POI: %s", w.Body.String())
	}
}

// TestGetTripPOIsRequiresView ensures a non-member gets a 404 rather than
// leaking the trip's existence or POIs.
func TestGetTripPOIsRequiresView(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "poiowner", false)
	outsider := e.user(t, "poioutsider", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Paris", Destination: "Paris"}, owner)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = &stubPOIs{}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=48.85&lon=2.35", trip.ID), nil, outsider)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripPOIsUpstreamUnavailable maps a transient Overpass failure to a 503
// (try again) rather than a blunt 500.
func TestGetTripPOIsUpstreamUnavailable(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poiuser503", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = &stubPOIs{err: providers.ErrPOIUnavailable}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12", trip.ID), nil, uid)
	if w.Code != 503 {
		t.Fatalf("status = %d, want 503, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "temporarily unavailable") {
		t.Errorf("body should explain the transient failure: %s", w.Body.String())
	}
}

// stubGeocoder implements geocode.Geocoder for the place= branch.
type stubGeocoder struct {
	lat, lon float64
	ok       bool
}

// Candidates is unused by these tests: they exercise callers of Geocode only.
func (s stubGeocoder) Candidates(ctx context.Context, q geocode.Query) ([]geocode.Candidate, error) {
	return nil, nil
}

func (s stubGeocoder) Geocode(ctx context.Context, query, countryCode string) (float64, float64, bool, error) {
	return s.lat, s.lon, s.ok, nil
}
func (s stubGeocoder) GeocodeCountry(ctx context.Context, query string) (string, bool, error) {
	return "", false, nil
}
func (s stubGeocoder) ReverseCountry(ctx context.Context, lat, lon float64) (string, bool, error) {
	return "", false, nil
}
func (s stubGeocoder) ReversePlace(ctx context.Context, lat, lon float64) (string, string, bool, error) {
	return "", "", false, nil
}

// TestGetTripPOIsByPlace covers the place=→geocode branch of
// resolvePOICenter.
func TestGetTripPOIsByPlace(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poiplaceuser", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Rome", Destination: "Rome"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}

	e.api.POIs = &stubPOIs{pois: []providers.POI{{
		ID: "way/2", Name: "Example Colosseum", Category: "sights",
		Lat: 41.89, Lon: 12.49, DistanceM: 10,
	}}}
	e.api.Geocoder = stubGeocoder{lat: 41.89, lon: 12.49, ok: true}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?place=Rome", trip.ID), nil, uid)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Example Colosseum") {
		t.Errorf("body missing POI: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"center_label":"Rome"`) {
		t.Errorf("body missing center_label: %s", w.Body.String())
	}
}

// TestGetTripPOIsRadiusClamp locks the clamping in getTripPOIs: an
// oversized radius is capped at poiMaxRadius (not reset to the default), and
// a missing/zero radius falls back to poiDefaultRadius.
func TestGetTripPOIsRadiusClamp(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poiradiususer", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	stub := &stubPOIs{}
	e.api.POIs = stub

	// Oversized radius clamps down to the maximum, not the default.
	if w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12&radius=50000", trip.ID), nil, uid); w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if stub.gotRadius != 10000 {
		t.Errorf("radius = %d, want 10000 (poiMaxRadius)", stub.gotRadius)
	}

	// Zero/omitted radius falls back to the default.
	if w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12&radius=0", trip.ID), nil, uid); w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if stub.gotRadius != 2000 {
		t.Errorf("radius = %d, want 2000 (poiDefaultRadius)", stub.gotRadius)
	}
}

// TestGetTripPOIsUnavailable covers the nil-Overpass guard: the endpoint
// returns 501 when POI lookups aren't configured.
func TestGetTripPOIsUnavailable(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poiunavailuser", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = nil

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12", trip.ID), nil, uid)
	if w.Code != 501 {
		t.Fatalf("status = %d, want 501, body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripPOIsBadTripID covers the pathID parse-failure branch: a
// non-numeric {id} path segment must 400 rather than panic or fall through to
// a store lookup.
func TestGetTripPOIsBadTripID(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poibadid", false)
	e.api.POIs = &stubPOIs{}

	w := e.req(t, "GET", "/api/trips/notanumber/pois?lat=51.5&lon=-0.12", nil, uid)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripPOIsGenericProviderError covers the plain-error branch of the
// Nearby error handling: anything other than providers.ErrPOIUnavailable must
// map to a 500, not the transient 503.
func TestGetTripPOIsGenericProviderError(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poigenericerr", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = &stubPOIs{err: errors.New("boom")}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12", trip.ID), nil, uid)
	if w.Code != 500 {
		t.Fatalf("status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripPOIsFilterByName covers the q= name-filter branch (filterByName):
// a non-matching POI must be excluded from the response body whilst a
// matching one survives.
func TestGetTripPOIsFilterByName(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poinamefilter", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = &stubPOIs{pois: []providers.POI{
		{ID: "node/1", Name: "Example Tower", Category: "sights", Lat: 51.5, Lon: -0.12},
		{ID: "node/2", Name: "Other Landmark", Category: "landmark", Lat: 51.5, Lon: -0.12},
	}}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12&q=tower", trip.ID), nil, uid)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Example Tower") {
		t.Errorf("body should keep the matching POI: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Other Landmark") {
		t.Errorf("body should drop the non-matching POI: %s", w.Body.String())
	}
}

// TestGetTripPOIsDefaultCats covers the default-categories branch: an
// omitted cats= param falls back to the fixed sights/museum/landmark/park set.
func TestGetTripPOIsDefaultCats(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poidefaultcats", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	stub := &stubPOIs{}
	e.api.POIs = stub

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12", trip.ID), nil, uid)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	want := []string{"sights", "museum", "landmark", "park"}
	if len(stub.gotCats) != len(want) {
		t.Fatalf("gotCats = %v, want %v", stub.gotCats, want)
	}
	for i, c := range want {
		if stub.gotCats[i] != c {
			t.Errorf("gotCats[%d] = %q, want %q", i, stub.gotCats[i], c)
		}
	}
}

// TestGetTripPOIsBadCoords covers resolvePOICenter's coordinate-parse-failure
// branch: unparseable lat/lon values must 400 rather than silently falling
// back to place-based geocoding.
func TestGetTripPOIsBadCoords(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poibadcoords", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = &stubPOIs{}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=abc&lon=def", trip.ID), nil, uid)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripPOIsNoLocation covers resolvePOICenter's fallthrough: neither
// coords nor a place param leaves nothing to search from, so it 400s.
func TestGetTripPOIsNoLocation(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poinolocation", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = &stubPOIs{}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois", trip.ID), nil, uid)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// TestGetTripPOIsPlaceGeocodeMiss covers resolvePOICenter's geocode-failure
// branch: a place= that the geocoder can't resolve (ok=false) must 400.
func TestGetTripPOIsPlaceGeocodeMiss(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "poigeomiss", false)
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "London", Destination: "London"}, uid)
	if err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	e.api.POIs = &stubPOIs{}
	e.api.Geocoder = stubGeocoder{ok: false}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?place=Nowhereville", trip.ID), nil, uid)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}
