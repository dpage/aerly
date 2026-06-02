package geocode

import "testing"

func TestGeocodeQuery(t *testing.T) {
	cases := []struct {
		name              string
		partType, address string
		label, want       string
	}{
		{"address wins", "hotel", "1 Main St", "Hotel Lisboa", "1 Main St"},
		{"label fallback for non-flight", "ground", "", "Alicante Airport", "Alicante Airport"},
		{"flight never label-geocoded", "flight", "", "LHR", ""},
		{"flight with address still geocodes", "flight", "Heathrow T5", "LHR", "Heathrow T5"},
		{"nothing to geocode", "ground", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := geocodeQuery(c.partType, c.address, c.label); got != c.want {
				t.Errorf("geocodeQuery(%q,%q,%q) = %q, want %q", c.partType, c.address, c.label, got, c.want)
			}
		})
	}
}
