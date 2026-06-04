package tripitics

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
)

func TestDetectSource(t *testing.T) {
	if src := Detect(parseFixture(t, "pgconfeu_2016.ics")); src != SourceTripIt {
		t.Errorf("Detect(TripIt fixture) = %q, want tripit", src)
	}
	// A standard calendar from some other producer isn't recognised, so Map
	// reports ok=false and the caller falls back.
	other := &Calendar{Name: "Kayak Itinerary", Events: []Event{{UID: "evt-1@kayak.com", Summary: "Flight"}}}
	if src := Detect(other); src != SourceUnknown {
		t.Errorf("Detect(non-TripIt) = %q, want unknown", src)
	}
	if _, _, ok := Map(other); ok {
		t.Error("Map(non-TripIt) ok = true, want false (caller should fall back)")
	}
}

// mustMapTripIt runs source detection + dispatch and asserts the calendar was
// recognised as TripIt, returning the mapped trip.
func mustMapTripIt(t *testing.T, cal *Calendar) *MappedTrip {
	t.Helper()
	mt, src, ok := Map(cal)
	if !ok || src != SourceTripIt {
		t.Fatalf("Map: src=%q ok=%v, want a TripIt mapping", src, ok)
	}
	return mt
}

// plansByType groups a mapped trip's plans by type for easy assertions.
func plansByType(mt *MappedTrip) map[string][]planops.ConfirmPlanInput {
	m := map[string][]planops.ConfirmPlanInput{}
	for _, p := range mt.Plans {
		m[p.Type] = append(m[p.Type], p)
	}
	return m
}

func TestMapPGConfEU2016(t *testing.T) {
	mt := mustMapTripIt(t, parseFixture(t, "pgconfeu_2016.ics"))

	if mt.Name != "PGConf.EU 2016" {
		t.Errorf("trip name = %q, want PGConf.EU 2016", mt.Name)
	}
	// Envelope DTSTART 20161030 / DTEND 20161107 (exclusive) → Oct 30–Nov 6.
	if mt.StartsOn == nil || mt.StartsOn.Format("2006-01-02") != "2016-10-30" {
		t.Errorf("StartsOn = %v, want 2016-10-30", mt.StartsOn)
	}
	if mt.EndsOn == nil || mt.EndsOn.Format("2006-01-02") != "2016-11-06" {
		t.Errorf("EndsOn = %v, want 2016-11-06", mt.EndsOn)
	}

	by := plansByType(mt)
	if len(by["flight"]) != 4 {
		t.Errorf("got %d flight plans, want 4", len(by["flight"]))
	}
	if len(by["ground"]) != 2 {
		t.Errorf("got %d ground plans, want 2 (taxis)", len(by["ground"]))
	}
	if len(by["hotel"]) != 1 {
		t.Fatalf("got %d hotel plans, want 1", len(by["hotel"]))
	}

	// The outbound LHR→HEL leg: ident, IATA route, UTC schedule, terminal status.
	var ay832 *planops.ConfirmPartInput
	for i := range by["flight"] {
		p := by["flight"][i]
		if p.Parts[0].Flight.Ident == "AY832" {
			ay832 = &by["flight"][i].Parts[0]
		}
	}
	if ay832 == nil {
		t.Fatal("AY832 flight not mapped")
	}
	fd := ay832.Flight
	if fd.OriginIATA != "LHR" || fd.DestIATA != "HEL" {
		t.Errorf("AY832 route = %s→%s, want LHR→HEL", fd.OriginIATA, fd.DestIATA)
	}
	if fd.FlightStatus != "Arrived" {
		t.Errorf("AY832 status = %q, want Arrived (so the poller skips it)", fd.FlightStatus)
	}
	if fd.ScheduledOut.Format("2006-01-02T15:04:05Z") != "2016-10-30T10:20:00Z" {
		t.Errorf("AY832 scheduled_out = %v, want 2016-10-30T10:20:00Z", fd.ScheduledOut)
	}
	// LHR is in the embedded IATA table, so the origin gets coords + a zone.
	if ay832.StartTZ != "Europe/London" {
		t.Errorf("AY832 start tz = %q, want Europe/London", ay832.StartTZ)
	}
	if ay832.StartLat == nil || ay832.StartLon == nil {
		t.Error("AY832 should have origin coordinates for the great-circle arc")
	}

	// The hotel stay pairs into one plan spanning check-in → check-out.
	h := by["hotel"][0].Parts[0]
	if h.StartsAt.Format("2006-01-02") != "2016-10-30" {
		t.Errorf("hotel check-in = %v, want 2016-10-30", h.StartsAt)
	}
	if h.EndsAt == nil || h.EndsAt.Format("2006-01-02") != "2016-11-06" {
		t.Errorf("hotel check-out = %v, want 2016-11-06", h.EndsAt)
	}
	if h.Hotel == nil || h.Hotel.PropertyName != "Radisson Blu Hotel Olumpia, Tallinn" {
		t.Errorf("hotel name = %+v", h.Hotel)
	}
}

