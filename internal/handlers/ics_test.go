package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func TestEscapeText(t *testing.T) {
	cases := map[string]string{
		`a,b;c\d`:    `a\,b\;c\\d`,
		"line1\nl2":  `line1\nl2`,
		"crlf\r\nx":  `crlf\nx`,
		"plain text": "plain text",
	}
	for in, want := range cases {
		if got := escapeText(in); got != want {
			t.Errorf("escapeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTZOffset(t *testing.T) {
	cases := map[int]string{
		0:      "+0000",
		3600:   "+0100",
		-18000: "-0500",
		19800:  "+0530",
	}
	for secs, want := range cases {
		if got := formatTZOffset(secs); got != want {
			t.Errorf("formatTZOffset(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestWriteLineFolding(t *testing.T) {
	var b strings.Builder
	long := "SUMMARY:" + strings.Repeat("x", 200)
	writeLine(&b, long)
	out := b.String()
	for _, line := range strings.Split(strings.TrimRight(out, "\r\n"), "\r\n") {
		// Continuation lines start with a space; none may exceed 75 octets.
		if len(line) > 75 {
			t.Errorf("folded line exceeds 75 octets: %d %q", len(line), line)
		}
	}
	// Unfolding (strip CRLF + leading space) must restore the original.
	unfolded := strings.ReplaceAll(out, "\r\n ", "")
	unfolded = strings.TrimRight(unfolded, "\r\n")
	if unfolded != long {
		t.Errorf("unfold mismatch:\n got %q\nwant %q", unfolded, long)
	}
}

func TestRenderICSStructure(t *testing.T) {
	end := time.Date(2026, 7, 1, 14, 30, 0, 0, time.UTC)
	events := []*store.CalendarEvent{
		{
			PartID:          7,
			PlanID:          3,
			Type:            "flight",
			Title:           "BA286",
			ConfirmationRef: "ABC123",
			Notes:           "window; seat",
			StartsAt:        time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
			EndsAt:          &end,
			StartTZ:         "Europe/London",
			EndTZ:           "America/New_York",
			StartLabel:      "LHR",
			Status:          "confirmed",
			UpdatedAt:       time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	out := renderICS("Aerly", events, false)

	mustContain := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:" + icsProdID,
		"BEGIN:VTIMEZONE",
		"TZID:Europe/London",
		"TZID:America/New_York",
		"BEGIN:VEVENT",
		"UID:plan-part-7@aerly",
		"DTSTART;TZID=Europe/London:20260701T100000",  // BST = UTC+1
		"DTEND;TZID=America/New_York:20260701T103000", // EDT = UTC-4
		"SUMMARY:BA286 (Flight)",
		"LOCATION:LHR",
		"STATUS:CONFIRMED",
		"END:VEVENT",
		"END:VCALENDAR",
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("ICS output missing %q\n---\n%s", m, out)
		}
	}
	// Confirmation + notes folded into DESCRIPTION, with escaping.
	if !strings.Contains(out, "DESCRIPTION:Confirmation: ABC123\\nwindow\\; seat") {
		t.Errorf("DESCRIPTION wrong/unescaped:\n%s", out)
	}
	// CRLF line endings.
	if !strings.Contains(out, "\r\n") {
		t.Error("ICS must use CRLF line endings")
	}
}

// TestRenderICSDSTTransitions verifies the VTIMEZONE carries proper
// DAYLIGHT/STANDARD observances with RRULE transitions, and that two events
// straddling a DST boundary render the correct local wall-clock times.
func TestRenderICSDSTTransitions(t *testing.T) {
	// Two events in Europe/London on either side of the autumn 2026 transition
	// (BST→GMT at 02:00 BST on the last Sunday of October, 2026-10-25).
	// Before: 2026-10-20 12:00Z = 13:00 BST (UTC+1).
	// After:  2026-11-03 12:00Z = 12:00 GMT (UTC+0).
	before := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	after := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	events := []*store.CalendarEvent{
		{PartID: 1, Type: "flight", Title: "Pre-DST", StartsAt: before, StartTZ: "Europe/London", UpdatedAt: before},
		{PartID: 2, Type: "flight", Title: "Post-DST", StartsAt: after, StartTZ: "Europe/London", UpdatedAt: after},
	}
	out := renderICS("Aerly", events, false)

	mustContain := []string{
		"BEGIN:VTIMEZONE",
		"TZID:Europe/London",
		"BEGIN:DAYLIGHT",
		"BEGIN:STANDARD",
		"RRULE:FREQ=YEARLY;BYMONTH=3",  // spring forward in March
		"RRULE:FREQ=YEARLY;BYMONTH=10", // fall back in October
		"TZOFFSETTO:+0100",             // BST
		"TZOFFSETTO:+0000",             // GMT
		// Local wall-clock times the client renders against the VTIMEZONE.
		"DTSTART;TZID=Europe/London:20261020T130000", // 13:00 BST
		"DTSTART;TZID=Europe/London:20261103T120000", // 12:00 GMT
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("ICS DST output missing %q\n---\n%s", m, out)
		}
	}

	// The DST RRULE should resolve to the last Sunday (BYDAY=-1SU) for both
	// transitions in Europe/London.
	if !strings.Contains(out, "BYDAY=-1SU") {
		t.Errorf("expected last-Sunday RRULE for Europe/London:\n%s", out)
	}
}

// TestRenderICSHotelSplitsIntoCheckinCheckout: a multi-night hotel stay must
// render as two point events (check-in + check-out), not one all-night banner
// (issue #101).
func TestRenderICSHotelSplitsIntoCheckinCheckout(t *testing.T) {
	checkout := time.Date(2026, 6, 14, 11, 0, 0, 0, time.UTC) // 13:00 CEST
	events := []*store.CalendarEvent{{
		PartID:     9,
		PlanID:     4,
		Type:       "hotel",
		Title:      "Hotel Astoria",
		StartsAt:   time.Date(2026, 6, 11, 13, 0, 0, 0, time.UTC), // 15:00 CEST check-in
		EndsAt:     &checkout,
		StartTZ:    "Europe/Zurich",
		EndTZ:      "Europe/Zurich",
		StartLabel: "Astoria",
		Status:     "confirmed",
		UpdatedAt:  time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}}
	out := renderICS("Aerly", events, false)

	mustContain := []string{
		"UID:plan-part-9-checkin@aerly",
		"UID:plan-part-9-checkout@aerly",
		"SUMMARY:Hotel Astoria (Check-in)",
		"SUMMARY:Hotel Astoria (Check-out)",
		"DTSTART;TZID=Europe/Zurich:20260611T150000", // 15:00 CEST check-in
		"DTSTART;TZID=Europe/Zurich:20260614T130000", // 13:00 CEST check-out
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("hotel split ICS missing %q\n---\n%s", m, out)
		}
	}
	// The stay must NOT render as one spanning event with a DTEND on the checkout.
	if strings.Contains(out, "UID:plan-part-9@aerly") {
		t.Errorf("multi-night hotel still rendered as a single banner event:\n%s", out)
	}
	if strings.Contains(out, "DTEND;TZID=Europe/Zurich:20260614") {
		t.Errorf("multi-night hotel still has a spanning DTEND:\n%s", out)
	}
}

// TestRenderICSSameDayHotelStaysSingle: a same-day hotel (checkout the same
// local day) is not a band and renders as one event with a DTEND.
func TestRenderICSSameDayHotelStaysSingle(t *testing.T) {
	checkout := time.Date(2026, 6, 11, 16, 0, 0, 0, time.UTC)
	events := []*store.CalendarEvent{{
		PartID:    9,
		Type:      "hotel",
		Title:     "Day Room",
		StartsAt:  time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC),
		EndsAt:    &checkout,
		StartTZ:   "Europe/Zurich",
		EndTZ:     "Europe/Zurich",
		UpdatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}}
	out := renderICS("Aerly", events, false)
	if !strings.Contains(out, "UID:plan-part-9@aerly") {
		t.Errorf("same-day hotel should render as a single event:\n%s", out)
	}
	if strings.Contains(out, "checkin@aerly") || strings.Contains(out, "checkout@aerly") {
		t.Errorf("same-day hotel should not split:\n%s", out)
	}
}

// TestRenderICSTripBand: with tripBands on, the feed emits one all-day,
// multi-day event named after the trip, with an exclusive DTEND (issue #101).
func TestRenderICSTripBand(t *testing.T) {
	start := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	events := []*store.CalendarEvent{{
		PartID:       1,
		PlanID:       1,
		Type:         "flight",
		Title:        "LX317",
		StartsAt:     time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC),
		StartTZ:      "Europe/Zurich",
		Status:       "confirmed",
		UpdatedAt:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		TripID:       42,
		TripName:     "pgconf.ch 2026",
		TripStartsOn: &start,
		TripEndsOn:   &end,
	}}
	out := renderICS("Aerly", events, true)

	mustContain := []string{
		"UID:trip-42@aerly",
		"SUMMARY:pgconf.ch 2026",
		"DTSTART;VALUE=DATE:20260611",
		"DTEND;VALUE=DATE:20260615", // exclusive: day after 14 Jun
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("trip band ICS missing %q\n---\n%s", m, out)
		}
	}
	// With tripBands off, no trip banner is emitted.
	if off := renderICS("Aerly", events, false); strings.Contains(off, "UID:trip-42@aerly") {
		t.Errorf("trip banner emitted when tripBands=false:\n%s", off)
	}
}

// TestRenderICSTripBandDerivedFromParts: when the trip has no stored date span,
// the banner is derived from the min/max local date of its parts.
func TestRenderICSTripBandDerivedFromParts(t *testing.T) {
	endP1 := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	events := []*store.CalendarEvent{
		{
			PartID: 1, Type: "flight", Title: "Out", TripID: 7, TripName: "Road Trip",
			StartsAt: time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC), EndsAt: &endP1,
			StartTZ: "UTC", UpdatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			PartID: 2, Type: "flight", Title: "Back", TripID: 7, TripName: "Road Trip",
			StartsAt: time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC),
			StartTZ:  "UTC", UpdatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	out := renderICS("Aerly", events, true)
	if !strings.Contains(out, "UID:trip-7@aerly") || !strings.Contains(out, "SUMMARY:Road Trip") {
		t.Errorf("derived trip band missing:\n%s", out)
	}
	if !strings.Contains(out, "DTSTART;VALUE=DATE:20260611") {
		t.Errorf("derived band start wrong (want 11 Jun):\n%s", out)
	}
	if !strings.Contains(out, "DTEND;VALUE=DATE:20260617") { // exclusive: day after 16 Jun
		t.Errorf("derived band end wrong (want 17 Jun exclusive):\n%s", out)
	}
}

func TestRenderICSNoTZFallsBackToUTC(t *testing.T) {
	events := []*store.CalendarEvent{{
		PartID:    1,
		Type:      "dining",
		Title:     "Dinner",
		StartsAt:  time.Date(2026, 7, 1, 19, 0, 0, 0, time.UTC),
		UpdatedAt: time.Now(),
	}}
	out := renderICS("Aerly", events, false)
	if !strings.Contains(out, "DTSTART:20260701T190000Z") {
		t.Errorf("expected UTC DTSTART fallback when tz empty:\n%s", out)
	}
}
