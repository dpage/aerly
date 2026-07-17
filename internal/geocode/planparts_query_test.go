package geocode

import (
	"context"
	"testing"
)

// stubGeo is the Geocoder test double shared by this file and the DB-backed
// PlanParts tests. Candidates answers from resolves keyed by exact query text,
// synthesising a single confident candidate for a hit so a permissive Resolver
// (see testResolver) behaves like the old blind-first-hit lookup these tests
// were written against. queries records every (query, countryCode) pair, so
// tests can assert both what was looked up and whether a country bias was
// applied.
type stubGeo struct {
	resolves map[string][2]float64
	queries  *[]stubQuery
}

type stubQuery struct{ q, country string }

func (s stubGeo) Candidates(_ context.Context, q Query) ([]Candidate, error) {
	if s.queries != nil {
		*s.queries = append(*s.queries, stubQuery{q.Text, q.CountryCode})
	}
	if c, ok := s.resolves[q.Text]; ok {
		return []Candidate{{Lat: c[0], Lon: c[1], Confidence: 1}}, nil
	}
	return nil, nil
}

func (s stubGeo) Geocode(_ context.Context, q, countryCode string) (float64, float64, bool, error) {
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

func (s stubGeo) ReversePlace(context.Context, float64, float64) (string, string, bool, error) {
	return "", "", false, nil
}

// fakeGeocoder is a second, minimal Geocoder double built to the exact ranked
// candidate list a test wants, used by the Resolver-focused tests below where a
// per-candidate Confidence needs to be spelled out explicitly (stubGeo always
// answers confidence 1).
type fakeGeocoder struct {
	byText map[string][]Candidate
	calls  []string
}

func (f *fakeGeocoder) Candidates(ctx context.Context, q Query) ([]Candidate, error) {
	f.calls = append(f.calls, q.Text)
	return f.byText[q.Text], nil
}
func (f *fakeGeocoder) Geocode(ctx context.Context, q, cc string) (float64, float64, bool, error) {
	c := f.byText[q]
	if len(c) == 0 {
		return 0, 0, false, nil
	}
	return c[0].Lat, c[0].Lon, true, nil
}
func (f *fakeGeocoder) GeocodeCountry(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (f *fakeGeocoder) ReverseCountry(context.Context, float64, float64) (string, bool, error) {
	return "", false, nil
}
func (f *fakeGeocoder) ReversePlace(context.Context, float64, float64) (string, string, bool, error) {
	return "", "", false, nil
}

func testResolver(g Geocoder, r Reranker) *Resolver {
	return &Resolver{Geo: g, Rerank: r, MinConfidence: 0.5, Margin: 0.15}
}

func TestEndpointAcceptsConfidentMatch(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{
		"Test Hotel, Example Street, London": {{Lat: 51.5, Lon: -0.14, Confidence: 0.95}},
	}}
	lat, lon, ok := testResolver(g, nil).Endpoint(context.Background(),
		"hotel", "Test Hotel, Example Street, London", "Test Hotel", "gb")
	if !ok || lat != 51.5 || lon != -0.14 {
		t.Fatalf("got %v,%v ok=%v", lat, lon, ok)
	}
}

func TestEndpointRejectsDoubtfulMatchWithoutReranker(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{
		"Somewhere Vague": {{Lat: 1, Lon: 2, Confidence: 0.3}},
	}}
	if _, _, ok := testResolver(g, nil).Endpoint(context.Background(),
		"hotel", "Somewhere Vague", "", "gb"); ok {
		t.Fatal("a doubtful match with no re-ranker must not plot: a missing pin beats a wrong one")
	}
}

func TestEndpointRerankerResolvesAmbiguity(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{
		"Test Hotel": {
			{Lat: 51.5, Lon: -0.14, Confidence: 0.9, Formatted: "Test Hotel, London"},
			{Lat: 52.4, Lon: -1.9, Confidence: 0.88, Formatted: "Test Hotel, Birmingham"},
		},
	}}
	r := NewLLMReranker(func(ctx context.Context, p string) (string, error) { return `{"index":1}`, nil })
	lat, _, ok := testResolver(g, r).Endpoint(context.Background(), "hotel", "Test Hotel", "", "gb")
	if !ok || lat != 52.4 {
		t.Fatalf("re-ranker's pick should win: lat=%v ok=%v", lat, ok)
	}
}

func TestEndpointRerankerDeclineMeansNoPin(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{
		"Test Hotel": {
			{Lat: 51.5, Lon: -0.14, Confidence: 0.9},
			{Lat: 52.4, Lon: -1.9, Confidence: 0.88},
		},
	}}
	r := NewLLMReranker(func(ctx context.Context, p string) (string, error) { return `{"index":null}`, nil })
	if _, _, ok := testResolver(g, r).Endpoint(context.Background(), "hotel", "Test Hotel", "", "gb"); ok {
		t.Fatal("a declining re-ranker must yield no pin")
	}
}

