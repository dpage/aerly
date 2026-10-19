package handlers

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/importics"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// TestTitleCaseType pins the SUMMARY parenthetical for a non-banded event.
// vehicle_hire and hotel get their own human labels ("Car hire",
// "Accommodation"); every other type keeps rendering the plain title-cased
// wire key, unchanged, since the feed is subscribable.
func TestTitleCaseType(t *testing.T) {
	cases := map[string]string{
		"vehicle_hire": "Car hire",
		"flight":       "Flight",
		"hotel":        "Accommodation",
		"ground":       "Ground",
		"train":        "Train",
		"":             "Plan",
	}
	for in, want := range cases {
		if got := titleCaseType(in); got != want {
			t.Errorf("titleCaseType(%q) = %q, want %q", in, got, want)
		}
	}
}

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
	// The time labels, then confirmation and notes, in DESCRIPTION, escaped.
	// The line is long enough to fold, so compare it unfolded.
	unfolded := strings.ReplaceAll(out, "\r\n ", "")
	if !strings.Contains(unfolded, `DESCRIPTION:Starts: Wed 1 Jul\, 10:00 London (UTC+1)\n`+
		`Ends: Wed 1 Jul\, 10:30 New York (UTC-4)\nConfirmation: ABC123\nwindow\; seat`+"\r\n") {
		t.Errorf("DESCRIPTION wrong/unescaped:\n%s", out)
	}
	// CRLF line endings.
	if !strings.Contains(out, "\r\n") {
		t.Error("ICS must use CRLF line endings")
	}
}

