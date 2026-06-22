package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

// newFeedService wires a Service to a real store and a Fetcher whose SSRF guard
// is relaxed (httptest binds to loopback). The returned trip id is used to hang
// feeds off a valid trip row (the trip_feeds FK requires it).
func newFeedService(t *testing.T, interval time.Duration) (*Service, *store.Store, int64) {
	t.Helper()
	pool := testsupport.NewPool(t)
	st := store.New(pool)
	owner := testsupport.InsertUser(t, pool, "feeds-owner", false, true)
	trip, err := st.CreateTrip(context.Background(), store.CreateTripPayload{Name: "Conf Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	svc := NewService(st, "test-agent", interval)
	svc.Fetcher.AllowPrivate = true
	return svc, st, trip.ID
}

func TestNewServiceDefaultInterval(t *testing.T) {
	// A non-positive interval is replaced by the 15-minute default; a positive
	// one is kept verbatim.
	if got := NewService(nil, "ua", 0).Interval; got != 15*time.Minute {
		t.Errorf("default interval = %v, want 15m", got)
	}
	if got := NewService(nil, "ua", -5).Interval; got != 15*time.Minute {
		t.Errorf("negative interval = %v, want 15m default", got)
	}
	if got := NewService(nil, "ua", 90*time.Second).Interval; got != 90*time.Second {
		t.Errorf("explicit interval = %v, want 90s", got)
	}
}

func TestRefreshFeedSuccess(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("Last-Modified", "Tue, 20 Oct 2026 08:00:00 GMT")
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	feed, err := st.AddTripFeed(ctx, tripID, srv.URL, "Schedule", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}

	if err := svc.RefreshFeed(ctx, feed); err != nil {
		t.Fatalf("RefreshFeed: %v", err)
	}

	// Events were cached and the validators/timestamp recorded.
	evs, err := st.TripFeedEventsForTrip(ctx, tripID)
	if err != nil {
		t.Fatalf("TripFeedEventsForTrip: %v", err)
	}
	if len(evs) != 1 || evs[0].UID != "talk-1@example.com" {
		t.Fatalf("cached events = %+v, want the one keynote", evs)
	}
	got, err := st.TripFeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("TripFeedByID: %v", err)
	}
	if got.ETag != `"abc"` || got.LastModified == "" {
		t.Errorf("validators not stored: etag=%q lastmod=%q", got.ETag, got.LastModified)
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want empty after success", got.LastError)
	}
	if got.LastFetchedAt == nil {
		t.Error("LastFetchedAt not stamped")
	}
}

func TestRefreshFeedNotModified(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	feed, err := st.AddTripFeed(ctx, tripID, srv.URL, "Schedule", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}
	// Seed the cached ETag so the second poll is a conditional GET that 304s.
	feed.ETag = `"v1"`

	if err := svc.RefreshFeed(ctx, feed); err != nil {
		t.Fatalf("RefreshFeed (304): %v", err)
	}
	got, err := st.TripFeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("TripFeedByID: %v", err)
	}
	if got.LastFetchedAt == nil {
		t.Error("304 should still bump LastFetchedAt")
	}
	if got.ETag != `"v1"` {
		t.Errorf("ETag = %q, want preserved %q on 304", got.ETag, `"v1"`)
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want cleared on 304", got.LastError)
	}
}

func TestRefreshFeedRecordsError(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	feed, err := st.AddTripFeed(ctx, tripID, srv.URL, "Schedule", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}
	feed.ETag = `"keep"`

	if err := svc.RefreshFeed(ctx, feed); err == nil {
		t.Fatal("RefreshFeed = nil, want an error for a 500")
	}
	got, err := st.TripFeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("TripFeedByID: %v", err)
	}
	if got.LastError == "" {
		t.Error("LastError not recorded for a failed fetch")
	}
	// Prior validators are kept so a future conditional GET still works.
	if got.ETag != `"keep"` {
		t.Errorf("ETag = %q, want prior %q preserved on error", got.ETag, `"keep"`)
	}
}