func TestEndpointSkipsRerankerWhenConfident(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{
		"Test Hotel, London": {{Lat: 51.5, Lon: -0.14, Confidence: 0.95}},
	}}
	called := false
	r := NewLLMReranker(func(ctx context.Context, p string) (string, error) {
		called = true
		return `{"index":0}`, nil
	})
	testResolver(g, r).Endpoint(context.Background(), "hotel", "Test Hotel, London", "", "gb")
	if called {
		t.Fatal("the LLM must stay off the happy path: a lone confident match needs no re-rank")
	}
}

func TestEndpointIATAShortcutSkipsGeocoder(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{}}
	_, _, ok := testResolver(g, nil).Endpoint(context.Background(), "transfer", "", "LHR Terminal 5", "gb")
	if !ok {
		t.Fatal("an IATA label must resolve offline")
	}
	if len(g.calls) != 0 {
		t.Fatalf("IATA lookup must not hit the geocoder, saw %v", g.calls)
	}
}

func TestEndpointGenericLabelNeverGeocoded(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{
		"Home, United Kingdom": {{Lat: 53.4, Lon: -2.2, Confidence: 0.99}},
	}}
	if _, _, ok := testResolver(g, nil).Endpoint(context.Background(),
		"hotel", "", "Home", "gb"); ok {
		t.Fatal(`"Home" must never geocode: it resolved to the HOME arts centre in Manchester`)
	}
}

// A flight part must never resolve from its label, even when that label carries
// a resolvable IATA code. A flight's label is ordinarily an IATA code, and it
// IS located via the airport table (or the poller, for an off-table airport),
// but via the dedicated flight-coordinate resolution path (resolveFlightCoordsAsync),
// not Endpoint's own label lookup. Endpoint must not pre-empt that path with its
// own fuzzy IATA scan here.
func TestEndpointIATALabelSkippedForFlightParts(t *testing.T) {
	g := &fakeGeocoder{byText: map[string][]Candidate{}}
	if _, _, ok := testResolver(g, nil).Endpoint(context.Background(), "flight", "", "LHR T5", ""); ok {
		t.Fatal("a flight part must not resolve from an IATA label")
	}
}

// TestEndpointSignalPriority walks the fallback order end to end (address, then
// name+country, then bare airport label), including the "never geocode a bare
// ambiguous name" and "flight never uses the label" guarantees. These migrate
// the pre-Resolver geocodeEndpoint table; the two cases that only ever exercised
// the deleted address-tail backoff are gone (see
// TestEndpointFullMessyAddressResolvesWithoutTailBackoff and the report for why).
func TestEndpointSignalPriority(t *testing.T) {
	g := stubGeo{resolves: map[string][2]float64{
		"1 Main St":                         {1, 2},
		"Alicante Airport":                  {38, -0.5},
		"London Heathrow Terminal 5":        {51, -0.4},
		"Ukino Palmeiras Village, Portugal": {37.1, -8.38}, // name + country
		"Honeysuckle Cottage":               {9, 9},        // bare name — must NEVER be queried
		"Terminal 3":                        {-6.12, 106.66}, // Jakarta CGK T3 — must NEVER be queried bare
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
		// A bare terminal reference carries no airport name, so it must not be
		// geocoded on its own (it once resolved a UK transfer to Jakarta CGK T3).
		{"bare terminal number is NEVER geocoded", "ground", "", "Terminal 3", false, 0},
		{"bare terminal designator is NEVER geocoded", "ground", "", "Terminal", false, 0},
		{"flight never uses label", "flight", "", "LHR", false, 0},
		{"flight still uses a resolving address", "flight", "1 Main St", "LHR", true, 1},
		{"airport label that doesn't resolve", "ground", "", "Faro Airport", false, 0},
		// Full address fails; the property name + country tail resolves the exact hotel.
		{"name + country wins", "hotel", "Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal", "Ukino Palmeiras Village", true, 37.1},
		// 2-segment address can't reach a non-bare tail, and the name lookup is only
		// ever country-qualified — the bare "Honeysuckle Cottage" entry stays untouched.
		{"name fallback appends country, never bare", "ground", "Honeysuckle Cottage, Atlantis", "Honeysuckle Cottage", false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lat, _, ok := testResolver(g, nil).Endpoint(ctx, c.pt, c.addr, c.label, "")
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && lat != c.wantLat {
				t.Errorf("lat = %v, want %v", lat, c.wantLat)
			}
		})
	}
}

// TestEndpointFullMessyAddressResolvesWithoutTailBackoff replaces the old
// "tail backoff" cases: Geoapify's own matching resolves a full, messy,
// multi-line address in a single query, so there is no longer a client-side
// backoff rung that strips leading segments looking for a hit. normalizeAddress
// still collapses the multi-line form to one comma-separated line first.
func TestEndpointFullMessyAddressResolvesWithoutTailBackoff(t *testing.T) {
	g := stubGeo{resolves: map[string][2]float64{
		"Honeysuckle Cottage, 1 Example Lane, AB12 3CD, United Kingdom": {51.6, -1.5},
	}}
	lat, _, ok := testResolver(g, nil).Endpoint(context.Background(), "ground",
		"Honeysuckle Cottage\n1 Example Lane\nAB12 3CD\nUnited Kingdom", "Honeysuckle Cottage", "")
	if !ok || lat != 51.6 {
		t.Fatalf("got lat=%v ok=%v, want 51.6/true", lat, ok)
	}
}

