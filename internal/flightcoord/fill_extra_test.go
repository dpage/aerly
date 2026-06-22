package flightcoord

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

func TestStrPtrIfSet(t *testing.T) {
	if p := strPtrIfSet("  abc  "); p == nil || *p != "abc" {
		t.Errorf("strPtrIfSet trims and returns a pointer, got %v", p)
	}
	if p := strPtrIfSet("   "); p != nil {
		t.Errorf("strPtrIfSet(blank) = %v, want nil", p)
	}
	if p := strPtrIfSet(""); p != nil {
		t.Errorf("strPtrIfSet(empty) = %v, want nil", p)
	}
}

func TestAirportCoords(t *testing.T) {
	// Provider coords present: used verbatim, no clear.
	lat, lon, clear := AirportCoords("ZZZ", 12.5, -3.5)
	if lat == nil || lon == nil || *lat != 12.5 || *lon != -3.5 || clear {
		t.Errorf("provider coords: got (%v,%v,clear=%v), want (12.5,-3.5,false)", lat, lon, clear)
	}
	// No provider coords but the code is in the embedded table: table value.
	lat, lon, clear = AirportCoords("BRS", 0, 0)
	if lat == nil || lon == nil || clear {
		t.Errorf("table fallback: expected coords with clear=false, got (%v,%v,%v)", lat, lon, clear)
	}
	// Neither provider nor table: signal a clear so a stale pin is dropped.
	lat, lon, clear = AirportCoords("ZZZ", 0, 0)
	if lat != nil || lon != nil || !clear {
		t.Errorf("off-table no-coords: want (nil,nil,true), got (%v,%v,%v)", lat, lon, clear)
	}
}

func TestRouteUpdateFromResolved(t *testing.T) {
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	rf := &providers.ResolvedFlight{
		OriginIATA: " lhr ", DestIATA: "jfk", OriginName: "Heathrow", DestName: "Kennedy",
		ICAO24: "abc123", Callsign: "BAW123", OriginGate: "A1", DestGate: "B2",
		OriginTerminal: "5", DestTerminal: "4", AircraftType: "B772",
		ScheduledOut: out, ScheduledIn: in,
	}
	up := RouteUpdateFromResolved(rf)
	if up.Resolved == nil || !*up.Resolved {
		t.Error("Resolved should be true")
	}
	// Codes are upper-cased and trimmed.
	if up.OriginIATA == nil || *up.OriginIATA != "LHR" || up.DestIATA == nil || *up.DestIATA != "JFK" {
		t.Errorf("codes = %v/%v, want LHR/JFK", deref(up.OriginIATA), deref(up.DestIATA))
	}
	if up.ICAO24 == nil || *up.ICAO24 != "abc123" || up.AircraftType == nil || *up.AircraftType != "B772" {
		t.Error("airframe fields not carried")
	}
	// Schedule mirrored into the part instants.
	if up.ScheduledOut == nil || up.StartsAt == nil || !up.StartsAt.Equal(out) {
		t.Errorf("StartsAt should mirror ScheduledOut")
	}
	if up.ScheduledIn == nil || up.EndsAt == nil || !up.EndsAt.Equal(in) {
		t.Errorf("EndsAt should mirror ScheduledIn")
	}
	// LHR and JFK are in the embedded table, so timezones resolve.
	if up.StartTZ == nil || up.EndTZ == nil {
		t.Errorf("expected timezones for table airports, got start=%v end=%v", up.StartTZ, up.EndTZ)
	}
	// No provider coords, but table-known: coords filled, not cleared.
	if up.StartLat == nil || up.ClearStartCoords {
		t.Errorf("expected table coords for LHR with no clear, got lat=%v clear=%v", up.StartLat, up.ClearStartCoords)
	}
	if up.StartLabel == nil || *up.StartLabel == "" {
		t.Error("StartLabel should be the friendly Name (CODE) form")
	}
}

