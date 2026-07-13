package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

// Synthetic Overpass payload — invented landmarks, no real people's data.
const overpassSample = `{"elements":[
  {"type":"node","id":1,"lat":51.5010,"lon":-0.1245,"tags":{"tourism":"attraction","name":"Example Tower","wikidata":"Q1","addr:street":"Example Road"}},
  {"type":"way","id":2,"center":{"lat":51.5008,"lon":-0.1240},"tags":{"tourism":"museum","name":"Example Museum"}},
  {"type":"node","id":3,"lat":51.5,"lon":-0.12,"tags":{"amenity":"bench"}}
]}`

func newOverpass(t *testing.T, h http.HandlerFunc) *Overpass {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	o := NewOverpass(srv.URL, "aerly-test")
	o.Limiter = rate.NewLimiter(rate.Inf, 1)
	return o
}

func TestOverpassNearbyParsesAndFilters(t *testing.T) {
	var gotBody string
	o := newOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		if ua := r.Header.Get("User-Agent"); ua != "aerly-test" {
			t.Errorf("User-Agent = %q", ua)
		}
		_, _ = w.Write([]byte(overpassSample))
	})

	pois, err := o.Nearby(context.Background(), 51.5010, -0.1245, 2000, []string{"sights", "museum"})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 2 {
		t.Fatalf("want 2 named POIs (bench dropped), got %d", len(pois))
	}
	if pois[0].Name != "Example Tower" || pois[0].Category != "sights" {
		t.Errorf("poi[0] = %+v", pois[0])
	}
	if pois[0].Wikidata != "Q1" || pois[0].Address == "" {
		t.Errorf("expected wikidata + address on poi[0]: %+v", pois[0])
	}
	if pois[1].Lat == 0 || pois[1].Lon == 0 {
		t.Errorf("way should use center coords: %+v", pois[1])
	}
	if !strings.Contains(gotBody, "around:2000") {
		t.Errorf("query missing radius: %s", gotBody)
	}
}

func TestCategoryOf(t *testing.T) {
	cases := []struct {
		tags map[string]string
		want string
	}{
		{map[string]string{"tourism": "museum"}, "museum"},
		{map[string]string{"historic": "castle"}, "landmark"},
		{map[string]string{"leisure": "park"}, "park"},
		{map[string]string{"amenity": "cafe"}, "food"},
		{map[string]string{"tourism": "attraction"}, "sights"},
	}
	for _, c := range cases {
		if got := categoryOf(c.tags); got != c.want {
			t.Errorf("categoryOf(%v) = %s, want %s", c.tags, got, c.want)
		}
	}
}

// TestOverpassFetchLabelAlignment guards against the fetch query and the
// categoryOf label drifting apart: historic sites must be fetched under
// "landmark" (categoryOf labels them "landmark"), and "sights" must not
// pull in historic elements it would then mislabel.
func TestOverpassFetchLabelAlignment(t *testing.T) {
	o := NewOverpass("http://example.invalid", "aerly-test")
	sights := o.buildQuery(51.5, -0.12, 2000, []string{"sights"})
	if strings.Contains(sights, "historic") {
		t.Errorf("sights query should not fetch historic sites: %s", sights)
	}
	landmark := o.buildQuery(51.5, -0.12, 2000, []string{"landmark"})
	if !strings.Contains(landmark, "historic") {
		t.Errorf("landmark query should fetch historic sites: %s", landmark)
	}
}
