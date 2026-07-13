package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

// Synthetic Geoapify GeoJSON — invented places, no real people's data.
const geoapifySample = `{"features":[
  {"properties":{"name":"Example Museum","categories":["entertainment.museum","tourism"],"lat":51.5008,"lon":-0.124,"formatted":"Example Museum, London","distance":120,"place_id":"place-museum","datasource":{"raw":{"wikidata":"Q2","website":"https://museum.example"}}}},
  {"properties":{"name":"Example Church","categories":["religion.place_of_worship"],"lat":51.5010,"lon":-0.125,"address_line1":"1 Church Street","distance":60,"place_id":"place-church"}},
  {"properties":{"categories":["tourism.attraction"],"lat":51.5,"lon":-0.12,"distance":10,"place_id":"place-noname"}}
]}`

func newGeoapify(t *testing.T, h http.HandlerFunc) *Geoapify {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	g := NewGeoapify("testkey")
	g.BaseURL = srv.URL
	g.Limiter = rate.NewLimiter(rate.Inf, 1)
	return g
}

func TestGeoapifyNearbyParsesAndClassifies(t *testing.T) {
	var gotQuery string
	g := newGeoapify(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(geoapifySample))
	})

	pois, err := g.Nearby(context.Background(), 51.5010, -0.1245, 2000, []string{"museum", "landmark"})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 2 {
		t.Fatalf("want 2 named POIs (unnamed dropped), got %d", len(pois))
	}
	// Sorted by distance: church (60m) before museum (120m).
	if pois[0].Name != "Example Church" || pois[0].Category != "landmark" {
		t.Errorf("poi[0] = %+v", pois[0])
	}
	if pois[0].Address != "1 Church Street" || pois[0].ID != "place-church" {
		t.Errorf("church address/id wrong: %+v", pois[0])
	}
	if pois[1].Name != "Example Museum" || pois[1].Category != "museum" {
		t.Errorf("poi[1] = %+v", pois[1])
	}
	if pois[1].Wikidata != "Q2" || pois[1].Website != "https://museum.example" {
		t.Errorf("museum should carry wikidata + website from datasource.raw: %+v", pois[1])
	}
	if pois[1].DistanceM != 120 {
		t.Errorf("museum distance = %d, want 120", pois[1].DistanceM)
	}

	// Request shape: unioned categories, circle filter (lon,lat order), api key.
	if !strings.Contains(gotQuery, "entertainment.museum") || !strings.Contains(gotQuery, "religion.place_of_worship") {
		t.Errorf("categories not unioned into the request: %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "circle") || !strings.Contains(gotQuery, "apiKey=testkey") {
		t.Errorf("request missing circle filter / api key: %s", gotQuery)
	}
}

func TestGeoapifyClassify(t *testing.T) {
	cases := []struct {
		cats []string
		want string
	}{
		{[]string{"entertainment.museum"}, "museum"},
		{[]string{"religion.place_of_worship.church"}, "landmark"},
		{[]string{"heritage.unesco"}, "landmark"},
		{[]string{"leisure.park"}, "park"},
		{[]string{"natural.forest"}, "park"},
		{[]string{"catering.restaurant"}, "food"},
		{[]string{"tourism.attraction"}, "sights"},
	}
	for _, c := range cases {
		if got := geoapifyCategory(c.cats); got != c.want {
			t.Errorf("geoapifyCategory(%v) = %s, want %s", c.cats, got, c.want)
		}
	}
}

func TestGeoapifyTransientStatus(t *testing.T) {
	g := newGeoapify(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := g.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); err != ErrPOIUnavailable {
		t.Fatalf("err = %v, want ErrPOIUnavailable", err)
	}
}

// TestGeoapifyHardStatus: a non-transient status (our malformed request, not
// Geoapify's fault) must surface as a real error mentioning the status code,
// never as ErrPOIUnavailable (which would tell the user to just try again).
func TestGeoapifyHardStatus(t *testing.T) {
	g := newGeoapify(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	_, err := g.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	if err == nil || err == ErrPOIUnavailable {
		t.Fatalf("err = %v, want a hard status error", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v, want it to mention status 400", err)
	}
}

// TestGeoapifyBadJSON covers the malformed-body branch: a 200 with an
// unparseable body is a real error, not an empty result.
func TestGeoapifyBadJSON(t *testing.T) {
	g := newGeoapify(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	})
	if _, err := g.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); err == nil {
		t.Fatal("Nearby with malformed JSON should error")
	}
}

// TestGeoapifyEmptyCats covers the empty-cats short-circuit: no categories
// means nothing to look up, so Nearby must return without ever calling out.
func TestGeoapifyEmptyCats(t *testing.T) {
	called := false
	g := newGeoapify(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(geoapifySample))
	})
	pois, err := g.Nearby(context.Background(), 51.5, -0.12, 2000, nil)
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 0 {
		t.Errorf("pois = %v, want empty", pois)
	}
	if called {
		t.Error("Nearby with no categories should not make an HTTP call")
	}
}

// TestGeoapifyUnknownCat covers cats that map to no known Geoapify codes: the
// union is empty, so — like the empty-cats case — Nearby returns empty
// without calling out.
func TestGeoapifyUnknownCat(t *testing.T) {
	called := false
	g := newGeoapify(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(geoapifySample))
	})
	pois, err := g.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"not-a-real-category"})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 0 {
		t.Errorf("pois = %v, want empty", pois)
	}
	if called {
		t.Error("Nearby with an unmapped category should not make an HTTP call")
	}
}

// TestGeoapifyNetworkError covers the transport-failure branch: HTTP.Do
// itself erroring (here, the server is closed before the call) must surface
// as an error.
func TestGeoapifyNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	g := NewGeoapify("testkey")
	g.BaseURL = srv.URL
	g.Limiter = rate.NewLimiter(rate.Inf, 1)
	srv.Close() // closed before use, so HTTP.Do fails with a connection error

	if _, err := g.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); err == nil {
		t.Fatal("Nearby against a closed server should error")
	}
}
