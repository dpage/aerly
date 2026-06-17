package importics

import (
	"strings"
	"testing"
)

// sampleICS exercises the iCalendar features the parser must handle. It is a
// representative shape (folded lines, TZID-local / UTC / date-only values,
// quoted params, escaped text) — not a verified TripIt export. The mapper will
// be written and fixtured against a real sample.
const sampleICS = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//TripIt//TripIt Events//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:trip-1-air-1@tripit.com\r\n" +
	"DTSTART;TZID=America/New_York:20170223T080000\r\n" +
	"DTEND;TZID=Europe/London:20170223T200000\r\n" +
	"SUMMARY:AA 100 JFK to LHR\r\n" +
	"LOCATION:John F Kennedy Intl (JFK)\r\n" +
	"DESCRIPTION:Flight AA100\\nConfirmation: ABC123\\, seat 4A\r\n" +
	" \\nThis line was folded onto the previous one.\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:trip-1-lodging-1@tripit.com\r\n" +
	"DTSTART;VALUE=DATE:20170223\r\n" +
	"DTEND;VALUE=DATE:20170226\r\n" +
	"SUMMARY:The Savoy\r\n" +
	"X-ALT-DESC;FMTTYPE=text/html:<p>ignored</p>\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:trip-1-air-2@tripit.com\r\n" +
	"DTSTART:20170301T093000Z\r\n" +
	"SUMMARY:BA 178 LHR to JFK\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseSample(t *testing.T) {
	cal, err := Parse(strings.NewReader(sampleICS))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(cal.ProdID, "TripIt") {
		t.Errorf("ProdID = %q, want it to contain TripIt", cal.ProdID)
	}
	if len(cal.Events) != 3 {
		t.Fatalf("got %d events, want 3", len(cal.Events))
	}

	// Event 1: TZID-local flight with a folded, escaped description.
	e := cal.Events[0]
	if e.Summary != "AA 100 JFK to LHR" {
		t.Errorf("summary = %q", e.Summary)
	}
	if e.Start.TZID != "America/New_York" || !e.Start.HasTime || e.Start.IsUTC {
		t.Errorf("start = %+v, want TZID-local with time", e.Start)
	}
	if got := e.Start.Time.Format("2006-01-02 15:04"); got != "2017-02-23 08:00" {
		t.Errorf("start wall clock = %q, want 2017-02-23 08:00", got)
	}
	if !strings.Contains(e.Description, "Confirmation: ABC123, seat 4A") {
		t.Errorf("description not unescaped: %q", e.Description)
	}
	if !strings.Contains(e.Description, "folded onto the previous one") {
		t.Errorf("folded continuation lost: %q", e.Description)
	}
	if !strings.Contains(e.Description, "\n") {
		t.Errorf("escaped newline not decoded: %q", e.Description)
	}

	// Event 2: date-only lodging.
	h := cal.Events[1]
	if h.Start.HasTime || h.Start.Time.Format("2006-01-02") != "2017-02-23" {
		t.Errorf("lodging start = %+v, want date-only 2017-02-23", h.Start)
	}
	if h.End.Time.Format("2006-01-02") != "2017-02-26" {
		t.Errorf("lodging end = %+v, want 2017-02-26", h.End)
	}

	// Event 3: UTC instant.
	r := cal.Events[2]
	if !r.Start.IsUTC || r.Start.Time.Format("2006-01-02T15:04:05Z") != "2017-03-01T09:30:00Z" {
		t.Errorf("return start = %+v, want UTC 2017-03-01T09:30:00Z", r.Start)
	}
}

func TestParseQuotedParamColon(t *testing.T) {
	// A quoted param value containing a ':' must not split the name/params off
	// from the value early.
	const ics = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n" +
		"DTSTART;TZID=\"Some/Zone:With:Colons\":20170223T080000\r\n" +
		"SUMMARY:Edge case\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(cal.Events))
	}
	if got := cal.Events[0].Start.TZID; got != "Some/Zone:With:Colons" {
		t.Errorf("TZID = %q, want Some/Zone:With:Colons", got)
	}
	if cal.Events[0].Start.Raw != "20170223T080000" {
		t.Errorf("raw = %q, want 20170223T080000", cal.Events[0].Start.Raw)
	}
	// The TZID can't be loaded, so the time must be flagged Floating (not a
	// resolved zoned instant) while keeping the original name for later mapping.
	st := cal.Events[0].Start
	if !st.Floating || st.IsUTC {
		t.Errorf("unresolvable TZID should be floating & non-UTC, got %+v", st)
	}
}

func TestParseEmpty(t *testing.T) {
	cal, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Events) != 0 {
		t.Errorf("got %d events, want 0", len(cal.Events))
	}
}
