package importics

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/timing"
)

// wall is 15:00 on 1 July 2026 as UTC digits: the instant for a UTC value, or
// the wall-clock reading for a floating one.
var wall = time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)

func floatingDT() *DateTime {
	return &DateTime{Raw: "20260701T150000", HasTime: true, Floating: true, Time: wall}
}

func utcDT() *DateTime {
	return &DateTime{Raw: "20260701T150000Z", HasTime: true, IsUTC: true, Time: wall}
}

func zonedDT(t *testing.T, tzid string) *DateTime {
	t.Helper()
	loc, err := time.LoadLocation(tzid)
	if err != nil {
		t.Fatal(err)
	}
	return &DateTime{Raw: "20260701T150000", TZID: tzid, HasTime: true,
		Time: time.Date(2026, 7, 1, 15, 0, 0, 0, loc)}
}

func checkEnd(t *testing.T, what string, gotAt time.Time, gotTZ, gotSrc string, wantAt, wantTZ string, wantSrc timing.Source) {
	t.Helper()
	if g := gotAt.UTC().Format(time.RFC3339); g != wantAt {
		t.Errorf("%s instant = %s, want %s", what, g, wantAt)
	}
	if gotTZ != wantTZ {
		t.Errorf("%s zone = %q, want %q", what, gotTZ, wantTZ)
	}
	if gotSrc != string(wantSrc) {
		t.Errorf("%s source = %q, want %q", what, gotSrc, wantSrc)
	}
}