func TestMapCalaisEurotunnelAsTrain(t *testing.T) {
	mt := mustMapTripIt(t, parseFixture(t, "calais_2025.ics"))
	by := plansByType(mt)

	// Both Eurotunnel crossings are lifted to "train"; there are no taxis here.
	if len(by["train"]) != 2 {
		t.Errorf("got %d train plans, want 2 Eurotunnel crossings; ground=%d",
			len(by["train"]), len(by["ground"]))
	}
	if len(by["ground"]) != 0 {
		t.Errorf("got %d ground plans, want 0", len(by["ground"]))
	}
	if len(by["hotel"]) != 1 {
		t.Errorf("got %d hotel plans, want 1", len(by["hotel"]))
	}

	for _, p := range by["train"] {
		part := p.Parts[0]
		if part.Train == nil || part.Train.Operator != "Eurotunnel" {
			t.Errorf("train operator = %+v, want Eurotunnel", part.Train)
		}
	}
}

// TestMapTransferEndpointsAreDistinct guards the crow-flight bug: TripIt puts a
// single GEO (the *arrival* point) on each transport event, so a naive importer
// collapses both endpoints onto it (start == end) and the map can't draw a leg.
// The importer must instead set the end from the event's own GEO and recover the
// start from a calendar-wide label→GEO index — in a round trip each leg's start
// is the other leg's arrival. Folkestone ≈ (51.08169, 1.16734), Calais ≈
// (50.95194, 1.85635).
func TestMapTransferEndpointsAreDistinct(t *testing.T) {
	mt := mustMapTripIt(t, parseFixture(t, "calais_2025.ics"))

	const folkLat, folkLon = 51.08169, 1.16734
	const calaisLat, calaisLon = 50.95194, 1.85635

	byStart := map[string]planops.ConfirmPartInput{}
	for _, p := range plansByType(mt)["train"] {
		part := p.Parts[0]
		byStart[part.StartLabel] = part
	}

	check := func(startLabel string, wantSLat, wantSLon, wantELat, wantELon float64) {
		t.Helper()
		part, ok := byStart[startLabel]
		if !ok {
			t.Fatalf("no train leg starting at %q", startLabel)
		}
		if part.StartLat == nil || part.StartLon == nil || part.EndLat == nil || part.EndLon == nil {
			t.Fatalf("leg from %q missing an endpoint coordinate: %+v", startLabel, part)
		}
		if *part.StartLat == *part.EndLat && *part.StartLon == *part.EndLon {
			t.Fatalf("leg from %q collapsed start onto end (%v,%v) — no crow-flight line",
				startLabel, *part.StartLat, *part.StartLon)
		}
		if *part.StartLat != wantSLat || *part.StartLon != wantSLon {
			t.Errorf("leg from %q start = (%v,%v), want (%v,%v)",
				startLabel, *part.StartLat, *part.StartLon, wantSLat, wantSLon)
		}
		if *part.EndLat != wantELat || *part.EndLon != wantELon {
			t.Errorf("leg from %q end = (%v,%v), want (%v,%v)",
				startLabel, *part.EndLat, *part.EndLon, wantELat, wantELon)
		}
	}

	// Outbound departs Folkestone, arrives Calais; return is the mirror.
	check("Folkestone", folkLat, folkLon, calaisLat, calaisLon)
	check("Calais", calaisLat, calaisLon, folkLat, folkLon)
}