// A generic personal label ("Home") must never be geocoded qualified only by a
// country, even when the full address fails to resolve and there is no tail
// backoff left to rescue it: "Home, United Kingdom" would resolve to an
// unrelated venue (the HOME arts centre in Manchester) if it were ever tried.
func TestEndpointGenericLabelNeverNameGeocodedEvenOnAddressMiss(t *testing.T) {
	var got []stubQuery
	g := stubGeo{
		resolves: map[string][2]float64{
			// A trap: if "Home" were name-geocoded, this would win and land on an
			// unrelated same-named venue (the real-world failure was Manchester).
			"Home, United Kingdom": {53.47, -2.25},
		},
		queries: &got,
	}
	addr := "The Old House, 1 Example Lane, Exampleton, Testford, ZZ9 9ZZ, United Kingdom"
	if _, _, ok := testResolver(g, nil).Endpoint(context.Background(), "ground", addr, "Home", ""); ok {
		t.Fatal("a generic label must never rescue an unresolved address by name-geocoding it")
	}
	for _, q := range got {
		if q.q == "Home, United Kingdom" {
			t.Errorf("generic label was name-geocoded as %q", q.q)
		}
	}
}

// An addressless, ambiguous airport label must be geocoded constrained to the
// trip's country so it can't resolve on the wrong continent ("Sal Airport" →
// El Salvador rather than Cape Verde).
func TestEndpointBareAirportBiasedToTripCountry(t *testing.T) {
	var got []stubQuery
	g := stubGeo{resolves: map[string][2]float64{}, queries: &got}
	testResolver(g, nil).Endpoint(context.Background(), "ground", "", "Sal Airport", "cv")
	found := false
	for _, q := range got {
		if q.q == "Sal Airport" {
			found = true
			if q.country != "cv" {
				t.Errorf("bare airport label geocoded with country %q, want \"cv\"", q.country)
			}
		}
	}
	if !found {
		t.Fatal("expected the bare airport label to be geocoded")
	}
}

// TestEndpointFullAddressNotCountryFiltered pins the fix for a real regression:
// the full-address rung must NOT pass CountryCode, even when a tripCountry is
// supplied. Geoapify's countrycode filter is a hard exclusion, not a soft bias,
// and a full postal address already names its own country. The concrete broken
// case this guards is a home-to-airport transfer whose start address is in gb
// on a trip whose derived country is pt: filtering the address lookup by the
// trip's country would return zero candidates and plot nothing, breaking
// Aerly's pinned-home-location feature for exactly this kind of plan. Do not
// re-add CountryCode here.
func TestEndpointFullAddressNotCountryFiltered(t *testing.T) {
	var got []stubQuery
	g := stubGeo{
		resolves: map[string][2]float64{
			"Test Hotel, Example Street, London, United Kingdom": {51.5, -0.14},
		},
		queries: &got,
	}
	lat, lon, ok := testResolver(g, nil).Endpoint(context.Background(),
		"transfer", "Test Hotel, Example Street, London, United Kingdom", "", "pt")
	if !ok || lat != 51.5 || lon != -0.14 {
		t.Fatalf("got %v,%v ok=%v, want 51.5,-0.14,true", lat, lon, ok)
	}
	found := false
	for _, q := range got {
		if q.q == "Test Hotel, Example Street, London, United Kingdom" {
			found = true
			if q.country != "" {
				t.Errorf("full-address rung queried with CountryCode %q, want empty (a gb address on a pt trip must not be country-filtered)", q.country)
			}
		}
	}
	if !found {
		t.Fatal("expected the full address to be queried")
	}
}

func TestCountryFromAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Quinta das Palmeiras, Porches, Portugal", "Portugal"},
		{"Honeysuckle Cottage\n1 Example Lane\nAB12 3CD\nUnited Kingdom", "United Kingdom"},
		{"Nowhere Addr", ""}, // single segment → no country tail
		{"", ""},
	}
	for _, c := range cases {
		if got := countryFromAddress(normalizeAddress(c.in)); got != c.want {
			t.Errorf("countryFromAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// An IATA code in the label resolves via the airport table (no geocoder call).
func TestEndpointIATAFromLabel(t *testing.T) {
	// Empty stub: if it resolved anything we'd know the table path wasn't taken.
	g := stubGeo{resolves: map[string][2]float64{}}
	lat, lon, ok := testResolver(g, nil).Endpoint(context.Background(), "ground", "", "LHR T5", "")
	if !ok {
		t.Fatal("expected LHR T5 to resolve via the airport table")
	}
	if lat < 51 || lat > 52 || lon > 0 {
		t.Errorf("coords (%v,%v) don't look like LHR", lat, lon)
	}
}
