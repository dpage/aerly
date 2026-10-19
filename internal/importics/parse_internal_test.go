package importics

import (
	"strings"
	"testing"
)

func TestParseGeo(t *testing.T) {
	if g := parseGeo("51.5;-0.12"); g == nil || g.Lat != 51.5 || g.Lon != -0.12 {
		t.Errorf("valid GEO = %+v, want {51.5, -0.12}", g)
	}
	if g := parseGeo("51.5"); g != nil {
		t.Errorf("GEO without ';' should be nil, got %+v", g)
	}
	if g := parseGeo("notanumber;-0.12"); g != nil {
		t.Errorf("GEO with bad lat should be nil, got %+v", g)
	}
	if g := parseGeo("51.5;notanumber"); g != nil {
		t.Errorf("GEO with bad lon should be nil, got %+v", g)
	}
}

func TestParseLineEdgeCases(t *testing.T) {
	if _, ok := parseLine("NOCOLONHERE"); ok {
		t.Error("a line without ':' should not parse")
	}
	if _, ok := parseLine(":valuewithnoname"); ok {
		t.Error("a line with an empty name should not parse")
	}
	// A param fragment with no '=' is skipped, not turned into a param.
	p, ok := parseLine(`DTSTART;BARE;TZID="Europe/London":20260101T090000`)
	if !ok {
		t.Fatal("expected the line to parse")
	}
	if p.Name != "DTSTART" {
		t.Errorf("Name = %q, want DTSTART", p.Name)
	}
	if _, exists := p.Params["BARE"]; exists {
		t.Error("a param with no '=' should be skipped")
	}
	if p.Params["TZID"] != "Europe/London" {
		t.Errorf("TZID param = %q, want Europe/London (quotes trimmed)", p.Params["TZID"])
	}
}

func TestIndexUnquoted(t *testing.T) {
	// The ':' inside the quoted run is ignored; the bare one is found.
	if i := indexUnquoted(`A="x:y":z`, ':'); i != 7 {
		t.Errorf("indexUnquoted = %d, want 7", i)
	}
	if i := indexUnquoted("noseparator", ':'); i != -1 {
		t.Errorf("indexUnquoted with no match = %d, want -1", i)
	}
}

func TestUnescapeText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},         // no backslash: early return
		{`a\nb`, "a\nb"},           // \n -> newline
		{`a\Nb`, "a\nb"},           // \N -> newline
		{`a\\b`, `a\b`},            // escaped backslash
		{`a\;b`, "a;b"},            // escaped semicolon
		{`a\,b`, "a,b"},            // escaped comma
		{`a\xb`, "axb"},            // unknown escape: drop the backslash
		{`trailing\`, `trailing\`}, // dangling backslash kept verbatim
	}
	for _, c := range cases {
		if got := unescapeText(c.in); got != c.want {
			t.Errorf("unescapeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseKeepsUnterminatedVEVENT(t *testing.T) {
	// No END:VEVENT — the parser should still emit the partial event rather than
	// dropping it, and ignore a content-less line.
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:abc@example.com\r\nSUMMARY:Lonely\r\n"
	cal, err := Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Events) != 1 {
		t.Fatalf("got %d events, want 1 (unterminated VEVENT retained)", len(cal.Events))
	}
	if cal.Events[0].Summary != "Lonely" || cal.Events[0].UID != "abc@example.com" {
		t.Errorf("unexpected event: %+v", cal.Events[0])
	}
}

// TestResolveTZIDWindowsMappingMustLoad: a Windows name whose mapped IANA zone
// does not load is unknown, so a nil location can never reach ParseInLocation.
func TestResolveTZIDWindowsMappingMustLoad(t *testing.T) {
	windowsZones["Test Bogus Standard Time"] = "Nowhere/Nonexistent"
	defer delete(windowsZones, "Test Bogus Standard Time")
	if tz, known := resolveTZID("Test Bogus Standard Time"); known || tz != "" {
		t.Errorf("resolveTZID = %q, %v, want unknown", tz, known)
	}
	if tz, known := resolveTZID("GMT Standard Time"); !known || tz != "Europe/London" {
		t.Errorf("resolveTZID(GMT Standard Time) = %q, %v, want Europe/London", tz, known)
	}
}

// TestParseXWRTimezoneWindowsName: a Windows zone name in X-WR-TIMEZONE maps to
// its IANA zone; an IANA name and an unrecognised one pass through unchanged.
func TestParseXWRTimezoneWindowsName(t *testing.T) {
	for in, want := range map[string]string{
		"GMT Standard Time": "Europe/London",
		"Asia/Tashkent":     "Asia/Tashkent",
		"Not A Zone":        "Not A Zone",
	} {
		cal, err := Parse(strings.NewReader("BEGIN:VCALENDAR\nX-WR-TIMEZONE:" + in + "\nEND:VCALENDAR\n"))
		if err != nil {
			t.Fatal(err)
		}
		if cal.Timezone != want {
			t.Errorf("X-WR-TIMEZONE:%s -> %q, want %q", in, cal.Timezone, want)
		}
	}
}
