package importics

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

const utcStamp = "2006-01-02T15:04:05Z"

func TestWindowsToIANA(t *testing.T) {
	if got, ok := WindowsToIANA("W. Europe Standard Time"); !ok || got != "Europe/Berlin" {
		t.Errorf("W. Europe Standard Time = %q, %v; want Europe/Berlin", got, ok)
	}
	if got, ok := WindowsToIANA("GMT Standard Time"); !ok || got != "Europe/London" {
		t.Errorf("GMT Standard Time = %q, %v; want Europe/London", got, ok)
	}
	if _, ok := WindowsToIANA("Example Zone"); ok {
		t.Error("a name that is not a Windows zone should not map")
	}
}

func TestParseOutlookWindowsTZID(t *testing.T) {
	cal := parseFixture(t, "outlook_windows_tz.ics")
	if len(cal.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(cal.Events))
	}
	if z := cal.Zones["W. Europe Standard Time"]; z == nil || len(z.Observances) != 2 {
		t.Fatalf("VTIMEZONE not captured: %+v", cal.Zones)
	}

	// The quoted Windows name maps to Europe/Berlin, in summer time (+02:00).
	st := cal.Events[0].Start
	if st.TZID != "Europe/Berlin" || st.RawTZID != "W. Europe Standard Time" {
		t.Errorf("start zone = %q (raw %q), want Europe/Berlin (raw W. Europe Standard Time)", st.TZID, st.RawTZID)
	}
	if st.Floating || st.UnknownTZID || st.IsUTC {
		t.Errorf("start flags = %+v, want a resolved zoned instant", st)
	}
	if got := st.Time.UTC().Format(utcStamp); got != "2026-07-01T07:00:00Z" {
		t.Errorf("start = %s, want 2026-07-01T07:00:00Z", got)
	}
	if got := cal.Events[0].End.Time.UTC().Format(utcStamp); got != "2026-07-01T08:00:00Z" {
		t.Errorf("end = %s, want 2026-07-01T08:00:00Z", got)
	}

	// TZID=Local, in any case, is no zone at all: floating, not unknown.
	for _, dt := range []DateTime{cal.Events[1].Start, cal.Events[1].End} {
		if !dt.Floating || dt.UnknownTZID || dt.TZID != "" {
			t.Errorf("TZID=%s = %+v, want floating with no zone", dt.RawTZID, dt)
		}
	}
	if got := cal.Events[1].Start.Time.Format(utcStamp); got != "2026-07-02T09:00:00Z" {
		t.Errorf("local start digits = %s, want 09:00 untouched", got)
	}
}

func TestParseCustomVTimezone(t *testing.T) {
	// The VTIMEZONE blocks follow the VEVENTs in this file, so the instants
	// can only be settled once the whole calendar has been read.
	cal := parseFixture(t, "custom_vtimezone.ics")
	if len(cal.Events) != 2 || len(cal.Zones) != 2 {
		t.Fatalf("got %d events and %d zones, want 2 and 2", len(cal.Events), len(cal.Zones))
	}
	cases := []struct {
		name string
		dt   DateTime
		want string
	}{
		{"fixed +05:30 start", cal.Events[0].Start, "2026-07-01T03:30:00Z"},
		{"fixed +05:30 end", cal.Events[0].End, "2026-07-01T05:30:00Z"},
		{"day before the change (+01:00)", cal.Events[1].Start, "2026-03-28T11:00:00Z"},
		{"day of the change (+02:00)", cal.Events[1].End, "2026-03-29T10:00:00Z"},
	}
	for _, c := range cases {
		if got := c.dt.Time.UTC().Format(utcStamp); got != c.want {
			t.Errorf("%s = %s, want %s", c.name, got, c.want)
		}
		// The instant is known, but no IANA name is, so the zone still needs
		// a location before it can be stored.
		if c.dt.Floating || !c.dt.UnknownTZID || c.dt.TZID != "" {
			t.Errorf("%s flags = %+v, want an instant with an unknown zone", c.name, c.dt)
		}
	}
	if got := cal.Events[1].Start.RawTZID; got != "Example DST Zone" {
		t.Errorf("RawTZID = %q, want Example DST Zone (quotes trimmed)", got)
	}
}