func TestRouteUpdateFromResolvedZeroScheduleAndBlankAirframe(t *testing.T) {
	// Zero schedule times are skipped; blank airframe strings become nil pointers.
	rf := &providers.ResolvedFlight{OriginIATA: "ZZZ", DestIATA: "QQQ"}
	up := RouteUpdateFromResolved(rf)
	if up.ScheduledOut != nil || up.StartsAt != nil || up.ScheduledIn != nil || up.EndsAt != nil {
		t.Error("zero schedule times should leave the instant pointers nil")
	}
	if up.ICAO24 != nil || up.Callsign != nil || up.AircraftType != nil {
		t.Error("blank airframe strings should map to nil pointers")
	}
	// Off-table codes with no provider coords: timezones absent, coords cleared.
	if up.StartTZ != nil || up.EndTZ != nil {
		t.Error("off-table codes should have no timezone")
	}
	if !up.ClearStartCoords || !up.ClearEndCoords {
		t.Error("off-table codes with no coords should signal a clear")
	}
}

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// errBackfiller fails the BackfillFlightPart write so the error-return path of
// Fill is exercised.
type errBackfiller struct{ fakeBackfiller }

func (e *errBackfiller) BackfillFlightPart(context.Context, int64, store.BackfillPayload) error {
	return errors.New("write failed")
}

// The flight lookup fills an off-table ORIGIN leg (the mirror of the existing
// dest-leg tests), exercising the origin merge and label branches.
func TestFill_ResolverFillsOffTableOriginLeg(t *testing.T) {
	st := &fakeBackfiller{}
	r := &fakeResolver{rf: &providers.ResolvedFlight{
		OriginIATA: "AAA", OriginLat: 10.5, OriginLon: 20.5, OriginName: "Alpha",
		DestIATA: "BRS",
	}}
	// Origin ZZZ is off-table (needs the resolver); dest BRS is table-known.
	f := &store.Flight{ID: 20, Ident: "FR1", OriginIATA: "ZZZ", DestIATA: "BRS"}

	changed, err := Fill(context.Background(), st, r, nil, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if !changed || st.backfilled == nil || st.backfilled.OriginLat != 10.5 {
		t.Errorf("expected resolver origin coords, got %+v", st.backfilled)
	}
	if st.startLabel != "Alpha (AAA)" {
		t.Errorf("origin label = %q, want Alpha (AAA)", st.startLabel)
	}
}

// A transient (non not-found) airport-resolver error is logged and left for the
// next sweep, not fatal.
func TestFill_AirportResolverTransientErrorWarned(t *testing.T) {
	st := &fakeBackfiller{}
	// Use ErrFlightNotFound so the airport fallback runs, then have the airport
	// resolver itself fail transiently.
	rNF := &fakeResolver{err: providers.ErrFlightNotFound}
	ar := &fakeAirportResolver{err: errors.New("airport api down")}
	f := &store.Flight{ID: 21, Ident: "FR2", OriginIATA: "ZZZ", DestIATA: "BRS"}

	changed, err := Fill(context.Background(), st, rNF, ar, f, time.Now())
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if ar.calls != 1 {
		t.Errorf("airport resolver should be tried once for the off-table origin, got %d", ar.calls)
	}
	// The dest (BRS) still table-fills, so the row changes; the origin stays NULL.
	if !changed || st.backfilled == nil {
		t.Errorf("dest BRS should still table-fill, got changed=%v", changed)
	}
	if st.resolved != 0 {
		t.Errorf("a failed airport resolve must not mark resolved, got %d", st.resolved)
	}
}

func TestFill_BackfillWriteErrorPropagates(t *testing.T) {
	st := &errBackfiller{}
	f := &store.Flight{ID: 99, Ident: "EZY1", OriginIATA: "BRS", DestIATA: "BRS"}
	changed, err := Fill(context.Background(), st, nil, nil, f, time.Now())
	if err == nil {
		t.Fatal("expected the backfill write error to propagate")
	}
	if changed {
		t.Error("changed should be false when the write fails")
	}
}
