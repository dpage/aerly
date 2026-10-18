package handlers

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// TestPinnedCoordsSurviveAddressEdit proves a manually-pinned coordinate is a
// hard override: once pinned, a later address edit does NOT re-geocode over it,
// and clearing the pin reverts the endpoint to address-derived coordinates.
func TestPinnedCoordsSurviveAddressEdit(t *testing.T) {
	e := setup(t, nil, nil)
	// The geocoder resolves every address to this (deliberately "wrong") spot.
	geo := fakeGeocoder{lat: 50.0, lon: 4.0}
	e.api.Geocoder = geo
	e.api.GeoResolver = geoResolver(geo)
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

// pinHotel creates a hotel plan with one part whose zones and sources are set
// explicitly, and returns the part's PATCH path and id.
func pinHotel(t *testing.T, e *testEnv, uid int64, part store.CreatePlanPartPayload) (string, int64) {
	t.Helper()
	ctx := context.Background()
	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Test Trip"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	part.StartLabel = "Test Hotel"
	plan, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Test Hotel",
		Parts: []store.CreatePlanPartPayload{part},
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := e.store.PartsByPlan(ctx, plan.ID)
	return "/api/plan-parts/" + strconv.FormatInt(parts[0].ID, 10), parts[0].ID
}

// TestPinRederivesZoneKeepingWallClock is the hotel-in-the-wrong-zone fix: a
// hotel stored in Madrid but pinned in Tashkent takes Tashkent's zone, and its
// check-in and check-out keep the wall-clock times the old zone showed.
func TestPinRederivesZoneKeepingWallClock(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	uid := e.user(t, "pinzone", false)
	// 14:00 CEST (UTC+2) on 1 July is 12:00Z; 11:00 CEST on 3 July is 09:00Z.
	checkin := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	path, id := pinHotel(t, e, uid, store.CreatePlanPartPayload{
		StartsAt: checkin, EndsAt: &checkout,
		StartTZ: "Europe/Madrid", EndTZ: "Europe/Madrid",
		StartLat: pinF(40.4168), StartLon: pinF(-3.7038),
	})

	// Coordinates in Tashkent (UTC+5, no DST).
	pin := map[string]any{"start_lat": 41.3, "start_lon": 69.24, "start_coords_pinned": true}
	if w := e.req(t, "PATCH", path, pin, uid); w.Code != http.StatusOK {
		t.Fatalf("pin PATCH = %d, body %s", w.Code, w.Body.String())
	}
	got, _ := e.store.PlanPartByID(ctx, id)
	if got.StartTZ != "Asia/Tashkent" || got.StartTZSource != "confirmed" {
		t.Errorf("start zone = %q/%q, want Asia/Tashkent/confirmed", got.StartTZ, got.StartTZSource)
	}
	// 14:00 in Tashkent is 09:00Z.
	if want := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC); !got.StartsAt.Equal(want) {
		t.Errorf("starts_at = %v, want %v", got.StartsAt.UTC(), want)
	}
	// The checkout has no coordinates of its own, so it follows the start.
	if got.EndTZ != "Asia/Tashkent" || got.EndTZSource != "confirmed" {
		t.Errorf("end zone = %q/%q, want Asia/Tashkent/confirmed", got.EndTZ, got.EndTZSource)
	}
	// 11:00 in Tashkent is 06:00Z.
	if want := time.Date(2026, 7, 3, 6, 0, 0, 0, time.UTC); got.EndsAt == nil || !got.EndsAt.Equal(want) {
		t.Errorf("ends_at = %v, want %v", got.EndsAt, want)
	}
}

