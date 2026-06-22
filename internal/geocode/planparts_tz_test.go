package geocode

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// fptr is a small helper for the *float64 fields the geocode payloads use.
func fptr(f float64) *float64 { return &f }

// London and New York coordinates resolve to known IANA zones via the embedded
// tzf boundary data. They're public city coordinates, not personal data.
const (
	londonLat, londonLon = 51.5074, -0.1278
	nyLat, nyLon         = 40.7128, -74.0060
)

func TestReinterpretLocal(t *testing.T) {
	// 16:00Z reinterpreted as a New York wall-clock is 16:00 local, which in
	// June (EDT, UTC-4) is 20:00Z.
	in := time.Date(2026, 6, 1, 16, 0, 0, 0, time.UTC)
	got, ok := reinterpretLocal(in, "America/New_York")
	if !ok {
		t.Fatal("reinterpretLocal: ok=false for a valid zone")
	}
	if want := time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC); !got.UTC().Equal(want) {
		t.Errorf("got %v, want %v", got.UTC(), want)
	}

	// An unloadable zone returns the original instant and ok=false.
	if g, ok := reinterpretLocal(in, "Mars/Olympus_Mons"); ok || !g.Equal(in) {
		t.Errorf("bad zone: got (%v,%v), want (%v,false)", g, ok, in)
	}
}

func TestResolvePartTZ(t *testing.T) {
	starts := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)

	t.Run("anchors start and end from their own coords", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, fptr(londonLat), fptr(londonLon), fptr(nyLat), fptr(nyLon))
		if pl.StartTZ == nil || *pl.StartTZ != "Europe/London" {
			t.Errorf("StartTZ = %v, want Europe/London", pl.StartTZ)
		}
		if pl.EndTZ == nil || *pl.EndTZ != "America/New_York" {
			t.Errorf("EndTZ = %v, want America/New_York", pl.EndTZ)
		}
		// Wall-clock preserved: 09:00 London (BST, UTC+1) -> 08:00Z.
		if pl.StartsAt == nil || !pl.StartsAt.UTC().Equal(time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)) {
			t.Errorf("StartsAt = %v, want 08:00Z", pl.StartsAt)
		}
		// 17:00 New York (EDT, UTC-4) -> 21:00Z.
		if pl.EndsAt == nil || !pl.EndsAt.UTC().Equal(time.Date(2026, 6, 1, 21, 0, 0, 0, time.UTC)) {
			t.Errorf("EndsAt = %v, want 21:00Z", pl.EndsAt)
		}
	})

	t.Run("end with no own coords inherits the start (primary) tz", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends}
		var pl store.UpdatePlanPartPayload
		// Only start coords supplied; end inherits primary (London).
		ResolvePartTZ(p, &pl, fptr(londonLat), fptr(londonLon), nil, nil)
		if pl.EndTZ == nil || *pl.EndTZ != "Europe/London" {
			t.Errorf("EndTZ = %v, want inherited Europe/London", pl.EndTZ)
		}
	})

	t.Run("primary falls back to end coords when start absent", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, nil, nil, fptr(nyLat), fptr(nyLon))
		if pl.StartTZ == nil || *pl.StartTZ != "America/New_York" {
			t.Errorf("StartTZ = %v, want America/New_York (from end coords)", pl.StartTZ)
		}
	})

	t.Run("existing tz left untouched", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends, StartTZ: "Europe/Paris", EndTZ: "Europe/Paris"}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, fptr(londonLat), fptr(londonLon), fptr(nyLat), fptr(nyLon))
		if pl.StartTZ != nil || pl.EndTZ != nil || pl.StartsAt != nil || pl.EndsAt != nil {
			t.Errorf("payload mutated despite existing tz: %+v", pl)
		}
	})

	t.Run("no usable coordinate is a no-op", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, nil, nil, nil, nil)
		if !pl.IsEmpty() {
			t.Errorf("expected empty payload, got %+v", pl)
		}
	})

	t.Run("nil EndsAt leaves the end tz alone", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts} // EndsAt nil
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, fptr(londonLat), fptr(londonLon), nil, nil)
		if pl.EndTZ != nil || pl.EndsAt != nil {
			t.Errorf("end fields set despite nil EndsAt: %+v", pl)
		}
		if pl.StartTZ == nil {
			t.Error("start tz should still be resolved")
		}
	})
}

// Endpoint is a thin exported wrapper over geocodeEndpoint; verify it forwards
// through the shared chain.
func TestEndpointWrapper(t *testing.T) {
	g := stubGeo{resolves: map[string][2]float64{"1 Main St": {1, 2}}}
	lat, lon, ok := Endpoint(context.Background(), g, "hotel", "1 Main St", "Hotel")
	if !ok || lat != 1 || lon != 2 {
		t.Errorf("Endpoint = (%v,%v,%v), want (1,2,true)", lat, lon, ok)
	}
}

// isAllDigits's empty-string guard (the only uncovered branch) returns false.
func TestIsAllDigitsEmpty(t *testing.T) {
	if isAllDigits("") {
		t.Error("isAllDigits(\"\") = true, want false")
	}
}
