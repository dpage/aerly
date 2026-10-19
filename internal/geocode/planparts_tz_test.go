package geocode

import (
	"context"
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

func TestResolveFlightTZ(t *testing.T) {
	out := time.Date(2026, 6, 1, 18, 50, 0, 0, time.UTC)
	in := out.Add(8 * time.Hour)
	fd := &store.FlightDetail{OriginIATA: "LHR", DestIATA: "JFK"}

	t.Run("nil detail leaves the part to the coordinate pass", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: out, EndsAt: &in, StartTZSource: unknown, EndTZSource: unknown}
		var pl store.UpdatePlanPartPayload
		if got := ResolveFlightTZ(p, nil, &pl); got != p || !pl.IsEmpty() {
			t.Errorf("nil detail changed something: %+v %+v", got, pl)
		}
	})

	t.Run("unknown ends take their own airport zone, confirmed, instant kept", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: out, EndsAt: &in, StartTZSource: unknown, EndTZSource: unknown}
		var pl store.UpdatePlanPartPayload
		view := ResolveFlightTZ(p, fd, &pl)
		if pl.StartTZ == nil || *pl.StartTZ != "Europe/London" || *pl.StartTZSource != confirmed {
			t.Errorf("start = %v/%v, want Europe/London confirmed", pl.StartTZ, pl.StartTZSource)
		}
		if pl.EndTZ == nil || *pl.EndTZ != "America/New_York" || *pl.EndTZSource != confirmed {
			t.Errorf("end = %v/%v, want America/New_York confirmed", pl.EndTZ, pl.EndTZSource)
		}
		if pl.StartsAt != nil || pl.EndsAt != nil {
			t.Errorf("a true instant was re-anchored: %v %v", pl.StartsAt, pl.EndsAt)
		}
		if view.StartTZSource != confirmed || view.EndTZSource != confirmed || p.StartTZSource != unknown {
			t.Errorf("view not settled, or the stored part mutated: %+v / %+v", view, p)
		}
	})

	t.Run("an off-table arrival is not given the departure's coordinate zone", func(t *testing.T) {
		p := &store.PlanPart{
			StartsAt: out, EndsAt: &in, StartTZSource: unknown, EndTZSource: unknown,
			StartLat: fptr(londonLat), StartLon: fptr(londonLon),
		}
		var pl store.UpdatePlanPartPayload
		view := ResolveFlightTZ(p, &store.FlightDetail{OriginIATA: "ZZZ", DestIATA: "ZZY"}, &pl)
		// The start falls back to its own coordinates.
		if pl.StartTZ == nil || *pl.StartTZ != "Europe/London" {
			t.Errorf("StartTZ = %v, want Europe/London from its coordinates", pl.StartTZ)
		}
		ResolvePartTZ(view, &pl, p.StartLat, p.StartLon, p.EndLat, p.EndLon)
		if pl.EndTZ != nil {
			t.Errorf("arrival given %q, want left unknown", *pl.EndTZ)
		}
	})

	t.Run("floating ends are anchored at their own airports, not the departure's coordinates", func(t *testing.T) {
		// Coordinates on the departure only: the arrival must take JFK's zone,
		// not London's from the start coordinate.
		p := &store.PlanPart{
			StartsAt: out, EndsAt: &in, StartTZSource: floating, EndTZSource: floating,
			StartLat: fptr(londonLat), StartLon: fptr(londonLon),
		}
		var pl store.UpdatePlanPartPayload
		view := ResolveFlightTZ(p, fd, &pl)
		ResolvePartTZ(view, &pl, p.StartLat, p.StartLon, p.EndLat, p.EndLon)
		if pl.StartTZ == nil || *pl.StartTZ != "Europe/London" || *pl.StartTZSource != confirmed {
			t.Errorf("start = %v/%v, want Europe/London confirmed", pl.StartTZ, pl.StartTZSource)
		}
		if pl.EndTZ == nil || *pl.EndTZ != "America/New_York" || *pl.EndTZSource != confirmed {
			t.Errorf("end = %v/%v, want America/New_York confirmed", pl.EndTZ, pl.EndTZSource)
		}
		// 18:50 BST is 17:50Z; 02:50 EDT is 06:50Z.
		if pl.StartsAt == nil || !pl.StartsAt.Equal(out.Add(-time.Hour)) {
			t.Errorf("StartsAt = %v, want %v", pl.StartsAt, out.Add(-time.Hour))
		}
		if pl.EndsAt == nil || !pl.EndsAt.Equal(in.Add(4*time.Hour)) {
			t.Errorf("EndsAt = %v, want %v", pl.EndsAt, in.Add(4*time.Hour))
		}

		// A second pass sees confirmed ends and changes nothing.
		next := *p
		next.StartsAt, next.EndsAt = *pl.StartsAt, pl.EndsAt
		next.StartTZ, next.EndTZ = *pl.StartTZ, *pl.EndTZ
		next.StartTZSource, next.EndTZSource = confirmed, confirmed
		var again store.UpdatePlanPartPayload
		ResolvePartTZ(ResolveFlightTZ(&next, fd, &again), &again, next.StartLat, next.StartLon, nil, nil)
		if !again.IsEmpty() {
			t.Errorf("second pass changed something: %+v", again)
		}
	})

	t.Run("a floating end at an airport that cannot be placed stays floating", func(t *testing.T) {
		p := &store.PlanPart{
			StartsAt: out, EndsAt: &in, StartTZSource: floating, EndTZSource: floating,
			StartLat: fptr(londonLat), StartLon: fptr(londonLon),
		}
		var pl store.UpdatePlanPartPayload
		view := ResolveFlightTZ(p, &store.FlightDetail{OriginIATA: "LHR", DestIATA: "ZZZ"}, &pl)
		ResolvePartTZ(view, &pl, p.StartLat, p.StartLon, p.EndLat, p.EndLon)
		if pl.StartTZ == nil || *pl.StartTZ != "Europe/London" {
			t.Errorf("StartTZ = %v, want Europe/London", pl.StartTZ)
		}
		if pl.EndTZ != nil || pl.EndTZSource != nil || pl.EndsAt != nil {
			t.Errorf("off-table arrival touched: %v %v %v", pl.EndTZ, pl.EndTZSource, pl.EndsAt)
		}
		if view.EndTZSource != confirmed || p.EndTZSource != floating {
			t.Errorf("arrival not settled for the coordinate pass, or the stored part mutated")
		}
	})

	t.Run("a floating no-arrival end takes the departure's anchoring", func(t *testing.T) {
		same := out
		p := &store.PlanPart{StartsAt: out, EndsAt: &same, StartTZSource: floating, EndTZSource: floating}
		var pl store.UpdatePlanPartPayload
		ResolveFlightTZ(p, fd, &pl)
		if pl.EndTZ == nil || *pl.EndTZ != "Europe/London" || pl.EndsAt == nil || !pl.EndsAt.Equal(*pl.StartsAt) {
			t.Errorf("end = %v %v, want the departure's Europe/London %v", pl.EndTZ, pl.EndsAt, pl.StartsAt)
		}
		if pl.EndsAt == pl.StartsAt {
			t.Error("end shares the start's pointer")
		}

		// When the departure cannot be placed the arrival stays floating too,
		// rather than being pulled to the destination's zone.
		var off store.UpdatePlanPartPayload
		ResolveFlightTZ(p, &store.FlightDetail{OriginIATA: "ZZZ", DestIATA: "JFK"}, &off)
		if !off.IsEmpty() {
			t.Errorf("unplaced departure: %+v, want nothing written", off)
		}
	})

	t.Run("a missing end is left alone", func(t *testing.T) {
		p := &store.PlanPart{StartsAt: out, StartTZSource: confirmed, StartTZ: "Europe/London", EndTZSource: unknown}
		var pl store.UpdatePlanPartPayload
		view := ResolveFlightTZ(p, fd, &pl)
		if !pl.IsEmpty() || view.EndTZSource != unknown {
			t.Errorf("touched an absent end: %+v %+v", pl, view)
		}
	})
}

