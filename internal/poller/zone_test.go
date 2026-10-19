package poller

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

func f64(v float64) *float64 { return &v }

// TestAirportZone_FromTable: an on-table IATA code resolves straight from the
// embedded airport table, without consulting coordinates at all.
func TestAirportZone_FromTable(t *testing.T) {
	if got := airportZone("LHR", nil, nil); got != "Europe/London" {
		t.Fatalf("LHR zone = %q, want Europe/London", got)
	}
}

// TestAirportZone_FromCoords: an off-table code falls through to the coordinate
// the part carries, which is how the airports the embedded table doesn't name
// still get a real zone.
func TestAirportZone_FromCoords(t *testing.T) {
	// Newquay (NQY) — deliberately off-table, per the airports package doc.
	if got := airportZone("NQY", f64(50.4406), f64(-4.9954)); got != "Europe/London" {
		t.Fatalf("NQY zone = %q, want Europe/London", got)
	}
}

// TestAirportZone_Unresolvable: an unknown code with no coordinate to fall back
// on leaves the caller to label the time unknown rather than inventing a zone.
func TestAirportZone_Unresolvable(t *testing.T) {
	if got := airportZone("", nil, nil); got != "" {
		t.Fatalf("empty input zone = %q, want \"\"", got)
	}
	if got := airportZone("ZZZ", nil, f64(-4.99)); got != "" {
		t.Fatalf("half a coordinate should not resolve, got %q", got)
	}
}

// TestLabelDay: the date is judged in the zone the block is labelled in, so a
// 23:30 UTC departure is already the next day in Tokyo, a floating block keeps
// its UTC digits whatever the home zone, and an unknown block follows the home
// zone, or UTC without one or with one that does not exist.
func TestLabelDay(t *testing.T) {
	at := time.Date(2026, 8, 31, 23, 30, 0, 0, time.UTC)
	cases := []struct {
		name string
		b    timing.Block
		home string
		want string
	}{
		{"zone", timing.Block{TZ: "Asia/Tokyo", Source: timing.Confirmed}, "", "2026-09-01"},
		{"zone London", timing.Block{TZ: "Europe/London", Source: timing.Confirmed}, "Asia/Tokyo", "2026-09-01"},
		{"zone New York", timing.Block{TZ: "America/New_York", Source: timing.Confirmed}, "Asia/Tokyo", "2026-08-31"},
		{"floating ignores home", timing.Block{Source: timing.Floating}, "Asia/Tokyo", "2026-08-31"},
		{"unknown uses home", timing.Block{Source: timing.Unknown}, "Asia/Tokyo", "2026-09-01"},
		{"unknown blank home", timing.Block{Source: timing.Unknown}, "", "2026-08-31"},
		{"unknown invalid home", timing.Block{Source: timing.Unknown}, "Mars/Olympus_Mons", "2026-08-31"},
	}
	for _, c := range cases {
		// The block's own At is ignored: the date is of t.
		c.b.At = at.AddDate(1, 0, 0)
		if got := labelDay(at, c.b, c.home); got != c.want {
			t.Errorf("%s: labelDay = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestFlightZone: an airport placed by the table or its own coordinates is a
// confirmed zone; one that cannot be placed is unknown.
func TestFlightZone(t *testing.T) {
	if got := flightZone("LHR", nil, nil); got != (endZone{TZ: "Europe/London", Source: timing.Confirmed}) {
		t.Errorf("table = %+v", got)
	}
	if got := flightZone("NQY", f64(50.4406), f64(-4.9954)); got != (endZone{TZ: "Europe/London", Source: timing.Confirmed}) {
		t.Errorf("coords = %+v", got)
	}
	if got := flightZone("ZZZ", nil, nil); got != (endZone{Source: timing.Unknown}) {
		t.Errorf("unplaceable = %+v", got)
	}
}

// TestStartZone: a stored departure zone keeps the source it was stored with,
// rather than being relabelled confirmed; without one the airport's is used.
func TestStartZone(t *testing.T) {
	if got := startZone("Europe/Paris", "inferred", "LHR", nil, nil); got != (endZone{TZ: "Europe/Paris", Source: timing.Inferred}) {
		t.Errorf("stored inferred = %+v", got)
	}
	if got := startZone("", "unknown", "LHR", nil, nil); got != (endZone{TZ: "Europe/London", Source: timing.Confirmed}) {
		t.Errorf("no stored zone = %+v", got)
	}
	// A floating start is wall-clock digits: it is not given its airport's zone.
	if got := startZone("", "floating", "LHR", nil, nil); got != (endZone{Source: timing.Floating}) {
		t.Errorf("floating = %+v", got)
	}
}

func TestInstantBlockCarriesSource(t *testing.T) {
	at := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	b := instantBlock(at, timing.Estimated, endZone{TZ: "Europe/Paris", Source: timing.Inferred}, "CDG")
	if b.Source != timing.Inferred || b.TZ != "Europe/Paris" || b.Kind != timing.Estimated || b.Place == "" {
		t.Errorf("zoned = %+v", b)
	}
	if b := instantBlock(at, timing.Scheduled, endZone{Source: timing.Unknown}, "ZZZ"); b.Source != timing.Unknown || b.TZ != "" {
		t.Errorf("zoneless = %+v", b)
	}
	if b := instantBlock(at, timing.Scheduled, endZone{Source: timing.Floating}, "LHR"); b.Source != timing.Floating || b.TZ != "" {
		t.Errorf("floating = %+v", b)
	}
	if b := instantBlock(at, timing.Scheduled, endZone{}, "LHR"); b.Source != timing.Unknown {
		t.Errorf("blank source = %+v", b)
	}
}

// A floating start's scheduled time is wall-clock digits, but an estimate or
// actual is a true instant and is labelled in the airport's zone.
func TestDepartureZone_FloatingByKind(t *testing.T) {
	if got := departureZone(timing.Scheduled, "", "floating", "LHR", nil, nil); got != (endZone{Source: timing.Floating}) {
		t.Errorf("scheduled = %+v", got)
	}
	want := endZone{TZ: "Europe/London", Source: timing.Confirmed}
	for _, k := range []timing.Kind{timing.Estimated, timing.Actual, timing.Derived} {
		if got := departureZone(k, "", "floating", "LHR", nil, nil); got != want {
			t.Errorf("kind %v = %+v", k, got)
		}
	}
	if got := departureZone(timing.Estimated, "", "floating", "ZZZ", nil, nil); got != (endZone{Source: timing.Unknown}) {
		t.Errorf("unplaceable airport = %+v", got)
	}
	if got := departureZone(timing.Estimated, "Europe/Paris", "inferred", "LHR", nil, nil); got != (endZone{TZ: "Europe/Paris", Source: timing.Inferred}) {
		t.Errorf("non-floating = %+v", got)
	}
}
