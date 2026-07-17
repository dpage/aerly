package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const searchFixture = `{"results":[
  {"lat":51.5,"lon":-0.14,"formatted":"Test Hotel, Example Street, London",
   "country_code":"gb","rank":{"confidence":0.95,"match_type":"full_match"},
   "datasource":{"sourcename":"openstreetmap"}},
  {"lat":52.4,"lon":-1.9,"formatted":"Test Hotel, Example Road, Birmingham",
   "country_code":"gb","rank":{"confidence":0.42,"match_type":"inner_part"},
   "datasource":{"sourcename":"openstreetmap"}}]}`

func newTestGeoapify(t *testing.T, body string, status int) (*Geoapify, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RawQuery)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	g := NewGeoapify("test-key")
	g.BaseURL = srv.URL
	return g, &seen
}

func TestCandidatesParsesRanked(t *testing.T) {
	g, _ := newTestGeoapify(t, searchFixture, http.StatusOK)
	got, err := g.Candidates(context.Background(), Query{Text: "Test Hotel, London"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(got))
	}
	if got[0].Confidence != 0.95 || got[0].MatchType != "full_match" {
		t.Errorf("bad first candidate: %+v", got[0])
	}
	if got[0].Formatted != "Test Hotel, Example Street, London" {
		t.Errorf("bad formatted: %q", got[0].Formatted)
	}
	if got[0].SourceName != "openstreetmap" {
		t.Errorf("bad sourcename: %q", got[0].SourceName)
	}
}

func TestCandidatesSendsFilters(t *testing.T) {
	g, seen := newTestGeoapify(t, searchFixture, http.StatusOK)
	_, err := g.Candidates(context.Background(), Query{
		Text: "Test Hotel", CountryCode: "gb",
		Bias: &LatLon{Lat: 51.5, Lon: -0.14}, Type: "amenity", Limit: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q := (*seen)[0]
	for _, want := range []string{
		"filter=countrycode%3Agb", "bias=proximity%3A-0.14%2C51.5",
		"type=amenity", "limit=5", "apiKey=test-key", "format=json",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q missing %q", q, want)
		}
	}
}

func TestCandidatesErrorsOnBadStatus(t *testing.T) {
	g, _ := newTestGeoapify(t, `{}`, http.StatusTooManyRequests)
	if _, err := g.Candidates(context.Background(), Query{Text: "x"}); err == nil {
		t.Fatal("want an error on 429, got nil: a rate-limited provider must never look like a miss")
	}
}

func TestGeocodeReturnsTopCandidate(t *testing.T) {
	g, _ := newTestGeoapify(t, searchFixture, http.StatusOK)
	lat, lon, ok, err := g.Geocode(context.Background(), "Test Hotel, London", "")
	if err != nil || !ok {
		t.Fatalf("want a hit, got ok=%v err=%v", ok, err)
	}
	if lat != 51.5 || lon != -0.14 {
		t.Errorf("want the top candidate, got %v,%v", lat, lon)
	}
}

func TestGeocodeEmptyQueryIsMiss(t *testing.T) {
	g, _ := newTestGeoapify(t, searchFixture, http.StatusOK)
	if _, _, ok, err := g.Geocode(context.Background(), "   ", ""); ok || err != nil {
		t.Fatalf("blank query must miss without a request: ok=%v err=%v", ok, err)
	}
}