// TestRenderICSDSTTransitions verifies the VTIMEZONE carries DAYLIGHT and
// STANDARD observances, and that two events straddling a DST boundary render
// the correct local wall-clock times. TestICSVTimezoneOneBlockPerChange pins
// the observances themselves.
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
		"TZOFFSETTO:+0100", // BST
		"TZOFFSETTO:+0000", // GMT
		// Local wall-clock times the client renders against the VTIMEZONE.
		"DTSTART;TZID=Europe/London:20261020T130000", // 13:00 BST
		"DTSTART;TZID=Europe/London:20261103T120000", // 12:00 GMT
	}
	for _, m := range mustContain {
		if !strings.Contains(out, m) {
			t.Errorf("ICS DST output missing %q\n---\n%s", m, out)
		}
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

// TestICSHotelOutputUnchangedByBandingRefactor is the golden test for the
// banding unification: a two-night hotel stay must keep producing exactly the
// UIDs, SUMMARY lines and DTSTARTs it produced before the refactor (the
// DESCRIPTION gained each edge's own time label with the zone export). The UIDs in
// particular are load-bearing, because real calendar clients have already
// subscribed to this feed and key their stored events off the UID; changing one
// duplicates the event for every subscriber and orphans the original, which
// cannot be undone once it has synced.
func TestICSHotelOutputUnchangedByBandingRefactor(t *testing.T) {
	checkout := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	events := []*store.CalendarEvent{{
		PartID:          42,
		PlanID:          7,
		Type:            "hotel",
		Title:           "Test Hotel",
		ConfirmationRef: "TESTREF1",
		Notes:           "Late arrival",
		StartsAt:        time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC),
		EndsAt:          &checkout,
		StartTZ:         "Europe/London",
		EndTZ:           "Europe/London",
		StartTZSource:   "confirmed",
		EndTZSource:     "confirmed",
		StartLabel:      "Test Hotel, Testville",
		EndLabel:        "Test Hotel, Testville",
		Status:          "confirmed",
		UpdatedAt:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}

	got := vEventBlocks(renderICS("Aerly", events, false))
	want := strings.Join([]string{
		"BEGIN:VEVENT",
		"UID:plan-part-42-checkin@aerly",
		"DTSTAMP:20260801T000000Z",
		"LAST-MODIFIED:20260801T000000Z",
		"DTSTART;TZID=Europe/London:20260907T150000",
		"SUMMARY:Test Hotel (Check-in)",
		`LOCATION:Test Hotel\, Testville`,
		`DESCRIPTION:Starts: Mon 7 Sep\, 15:00 London (UTC+1)\nConfirmation: TESTREF`,
		` 1\nLate arrival`,
		"STATUS:CONFIRMED",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:plan-part-42-checkout@aerly",
		"DTSTAMP:20260801T000000Z",
		"LAST-MODIFIED:20260801T000000Z",
		"DTSTART;TZID=Europe/London:20260909T110000",
		"SUMMARY:Test Hotel (Check-out)",
		`LOCATION:Test Hotel\, Testville`,
		`DESCRIPTION:Ends: Wed 9 Sep\, 11:00 London (UTC+1)\nConfirmation: TESTREF1\`,
		` nLate arrival`,
		"STATUS:CONFIRMED",
		"END:VEVENT",
	}, "\n")
	if got != want {
		t.Fatalf("hotel VEVENT output changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// vEventBlocks returns just the VEVENT sections of an iCal document, so a golden
// assertion covers the events without being disturbed by VTIMEZONE detail that
// tracks the tzdata Go happens to be built with.
func vEventBlocks(ics string) string {
	var out []string
	in := false
	for _, line := range strings.Split(ics, "\r\n") {
		if line == "BEGIN:VEVENT" {
			in = true
		}
		if in {
			out = append(out, line)
		}
		if line == "END:VEVENT" {
			in = false
		}
	}
	return strings.Join(out, "\n")
}

// TestICSVehicleHireBandsIntoPickupAndReturn: a multi-day vehicle hire splits
// into two point events, exactly as a multi-night hotel does, but labelled for
// a hire and carrying its own UID suffixes.
func TestICSVehicleHireBandsIntoPickupAndReturn(t *testing.T) {
	ret := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC) // 10:00 CEST
	events := []*store.CalendarEvent{{
		PartID:     55,
		PlanID:     8,
		Type:       "vehicle_hire",
		Title:      "Test Car Hire",
		StartsAt:   time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC), // 09:00 CEST pickup
		EndsAt:     &ret,
		StartTZ:    "Europe/Zurich",
		EndTZ:      "Europe/Zurich",
		StartLabel: "Zurich Airport",
		EndLabel:   "Zurich Hauptbahnhof",
		Status:     "confirmed",
		UpdatedAt:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}
	out := renderICS("Aerly", events, false)

	for _, want := range []string{
		"UID:plan-part-55-pickup@aerly",
		"UID:plan-part-55-return@aerly",
		"SUMMARY:Test Car Hire (Pickup)",
		"SUMMARY:Test Car Hire (Return)",
		"DTSTART;TZID=Europe/Zurich:20260909T090000",
		"DTSTART;TZID=Europe/Zurich:20260911T100000",
		// The return event locates at the drop-off, not the pickup desk.
		"LOCATION:Zurich Hauptbahnhof",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("banded hire ICS missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "UID:plan-part-55@aerly") {
		t.Errorf("a multi-day hire should not also render as one spanning event:\n%s", out)
	}
	if strings.Contains(out, "DTEND;TZID=Europe/Zurich:20260911") {
		t.Errorf("a banded hire must not keep a spanning DTEND:\n%s", out)
	}
}

// TestICSSameDayVehicleHireDoesNotBand: a hire picked up and returned on the
// same local day is one event with a DTEND, not two.
func TestICSSameDayVehicleHireDoesNotBand(t *testing.T) {
	ret := time.Date(2026, 9, 9, 15, 0, 0, 0, time.UTC) // 17:00 CEST
	events := []*store.CalendarEvent{{
		PartID:    55,
		Type:      "vehicle_hire",
		Title:     "Test Day Hire",
		StartsAt:  time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC), // 09:00 CEST
		EndsAt:    &ret,
		StartTZ:   "Europe/Zurich",
		EndTZ:     "Europe/Zurich",
		UpdatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}
	out := renderICS("Aerly", events, false)
	if !strings.Contains(out, "UID:plan-part-55@aerly") {
		t.Errorf("a same-day hire should render as a single event:\n%s", out)
	}
	if strings.Contains(out, "pickup@aerly") || strings.Contains(out, "return@aerly") {
		t.Errorf("a same-day hire should not split:\n%s", out)
	}
	if !strings.Contains(out, "DTEND;TZID=Europe/Zurich:20260909T170000") {
		t.Errorf("a same-day hire should keep its span:\n%s", out)
	}
	if !strings.Contains(out, "SUMMARY:Test Day Hire (Car hire)") {
		t.Errorf("a same-day hire's summary should read \"(Car hire)\", not \"(Vehicle_hire)\":\n%s", out)
	}
}

// TestICSOvernightJourneysNeverBand: banding is opt-in by type. A red-eye
// flight and an overnight train both end on a later local day, and both must
// still render as one event, or every such journey would split into a
// departure tile and an arrival tile.
func TestICSOvernightJourneysNeverBand(t *testing.T) {
	for _, typ := range []string{"flight", "train", "ground", "ferry"} {
		arrive := time.Date(2026, 9, 10, 5, 30, 0, 0, time.UTC)
		events := []*store.CalendarEvent{{
			PartID:    61,
			Type:      typ,
			Title:     "Overnight",
			StartsAt:  time.Date(2026, 9, 9, 21, 0, 0, 0, time.UTC),
			EndsAt:    &arrive,
			StartTZ:   "Europe/London",
			EndTZ:     "Europe/London",
			UpdatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		}}
		out := renderICS("Aerly", events, false)
		if !strings.Contains(out, "UID:plan-part-61@aerly") {
			t.Errorf("%s: an overnight journey must render as a single event:\n%s", typ, out)
		}
		if strings.Count(out, "BEGIN:VEVENT") != 1 {
			t.Errorf("%s: an overnight journey must not band into two events:\n%s", typ, out)
		}
	}
}

// xx100 is a fake flight from Tashkent to Istanbul whose two ends carry the
// given zones and sources.
func xx100(startTZ string, startSrc timing.Source, endTZ string, endSrc timing.Source) *store.CalendarEvent {
	end := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC) // 16:00 in Istanbul
	return &store.CalendarEvent{
		PartID:          100,
		PlanID:          10,
		Type:            "flight",
		Title:           "XX100",
		ConfirmationRef: "TEST01",
		StartsAt:        time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC), // 14:00 in Tashkent
		EndsAt:          &end,
		StartTZ:         startTZ,
		EndTZ:           endTZ,
		StartTZSource:   string(startSrc),
		EndTZSource:     string(endSrc),
		Status:          "confirmed",
		UpdatedAt:       time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestICSTimesWrittenByZoneSource: a zoned time (confirmed or inferred) is
// written against its VTIMEZONE, a floating one in the RFC 5545 floating form
// (its digits, no Z and no TZID), and an unknown one as UTC.
func TestICSTimesWrittenByZoneSource(t *testing.T) {
	cases := []struct {
		name    string
		ev      *store.CalendarEvent
		want    []string
		mustNot []string
	}{
		{
			name: "confirmed",
			ev:   xx100("Asia/Tashkent", timing.Confirmed, "Europe/Istanbul", timing.Confirmed),
			want: []string{"DTSTART;TZID=Asia/Tashkent:20261018T140000", "DTEND;TZID=Europe/Istanbul:20261018T160000"},
		},
		{
			name: "inferred",
			ev:   xx100("Asia/Tashkent", timing.Inferred, "Europe/Istanbul", timing.Inferred),
			want: []string{"DTSTART;TZID=Asia/Tashkent:20261018T140000", "DTEND;TZID=Europe/Istanbul:20261018T160000"},
		},
		{
			name:    "floating",
			ev:      xx100("", timing.Floating, "", timing.Floating),
			want:    []string{"DTSTART:20261018T090000\r\n", "DTEND:20261018T130000\r\n"},
			mustNot: []string{"DTSTART:20261018T090000Z", "TZID="},
		},
		{
			name:    "unknown",
			ev:      xx100("", timing.Unknown, "", timing.Unknown),
			want:    []string{"DTSTART:20261018T090000Z", "DTEND:20261018T130000Z"},
			mustNot: []string{"TZID="},
		},
	}
	for _, tc := range cases {
		out := renderICS("Aerly", []*store.CalendarEvent{tc.ev}, false)
		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q\n---\n%s", tc.name, w, out)
			}
		}
		for _, n := range tc.mustNot {
			if strings.Contains(out, n) {
				t.Errorf("%s: must not contain %q\n---\n%s", tc.name, n, out)
			}
		}
	}
}

// TestICSDescriptionCarriesFullLabels: the description opens with the start
// and end in their own zones, in the same words the app shows, before the
// confirmation and notes.
func TestICSDescriptionCarriesFullLabels(t *testing.T) {
	ev := xx100("Asia/Tashkent", timing.Confirmed, "Europe/Istanbul", timing.Confirmed)
	ev.Notes = "Aisle seat"
	want := "Starts: Sun 18 Oct, 14:00 Tashkent (UTC+5)\n" +
		"Ends: Sun 18 Oct, 16:00 Istanbul (UTC+3)\n" +
		"Confirmation: TEST01\nAisle seat"
	if got := descriptionFor(ev); got != want {
		t.Errorf("descriptionFor =\n%q\nwant\n%q", got, want)
	}
	// With no end there is no Ends line.
	ev.EndsAt = nil
	if got := descriptionFor(ev); strings.Contains(got, "Ends:") {
		t.Errorf("an event with no end must not carry an Ends line: %q", got)
	}
}

// TestICSDescriptionSaysTimeZoneUnknown: when either end has no zone, the
// description says so on its own line, after the time labels.
func TestICSDescriptionSaysTimeZoneUnknown(t *testing.T) {
	cases := map[string]struct {
		ev   *store.CalendarEvent
		want string
	}{
		"floating start": {
			ev: xx100("", timing.Floating, "Europe/Istanbul", timing.Confirmed),
			want: "Starts: Sun 18 Oct, 09:00\n" +
				"Ends: Sun 18 Oct, 16:00 Istanbul (UTC+3)\n" +
				"Time zone unknown\nConfirmation: TEST01",
		},
		"unknown end": {
			ev: xx100("Asia/Tashkent", timing.Confirmed, "", timing.Unknown),
			want: "Starts: Sun 18 Oct, 14:00 Tashkent (UTC+5)\n" +
				"Ends: Sun 18 Oct, 13:00 UTC\n" +
				"Time zone unknown\nConfirmation: TEST01",
		},
	}
	for name, tc := range cases {
		if got := descriptionFor(tc.ev); got != tc.want {
			t.Errorf("%s: descriptionFor =\n%q\nwant\n%q", name, got, tc.want)
		}
	}
	if got := descriptionFor(xx100("Asia/Tashkent", timing.Inferred, "Europe/Istanbul", timing.Confirmed)); strings.Contains(got, "Time zone unknown") {
		t.Errorf("a zoned event must not say its time zone is unknown: %q", got)
	}
}

// vTimezoneBlock returns the VTIMEZONE section for tzid, CRLFs intact.
func vTimezoneBlock(t *testing.T, ics, tzid string) string {
	t.Helper()
	start := strings.Index(ics, "BEGIN:VTIMEZONE\r\nTZID:"+tzid+"\r\n")
	if start < 0 {
		t.Fatalf("no VTIMEZONE for %s:\n%s", tzid, ics)
	}
	end := strings.Index(ics[start:], "END:VTIMEZONE\r\n")
	return ics[start : start+end]
}

// TestICSVTimezoneOneBlockPerChange: a zone with daylight saving gets one
// observance per offset change in the window (the referenced instants padded
// by a year either side), each starting at the local time just before the
// change, and none of them recurring.
func TestICSVTimezoneOneBlockPerChange(t *testing.T) {
	events := []*store.CalendarEvent{
		{PartID: 1, Type: "flight", Title: "XX100", StartsAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
			StartTZ: "Europe/London", StartTZSource: "confirmed"},
		{PartID: 2, Type: "flight", Title: "XX101", StartsAt: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
			StartTZ: "Europe/London", StartTZSource: "confirmed"},
	}
	vtz := vTimezoneBlock(t, renderICS("Aerly", events, false), "Europe/London")
	// 20 Oct 2025 to 3 Nov 2027 holds five changes (Oct 2025, Mar and Oct 2026,
	// Mar and Oct 2027), after an opening observance at the window start.
	if n := strings.Count(vtz, "BEGIN:STANDARD") + strings.Count(vtz, "BEGIN:DAYLIGHT"); n != 6 {
		t.Errorf("observances = %d, want 6\n%s", n, vtz)
	}
	if strings.Contains(vtz, "RRULE") {
		t.Errorf("observances must not recur:\n%s", vtz)
	}
	for _, want := range []string{
		"BEGIN:DAYLIGHT\r\nDTSTART:20251020T130000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0100\r\nTZNAME:BST\r\nEND:DAYLIGHT",
		"BEGIN:STANDARD\r\nDTSTART:20251026T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0000\r\nTZNAME:GMT\r\nEND:STANDARD",
		"BEGIN:DAYLIGHT\r\nDTSTART:20260329T010000\r\nTZOFFSETFROM:+0000\r\nTZOFFSETTO:+0100\r\nTZNAME:BST\r\nEND:DAYLIGHT",
		"BEGIN:STANDARD\r\nDTSTART:20261025T020000\r\n",
		"BEGIN:DAYLIGHT\r\nDTSTART:20270328T010000\r\n",
		"BEGIN:STANDARD\r\nDTSTART:20271031T020000\r\n",
	} {
		if !strings.Contains(vtz, want) {
			t.Errorf("VTIMEZONE missing %q\n%s", want, vtz)
		}
	}
}

// TestICSVTimezoneFixedZone: a zone with no change in the window gets one
// STANDARD observance starting at the window start, and a numeric Go
// abbreviation ("+05") is named by its offset instead.
func TestICSVTimezoneFixedZone(t *testing.T) {
	ev := xx100("Asia/Tashkent", timing.Confirmed, "", timing.Unknown)
	vtz := vTimezoneBlock(t, renderICS("Aerly", []*store.CalendarEvent{ev}, false), "Asia/Tashkent")
	want := "BEGIN:STANDARD\r\nDTSTART:20251018T140000\r\nTZOFFSETFROM:+0500\r\n" +
		"TZOFFSETTO:+0500\r\nTZNAME:UTC+5\r\nEND:STANDARD\r\n"
	if !strings.HasSuffix(vtz, want) || strings.Count(vtz, "BEGIN:") != 2 {
		t.Errorf("fixed-offset VTIMEZONE =\n%s\nwant one observance ending\n%s", vtz, want)
	}
}

// TestICSHotelBlankEndZoneUsesItsOwnSource: a hotel whose check-out has no
// zone writes the check-out by its own source, not by the check-in's zone,
// keeps both historic UIDs, and each edge describes only its own end.
func TestICSHotelBlankEndZoneUsesItsOwnSource(t *testing.T) {
	for _, tc := range []struct {
		src     timing.Source
		dtstart string
		label   string
	}{
		{timing.Unknown, "DTSTART:20260909T100000Z", `Ends: Wed 9 Sep\, 10:00 UTC`},
		{timing.Floating, "DTSTART:20260909T100000\r\n", `Ends: Wed 9 Sep\, 10:00`},
	} {
		checkout := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
		ev := &store.CalendarEvent{
			PartID: 42, PlanID: 7, Type: "hotel", Title: "Test Hotel",
			StartsAt: time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC), EndsAt: &checkout,
			StartTZ: "Europe/London", StartTZSource: "confirmed", EndTZSource: string(tc.src),
			UpdatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		}
		out := vEventBlocks(renderICS("Aerly", []*store.CalendarEvent{ev}, false))
		checkin, checkoutEv, _ := strings.Cut(out, "END:VEVENT\nBEGIN:VEVENT")
		for _, want := range []string{"UID:plan-part-42-checkin@aerly", `DESCRIPTION:Starts: Mon 7 Sep\, 15:00 London (UTC+1)`} {
			if !strings.Contains(checkin, want) {
				t.Errorf("%s: check-in missing %q\n%s", tc.src, want, checkin)
			}
		}
		if strings.Contains(checkin, "Ends:") || strings.Contains(checkin, "Time zone unknown") {
			t.Errorf("%s: check-in must describe only its own end:\n%s", tc.src, checkin)
		}
		if !strings.Contains(checkoutEv, "UID:plan-part-42-checkout@aerly") ||
			!strings.Contains(checkoutEv, "DESCRIPTION:"+tc.label+`\nTime zone unknown`) ||
			strings.Contains(checkoutEv, "Starts:") {
			t.Errorf("%s: check-out UID or description wrong:\n%s", tc.src, checkoutEv)
		}
		raw := renderICS("Aerly", []*store.CalendarEvent{ev}, false)
		if !strings.Contains(raw, tc.dtstart) || strings.Contains(raw, "TZID=Europe/London:20260909") {
			t.Errorf("%s: check-out must be written by its own source (%q):\n%s", tc.src, tc.dtstart, raw)
		}
	}
}

// TestICSBlankEndZoneNeverBorrowsStartZone: banding and the trip banner read a
// blank end zone by its own source (UTC for an unknown end), never the start
// zone. In Tokyo the end would fall on the next day; in UTC it does not.
func TestICSBlankEndZoneNeverBorrowsStartZone(t *testing.T) {
	end := time.Date(2026, 6, 11, 20, 0, 0, 0, time.UTC) // 12 Jun 05:00 in Tokyo
	ev := &store.CalendarEvent{
		PartID: 5, Type: "hotel", Title: "Test Hotel", TripID: 3, TripName: "Test Trip",
		StartsAt: time.Date(2026, 6, 11, 1, 0, 0, 0, time.UTC), EndsAt: &end,
		StartTZ: "Asia/Tokyo", StartTZSource: "confirmed", EndTZSource: "unknown",
	}
	if isBandedEvent(ev) {
		t.Error("an unknown end on the same UTC day must not band by the start zone")
	}
	bands := tripBandEvents([]*store.CalendarEvent{ev})
	if len(bands) != 1 || !bands[0].end.Equal(time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("trip band = %+v, want it to end on 11 Jun", bands)
	}
}

// TestICSRoundTripKeepsZonesAndSources exports a trip with a part of every
// zone source and imports it back through the real parser and resolver: each
// end keeps its instant, its zone and its source, except that inferred comes
// back confirmed, because on import a TZID is the time's own zone.
func TestICSRoundTripKeepsZonesAndSources(t *testing.T) {
	mk := func(id int64, tz string, src timing.Source) *store.CalendarEvent {
		ev := xx100(tz, src, tz, src)
		ev.PartID = id
		return ev
	}
	events := []*store.CalendarEvent{
		mk(1, "Asia/Tashkent", timing.Confirmed),
		mk(2, "Europe/London", timing.Inferred),
		mk(3, "", timing.Floating),
		mk(4, "", timing.Unknown),
	}
	want := map[string]struct {
		tz  string
		src timing.Source
	}{
		"plan-part-1@aerly": {"Asia/Tashkent", timing.Confirmed},
		"plan-part-2@aerly": {"Europe/London", timing.Confirmed}, // inferred is confirmed on import
		"plan-part-3@aerly": {"", timing.Floating},
		"plan-part-4@aerly": {"", timing.Unknown},
	}
	cal, err := importics.Parse(strings.NewReader(renderICS("Test Traveller", events, true)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	seen := 0
	for _, ev := range cal.Events {
		w, ok := want[ev.UID]
		if !ok {
			continue // the trip banner
		}
		seen++
		src := events[0]
		for _, e := range events {
			if fmt.Sprintf("plan-part-%d@aerly", e.PartID) == ev.UID {
				src = e
			}
		}
		for _, end := range []struct {
			dt importics.DateTime
			at time.Time
		}{{ev.Start, src.StartsAt}, {ev.End, *src.EndsAt}} {
			r, ok := importics.Resolve(end.dt, importics.Context{})
			if !ok {
				t.Fatalf("%s: %q did not resolve", ev.UID, end.dt.Raw)
			}
			if !r.At.Equal(end.at) || r.TZ != w.tz || r.Source != w.src {
				t.Errorf("%s: round trip = %s %q %s, want %s %q %s",
					ev.UID, r.At, r.TZ, r.Source, end.at, w.tz, w.src)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("imported %d of %d parts", seen, len(want))
	}
	if events[1].StartTZSource != string(timing.Inferred) {
		t.Fatal("the fixture must export an inferred part for the round trip to test it")
	}
}

// icsOffsetAt exports one event in tz, re-parses the calendar with importics and
// returns the VTIMEZONE's offset for the event's DTSTART wall clock.
func icsOffsetAt(t *testing.T, tz string, at time.Time) (time.Duration, bool) {
	t.Helper()
	ev := &store.CalendarEvent{PartID: 1, Type: "flight", Title: "XX100", StartsAt: at,
		StartTZ: tz, StartTZSource: "confirmed", UpdatedAt: at}
	cal, err := importics.Parse(strings.NewReader(renderICS("Aerly", []*store.CalendarEvent{ev}, false)))
	if err != nil {
		t.Fatal(err)
	}
	z := cal.Zones[tz]
	if z == nil {
		t.Fatalf("no VTIMEZONE for %s", tz)
	}
	loc, _ := time.LoadLocation(tz)
	l := at.In(loc)
	return z.Offset(time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), l.Second(), 0, time.UTC))
}

// TestICSVTimezoneCoversInstantsBeforeFirstChange: a zone with one offset
// change in the window (or none) still resolves an instant before that change,
// because an opening observance starts at the window start.
func TestICSVTimezoneCoversInstantsBeforeFirstChange(t *testing.T) {
	for _, tc := range []struct {
		tz   string
		at   time.Time
		want time.Duration
	}{
		// Almaty moved from +06 to +05 on 2024-03-01.
		{"Asia/Almaty", time.Date(2024, 2, 10, 6, 0, 0, 0, time.UTC), 6 * time.Hour},
		// Pyongyang was on +0830 until it moved to +09 on 2018-05-05.
		{"Asia/Pyongyang", time.Date(2018, 4, 10, 3, 0, 0, 0, time.UTC), 8*time.Hour + 30*time.Minute},
	} {
		off, ok := icsOffsetAt(t, tc.tz, tc.at)
		if !ok || off != tc.want {
			t.Errorf("%s: Offset = %v, %v, want %v, true", tc.tz, off, ok, tc.want)
		}
	}
}

// TestICSVTimezonePermanentChangeIsStandard: a one-way change of standard time
// (Pyongyang +0830 to +09) is a STANDARD observance, not DAYLIGHT.
func TestICSVTimezonePermanentChangeIsStandard(t *testing.T) {
	ev := xx100("Asia/Pyongyang", timing.Confirmed, "", timing.Unknown)
	ev.StartsAt = time.Date(2018, 4, 10, 3, 0, 0, 0, time.UTC)
	vtz := vTimezoneBlock(t, renderICS("Aerly", []*store.CalendarEvent{ev}, false), "Asia/Pyongyang")
	if strings.Contains(vtz, "DAYLIGHT") || !strings.Contains(vtz, "TZOFFSETFROM:+0830\r\nTZOFFSETTO:+0900") {
		t.Errorf("permanent change must be STANDARD:\n%s", vtz)
	}
}

// TestICSVTimezoneDublinSummerIsDaylight: tzdata models Europe/Dublin with
// negative DST, so Go flags winter GMT as DST; the export must still label
// summer IST DAYLIGHT and winter GMT STANDARD.
func TestICSVTimezoneDublinSummerIsDaylight(t *testing.T) {
	ev := xx100("Europe/Dublin", timing.Confirmed, "", timing.Unknown)
	ev.StartsAt = time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	vtz := vTimezoneBlock(t, renderICS("Aerly", []*store.CalendarEvent{ev}, false), "Europe/Dublin")
	for _, want := range []string{
		"BEGIN:DAYLIGHT\r\nDTSTART:20260329T010000\r\nTZOFFSETFROM:+0000\r\nTZOFFSETTO:+0100\r\nTZNAME:IST",
		"BEGIN:STANDARD\r\nDTSTART:20261025T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0000\r\nTZNAME:GMT",
	} {
		if !strings.Contains(vtz, want) {
			t.Errorf("missing %q in:\n%s", want, vtz)
		}
	}
}

// A floating end reads its UTC digits for banding and the trip banner, and
// the same boundary on a zoned end reads in its zone.
func TestICSFloatingEndReadsDigits(t *testing.T) {
	end := time.Date(2026, 6, 12, 0, 30, 0, 0, time.UTC)
	ev := &store.CalendarEvent{
		PartID: 6, Type: "hotel", Title: "Test Hotel", TripID: 3, TripName: "Test Trip",
		StartsAt: time.Date(2026, 6, 11, 14, 0, 0, 0, time.UTC), EndsAt: &end,
		StartTZ: "America/New_York", StartTZSource: "confirmed", EndTZSource: "floating",
	}
	if !isBandedEvent(ev) {
		t.Error("a floating end on the next digit day must band")
	}
	bands := tripBandEvents([]*store.CalendarEvent{ev})
	if len(bands) != 1 || !bands[0].end.Equal(time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("trip band = %+v, want it to end on 12 Jun", bands)
	}
	ev.EndTZ, ev.EndTZSource = "America/New_York", "confirmed" // 11 Jun 20:30 local
	if isBandedEvent(ev) {
		t.Error("the same instant in New York is still the 11th and must not band")
	}
}
