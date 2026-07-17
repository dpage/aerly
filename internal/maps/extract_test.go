package maps

import "testing"

func TestExtractHint(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"q parameter from an iOS share link",
			"https://www.google.com/maps/search/?api=1&q=Test+Hotel%2C+Example+Street%2C+London&ftid=0x487f:0xabc",
			"Test Hotel, Example Street, London"},
		{"place path segment",
			"https://www.google.com/maps/place/Test+Hotel+London",
			"Test Hotel London"},
		{"place path with trailing data segment",
			"https://www.google.com/maps/place/Test+Caf%C3%A9/data=!4m2!3m1",
			"Test Café"},
		{"q wins over the place path",
			"https://www.google.com/maps/place/Ignore+Me?q=Test+Hotel",
			"Test Hotel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractHint(tt.url)
			if !ok || got != tt.want {
				t.Errorf("got %q ok=%v, want %q", got, ok, tt.want)
			}
		})
	}
}

func TestExtractHintNoText(t *testing.T) {
	for _, u := range []string{
		"https://www.google.com/maps/@51.5,-0.14,15z",
		"https://www.google.com/maps",
		"https://maps.app.goo.gl/abc123",
		"https://www.google.com/maps/place/",
	} {
		if got, ok := ExtractHint(u); ok {
			t.Errorf("%s: want no hint, got %q", u, got)
		}
	}
}

// A hint that is itself a coordinate pair is not a hint: ExtractLatLon already
// handles those exactly, and geocoding them would be a pointless round trip.
func TestExtractHintIgnoresCoordinateQuery(t *testing.T) {
	if got, ok := ExtractHint("https://www.google.com/maps?q=51.5,-0.14"); ok {
		t.Errorf("want no hint for a coordinate q, got %q", got)
	}
}

func TestExtractLatLon(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantLat float64
		wantLon float64
		wantOK  bool
	}{
		{"place over viewport", "https://www.google.com/maps/place/X/@48.86,2.29,17z/data=!3d48.8584!4d2.2945", 48.8584, 2.2945, true},
		{"viewport only", "https://www.google.com/maps/@51.5074,-0.1278,15z", 51.5074, -0.1278, true},
		{"q pair", "https://maps.google.com/?q=40.7128,-74.006", 40.7128, -74.006, true},
		{"ll pair", "https://maps.google.com/?ll=40.7128,-74.006", 40.7128, -74.006, true},
		{"query pair", "https://www.google.com/maps/search/?api=1&query=40.7128,-74.006", 40.7128, -74.006, true},
		{"encoded comma", "https://maps.google.com/?q=40.7128%2C-74.006", 40.7128, -74.006, true},
		{"place only", "https://www.google.com/maps/place/Somewhere+Cafe", 0, 0, false},
		{"lat out of range", "https://www.google.com/maps/@91,0,5z", 0, 0, false},
		{"lon out of range", "https://maps.google.com/?q=0,181", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lat, lon, ok := ExtractLatLon(c.url)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && (lat != c.wantLat || lon != c.wantLon) {
				t.Fatalf("got (%v,%v), want (%v,%v)", lat, lon, c.wantLat, c.wantLon)
			}
		})
	}
}