func TestMapAlphanumericAirlineCode(t *testing.T) {
	// IATA airline designators can carry a digit in either position — easyJet
	// "U2" (position 2), Sichuan "3U" (position 1). The pinned fixtures are all
	// all-letter carriers, so synthesize legs for both shapes here.
	cases := []struct {
		name        string
		summary     string
		ident, o, d string
	}{
		{"digit in position 2", "U21234 LHR to AGP", "U21234", "LHR", "AGP"},
		{"digit in position 1", "3U8888 CTU to PKX", "3U8888", "CTU", "PKX"},
	}
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cal := &Calendar{Events: []Event{{
				UID:         "item-x@tripit.com",
				Summary:     tc.summary,
				Description: "[Flight] " + tc.o + " to " + tc.d,
				Start:       DateTime{Raw: "20260701T090000Z", Time: out, HasTime: true, IsUTC: true},
				End:         DateTime{Raw: "20260701T120000Z", Time: in, HasTime: true, IsUTC: true},
			}}}
			mt := mustMapTripIt(t, cal)
			if len(mt.Plans) != 1 || mt.Plans[0].Type != "flight" {
				t.Fatalf("got %d plans (%v), want 1 flight", len(mt.Plans), mt.Plans)
			}
			fd := mt.Plans[0].Parts[0].Flight
			if fd.Ident != tc.ident || fd.OriginIATA != tc.o || fd.DestIATA != tc.d {
				t.Errorf("mapped flight = %s %s→%s, want %s %s→%s",
					fd.Ident, fd.OriginIATA, fd.DestIATA, tc.ident, tc.o, tc.d)
			}
		})
	}
}

// flightCal builds a minimal TripIt-style calendar carrying one flight leg,
// with no calendar name of its own (no X-WR-CALNAME / X-WR-CALDESC).
func flightCal(summary string) *Calendar {
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return &Calendar{Events: []Event{{
		UID:         "item-x@tripit.com",
		Summary:     summary,
		Description: "[Flight]",
		Start:       DateTime{Raw: "20260701T090000Z", Time: out, HasTime: true, IsUTC: true},
		End:         DateTime{Raw: "20260701T120000Z", Time: in, HasTime: true, IsUTC: true},
	}}}
}

// A nameless .ics is named for where it goes, not left as a generic label (#21).
func TestMapNamelessUsesDestinationCity(t *testing.T) {
	mt := mapTripIt(flightCal("BA286 LHR to IAD"))
	if mt.Name != "Trip to Washington" {
		t.Errorf("nameless flight import name = %q, want %q", mt.Name, "Trip to Washington")
	}
}

// With no calendar name and no flight to name it after, fall back to the
// generic label rather than an empty name.
func TestMapNamelessNoFlightFallsBackToGeneric(t *testing.T) {
	cal := &Calendar{Events: []Event{{UID: "item-y@tripit.com", Summary: "Nonsense entry"}}}
	mt := mapTripIt(cal)
	if mt.Name != "Imported trip" {
		t.Errorf("nameless import with no flight name = %q, want %q", mt.Name, "Imported trip")
	}
}

// A calendar that does carry a name keeps it — destination naming is only a
// fallback for nameless imports.
func TestMapNamedCalendarKeepsItsName(t *testing.T) {
	cal := flightCal("BA286 LHR to IAD")
	cal.Name = "Conference in DC"
	if mt := mapTripIt(cal); mt.Name != "Conference in DC" {
		t.Errorf("named import name = %q, want %q", mt.Name, "Conference in DC")
	}
}

// tripName returns "" when the calendar carries no name of its own, so the
// caller can fall back to a destination-based name.
func TestTripNameEmptyWhenNoCalendarName(t *testing.T) {
	if got := tripName(&Calendar{}); got != "" {
		t.Errorf("tripName(nameless) = %q, want empty", got)
	}
	// A whitespace-only name is treated as no name, so the caller falls back.
	if got := tripName(&Calendar{Desc: "   ", Name: "\t"}); got != "" {
		t.Errorf("tripName(whitespace) = %q, want empty", got)
	}
}

func TestMapTripProvenance(t *testing.T) {
	mt := mustMapTripIt(t, parseFixture(t, "pgconfeu_2016.ics"))
	// The TripIt trip id is lifted from the envelope's show URL, for re-import
	// trip dedupe.
	if mt.TripItID != "175153005" {
		t.Errorf("TripItID = %q, want 175153005", mt.TripItID)
	}
	// Every imported plan carries its source event UID, for per-plan dedupe.
	for _, p := range mt.Plans {
		if p.TripItUID == "" {
			t.Errorf("plan %q has no TripItUID", p.Title)
		}
	}
}

func TestMapSourceAlwaysUpload(t *testing.T) {
	mt := mustMapTripIt(t, parseFixture(t, "pgconfdev_2026.ics"))
	if len(mt.Plans) == 0 {
		t.Fatal("expected plans")
	}
	for _, p := range mt.Plans {
		if p.Source != "upload" {
			t.Errorf("plan %q source = %q, want upload", p.Title, p.Source)
		}
	}
}