// TestPinZoneRules covers the edges of the pin path: an explicit time in the
// same request is not re-anchored, an explicit zone wins, the same zone is a
// no-op, and pinned end coordinates derive the end's own zone.
func TestPinZoneRules(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	uid := e.user(t, "pinrules", false)
	checkin := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	madrid := store.CreatePlanPartPayload{
		StartsAt: checkin, EndsAt: &checkout,
		StartTZ: "Europe/Madrid", EndTZ: "Europe/Madrid",
	}

	t.Run("explicit starts_at is kept but the zone still moves", func(t *testing.T) {
		path, id := pinHotel(t, e, uid, madrid)
		at := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
		body := map[string]any{"start_lat": 41.3, "start_lon": 69.24, "start_coords_pinned": true,
			"starts_at": at.Format(time.RFC3339)}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		if got.StartTZ != "Asia/Tashkent" || got.StartTZSource != "confirmed" || !got.StartsAt.Equal(at) {
			t.Errorf("got %q/%q %v, want Asia/Tashkent/confirmed %v", got.StartTZ, got.StartTZSource, got.StartsAt.UTC(), at)
		}
	})

	t.Run("explicit start_tz wins", func(t *testing.T) {
		path, id := pinHotel(t, e, uid, madrid)
		body := map[string]any{"start_lat": 41.3, "start_lon": 69.24, "start_coords_pinned": true,
			"start_tz": "Europe/London"}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		if got.StartTZ != "Europe/London" || got.StartTZSource != "confirmed" || !got.StartsAt.Equal(checkin) {
			t.Errorf("got %q/%q %v, want Europe/London/confirmed %v", got.StartTZ, got.StartTZSource, got.StartsAt.UTC(), checkin)
		}
	})

	t.Run("an end with its own different zone is kept", func(t *testing.T) {
		// A transfer from London to Paris with no end coordinates.
		path, id := pinHotel(t, e, uid, store.CreatePlanPartPayload{
			StartsAt: checkin, EndsAt: &checkout,
			StartTZ: "Europe/London", EndTZ: "Europe/Paris",
		})
		body := map[string]any{"start_lat": 51.5074, "start_lon": -0.1278, "start_coords_pinned": true}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		if got.EndTZ != "Europe/Paris" || got.EndTZSource != "confirmed" || got.EndsAt == nil || !got.EndsAt.Equal(checkout) {
			t.Errorf("end = %q/%q %v, want Europe/Paris/confirmed %v untouched", got.EndTZ, got.EndTZSource, got.EndsAt, checkout)
		}
	})

	t.Run("an end sharing the start's old zone follows it", func(t *testing.T) {
		path, id := pinHotel(t, e, uid, store.CreatePlanPartPayload{
			StartsAt: checkin, EndsAt: &checkout,
			StartTZ: "Europe/Madrid", EndTZ: "Europe/Madrid",
		})
		body := map[string]any{"start_lat": 51.5074, "start_lon": -0.1278, "start_coords_pinned": true}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		// 11:00 CEST (09:00Z) becomes 11:00 BST, 10:00Z.
		want := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
		if got.EndTZ != "Europe/London" || got.EndTZSource != "confirmed" || got.EndsAt == nil || !got.EndsAt.Equal(want) {
			t.Errorf("end = %q/%q %v, want Europe/London/confirmed %v", got.EndTZ, got.EndTZSource, got.EndsAt, want)
		}
	})

	t.Run("pin inside the current zone changes nothing", func(t *testing.T) {
		path, id := pinHotel(t, e, uid, madrid)
		body := map[string]any{"start_lat": 41.39, "start_lon": 2.17, "start_coords_pinned": true}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		if got.StartTZ != "Europe/Madrid" || !got.StartsAt.Equal(checkin) {
			t.Errorf("got %q %v, want Europe/Madrid %v", got.StartTZ, got.StartsAt.UTC(), checkin)
		}
	})

	t.Run("unknown ends gain the zone and keep their instants", func(t *testing.T) {
		path, id := pinHotel(t, e, uid, store.CreatePlanPartPayload{
			StartsAt: checkin, EndsAt: &checkout,
			StartTZSource: "unknown", EndTZSource: "unknown",
		})
		body := map[string]any{"start_lat": 41.3, "start_lon": 69.24, "start_coords_pinned": true}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		if got.StartTZ != "Asia/Tashkent" || got.StartTZSource != "confirmed" || !got.StartsAt.Equal(checkin) {
			t.Errorf("start = %q/%q %v, want Asia/Tashkent/confirmed %v", got.StartTZ, got.StartTZSource, got.StartsAt.UTC(), checkin)
		}
		if got.EndTZ != "Asia/Tashkent" || got.EndTZSource != "confirmed" || got.EndsAt == nil || !got.EndsAt.Equal(checkout) {
			t.Errorf("end = %q/%q %v, want Asia/Tashkent/confirmed %v", got.EndTZ, got.EndTZSource, got.EndsAt, checkout)
		}
	})

	t.Run("floating ends keep their wall-clock digits", func(t *testing.T) {
		path, id := pinHotel(t, e, uid, store.CreatePlanPartPayload{
			StartsAt: checkin, EndsAt: &checkout,
			StartTZSource: "floating", EndTZSource: "floating",
		})
		body := map[string]any{"start_lat": 41.3, "start_lon": 69.24, "start_coords_pinned": true}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		// 12:00 and 09:00 read as Tashkent wall-clock (UTC+5) are 07:00Z and 04:00Z.
		wantIn := time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC)
		wantOut := time.Date(2026, 7, 3, 4, 0, 0, 0, time.UTC)
		if got.StartTZ != "Asia/Tashkent" || !got.StartsAt.Equal(wantIn) {
			t.Errorf("start = %q %v, want Asia/Tashkent %v", got.StartTZ, got.StartsAt.UTC(), wantIn)
		}
		if got.EndTZ != "Asia/Tashkent" || got.EndsAt == nil || !got.EndsAt.Equal(wantOut) {
			t.Errorf("end = %q %v, want Asia/Tashkent %v", got.EndTZ, got.EndsAt, wantOut)
		}
	})

	t.Run("pinned end coords derive the end zone", func(t *testing.T) {
		path, id := pinHotel(t, e, uid, madrid)
		body := map[string]any{"end_lat": 41.3, "end_lon": 69.24, "end_coords_pinned": true}
		if w := e.req(t, "PATCH", path, body, uid); w.Code != http.StatusOK {
			t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
		}
		got, _ := e.store.PlanPartByID(ctx, id)
		if got.StartTZ != "Europe/Madrid" || !got.StartsAt.Equal(checkin) {
			t.Errorf("start moved: %q %v", got.StartTZ, got.StartsAt.UTC())
		}
		// 11:00 CEST becomes 11:00 in Tashkent, 06:00Z.
		want := time.Date(2026, 7, 3, 6, 0, 0, 0, time.UTC)
		if got.EndTZ != "Asia/Tashkent" || got.EndTZSource != "confirmed" || got.EndsAt == nil || !got.EndsAt.Equal(want) {
			t.Errorf("end = %q/%q %v, want Asia/Tashkent/confirmed %v", got.EndTZ, got.EndTZSource, got.EndsAt, want)
		}
	})
}

