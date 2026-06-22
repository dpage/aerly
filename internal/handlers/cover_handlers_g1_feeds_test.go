package handlers

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// g1feedICS is a minimal, synthetic iCalendar payload with a single all-day
// event, used to exercise the feed refresh + external-events read paths.
const g1feedICS = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//Aerly Test//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:g1-event-1@example.com\r\n" +
	"DTSTAMP:20260101T000000Z\r\n" +
	"DTSTART:20260601T090000Z\r\n" +
	"DTEND:20260601T100000Z\r\n" +
	"SUMMARY:Synthetic Keynote\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// g1feedURL is a public-looking feed URL: it must pass feeds.NormalizeURL (which
// rejects literal private/loopback IPs but accepts named hosts), whilst the
// stub transport below intercepts the actual request so no real network call
// happens.
const g1feedURL = "https://feed.example.com/calendar.ics"

// g1RoundTripper serves the synthetic ICS for any request, standing in for the
// real network so the handler's synchronous refresh succeeds without leaving
// the loopback interface.
type g1RoundTripper struct{}

func (g1RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body := io.NopCloser(bytes.NewReader([]byte(g1feedICS)))
	h := make(http.Header)
	h.Set("Content-Type", "text/calendar")
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     h,
		Body:       body,
		Request:    req,
	}, nil
}

// g1FeedServer swaps the feed fetcher's HTTP client for one whose transport
// returns the synthetic ICS, so the handler's synchronous refresh succeeds.
func g1FeedServer(t *testing.T, e *testEnv) {
	t.Helper()
	e.api.Feeds.Fetcher.HTTP = &http.Client{Transport: g1RoundTripper{}}
}

