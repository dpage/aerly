package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

// stubPOIs is a fake providers.POIResolver returning a fixed POI set,
// regardless of the query, for handler tests. It records the radius it was
// last called with so tests can assert the handler's clamping.
type stubPOIs struct {
	pois      []providers.POI
	gotRadius int
}

func (s *stubPOIs) Nearby(ctx context.Context, lat, lon float64, r int, cats []string) ([]providers.POI, error) {
	s.gotRadius = r
	return s.pois, nil
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

	e.api.Overpass = &stubPOIs{pois: []providers.POI{{
		OSMType:   "node",
		OSMID:     1,
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
	e.api.Overpass = &stubPOIs{}

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=48.85&lon=2.35", trip.ID), nil, outsider)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// stubGeocoder implements geocode.Geocoder for the place= branch.
type stubGeocoder struct {
	lat, lon float64
	ok       bool
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

	e.api.Overpass = &stubPOIs{pois: []providers.POI{{
		OSMType: "way", OSMID: 2, Name: "Example Colosseum", Category: "sights",
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
	e.api.Overpass = stub

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
	e.api.Overpass = nil

	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d/pois?lat=51.5&lon=-0.12", trip.ID), nil, uid)
	if w.Code != 501 {
		t.Fatalf("status = %d, want 501, body=%s", w.Code, w.Body.String())
	}
}
