package tripitics

import (
	"os"
	"strings"
	"testing"
)

// These tests run against real TripIt .ics exports (Dave's own shared trips,
// provided as samples) so the parser is pinned to the actual format rather than
// a synthesized guess. They assert the structural facts the event->plan mapper
// will depend on.

func parseFixture(t *testing.T, name string) *Calendar {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	cal, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse %s: %v", name, err)
	}
	return cal
}

func TestRealPGConfEU2016(t *testing.T) {
	cal := parseFixture(t, "pgconfeu_2016.ics")

	// TripIt's trip name lives in the X-WR-CAL* headers, not PRODID (Bennu).
	if cal.Desc != "PGConf.EU 2016 (Trip Shared by Dave Page)" {
		t.Errorf("Desc = %q", cal.Desc)
	}
	if cal.Name == "" {
		t.Error("expected X-WR-CALNAME to be captured")
	}

	// Envelope (date-only span) + taxi + 2 flights out + hotel in/out + 2 flights
	// back + taxi = 9 events.
	if len(cal.Events) != 9 {
		t.Fatalf("got %d events, want 9", len(cal.Events))
	}

	// Event 1 is the whole-trip envelope: date-only, UID without the item- prefix.
	env := cal.Events[0]
	if env.Start.HasTime || env.Start.Time.Format("2006-01-02") != "2016-10-30" {
		t.Errorf("envelope start = %+v, want date-only 2016-10-30", env.Start)
	}
	if got := localUID(env.UID); got == "item" {
		t.Errorf("envelope UID %q should not be an item- UID", env.UID)
	}

	// A flight leg: SUMMARY carries ident + IATA route; DTSTART/DTEND are UTC.
	var flight *Event
	for i := range cal.Events {
		if cal.Events[i].Summary == "AY832 LHR to HEL" {
			flight = &cal.Events[i]
			break
		}
	}
	if flight == nil {
		t.Fatal("did not find the AY832 LHR to HEL flight event")
	}
	if !flight.Start.IsUTC || flight.Start.Time.Format("2006-01-02T15:04:05Z") != "2016-10-30T10:20:00Z" {
		t.Errorf("flight start = %+v, want UTC 2016-10-30T10:20:00Z", flight.Start)
	}
	if !flight.End.IsUTC || flight.End.Time.Format("15:04") != "13:15" {
		t.Errorf("flight end = %+v, want UTC 13:15", flight.End)
	}

	// Hotel arrives as two events whose SUMMARYs share the property name.
	var hotelIn, hotelOut bool
	for _, e := range cal.Events {
		switch e.Summary {
		case "Check-in: Radisson Blu Hotel Olumpia, Tallinn":
			hotelIn = true
		case "Check-out: Radisson Blu Hotel Olumpia, Tallinn":
			hotelOut = true
		}
	}
	if !hotelIn || !hotelOut {
		t.Errorf("expected paired hotel check-in/out events, got in=%v out=%v", hotelIn, hotelOut)
	}
}

func TestRealCalais2025Eurotunnel(t *testing.T) {
	cal := parseFixture(t, "calais_2025.ics")
	// The Eurotunnel crossing is present and its UTC instants are parsed; the
	// [Ground Transportation] tag in the description is how the mapper will
	// classify it (TripIt files it as ground, not rail).
	var euro *Event
	for i := range cal.Events {
		if cal.Events[i].Summary == "Eurotunnel - Folkestone to Calais" {
			euro = &cal.Events[i]
		}
	}
	if euro == nil {
		t.Fatal("did not find the Eurotunnel event")
	}
	if !euro.Start.IsUTC || euro.Start.Time.Format("2006-01-02T15:04:05Z") != "2025-09-12T09:24:00Z" {
		t.Errorf("eurotunnel start = %+v", euro.Start)
	}
	if want := "[Ground Transportation]"; !strings.Contains(euro.Description, want) {
		t.Errorf("description missing %q: %q", want, euro.Description)
	}
}

// localUID returns the leading token of a UID up to the first '-', so an
// "item-..." UID is distinguishable from the trip-envelope UID.
func localUID(uid string) string {
	if i := strings.IndexByte(uid, '-'); i >= 0 {
		return uid[:i]
	}
	return uid
}