func TestTripFeedsCRUDG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1feedowner", false)
	tripID := newTrip(t, e, owner, "Feed trip")
	g1FeedServer(t, e)
	url := g1feedURL
	base := "/api/trips/" + itoa(tripID) + "/feeds"

	// Empty list to start.
	w := e.req(t, "GET", base, nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("list code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := decodeBody[[]map[string]any](t, w); len(got) != 0 {
		t.Errorf("initial feeds = %d, want 0", len(got))
	}

	// Add a feed: synchronous refresh should populate it.
	w = e.req(t, "POST", base, map[string]any{"url": url, "name": "Schedule", "timezone": "Europe/London"}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("add code = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	feed := decodeBody[map[string]any](t, w)
	feedID := int64(feed["id"].(float64))

	// List now returns the feed.
	w = e.req(t, "GET", base, nil, owner)
	if got := decodeBody[[]map[string]any](t, w); len(got) != 1 {
		t.Fatalf("feeds after add = %d, want 1", len(got))
	}

	// External events should include the synthetic keynote.
	w = e.req(t, "GET", "/api/trips/"+itoa(tripID)+"/external-events", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("external-events code = %d; body=%s", w.Code, w.Body.String())
	}
	if got := decodeBody[[]map[string]any](t, w); len(got) != 1 {
		t.Errorf("external events = %d, want 1; body=%s", len(got), w.Body.String())
	}

	// Update the feed (name change), still 200.
	feedPath := base + "/" + itoa(feedID)
	w = e.req(t, "PATCH", feedPath, map[string]any{"url": url, "name": "Renamed", "timezone": ""}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("update code = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Delete the feed.
	w = e.req(t, "DELETE", feedPath, nil, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete code = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}

func TestTripFeedsValidationG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1feedval", false)
	tripID := newTrip(t, e, owner, "Feed validation trip")
	base := "/api/trips/" + itoa(tripID) + "/feeds"

	// Bad trip ID -> 400 across list/add/external-events.
	if w := e.req(t, "GET", "/api/trips/abc/feeds", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("list bad id = %d, want 400", w.Code)
	}
	if w := e.req(t, "POST", "/api/trips/abc/feeds", map[string]any{"url": "https://x.example/x.ics"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("add bad id = %d, want 400", w.Code)
	}
	if w := e.req(t, "GET", "/api/trips/abc/external-events", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("external bad id = %d, want 400", w.Code)
	}

	// Add with bad JSON body -> 400.
	if w := e.req(t, "POST", base, "??", owner); w.Code != http.StatusBadRequest {
		t.Errorf("add bad body = %d, want 400", w.Code)
	}
	// Add with an invalid URL -> 400.
	if w := e.req(t, "POST", base, map[string]any{"url": "not a url"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("add bad url = %d, want 400", w.Code)
	}
	// Add with a valid URL but bad timezone -> 400.
	if w := e.req(t, "POST", base, map[string]any{"url": "https://x.example/x.ics", "timezone": "Not/AZone"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("add bad tz = %d, want 400", w.Code)
	}

	// Non-editor cannot add.
	other := e.user(t, "g1feedother", false)
	if w := e.req(t, "POST", base, map[string]any{"url": "https://x.example/x.ics"}, other); w.Code != http.StatusForbidden {
		t.Errorf("non-editor add = %d, want 403", w.Code)
	}
	// Non-viewer cannot list / read events -> 404 (enumeration-safe).
	if w := e.req(t, "GET", base, nil, other); w.Code != http.StatusNotFound {
		t.Errorf("non-viewer list = %d, want 404", w.Code)
	}
	if w := e.req(t, "GET", "/api/trips/"+itoa(tripID)+"/external-events", nil, other); w.Code != http.StatusNotFound {
		t.Errorf("non-viewer external = %d, want 404", w.Code)
	}
}

func TestUpdateTripFeedValidationG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1updfeed", false)
	tripID := newTrip(t, e, owner, "Update feed trip")
	g1FeedServer(t, e)
	url := g1feedURL
	base := "/api/trips/" + itoa(tripID) + "/feeds"

	// Seed a feed.
	w := e.req(t, "POST", base, map[string]any{"url": url, "name": "S"}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed feed = %d; body=%s", w.Code, w.Body.String())
	}
	feedID := int64(decodeBody[map[string]any](t, w)["id"].(float64))
	feedPath := base + "/" + itoa(feedID)

	// Bad feed ID in path -> 400.
	if w := e.req(t, "PATCH", base+"/xyz", map[string]any{"url": url}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("update bad feed id = %d, want 400", w.Code)
	}
	// Bad trip ID in path -> 400.
	if w := e.req(t, "PATCH", "/api/trips/xyz/feeds/"+itoa(feedID), map[string]any{"url": url}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("update bad trip id = %d, want 400", w.Code)
	}
	// Feed that doesn't exist -> 404.
	if w := e.req(t, "PATCH", base+"/999999", map[string]any{"url": url}, owner); w.Code != http.StatusNotFound {
		t.Errorf("update missing feed = %d, want 404", w.Code)
	}
	// Bad JSON body -> 400.
	if w := e.req(t, "PATCH", feedPath, "??", owner); w.Code != http.StatusBadRequest {
		t.Errorf("update bad body = %d, want 400", w.Code)
	}
	// Bad URL -> 400.
	if w := e.req(t, "PATCH", feedPath, map[string]any{"url": "nope"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("update bad url = %d, want 400", w.Code)
	}
	// Valid URL but bad timezone -> 400.
	if w := e.req(t, "PATCH", feedPath, map[string]any{"url": url, "timezone": "Bad/Zone"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("update bad tz = %d, want 400", w.Code)
	}

	// Feed belonging to another trip addressed under this one -> 404 (resolveFeed
	// trip mismatch).
	otherTrip := newTrip(t, e, owner, "Other feed trip")
	ow := e.req(t, "POST", "/api/trips/"+itoa(otherTrip)+"/feeds", map[string]any{"url": url}, owner)
	otherFeedID := int64(decodeBody[map[string]any](t, ow)["id"].(float64))
	if w := e.req(t, "PATCH", base+"/"+itoa(otherFeedID), map[string]any{"url": url}, owner); w.Code != http.StatusNotFound {
		t.Errorf("cross-trip feed = %d, want 404", w.Code)
	}

	// Non-editor cannot update or delete.
	other := e.user(t, "g1updother", false)
	if w := e.req(t, "PATCH", feedPath, map[string]any{"url": url}, other); w.Code != http.StatusForbidden {
		t.Errorf("non-editor update = %d, want 403", w.Code)
	}
	if w := e.req(t, "DELETE", feedPath, nil, other); w.Code != http.StatusForbidden {
		t.Errorf("non-editor delete = %d, want 403", w.Code)
	}

	// Delete bad feed id -> 400.
	if w := e.req(t, "DELETE", base+"/xyz", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("delete bad feed id = %d, want 400", w.Code)
	}
}