func TestParseUnknownTZIDWithoutVTimezone(t *testing.T) {
	const ics = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n" +
		"DTSTART;TZID=Pacific Time:20260701T090000\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	st := cal.Events[0].Start
	if !st.Floating || !st.UnknownTZID || st.TZID != "" || st.RawTZID != "Pacific Time" {
		t.Errorf("start = %+v, want floating, unknown, raw Pacific Time", st)
	}
	if got := st.Time.Format(utcStamp); got != "2026-07-01T09:00:00Z" {
		t.Errorf("digits = %s, want 09:00 untouched", got)
	}
}

func TestParseIANATZIDKeepsRaw(t *testing.T) {
	const ics = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n" +
		"DTSTART;TZID=Europe/London:20260701T090000\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	st := cal.Events[0].Start
	if st.TZID != "Europe/London" || st.RawTZID != "Europe/London" || st.Floating || st.UnknownTZID {
		t.Errorf("start = %+v, want a resolved Europe/London instant", st)
	}
	if got := st.Time.UTC().Format(utcStamp); got != "2026-07-01T08:00:00Z" {
		t.Errorf("start = %s, want 2026-07-01T08:00:00Z", got)
	}
}

func TestParseVTimezoneEdgeCases(t *testing.T) {
	// A zone with no TZID is dropped; an observance with an unreadable offset
	// is dropped; an unknown nested component inside a VTIMEZONE is ignored;
	// a VTIMEZONE that cannot answer leaves the time floating.
	const ics = "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VTIMEZONE\r\n" +
		"BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\n" +
		"END:VTIMEZONE\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Example Broken\r\nX-EXAMPLE:ignored\r\n" +
		"BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:soon\r\nEND:STANDARD\r\n" +
		"BEGIN:X-EXAMPLE\r\nEND:X-EXAMPLE\r\n" +
		"END:VTIMEZONE\r\n" +
		"BEGIN:VEVENT\r\nDTSTART;TZID=Example Broken:20260701T090000\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Zones) != 1 {
		t.Fatalf("zones = %+v, want only Example Broken", cal.Zones)
	}
	if z := cal.Zones["Example Broken"]; z == nil || len(z.Observances) != 0 {
		t.Errorf("Example Broken = %+v, want no observances", z)
	}
	st := cal.Events[0].Start
	if !st.Floating || !st.UnknownTZID || st.Time.Format(utcStamp) != "2026-07-01T09:00:00Z" {
		t.Errorf("start = %+v, want floating digits untouched", st)
	}
}

