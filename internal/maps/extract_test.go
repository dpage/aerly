package maps

import "testing"

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
