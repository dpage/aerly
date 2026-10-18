package geocode

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// fptr is a small helper for the *float64 fields the geocode payloads use.
func fptr(f float64) *float64 { return &f }

// London and New York coordinates resolve to known IANA zones via the embedded
// tzf boundary data. They're public city coordinates, not personal data.
const (
	londonLat, londonLon = 51.5074, -0.1278
	nyLat, nyLon         = 40.7128, -74.0060
	parisLat, parisLon   = 48.8566, 2.3522
)

// Source values as the store carries them.
var (
	floating  = string(timing.Floating)
	unknown   = string(timing.Unknown)
	confirmed = string(timing.Confirmed)
)

func TestResolvePartTZ(t *testing.T) {
	starts := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)

	t.Run("anchors start and end from their own coords", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends, StartTZSource: floating, EndTZSource: floating}
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
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends, StartTZSource: floating, EndTZSource: floating}
		var pl store.UpdatePlanPartPayload
		// Only start coords supplied; end inherits primary (London).
		ResolvePartTZ(p, &pl, fptr(londonLat), fptr(londonLon), nil, nil)
		if pl.EndTZ == nil || *pl.EndTZ != "Europe/London" {
			t.Errorf("EndTZ = %v, want inherited Europe/London", pl.EndTZ)
		}
	})

	t.Run("primary falls back to end coords when start absent", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, StartTZSource: floating, EndTZSource: unknown}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, nil, nil, fptr(nyLat), fptr(nyLon))
		if pl.StartTZ == nil || *pl.StartTZ != "America/New_York" {
			t.Errorf("StartTZ = %v, want America/New_York (from end coords)", pl.StartTZ)
		}
	})

	t.Run("resolved flight with unknown ends gains zones but keeps its instants", func(t *testing.T) {
		// Its times are already real UTC; the migration marks its empty ends
		// unknown, so the source rule alone keeps the instant.
		p := &store.PlanPart{Type: "flight", FlightResolved: true, StartsAt: starts, EndsAt: &ends,
			StartTZSource: unknown, EndTZSource: unknown}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, fptr(londonLat), fptr(londonLon), fptr(nyLat), fptr(nyLon))
		if pl.StartTZ == nil || *pl.StartTZ != "Europe/London" {
			t.Errorf("StartTZ = %v, want Europe/London", pl.StartTZ)
		}
		if pl.EndTZ == nil || *pl.EndTZ != "America/New_York" {
			t.Errorf("EndTZ = %v, want America/New_York", pl.EndTZ)
		}
		if pl.StartTZSource == nil || *pl.StartTZSource != confirmed || pl.EndTZSource == nil || *pl.EndTZSource != confirmed {
			t.Errorf("sources = %v/%v, want confirmed", pl.StartTZSource, pl.EndTZSource)
		}
		if pl.StartsAt != nil || pl.EndsAt != nil {
			t.Errorf("instants rewritten for a resolved flight: start=%v end=%v", pl.StartsAt, pl.EndsAt)
		}
	})

	t.Run("existing tz left untouched", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends, StartTZ: "Europe/Paris", EndTZ: "Europe/Paris",
			StartTZSource: confirmed, EndTZSource: confirmed}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, fptr(londonLat), fptr(londonLon), fptr(nyLat), fptr(nyLon))
		if pl.StartTZ != nil || pl.EndTZ != nil || pl.StartsAt != nil || pl.EndsAt != nil {
			t.Errorf("payload mutated despite existing tz: %+v", pl)
		}
	})

	t.Run("no usable coordinate is a no-op", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, EndsAt: &ends, StartTZSource: floating, EndTZSource: floating}
		var pl store.UpdatePlanPartPayload
		ResolvePartTZ(p, &pl, nil, nil, nil, nil)
		if !pl.IsEmpty() {
			t.Errorf("expected empty payload, got %+v", pl)
		}
	})

	t.Run("nil EndsAt leaves the end tz alone", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: starts, StartTZSource: floating, EndTZSource: floating} // EndsAt nil
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