// cet is a central European zone written the way most producers write one.
func cet() *VTimezone {
	return &VTimezone{TZID: "Example CET", Observances: []Observance{
		{Start: digits("1981-03-29T02:00:00Z"), OffsetFrom: hours(1), OffsetTo: hours(2), RRule: "FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU", IsDST: true},
		{Start: digits("1996-10-27T03:00:00Z"), OffsetFrom: hours(2), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU"},
	}}
}

func digits(s string) time.Time {
	t, err := time.Parse(utcStamp, s)
	if err != nil {
		panic(err)
	}
	return t
}

func hours(h float64) time.Duration { return time.Duration(h * float64(time.Hour)) }

func TestOffset(t *testing.T) {
	// A US-style zone using the second-Sunday and first-Sunday forms, with
	// an older rule ended by UNTIL and one limited by COUNT.
	us := &VTimezone{TZID: "Example Eastern", Observances: []Observance{
		{Start: digits("1967-04-30T02:00:00Z"), OffsetFrom: hours(-5), OffsetTo: hours(-4), RRule: "FREQ=YEARLY;BYMONTH=4;BYDAY=-1SU;UNTIL=19730429T070000Z", IsDST: true},
		{Start: digits("2007-03-11T02:00:00Z"), OffsetFrom: hours(-5), OffsetTo: hours(-4), RRule: "FREQ=YEARLY;BYMONTH=3;BYDAY=2SU", IsDST: true},
		{Start: digits("2007-11-04T02:00:00Z"), OffsetFrom: hours(-4), OffsetTo: hours(-5), RRule: "FREQ=YEARLY;BYMONTH=11;BYDAY=1SU"},
		{Start: digits("1967-10-29T02:00:00Z"), OffsetFrom: hours(-4), OffsetTo: hours(-5), RRule: "FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU;COUNT=40"},
	}}
	// Changes by RDATE only, with no rule.
	rdates := &VTimezone{TZID: "Example RDATE", Observances: []Observance{
		{Start: digits("2000-01-01T00:00:00Z"), OffsetFrom: hours(3), OffsetTo: hours(3)},
		{Start: digits("2026-04-01T00:00:00Z"), OffsetFrom: hours(3), OffsetTo: hours(4), RDates: []time.Time{digits("2027-04-01T00:00:00Z")}, IsDST: true},
		{Start: digits("2026-10-01T00:00:00Z"), OffsetFrom: hours(4), OffsetTo: hours(3), RDates: []time.Time{digits("2027-10-01T00:00:00Z")}},
	}}
	cases := []struct {
		name  string
		z     *VTimezone
		local string
		want  time.Duration
	}{
		{"CET winter", cet(), "2026-01-15T12:00:00Z", hours(1)},
		{"CET just before spring change", cet(), "2026-03-29T01:59:00Z", hours(1)},
		{"CET in the spring gap keeps the old offset", cet(), "2026-03-29T02:30:00Z", hours(1)},
		{"CET once the gap is past", cet(), "2026-03-29T03:00:00Z", hours(2)},
		{"CET summer", cet(), "2026-07-01T09:00:00Z", hours(2)},
		{"CET autumn overlap takes the first occurrence", cet(), "2026-10-25T02:30:00Z", hours(2)},
		{"CET after autumn change", cet(), "2026-10-25T03:00:00Z", hours(1)},
		{"US second Sunday of March", us, "2026-03-08T12:00:00Z", hours(-4)},
		{"US day before second Sunday", us, "2026-03-07T12:00:00Z", hours(-5)},
		{"US first Sunday of November", us, "2026-11-01T12:00:00Z", hours(-5)},
		{"US 1970 summer under the UNTIL rule", us, "1970-07-01T12:00:00Z", hours(-4)},
		{"US 2005 April: UNTIL ended the old rule", us, "2005-05-15T12:00:00Z", hours(-5)},
		{"RDATE summer 2026 (DTSTART)", rdates, "2026-06-01T00:00:00Z", hours(4)},
		{"RDATE winter 2027", rdates, "2027-01-01T00:00:00Z", hours(3)},
		{"RDATE summer 2027", rdates, "2027-06-01T00:00:00Z", hours(4)},
		{"RDATE winter 2028", rdates, "2028-01-01T00:00:00Z", hours(3)},
	}
	for _, c := range cases {
		got, ok := c.z.Offset(digits(c.local))
		if !ok || got != c.want {
			t.Errorf("%s: Offset = %v, %v; want %v", c.name, got, ok, c.want)
		}
	}
}

func TestOffsetAgreesWithAnchor(t *testing.T) {
	// Around both changes, including the gap and the overlap, the instant a
	// VTIMEZONE gives must be the one timing.Anchor gives for the IANA zone.
	for _, s := range []string{
		"2026-03-29T01:59:00Z", "2026-03-29T02:00:00Z", "2026-03-29T02:30:00Z", "2026-03-29T03:00:00Z",
		"2026-10-25T01:59:00Z", "2026-10-25T02:00:00Z", "2026-10-25T02:30:00Z", "2026-10-25T03:00:00Z",
	} {
		local := digits(s)
		off, ok := cet().Offset(local)
		want, _ := timing.Anchor(local, "Europe/Berlin")
		if got := local.Add(-off); !ok || !got.Equal(want) {
			t.Errorf("%s: VTIMEZONE gives %s, timing.Anchor gives %s", s, got.Format(utcStamp), want.UTC().Format(utcStamp))
		}
	}
}

func TestOffsetCannotTell(t *testing.T) {
	cases := map[string]*VTimezone{
		"no observances": {TZID: "Example Empty"},
		"before every onset": {Observances: []Observance{
			{Start: digits("2030-01-01T00:00:00Z"), OffsetTo: hours(1)},
		}},
		"unsupported frequency": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=MONTHLY;BYDAY=1SU"},
		}},
		"unsupported part": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH=3;BYDAY=SU;BYMONTHDAY=8,9,10,11,12,13,14"},
		}},
		"BYDAY with no ordinal": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH=3;BYDAY=SU"},
		}},
		"bad weekday": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH=3;BYDAY=-1XX"},
		}},
		"bad month": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH=13;BYDAY=-1SU"},
		}},
		"bad COUNT": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU;COUNT=many"},
		}},
		"bad UNTIL": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU;UNTIL=someday"},
		}},
		"malformed part": {Observances: []Observance{
			{Start: digits("2000-01-01T00:00:00Z"), OffsetTo: hours(1), RRule: "FREQ=YEARLY;BYMONTH"},
		}},
	}
	for name, z := range cases {
		if got, ok := z.Offset(digits("2026-07-01T09:00:00Z")); ok {
			t.Errorf("%s: Offset = %v, true; want false", name, got)
		}
	}
}

