package tripitics

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
)

// plansByType groups a mapped trip's plans by type for easy assertions.
func plansByType(mt *MappedTrip) map[string][]planops.ConfirmPlanInput {
	m := map[string][]planops.ConfirmPlanInput{}
	for _, p := range mt.Plans {
		m[p.Type] = append(m[p.Type], p)
	}
	return m
}

func TestMapPGConfEU2016(t *testing.T) {
	mt := MapCalendar(parseFixture(t, "pgconfeu_2016.ics"))

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
	mt := MapCalendar(parseFixture(t, "calais_2025.ics"))
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

func TestMapAlphanumericAirlineCode(t *testing.T) {
	// IATA airline designators can carry a digit (easyJet U2, Wizz W6). The
	// pinned fixtures are all-letter carriers, so synthesize a leg here.
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	cal := &Calendar{Events: []Event{{
		UID:         "item-x@tripit.com",
		Summary:     "U21234 LHR to AGP",
		Description: "[Flight] LHR to AGP",
		Start:       DateTime{Raw: "20260701T090000Z", Time: out, HasTime: true, IsUTC: true},
		End:         DateTime{Raw: "20260701T120000Z", Time: in, HasTime: true, IsUTC: true},
	}}}
	mt := MapCalendar(cal)
	if len(mt.Plans) != 1 || mt.Plans[0].Type != "flight" {
		t.Fatalf("got %d plans (%v), want 1 flight", len(mt.Plans), mt.Plans)
	}
	fd := mt.Plans[0].Parts[0].Flight
	if fd.Ident != "U21234" || fd.OriginIATA != "LHR" || fd.DestIATA != "AGP" {
		t.Errorf("mapped flight = %s %s→%s, want U21234 LHR→AGP", fd.Ident, fd.OriginIATA, fd.DestIATA)
	}
}

func TestMapSourceAlwaysUpload(t *testing.T) {
	mt := MapCalendar(parseFixture(t, "pgconfdev_2026.ics"))
	if len(mt.Plans) == 0 {
		t.Fatal("expected plans")
	}
	for _, p := range mt.Plans {
		if p.Source != "upload" {
			t.Errorf("plan %q source = %q, want upload", p.Title, p.Source)
		}
	}
}