func TestSettleZones(t *testing.T) {
	t.Run("part zone anchors a floating time", func(t *testing.T) {
		p := planops.ConfirmPartInput{StartsAt: wall, StartTZ: "Europe/Paris"}
		settleZones(&p, floatingDT(), nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T13:00:00Z", "Europe/Paris", timing.Confirmed)
	})
	t.Run("part zone keeps a UTC instant", func(t *testing.T) {
		p := planops.ConfirmPartInput{StartsAt: wall, StartTZ: "Europe/Paris"}
		settleZones(&p, utcDT(), nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T15:00:00Z", "Europe/Paris", timing.Confirmed)
	})
	t.Run("invalid part zone is dropped", func(t *testing.T) {
		p := planops.ConfirmPartInput{StartsAt: wall, StartTZ: "Not/AZone"}
		settleZones(&p, floatingDT(), nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T15:00:00Z", "", timing.Floating)
	})
	t.Run("valid TZID gives the zone", func(t *testing.T) {
		dt := zonedDT(t, "America/New_York")
		p := planops.ConfirmPartInput{StartsAt: dt.Time}
		settleZones(&p, dt, nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T19:00:00Z", "America/New_York", timing.Confirmed)
	})
	t.Run("floating with no zone stays floating", func(t *testing.T) {
		p := planops.ConfirmPartInput{StartsAt: wall}
		settleZones(&p, floatingDT(), nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T15:00:00Z", "", timing.Floating)
	})
	t.Run("unresolvable TZID is floating", func(t *testing.T) {
		dt := floatingDT()
		dt.TZID = "Pacific Time"
		p := planops.ConfirmPartInput{StartsAt: wall}
		settleZones(&p, dt, nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T15:00:00Z", "", timing.Floating)
	})
	t.Run("UTC with no zone is unknown", func(t *testing.T) {
		p := planops.ConfirmPartInput{StartsAt: wall}
		settleZones(&p, utcDT(), nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T15:00:00Z", "", timing.Unknown)
	})
	t.Run("date-only is floating with no zone", func(t *testing.T) {
		day := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		p := planops.ConfirmPartInput{StartsAt: day, StartTZ: "Europe/Paris"}
		settleZones(&p, &DateTime{Raw: "20260701", Time: day}, nil)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T00:00:00Z", "", timing.Floating)
	})
	t.Run("end settles from its own DateTime", func(t *testing.T) {
		end := wall.Add(2 * time.Hour)
		p := planops.ConfirmPartInput{StartsAt: wall, EndsAt: &end, EndTZ: "Europe/Paris"}
		endDT := &DateTime{Raw: "20260701T170000", HasTime: true, Floating: true, Time: end}
		settleZones(&p, utcDT(), endDT)
		checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T15:00:00Z", "", timing.Unknown)
		checkEnd(t, "end", *p.EndsAt, p.EndTZ, p.EndTZSource, "2026-07-01T15:00:00Z", "Europe/Paris", timing.Confirmed)
	})
	t.Run("nil end takes the start's form", func(t *testing.T) {
		end := wall.Add(2 * time.Hour)
		p := planops.ConfirmPartInput{StartsAt: wall, EndsAt: &end}
		settleZones(&p, floatingDT(), nil)
		checkEnd(t, "end", *p.EndsAt, p.EndTZ, p.EndTZSource, "2026-07-01T17:00:00Z", "", timing.Floating)
	})
	t.Run("no end leaves the end source for the store", func(t *testing.T) {
		p := planops.ConfirmPartInput{StartsAt: wall}
		settleZones(&p, floatingDT(), floatingDT())
		if p.EndsAt != nil || p.EndTZSource != "" {
			t.Errorf("end = %v / %q, want nil / empty", p.EndsAt, p.EndTZSource)
		}
	})
}

// tripItVEvent renders one synthetic TripIt VEVENT; every value is invented.
func tripItVEvent(uid, summary, desc, start, end, geo string) string {
	s := "BEGIN:VEVENT\nUID:" + uid + "\nSUMMARY:" + summary + "\nDESCRIPTION:" + desc +
		"\nDTSTART:" + start + "\n"
	if end != "" {
		s += "DTEND:" + end + "\n"
	}
	if geo != "" {
		s += "GEO:" + geo + "\n"
	}
	return s + "END:VEVENT\n"
}

func parseICS(t *testing.T, vevents ...string) *Calendar {
	t.Helper()
	cal, err := Parse(strings.NewReader("BEGIN:VCALENDAR\nPRODID:-//Test//Synthetic//EN\n" +
		strings.Join(vevents, "") + "END:VCALENDAR\n"))
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

// TestKayakGroundUTCIsUnknown: a Kayak bus with a UTC DTSTART and no GEO is a
// true instant of unknown zone, never a floating wall-clock time.
func TestKayakGroundUTCIsUnknown(t *testing.T) {
	cal := parseSynthetic(t, "BEGIN:VEVENT\nUID:bus-1@kayak.com\n"+
		"DTSTART:20260701T140000Z\nDTEND:20260701T160000Z\nSUMMARY:Bus Test Lines 100\n"+
		`DESCRIPTION:Test Lines Bus Number 100\nDeparting from Test City - 07/01/2026 2:00PM\nArriving at Other City - 07/01/2026 4:00PM`+
		`\n\nView trip: https://www.kayak.com/trips/!TESTBUS?ref=calendar`+"\nEND:VEVENT\n")
	plans := syntheticPlans(t, cal)
	if len(plans) != 1 || plans[0].Type != "ground" {
		t.Fatalf("plans = %+v, want one ground", plans)
	}
	p := plans[0].Parts[0]
	checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T14:00:00Z", "", timing.Unknown)
	checkEnd(t, "end", *p.EndsAt, p.EndTZ, p.EndTZSource, "2026-07-01T16:00:00Z", "", timing.Unknown)
}

// TestTripItHotelFloatingAnchoredByGeo: a floating check-in at a GEO in Paris is
// anchored in Europe/Paris, and so is the check-out.
func TestTripItHotelFloatingAnchoredByGeo(t *testing.T) {
	cal := parseICS(t,
		tripItVEvent("item-in@tripit.com", "Check-in: Test Hotel", "[Lodging] Test Hotel",
			"20260701T150000", "20260701T153000", "48.8566;2.3522"),
		tripItVEvent("item-out@tripit.com", "Check-out: Test Hotel", "[Lodging] Test Hotel",
			"20260703T110000", "20260703T113000", "48.8566;2.3522"),
	)
	mt := mapTripIt(cal)
	hotels := plansByType(mt)["hotel"]
	if len(hotels) != 1 {
		t.Fatalf("got %d hotels, want 1", len(hotels))
	}
	p := hotels[0].Parts[0]
	checkEnd(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T13:00:00Z", "Europe/Paris", timing.Confirmed)
	checkEnd(t, "end", *p.EndsAt, p.EndTZ, p.EndTZSource, "2026-07-03T09:00:00Z", "Europe/Paris", timing.Confirmed)
}

// TestTripItMappersSettleZones covers the remaining TripIt mappers: a flight
// between table airports, a transfer with an unknown origin, and a single-point
// transport.
func TestTripItMappersSettleZones(t *testing.T) {
	cal := parseICS(t,
		tripItVEvent("item-f@tripit.com", "XX100 LHR to CDG", "[Flight]",
			"20260701T090000Z", "20260701T103000Z", ""),
		tripItVEvent("item-g@tripit.com", "Test Cabs - Somewhere to Test Hotel", "[Ground Transportation]",
			"20260701T120000", "20260701T130000", "48.8566;2.3522"),
		tripItVEvent("item-p@tripit.com", "Test Shuttle", "[Ground Transportation]",
			"20260702T120000Z", "", ""),
	)
	byType := plansByType(mapTripIt(cal))
	f := byType["flight"][0].Parts[0]
	checkEnd(t, "flight start", f.StartsAt, f.StartTZ, f.StartTZSource, "2026-07-01T09:00:00Z", "Europe/London", timing.Confirmed)
	checkEnd(t, "flight end", *f.EndsAt, f.EndTZ, f.EndTZSource, "2026-07-01T10:30:00Z", "Europe/Paris", timing.Confirmed)

	var transfer, point planops.ConfirmPartInput
	for _, p := range byType["ground"] {
		if p.TripItUID == "item-g@tripit.com" {
			transfer = p.Parts[0]
		} else {
			point = p.Parts[0]
		}
	}
	// The origin has no coordinate, so its floating time stays floating; the
	// arrival's GEO gives the end its zone and anchors it.
	checkEnd(t, "transfer start", transfer.StartsAt, transfer.StartTZ, transfer.StartTZSource, "2026-07-01T12:00:00Z", "", timing.Floating)
	checkEnd(t, "transfer end", *transfer.EndsAt, transfer.EndTZ, transfer.EndTZSource, "2026-07-01T11:00:00Z", "Europe/Paris", timing.Confirmed)
	checkEnd(t, "point start", point.StartsAt, point.StartTZ, point.StartTZSource, "2026-07-02T12:00:00Z", "", timing.Unknown)
	if point.EndsAt != nil || point.EndTZSource != "" {
		t.Errorf("point end = %v / %q, want none", point.EndsAt, point.EndTZSource)
	}
}

// TestKayakMappersSettleZones covers the remaining Kayak mappers.
func TestKayakMappersSettleZones(t *testing.T) {
	view := `\n\nView trip: https://www.kayak.com/trips/!TESTZONE?ref=calendar`
	ev := func(uid, summary, start, end, desc, geo string) string {
		s := "BEGIN:VEVENT\nUID:" + uid + "\nDTSTART" + start + "\nDTEND" + end +
			"\nSUMMARY:" + summary + "\nDESCRIPTION:" + desc + view + "\n"
		if geo != "" {
			s += "GEO:" + geo + "\n"
		}
		return s + "END:VEVENT\n"
	}
	cal := parseSynthetic(t,
		ev("fl@kayak.com", "XX 100 from London (LHR) to Paris (CDG)", ":20260701T090000Z", ":20260701T103000Z", "Flight", ""),
		ev("din@kayak.com", "Test Bistro", ":20260701T190000", ":20260701T210000", "Restaurant Name: Test Bistro", "48.8566;2.3522"),
		ev("in@kayak.com", "Check in to Test Hotel", ";TZID=Europe/Paris:20260701T150000", ";TZID=Europe/Paris:20260701T153000", "Hotel", ""),
		ev("out@kayak.com", "Check out from Test Hotel", ";TZID=Europe/Paris:20260703T110000", ";TZID=Europe/Paris:20260703T113000", "Hotel", ""),
		ev("pick@kayak.com", "Car Pickup (Agency: TestRent)", ":20260701T080000Z", ":20260701T083000Z", "Confirmation Number: ZZ1", ""),
		ev("drop@kayak.com", "Car Dropoff (Agency: TestRent)", ":20260703T080000", ":20260703T083000", "Confirmation Number: ZZ1", ""),
	)
	byType := map[string]planops.ConfirmPartInput{}
	for _, p := range syntheticPlans(t, cal) {
		byType[p.Type] = p.Parts[0]
	}
	f := byType["flight"]
	checkEnd(t, "flight start", f.StartsAt, f.StartTZ, f.StartTZSource, "2026-07-01T09:00:00Z", "Europe/London", timing.Confirmed)
	checkEnd(t, "flight end", *f.EndsAt, f.EndTZ, f.EndTZSource, "2026-07-01T10:30:00Z", "Europe/Paris", timing.Confirmed)
	d := byType["dining"]
	checkEnd(t, "dining start", d.StartsAt, d.StartTZ, d.StartTZSource, "2026-07-01T17:00:00Z", "Europe/Paris", timing.Confirmed)
	checkEnd(t, "dining end", *d.EndsAt, d.EndTZ, d.EndTZSource, "2026-07-01T19:00:00Z", "Europe/Paris", timing.Confirmed)
	h := byType["hotel"]
	checkEnd(t, "hotel start", h.StartsAt, h.StartTZ, h.StartTZSource, "2026-07-01T13:00:00Z", "Europe/Paris", timing.Confirmed)
	checkEnd(t, "hotel end", *h.EndsAt, h.EndTZ, h.EndTZSource, "2026-07-03T09:00:00Z", "Europe/Paris", timing.Confirmed)
	// The pickup is a UTC instant; the return its paired dropoff's floating time.
	c := byType["vehicle_hire"]
	checkEnd(t, "hire start", c.StartsAt, c.StartTZ, c.StartTZSource, "2026-07-01T08:00:00Z", "", timing.Unknown)
	checkEnd(t, "hire end", *c.EndsAt, c.EndTZ, c.EndTZSource, "2026-07-03T08:00:00Z", "", timing.Floating)
}

// TestKayakUnpairedCarEndFromPickup: with no return, the hire's end is the
// pickup's own DTEND, so it takes that value's form.
func TestKayakUnpairedCarEndFromPickup(t *testing.T) {
	cal := parseSynthetic(t, carVEvent("pick@kayak.com", "Car Pickup (Agency: TestRent)",
		"20260701T080000Z", "20260701T083000Z", "Confirmation Number: ZZ1"))
	plans := syntheticPlans(t, cal)
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	c := plans[0].Parts[0]
	checkEnd(t, "hire end", *c.EndsAt, c.EndTZ, c.EndTZSource, "2026-07-01T08:30:00Z", "", timing.Unknown)
}

// TestFloatingFlightScheduleFollowsAnchor: a floating flight time is anchored in
// its airport's zone, and the flight's schedule agrees with the part.
func TestFloatingFlightScheduleFollowsAnchor(t *testing.T) {
	tripIt := plansByType(mapTripIt(parseICS(t, tripItVEvent("item-f@tripit.com", "XX100 LHR to CDG", "[Flight]",
		"20260701T090000", "20260701T113000", ""))))["flight"][0].Parts[0]
	kayak := syntheticPlans(t, parseSynthetic(t, "BEGIN:VEVENT\nUID:fl@kayak.com\nDTSTART:20260701T090000\n"+
		"DTEND:20260701T113000\nSUMMARY:XX 100 from London (LHR) to Paris (CDG)\n"+
		`DESCRIPTION:Flight\n\nView trip: https://www.kayak.com/trips/!TESTFL?ref=calendar`+"\nEND:VEVENT\n"))[0].Parts[0]
	for name, p := range map[string]planops.ConfirmPartInput{"tripit": tripIt, "kayak": kayak} {
		checkEnd(t, name+" start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T08:00:00Z", "Europe/London", timing.Confirmed)
		checkEnd(t, name+" end", *p.EndsAt, p.EndTZ, p.EndTZSource, "2026-07-01T09:30:00Z", "Europe/Paris", timing.Confirmed)
		if !p.Flight.ScheduledOut.Equal(p.StartsAt) || !p.Flight.ScheduledIn.Equal(*p.EndsAt) {
			t.Errorf("%s schedule %s → %s, want the part's %s → %s", name,
				p.Flight.ScheduledOut, p.Flight.ScheduledIn, p.StartsAt, *p.EndsAt)
		}
	}
}
