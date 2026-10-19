package airports

import "testing"

func TestLookupKnown(t *testing.T) {
	lat, lon, ok := Lookup("LHR")
	if !ok {
		t.Fatal("LHR should be known")
	}
	if lat == 0 || lon == 0 {
		t.Errorf("LHR coords look wrong: (%v,%v)", lat, lon)
	}
}

func TestLookupCaseAndWhitespaceInsensitive(t *testing.T) {
	want, wlon, _ := Lookup("LHR")
	got, glon, ok := Lookup("  lhr ")
	if !ok || got != want || glon != wlon {
		t.Errorf("case/space-insensitive lookup failed: ok=%v (%v,%v) vs (%v,%v)",
			ok, got, glon, want, wlon)
	}
}

func TestLookupUnknown(t *testing.T) {
	lat, lon, ok := Lookup("ZZZ")
	if ok || lat != 0 || lon != 0 {
		t.Errorf("unknown code should return zeros+false, got (%v,%v,%v)", lat, lon, ok)
	}
}

func TestLookupEmpty(t *testing.T) {
	if _, _, ok := Lookup(""); ok {
		t.Error("empty code should not resolve")
	}
}

func TestTableEntriesPlausible(t *testing.T) {
	if len(table) < 5000 {
		t.Errorf("table has %d entries; the generated table should hold every IATA airport", len(table))
	}
	for code, e := range table {
		if len(code) != 3 {
			t.Errorf("IATA code %q is not 3 letters", code)
		}
		if e.Lat < -90 || e.Lat > 90 || e.Lon < -180 || e.Lon > 180 {
			t.Errorf("%s has out-of-range coords (%v,%v)", code, e.Lat, e.Lon)
		}
		if e.Name == "" {
			t.Errorf("%s has empty name", code)
		}
		if e.TZ == "" {
			t.Errorf("%s has empty TZ", code)
		}
	}
}

func TestLookupCity(t *testing.T) {
	city, ok := LookupCity("CDG")
	if !ok || city != "Paris" {
		t.Errorf("CDG should resolve to Paris, got (%q,%v)", city, ok)
	}
	// Case- and whitespace-insensitive, like the other lookups.
	if city, ok := LookupCity("  jfk "); !ok || city != "New York" {
		t.Errorf("jfk should resolve to New York, got (%q,%v)", city, ok)
	}
	if _, ok := LookupCity("ZZZ"); ok {
		t.Error("unknown code should not resolve")
	}
	if _, ok := LookupCity(""); ok {
		t.Error("empty code should not resolve")
	}
}

func TestLabel(t *testing.T) {
	// On-table: uses the embedded airport name, ignores any provider name.
	if got := Label("LHR", "ignored"); got != "London Heathrow (LHR)" {
		t.Errorf("LHR: got %q, want %q", got, "London Heathrow (LHR)")
	}
	// Case- and whitespace-insensitive.
	if got := Label("  lhr ", ""); got != "London Heathrow (LHR)" {
		t.Errorf("lhr: got %q, want %q", got, "London Heathrow (LHR)")
	}
	// Off-table with a provider name: "Name (CODE)".
	if got := Label("ZZZ", "Test Field"); got != "Test Field (ZZZ)" {
		t.Errorf("ZZZ: got %q, want %q", got, "Test Field (ZZZ)")
	}
	// Off-table without any name: bare code, never blank.
	if got := Label("ZZZ", ""); got != "ZZZ" {
		t.Errorf("ZZZ no-name: got %q, want %q", got, "ZZZ")
	}
	if got := Label("zzz", "  "); got != "ZZZ" {
		t.Errorf("ZZZ blank provider name: got %q, want %q", got, "ZZZ")
	}
	// Blank code yields blank.
	if got := Label("", "Test Field"); got != "" {
		t.Errorf("blank code: got %q, want \"\"", got)
	}
}

func TestLookupTZ(t *testing.T) {
	tz, ok := LookupTZ("LHR")
	if !ok || tz != "Europe/London" {
		t.Errorf("LHR should resolve to Europe/London, got (%q,%v)", tz, ok)
	}
	// The boundary data gives Hanoi as Asia/Bangkok; the generator corrects it.
	if tz, _ := LookupTZ("HAN"); tz != "Asia/Ho_Chi_Minh" {
		t.Errorf("HAN zone = %q, want Asia/Ho_Chi_Minh", tz)
	}
	// Tegel is closed, so OurAirports drops it, but old trips still name it.
	if tz, _ := LookupTZ("TXL"); tz != "Europe/Berlin" {
		t.Errorf("TXL zone = %q, want Europe/Berlin", tz)
	}
	if _, ok := LookupTZ("ZZZ"); ok {
		t.Error("unknown code should not resolve")
	}
}

func TestCodeIn(t *testing.T) {
	for label, want := range map[string]string{
		"LHR":                    "LHR",
		"  LHR ":                 "LHR",
		"Sal (SID)":              "SID",
		"Heathrow Airport LHR":   "LHR",
		"LHR Terminal 5":         "LHR",
		"LHR T5":                 "LHR",
		"T2 MAN":                 "MAN",
		"Hotel near LHR":         "",
		"THE Grand Hotel":        "",
		"Mountain INN":           "",
		"BUS station":            "",
		"lhr":                    "",
		"Terminal ZZZ":           "",
		"T5":                     "",
		"Gatwick Airport":        "",
		"":                       "",
		"Airport shuttle to THE": "THE",
		"Test Hotel (Testville)": "",
		"Heathrow T5 (lhr)":      "",
		"Station 2 TX1 LHR":      "",
		"Testville T123 LHR":     "",
		"Testville TA LHR":       "",
	} {
		if got := CodeIn(label); got != want {
			t.Errorf("CodeIn(%q) = %q, want %q", label, got, want)
		}
	}
}