// TestResolvePartTZSources covers the per-source rules: only a floating end
// is re-anchored, an unknown end gains its zone and keeps its instant, and a
// confirmed end is left alone. Dates are in July, when Paris is UTC+2.
func TestResolvePartTZSources(t *testing.T) {
	in := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
	out := time.Date(2026, 7, 3, 11, 0, 0, 0, time.UTC)
	paris := "Europe/Paris"
	cases := []struct {
		name             string
		part             store.PlanPart
		endLat, endLon   *float64
		wantStartTZ      *string
		wantStartSrc     *string
		wantStartsAt     *time.Time
		wantEndTZ        *string
		wantEndSrc       *string
		wantEndsAt       *time.Time
		wantEmptyPayload bool
	}{
		{
			name:         "floating start is anchored",
			part:         store.PlanPart{StartsAt: in, StartTZSource: floating, EndTZSource: unknown},
			wantStartTZ:  &paris,
			wantStartSrc: &confirmed,
			wantStartsAt: ptrTime(time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC)),
		},
		{
			name:         "unknown start gains the zone and keeps its instant",
			part:         store.PlanPart{StartsAt: in, StartTZSource: unknown, EndTZSource: unknown},
			wantStartTZ:  &paris,
			wantStartSrc: &confirmed,
		},
		{
			name: "confirmed start is left alone",
			part: store.PlanPart{StartsAt: in, StartTZ: "Europe/London", StartTZSource: confirmed,
				EndTZSource: unknown},
			wantEmptyPayload: true,
		},
		{
			name: "inferred ends are left alone",
			part: store.PlanPart{StartsAt: in, EndsAt: &out, StartTZ: "Europe/London", EndTZ: "Europe/London",
				StartTZSource: string(timing.Inferred), EndTZSource: string(timing.Inferred)},
			wantEmptyPayload: true,
		},
		{
			name: "floating hotel checkout with no end coords inherits the primary zone and anchors",
			part: store.PlanPart{Type: "hotel", StartsAt: in, EndsAt: &out, StartTZ: paris,
				StartTZSource: confirmed, EndTZSource: floating},
			wantEndTZ:  &paris,
			wantEndSrc: &confirmed,
			wantEndsAt: ptrTime(time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)),
		},
		{
			name: "unknown end with its own coords keeps its instant",
			part: store.PlanPart{StartsAt: in, EndsAt: &out, StartTZ: paris,
				StartTZSource: confirmed, EndTZSource: unknown},
			endLat: fptr(nyLat), endLon: fptr(nyLon),
			wantEndTZ:  ptrString("America/New_York"),
			wantEndSrc: &confirmed,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.part
			var pl store.UpdatePlanPartPayload
			ResolvePartTZ(&p, &pl, fptr(parisLat), fptr(parisLon), c.endLat, c.endLon)
			if c.wantEmptyPayload {
				if !pl.IsEmpty() {
					t.Fatalf("payload = %+v, want empty", pl)
				}
				return
			}
			checkStr(t, "StartTZ", pl.StartTZ, c.wantStartTZ)
			checkStr(t, "StartTZSource", pl.StartTZSource, c.wantStartSrc)
			checkTime(t, "StartsAt", pl.StartsAt, c.wantStartsAt)
			checkStr(t, "EndTZ", pl.EndTZ, c.wantEndTZ)
			checkStr(t, "EndTZSource", pl.EndTZSource, c.wantEndSrc)
			checkTime(t, "EndsAt", pl.EndsAt, c.wantEndsAt)
		})
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
func ptrString(s string) *string     { return &s }

func checkStr(t *testing.T, field string, got, want *string) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %q, want unset", field, *got)
	case want != nil && (got == nil || *got != *want):
		t.Errorf("%s = %v, want %q", field, got, *want)
	}
}

func checkTime(t *testing.T, field string, got, want *time.Time) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %v, want unset (instant kept)", field, got.UTC())
	case want != nil && (got == nil || !got.UTC().Equal(*want)):
		t.Errorf("%s = %v, want %v", field, got, want.UTC())
	}
}

// isAllDigits's empty-string guard (the only uncovered branch) returns false.
func TestIsAllDigitsEmpty(t *testing.T) {
	if isAllDigits("") {
		t.Error("isAllDigits(\"\") = true, want false")
	}
}