func TestOffsetRuleForms(t *testing.T) {
	// Each zone has a base observance that resets to +00:00 on the first
	// Sunday of every January, and a rule under test that moves to +01:00.
	cases := []struct {
		name  string
		rule  string
		start string
		at    string
		want  time.Duration
	}{
		// No BYDAY: the rule repeats on DTSTART's month and day each year.
		{"no BYDAY", "FREQ=YEARLY", "2000-06-15T00:00:00Z", "2026-07-01T09:00:00Z", hours(1)},
		// June 2026 has only four Sundays, so the rule has no onset that
		// year (rather than spilling into July) and January's reset holds.
		{"fifth Sunday missing", "FREQ=YEARLY;BYMONTH=6;BYDAY=5SU", "2000-01-01T00:00:00Z", "2026-07-10T09:00:00Z", 0},
		{"plus-signed ordinal", "FREQ=YEARLY;BYMONTH=6;BYDAY=+1SU", "2000-01-01T00:00:00Z", "2026-07-01T09:00:00Z", hours(1)},
		{"date-only UNTIL in the past", "FREQ=YEARLY;BYMONTH=6;BYDAY=1SU;UNTIL=20200101", "2000-01-01T00:00:00Z", "2026-07-01T09:00:00Z", 0},
		// COUNT=3 from 2020 gives onsets in 2020-2022 only, so by 2026 the
		// January reset is the latest; ignoring COUNT would give +01:00.
		{"COUNT expired", "FREQ=YEARLY;BYMONTH=6;BYDAY=1SU;COUNT=3", "2020-06-07T00:00:00Z", "2026-07-01T09:00:00Z", 0},
		{"COUNT not yet reached", "FREQ=YEARLY;BYMONTH=6;BYDAY=1SU;COUNT=10", "2020-06-07T00:00:00Z", "2026-07-01T09:00:00Z", hours(1)},
		{"INTERVAL=1 is every year", "FREQ=YEARLY;INTERVAL=1;BYMONTH=6;BYDAY=1SU", "2000-01-01T00:00:00Z", "2026-07-01T09:00:00Z", hours(1)},
	}
	for _, c := range cases {
		z := &VTimezone{Observances: []Observance{
			{Start: digits("1990-01-07T00:00:00Z"), RRule: "FREQ=YEARLY;BYMONTH=1;BYDAY=1SU"},
			{Start: digits(c.start), OffsetTo: hours(1), RRule: c.rule},
		}}
		got, ok := z.Offset(digits(c.at))
		if !ok || got != c.want {
			t.Errorf("%s: Offset = %v, %v; want %v", c.name, got, ok, c.want)
		}
	}
	interval2 := &VTimezone{Observances: []Observance{{Start: digits("2000-01-01T00:00:00Z"), RRule: "FREQ=YEARLY;INTERVAL=2"}}}
	if _, ok := interval2.Offset(digits("2026-07-01T09:00:00Z")); ok {
		t.Error("INTERVAL other than 1 should be unsupported")
	}
}

