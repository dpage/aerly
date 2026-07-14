package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// Synthetic Overpass payload — invented landmarks, no real people's data.
const overpassSample = `{"elements":[
  {"type":"node","id":1,"lat":51.5010,"lon":-0.1245,"tags":{"tourism":"attraction","name":"Example Tower","wikidata":"Q1","addr:street":"Example Road","description":"A tall example landmark"}},
  {"type":"way","id":2,"center":{"lat":51.5008,"lon":-0.1240},"tags":{"tourism":"museum","name":"Example Museum"}},
  {"type":"node","id":3,"lat":51.5,"lon":-0.12,"tags":{"amenity":"bench"}}
]}`

func newOverpass(t *testing.T, h http.HandlerFunc) *Overpass {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	o := NewOverpass(srv.URL, "aerly-test")
	o.Limiter = rate.NewLimiter(rate.Inf, 1)
	o.RetryWait = 0
	return o
}

// newOverpassEndpoints wires the client to several fake endpoints in order, so
// fallback behaviour can be exercised. Each handler serves one endpoint.
func newOverpassEndpoints(t *testing.T, handlers ...http.HandlerFunc) *Overpass {
	t.Helper()
	o := NewOverpass("", "aerly-test")
	o.Limiter = rate.NewLimiter(rate.Inf, 1)
	o.RetryWait = 0
	for _, h := range handlers {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		o.Endpoints = append(o.Endpoints, srv.URL)
	}
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
	if pois[0].Description != "A tall example landmark" {
		t.Errorf("expected description from the OSM tag on poi[0]: %+v", pois[0])
	}
	// The museum has no description tag, so it must come through empty (the UI
	// omits the line entirely in that case).
	if pois[1].Description != "" {
		t.Errorf("expected no description on poi[1]: %+v", pois[1])
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

// TestOverpassFallsBackToSecondEndpoint: a rate-limited first endpoint should
// fail over to a healthy second one rather than erroring.
func TestOverpassFallsBackToSecondEndpoint(t *testing.T) {
	o := newOverpassEndpoints(t,
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(overpassSample)) },
	)
	pois, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 2 {
		t.Fatalf("want 2 POIs from the fallback endpoint, got %d", len(pois))
	}
}

// TestOverpassAttemptTimeoutFailsOver: a stalled endpoint must not hang the
// call — the per-attempt timeout fires and we fall over to a healthy endpoint.
func TestOverpassAttemptTimeoutFailsOver(t *testing.T) {
	o := newOverpassEndpoints(t,
		// Doesn't answer within AttemptTimeout, so the client cancels and we
		// fail over. The timer fallback bounds teardown if the cancellation
		// doesn't reach the handler promptly.
		func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(500 * time.Millisecond):
			}
		},
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(overpassSample)) },
	)
	o.AttemptTimeout = 50 * time.Millisecond

	pois, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 2 {
		t.Fatalf("want 2 POIs from the healthy endpoint after the slow one timed out, got %d", len(pois))
	}
}

// TestOverpassUnavailableWhenAllTransient: when every endpoint keeps returning
// a transient status, Nearby reports ErrPOIUnavailable (not a generic error),
// so the handler can answer 503 rather than 500.
func TestOverpassUnavailableWhenAllTransient(t *testing.T) {
	o := newOverpassEndpoints(t,
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusGatewayTimeout) },
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
	)
	_, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	if !errors.Is(err, ErrPOIUnavailable) {
		t.Fatalf("err = %v, want ErrPOIUnavailable", err)
	}
}

// TestOverpassTimeoutRemarkIsTransient: Overpass answers a server-side timeout
// with HTTP 200 + a "remark" and no elements. That must be treated as a
// transient failure (fail over / try again), NOT as an authoritative empty
// result — otherwise a busy instance shows the user "no places found".
func TestOverpassTimeoutRemarkIsTransient(t *testing.T) {
	const timedOut = `{"elements":[],"remark":"runtime error: Query timed out in \"query\" at line 1"}`
	// First endpoint 200s-but-timed-out; the second is healthy → we fail over.
	o := newOverpassEndpoints(t,
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(timedOut)) },
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(overpassSample)) },
	)
	pois, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 2 {
		t.Fatalf("want 2 POIs from the healthy endpoint, got %d", len(pois))
	}

	// With only the timed-out endpoint, it surfaces as unavailable, not empty.
	o2 := newOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(timedOut))
	})
	if _, err := o2.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); !errors.Is(err, ErrPOIUnavailable) {
		t.Fatalf("err = %v, want ErrPOIUnavailable", err)
	}
}

// TestOverpassSurfacesHardStatus: a 400 (our malformed query) is a real error,
// not a transient one — it must surface immediately, never as ErrPOIUnavailable.
func TestOverpassSurfacesHardStatus(t *testing.T) {
	o := newOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	_, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	if err == nil || errors.Is(err, ErrPOIUnavailable) {
		t.Fatalf("err = %v, want a hard status error", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v, want it to mention status 400", err)
	}
}

// TestOverpassEmptyCats covers the empty-cats short-circuit: no categories
// means nothing to query, so Nearby returns without touching any endpoint.
func TestOverpassEmptyCats(t *testing.T) {
	called := false
	o := newOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(overpassSample))
	})
	pois, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, nil)
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

// TestOverpassNoEndpointsConfigured covers the no-endpoints guard: a client
// built from an empty baseURL has nothing to query and must error rather than
// silently returning no results.
func TestOverpassNoEndpointsConfigured(t *testing.T) {
	o := NewOverpass("", "aerly-test")
	if _, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); err == nil {
		t.Fatal("Nearby with no endpoints configured should error")
	}
}

// TestOverpassBadJSON covers the malformed-body branch: a 200 with an
// unparseable body is a real error, not an empty result.
func TestOverpassBadJSON(t *testing.T) {
	o := newOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	})
	if _, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); err == nil {
		t.Fatal("Nearby with malformed JSON should error")
	}
}

// TestOverpassRetriesBetweenPasses covers the between-pass retry loop: with
// MaxRetries=1, a first pass that's all-transient must wait RetryWait and
// take a second pass over the endpoints, succeeding there.
func TestOverpassRetriesBetweenPasses(t *testing.T) {
	calls := 0
	o := newOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(overpassSample))
	})
	o.MaxRetries = 1

	pois, err := o.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if len(pois) != 2 {
		t.Fatalf("want 2 POIs from the retried pass, got %d", len(pois))
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (first pass transient, second pass succeeds)", calls)
	}
}

// TestOverpassCancelledContext covers the cancelled-caller-context branch:
// Nearby must surface context.Canceled directly, never masking it as
// ErrPOIUnavailable (which would wrongly log an "upstream unavailable"
// warning for what was actually the caller giving up).
func TestOverpassCancelledContext(t *testing.T) {
	o := newOverpass(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(overpassSample))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := o.Nearby(ctx, 51.5, -0.12, 2000, []string{"sights"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrPOIUnavailable) {
		t.Error("a cancelled context must not be reported as ErrPOIUnavailable")
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