func TestRefreshDue(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	feed, err := st.AddTripFeed(ctx, tripID, srv.URL, "Schedule", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}

	// Never-fetched feed is due; the sweep should fetch and cache its events.
	svc.RefreshDue(ctx)

	evs, err := st.TripFeedEventsForTrip(ctx, tripID)
	if err != nil {
		t.Fatalf("TripFeedEventsForTrip: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("after RefreshDue, events = %d, want 1", len(evs))
	}
	got, err := st.TripFeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("TripFeedByID: %v", err)
	}
	if got.LastFetchedAt == nil {
		t.Error("RefreshDue did not mark the feed fetched")
	}
}

func TestRefreshDueRecordsPerFeedFailure(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	// This feed always 500s, so RefreshFeed fails; RefreshDue must log the
	// per-feed failure and carry on rather than abort the sweep.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	feed, err := st.AddTripFeed(ctx, tripID, srv.URL, "Broken", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}

	svc.RefreshDue(ctx)

	got, err := st.TripFeedByID(ctx, feed.ID)
	if err != nil {
		t.Fatalf("TripFeedByID: %v", err)
	}
	if got.LastError == "" {
		t.Error("RefreshDue did not record the per-feed failure as last_error")
	}
}

func TestRefreshDueCancelledContext(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	if _, err := st.AddTripFeed(ctx, tripID, srv.URL, "Schedule", ""); err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}

	// A context cancelled before the loop body runs bails out without fetching.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	svc.RefreshDue(cancelled)

	evs, err := st.TripFeedEventsForTrip(ctx, tripID)
	if err != nil {
		t.Fatalf("TripFeedEventsForTrip: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("cancelled RefreshDue cached %d events, want 0", len(evs))
	}
}

func TestRefreshDueListError(t *testing.T) {
	svc, st, _ := newFeedService(t, time.Minute)

	// A cancelled context makes FeedsDueForRefresh's query fail, exercising the
	// list-error branch. It must return without panicking.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	svc.RefreshDue(cancelled)
	_ = st // store retained only for symmetry with the other tests
}

func TestRefreshFeedAsync(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	feed, err := st.AddTripFeed(ctx, tripID, srv.URL, "Schedule", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}

	svc.RefreshFeedAsync(feed.ID)

	// The async refresh is a detached goroutine; poll until events appear.
	deadline := time.Now().Add(10 * time.Second)
	for {
		evs, err := st.TripFeedEventsForTrip(ctx, tripID)
		if err != nil {
			t.Fatalf("TripFeedEventsForTrip: %v", err)
		}
		if len(evs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("async refresh never cached events (got %d)", len(evs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRefreshFeedAsyncRefreshError(t *testing.T) {
	svc, st, tripID := newFeedService(t, time.Minute)
	ctx := context.Background()

	// The feed loads fine but its fetch 500s, so the async refresh logs a
	// refresh failure (the goroutine's second error branch).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	svc.Fetcher.HTTP = srv.Client()

	feed, err := st.AddTripFeed(ctx, tripID, srv.URL, "Broken", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}

	svc.RefreshFeedAsync(feed.ID)

	// Poll until the failure is recorded on the feed row.
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := st.TripFeedByID(ctx, feed.ID)
		if err != nil {
			t.Fatalf("TripFeedByID: %v", err)
		}
		if got.LastError != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("async refresh never recorded the failure")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRefreshFeedAsyncLoadError(t *testing.T) {
	svc, _, _ := newFeedService(t, time.Minute)
	// A non-existent feed id makes the async load fail; the goroutine must log
	// and return without touching the store further. We can only assert it does
	// not panic, so give it a moment to run.
	svc.RefreshFeedAsync(-1)
	time.Sleep(200 * time.Millisecond)
}
