package geocode

import (
	"context"
	"testing"
)

type stubGeo struct{ resolves map[string][2]float64 }

func (s stubGeo) Geocode(_ context.Context, q string) (float64, float64, bool, error) {
	if c, ok := s.resolves[q]; ok {
		return c[0], c[1], true, nil
	}
	return 0, 0, false, nil
}

func (s stubGeo) GeocodeCountry(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func TestGeocodeEndpoint(t *testing.T) {
	g := stubGeo{resolves: map[string][2]float64{
		"1 Main St":                  {1, 2},
		"Alicante Airport":           {38, -0.5},
		"London Heathrow Terminal 5": {51, -0.4},
	}}
	ctx := context.Background()
	cases := []struct {
		name, pt, addr, label string
		wantOK                bool
		wantLat               float64
	}{
		{"address resolves", "hotel", "1 Main St", "Hotel", true, 1},
		{"no address, airport label fallback", "ground", "", "Alicante Airport", true, 38},
		{"address fails, terminal label fallback", "ground", "Nowhere Addr", "London Heathrow Terminal 5", true, 51},
		{"ambiguous place label is NOT geocoded", "ground", "", "Honeysuckle Cottage", false, 0},
		{"flight never uses label", "flight", "", "LHR", false, 0},
		{"flight still uses a resolving address", "flight", "1 Main St", "LHR", true, 1},
		{"airport label that doesn't resolve", "ground", "", "Faro Airport", false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lat, _, ok := geocodeEndpoint(ctx, g, c.pt, c.addr, c.label)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && lat != c.wantLat {
				t.Errorf("lat = %v, want %v", lat, c.wantLat)
			}
		})
	}
}
