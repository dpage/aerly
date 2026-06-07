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

func (s stubGeo) ReverseCountry(context.Context, float64, float64) (string, bool, error) {
	return "", false, nil
}

func TestGeocodeEndpoint(t *testing.T) {
	g := stubGeo{resolves: map[string][2]float64{
		"1 Main St":                         {1, 2},
		"Alicante Airport":                  {38, -0.5},
		"London Heathrow Terminal 5":        {51, -0.4},
		"AB12 3CD, United Kingdom":          {51.6, -1.5},
		"Ukino Palmeiras Village, Portugal": {37.1, -8.38}, // name + country
		"8400-450 Porches, Portugal":        {37.0, -8.0},  // a resolvable tail
		"Honeysuckle Cottage":               {9, 9},        // bare name — must NEVER be queried
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
		{"bare ambiguous label is NEVER geocoded", "ground", "", "Honeysuckle Cottage", false, 0},
		{"flight never uses label", "flight", "", "LHR", false, 0},
		{"flight still uses a resolving address", "flight", "1 Main St", "LHR", true, 1},
		{"airport label that doesn't resolve", "ground", "", "Faro Airport", false, 0},
		// Full address fails; a shortened tail resolves (incl. multi-line, normalised
		// first). The embedded postcode rides along in the tail — no postcode regex.
		{"tail backoff (one line)", "ground", "Honeysuckle Cottage, 1 Example Lane, AB12 3CD, United Kingdom", "Honeysuckle Cottage", true, 51.6},
		{"tail backoff (multi-line)", "ground", "Honeysuckle Cottage\n1 Example Lane\nAB12 3CD\nUnited Kingdom", "Honeysuckle Cottage", true, 51.6},
		// Full address fails; the property name + country tail resolves the exact hotel.
		{"name + country wins", "hotel", "Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal", "Ukino Palmeiras Village", true, 37.1},
		// No usable name; a shortened tail resolves instead.
		{"tail backoff when name absent", "hotel", "Bloco E3-IV, Alporchinhos, 8400-450 Porches, Portugal", "", true, 37.0},
		// 2-segment address can't reach a non-bare tail, and the name lookup is only
		// ever country-qualified — the bare "Honeysuckle Cottage" entry stays untouched.
		{"name fallback appends country, never bare", "ground", "Honeysuckle Cottage, Atlantis", "Honeysuckle Cottage", false, 0},
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

func TestCountryFromAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Quinta das Palmeiras, Porches, Portugal", "Portugal"},
		{"Honeysuckle Cottage\n1 Example Lane\nAB12 3CD\nUnited Kingdom", "United Kingdom"},
		{"Nowhere Addr", ""},   // single segment → no country tail
		{"", ""},
	}
	for _, c := range cases {
		if got := countryFromAddress(normalizeAddress(c.in)); got != c.want {
			t.Errorf("countryFromAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAddressTails(t *testing.T) {
	addr := "Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450, Porches, Algarve, Portugal"
	got := addressTails(addr, 4)
	want := []string{
		"Bloco E3-IV, Alporchinhos, 8400-450, Porches, Algarve, Portugal",
		"Alporchinhos, 8400-450, Porches, Algarve, Portugal",
		"8400-450, Porches, Algarve, Portugal",
		"Porches, Algarve, Portugal",
	}
	if len(got) != len(want) {
		t.Fatalf("addressTails len = %d (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tail[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Too few segments → no shortened tail that isn't the bare last segment.
	if tails := addressTails("Porches, Portugal", 4); len(tails) != 0 {
		t.Errorf("2-segment tails = %v, want none", tails)
	}
	if tails := addressTails("Nowhere", 4); len(tails) != 0 {
		t.Errorf("1-segment tails = %v, want none", tails)
	}
}

// An IATA code in the label resolves via the airport table (no geocoder call).
func TestGeocodeEndpoint_IATAFromLabel(t *testing.T) {
	// Empty stub: if it resolved anything we'd know the table path wasn't taken.
	g := stubGeo{resolves: map[string][2]float64{}}
	lat, lon, ok := geocodeEndpoint(context.Background(), g, "ground", "", "LHR T5")
	if !ok {
		t.Fatal("expected LHR T5 to resolve via the airport table")
	}
	if lat < 51 || lat > 52 || lon > 0 {
		t.Errorf("coords (%v,%v) don't look like LHR", lat, lon)
	}
	// A flight part must NOT use the label path even with an IATA code.
	if _, _, ok := geocodeEndpoint(context.Background(), g, "flight", "", "LHR T5"); ok {
		t.Error("flight part should not resolve from an IATA label")
	}
}
