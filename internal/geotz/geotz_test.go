package geotz

import "testing"

// TestLookup_KnownPoints checks that well-known coordinates resolve to their
// IANA zone. It also pins the lon/lat argument order: the inputs are (lat, lon)
// but tzf wants (lon, lat) — a regression there would map e.g. JFK to a zone in
// the southern Indian Ocean (lat/lon swapped).
func TestLookup_KnownPoints(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		want     string
	}{
		{"JFK", 40.6413, -73.7781, "America/New_York"},
		{"LHR", 51.4700, -0.4543, "Europe/London"},
		{"SYD", -33.9399, 151.1753, "Australia/Sydney"},
	}
	for _, c := range cases {
		got, ok := Lookup(c.lat, c.lon)
		if !ok {
			t.Errorf("%s: Lookup returned not-found", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Lookup(%v,%v) = %q, want %q", c.name, c.lat, c.lon, got, c.want)
		}
	}
}

// TestLookup_OpenOcean confirms tzf's global coverage: a mid-Pacific point has
// no land zone but resolves to a nautical Etc/GMT offset rather than failing.
func TestLookup_OpenOcean(t *testing.T) {
	name, ok := Lookup(0, -150)
	if !ok {
		t.Fatal("mid-ocean lookup returned not-found, expected a nautical zone")
	}
	if name == "" {
		t.Error("ok=true but empty zone name")
	}
}