func TestParseOffsetValue(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"+0530", hours(5.5), true},
		{"-0800", hours(-8), true},
		{"+023000", 2*time.Hour + 30*time.Minute, true},
		{"+0000", 0, true},
		{"0100", 0, false},
		{"+01", 0, false},
		{"+01xx", 0, false},
		{"+0160", 0, false},
		{"+010060", 0, false},
		{"+2300", hours(23), true},
		{"+2400", 0, false},
		{"+9900", 0, false},
	}
	for _, c := range cases {
		got, ok := parseUTCOffset(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseUTCOffset(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseObservanceRDates(t *testing.T) {
	// RDATE lists, a date-only RDATE and an unreadable entry (skipped), with
	// a PERIOD-valued RDATE ignored entirely.
	const ics = "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Example RDATE\r\n" +
		"BEGIN:DAYLIGHT\r\nDTSTART:20260401T000000\r\nTZOFFSETFROM:+0300\r\nTZOFFSETTO:+0400\r\n" +
		"RDATE:20270401T000000,nonsense\r\nRDATE;VALUE=DATE:20280401\r\n" +
		"RDATE;VALUE=PERIOD:20290401T000000/PT1H\r\nEND:DAYLIGHT\r\n" +
		"END:VTIMEZONE\r\nEND:VCALENDAR\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obs := cal.Zones["Example RDATE"].Observances
	if len(obs) != 1 || !obs[0].IsDST || len(obs[0].RDates) != 2 {
		t.Fatalf("observances = %+v, want one DST observance with two RDATEs", obs)
	}
	if got := obs[0].RDates[1].Format(utcStamp); got != "2028-04-01T00:00:00Z" {
		t.Errorf("date-only RDATE = %s, want midnight 2028-04-01", got)
	}
}

func TestParseUnterminatedVTimezoneKeepsEvents(t *testing.T) {
	// A VTIMEZONE missing its END must not swallow the events after it: it
	// is dropped as incomplete and the VEVENT imports as before.
	const ics = "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID:Example Zone\r\n" +
		"BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0530\r\nTZOFFSETTO:+0530\r\nEND:STANDARD\r\n" +
		"BEGIN:VEVENT\r\nUID:after-zone@example.com\r\nSUMMARY:XX100\r\n" +
		"DTSTART;TZID=Example Zone:20260701T090000\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Events) != 1 || cal.Events[0].UID != "after-zone@example.com" {
		t.Fatalf("events = %+v, want the one VEVENT", cal.Events)
	}
	if len(cal.Zones) != 0 {
		t.Errorf("zones = %+v, want the incomplete VTIMEZONE dropped", cal.Zones)
	}
	if st := cal.Events[0].Start; !st.Floating || st.Time.Format(utcStamp) != "2026-07-01T09:00:00Z" {
		t.Errorf("start = %+v, want floating digits untouched", st)
	}

	// The same holds when the calendar simply ends inside the zone.
	cal, err = Parse(strings.NewReader("BEGIN:VCALENDAR\r\nBEGIN:VTIMEZONE\r\nTZID:Example Zone\r\nEND:VCALENDAR\r\n" +
		"BEGIN:VCALENDAR\r\nPRODID:-//Example//Second//EN\r\nEND:VCALENDAR\r\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Zones) != 0 || cal.ProdID != "-//Example//Second//EN" {
		t.Errorf("zones = %+v, ProdID = %q; want no zones and the later header read", cal.Zones, cal.ProdID)
	}
}

func TestParseTZIDWhitespaceMatchesVTimezone(t *testing.T) {
	// Stray spaces around the VTIMEZONE's TZID and inside the event's quoted
	// TZID param must not stop the two matching.
	const ics = "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VTIMEZONE\r\nTZID: Example Zone \r\n" +
		"BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0530\r\nTZOFFSETTO:+0530\r\nEND:STANDARD\r\n" +
		"END:VTIMEZONE\r\n" +
		"BEGIN:VEVENT\r\nDTSTART;TZID=\" Example Zone\":20260701T090000\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cal.Zones["Example Zone"] == nil {
		t.Fatalf("zones = %+v, want the TZID trimmed to Example Zone", cal.Zones)
	}
	st := cal.Events[0].Start
	if st.RawTZID != "Example Zone" || st.Floating || st.Time.UTC().Format(utcStamp) != "2026-07-01T03:30:00Z" {
		t.Errorf("start = %+v, want 03:30Z from the trimmed Example Zone", st)
	}
}
