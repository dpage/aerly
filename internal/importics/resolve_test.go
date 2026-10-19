package importics

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// londonGap is 01:30 on 29 March 2026 as UTC digits: a wall-clock time that
// Europe/London skips when its clocks go forward.
var londonGap = time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)

func TestResolve(t *testing.T) {
	outlook := parseFixture(t, "outlook_windows_tz.ics")
	custom := parseFixture(t, "custom_vtimezone.ics")
	gapAt, _ := timing.Anchor(londonGap, "Europe/London")
	paris := Context{LocationTZ: "Europe/Paris"}
	cases := []struct {
		name   string
		dt     DateTime
		c      Context
		wantAt string
		wantTZ string
		want   timing.Source
		allDay bool
	}{
		// Rule 1: a date with no time of day takes no zone, whatever is known.
		{"date only", DateTime{Raw: "20260701", Time: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
			Context{LocationTZ: "Europe/Paris", CalendarTZ: "Asia/Tashkent"}, "2026-07-01T00:00:00Z", "", timing.Floating, true},
		// Rule 2: the time's own TZID wins over every other zone.
		{"own TZID", *zonedDT(t, "America/New_York"), Context{LocationTZ: "Europe/Paris", CalendarTZ: "Asia/Tashkent"},
			"2026-07-01T19:00:00Z", "America/New_York", timing.Confirmed, false},
		{"Windows-mapped TZID", outlook.Events[0].Start, Context{},
			"2026-07-01T07:00:00Z", "Europe/Berlin", timing.Confirmed, false},
		// Rule 3: an instant with the item's own location zone.
		{"UTC with location", *utcDT(), paris, "2026-07-01T15:00:00Z", "Europe/Paris", timing.Confirmed, false},
		{"VTIMEZONE instant with location", custom.Events[0].Start, Context{LocationTZ: "Asia/Kolkata"},
			"2026-07-01T03:30:00Z", "Asia/Kolkata", timing.Confirmed, false},
		// Rule 4: an instant with only a calendar-level zone.
		{"UTC with calendar zone", *utcDT(), Context{CalendarTZ: "Asia/Tashkent"},
			"2026-07-01T15:00:00Z", "Asia/Tashkent", timing.Inferred, false},
		{"UTC with fallback zone", *utcDT(), Context{FallbackTZ: "Europe/London"},
			"2026-07-01T15:00:00Z", "Europe/London", timing.Inferred, false},
		{"VTIMEZONE instant with calendar zone", custom.Events[0].Start, Context{CalendarTZ: "Asia/Kolkata"},
			"2026-07-01T03:30:00Z", "Asia/Kolkata", timing.Inferred, false},
		// Rule 5: an instant and nothing else.
		{"UTC alone", *utcDT(), Context{}, "2026-07-01T15:00:00Z", "", timing.Unknown, false},
		{"VTIMEZONE instant alone", custom.Events[0].Start, Context{},
			"2026-07-01T03:30:00Z", "", timing.Unknown, false},
		{"invalid zones are skipped", *utcDT(), Context{LocationTZ: "Not/AZone", CalendarTZ: "Local"},
			"2026-07-01T15:00:00Z", "", timing.Unknown, false},
		// Rule 6: floating digits anchored in the location zone.
		{"floating with location", *floatingDT(), Context{LocationTZ: "Europe/Paris", CalendarTZ: "Asia/Tashkent"},
			"2026-07-01T13:00:00Z", "Europe/Paris", timing.Confirmed, false},
		{"London gap with location", DateTime{HasTime: true, Floating: true, Time: londonGap},
			Context{LocationTZ: "Europe/London"}, gapAt.UTC().Format(time.RFC3339), "Europe/London", timing.Confirmed, false},
		// Rule 7: floating digits anchored in the calendar zone, then the fallback.
		{"floating with calendar zone", *floatingDT(), Context{CalendarTZ: "Asia/Tashkent", FallbackTZ: "Europe/London"},
			"2026-07-01T10:00:00Z", "Asia/Tashkent", timing.Inferred, false},
		{"floating with fallback zone", *floatingDT(), Context{CalendarTZ: "Local", FallbackTZ: "Europe/London"},
			"2026-07-01T14:00:00Z", "Europe/London", timing.Inferred, false},
		{"London gap with calendar zone", DateTime{HasTime: true, Floating: true, Time: londonGap},
			Context{CalendarTZ: "Europe/London"}, gapAt.UTC().Format(time.RFC3339), "Europe/London", timing.Inferred, false},
		// Rule 8: floating and nothing known: the digits stay as they are.
		{"floating alone", *floatingDT(), Context{}, "2026-07-01T15:00:00Z", "", timing.Floating, false},
		{"TZID=Local alone", outlook.Events[1].Start, Context{}, "2026-07-02T09:00:00Z", "", timing.Floating, false},
		{"unresolvable TZID alone", DateTime{HasTime: true, Floating: true, UnknownTZID: true, RawTZID: "Pacific Time", Time: wall},
			Context{}, "2026-07-01T15:00:00Z", "", timing.Floating, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, ok := Resolve(c.dt, c.c)
			if !ok {
				t.Fatal("Resolve reported no time")
			}
			checkEnd(t, "resolved", r.At, r.TZ, string(r.Source), c.wantAt, c.wantTZ, c.want)
			if r.AllDay != c.allDay {
				t.Errorf("AllDay = %v, want %v", r.AllDay, c.allDay)
			}
		})
	}
}

// TestResolveLondonGapMatchesAnchor pins the gap time itself: 01:30 does not
// exist in London that night, and Resolve settles it exactly as Anchor does.
func TestResolveLondonGapMatchesAnchor(t *testing.T) {
	want, ok := timing.Anchor(londonGap, "Europe/London")
	if !ok {
		t.Fatal("Anchor refused Europe/London")
	}
	r, _ := Resolve(DateTime{HasTime: true, Floating: true, Time: londonGap}, Context{CalendarTZ: "Europe/London"})
	if !r.At.Equal(want) {
		t.Errorf("Resolve = %s, Anchor = %s", r.At, want)
	}
}

// TestResolveFloatingWithValidTZID: a floating value can only carry a valid
// TZID when built by hand, but if it does, that zone anchors it.
func TestResolveFloatingWithValidTZID(t *testing.T) {
	dt := *floatingDT()
	dt.TZID = "Europe/Paris"
	r, _ := Resolve(dt, Context{CalendarTZ: "America/Vancouver"})
	checkEnd(t, "resolved", r.At, r.TZ, string(r.Source), "2026-07-01T13:00:00Z", "Europe/Paris", timing.Confirmed)
}

func TestResolveNoTime(t *testing.T) {
	for name, dt := range map[string]DateTime{
		"empty":            {},
		"unparseable time": {Raw: "garbage", HasTime: true, Floating: true},
	} {
		if r, ok := Resolve(dt, Context{LocationTZ: "Europe/Paris"}); ok {
			t.Errorf("%s: Resolve = %+v, true; want false", name, r)
		}
	}
}