// TestFlightEndZoneUnplaced: an airport neither the table nor coordinates can
// place gives no zone.
func TestFlightEndZoneUnplaced(t *testing.T) {
	if _, _, ok := flightEndZone(time.Now(), floating, "ZZZ", nil, nil); ok {
		t.Error("flightEndZone placed an unknown airport")
	}
}

// TestSyncFlightScheduleSkips covers the cases that write nothing, which need
// no store: no detail, a provider resolved flight, no re-anchored time, no
// written part, and a schedule already in line.
func TestSyncFlightScheduleSkips(t *testing.T) {
	out := time.Date(2026, 6, 1, 17, 50, 0, 0, time.UTC)
	in := out.Add(9 * time.Hour)
	moved := store.UpdatePlanPartPayload{StartsAt: &out}
	part := &store.PlanPart{ID: 1, StartsAt: out, EndsAt: &in}
	inLine := &store.FlightDetail{ScheduledOut: out, ScheduledIn: in}
	for name, c := range map[string]struct {
		fd      *store.FlightDetail
		payload store.UpdatePlanPartPayload
		part    *store.PlanPart
	}{
		"no detail":        {nil, moved, part},
		"resolved":         {&store.FlightDetail{Resolved: true}, moved, part},
		"nothing anchored": {inLine, store.UpdatePlanPartPayload{}, part},
		"no written part":  {inLine, moved, nil},
		"schedule in line": {inLine, moved, part},
	} {
		if err := SyncFlightSchedule(context.Background(), nil, c.fd, c.payload, c.part); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
