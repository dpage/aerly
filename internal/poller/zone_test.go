package poller

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
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
// on leaves the caller on its own UTC fallback rather than inventing a zone.
func TestAirportZone_Unresolvable(t *testing.T) {
	if got := airportZone("", nil, nil); got != "" {
		t.Fatalf("empty input zone = %q, want \"\"", got)
	}
	if got := airportZone("ZZZ", nil, f64(-4.99)); got != "" {
		t.Fatalf("half a coordinate should not resolve, got %q", got)
	}
}

// TestReminderZone_StoredWins: a part that recorded its own zone is authoritative;
// we must not second-guess it from the airport table.
func TestReminderZone_StoredWins(t *testing.T) {
	d := store.DueReminder{StartTZ: "America/Denver", OriginIATA: "LHR"}
	if got := reminderZone(d); got != "America/Denver" {
		t.Fatalf("reminderZone = %q, want America/Denver", got)
	}
}

// TestReminderZone_Fallbacks: with no stored zone the flight's departure airport
// answers, and a non-flight part falls back to its geocoded coordinate.
func TestReminderZone_Fallbacks(t *testing.T) {
	if got := reminderZone(store.DueReminder{OriginIATA: "JFK"}); got != "America/New_York" {
		t.Fatalf("flight fallback = %q, want America/New_York", got)
	}
	hotel := store.DueReminder{StartLat: f64(48.2082), StartLon: f64(16.3738)} // Vienna
	if got := reminderZone(hotel); got != "Europe/Vienna" {
		t.Fatalf("coordinate fallback = %q, want Europe/Vienna", got)
	}
	if got := reminderZone(store.DueReminder{}); got != "" {
		t.Fatalf("bare reminder zone = %q, want \"\"", got)
	}
}

// TestInZone covers the render helper's three cases: a known zone, an empty
// name, and a name the zone database doesn't have — the last two both landing
// on UTC so a bad zone degrades rather than panics.
func TestInZone(t *testing.T) {
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	if got := inZone(at, "Europe/London").Format("15:04 MST"); got != "10:00 BST" {
		t.Fatalf("London render = %q, want 10:00 BST", got)
	}
	if got := inZone(at, "").Format("15:04 MST"); got != "09:00 UTC" {
		t.Fatalf("empty-zone render = %q, want 09:00 UTC", got)
	}
	if got := inZone(at, "Mars/Olympus_Mons").Format("15:04 MST"); got != "09:00 UTC" {
		t.Fatalf("unknown-zone render = %q, want 09:00 UTC", got)
	}
}