func pinF(f float64) *float64 { return &f }

// TestKeepWallClock covers the conversion behind the pin path, including a
// wall-clock time the new zone skips for daylight saving.
func TestKeepWallClock(t *testing.T) {
	// 14:00 CEST in Madrid read as 14:00 in Tashkent (UTC+5) is 09:00Z.
	madrid := time.Date(2026, 7, 1, 12, 0, 30, 0, time.UTC)
	if got, want := keepWallClock(madrid, "Europe/Madrid", "Asia/Tashkent"), time.Date(2026, 7, 1, 9, 0, 30, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Madrid to Tashkent = %v, want %v (seconds kept)", got.UTC(), want)
	}
	// No old zone: the UTC reading is the wall-clock time.
	floating := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
	if got, want := keepWallClock(floating, "", "Europe/Paris"), time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("UTC to Paris = %v, want %v", got.UTC(), want)
	}
	// 01:30 on 29 March 2026 does not exist in London (clocks go 01:00 to
	// 02:00), so the fallback takes timing.Anchor's instant.
	gap := time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)
	want, _ := timing.Anchor(gap, "Europe/London")
	if got := keepWallClock(gap, "", "Europe/London"); !got.Equal(want) {
		t.Errorf("gap time = %v, want Anchor's %v", got.UTC(), want.UTC())
	}
}
