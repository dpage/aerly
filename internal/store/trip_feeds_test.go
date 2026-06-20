package store

import (
	"testing"
	"time"
)

func TestTripFeedsCRUDAndEvents(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Add two feeds.
	f1, err := s.AddTripFeed(ctx, trip, "https://example.com/a.ics", "Schedule A", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}
	if _, err := s.AddTripFeed(ctx, trip, "https://example.com/b.ics", "", ""); err != nil {
		t.Fatalf("AddTripFeed b: %v", err)
	}

	feeds, err := s.ListTripFeeds(ctx, trip)
	if err != nil || len(feeds) != 2 {
		t.Fatalf("ListTripFeeds = %d, %v", len(feeds), err)
	}
	if feeds[0].URL != "https://example.com/a.ics" || feeds[0].Name != "Schedule A" {
		t.Errorf("feed[0] = %+v", feeds[0])
	}

	// Editing the URL clears the validators/error.
	if err := s.MarkFeedFetched(ctx, f1.ID, `"etag"`, "Mon", "boom"); err != nil {
		t.Fatalf("MarkFeedFetched: %v", err)
	}
	upd, err := s.UpdateTripFeed(ctx, f1.ID, "https://example.com/a2.ics", "A2", "America/Vancouver")
	if err != nil {
		t.Fatalf("UpdateTripFeed: %v", err)
	}
	if upd.URL != "https://example.com/a2.ics" || upd.Name != "A2" {
		t.Errorf("update = %+v", upd)
	}
	if upd.Timezone != "America/Vancouver" {
		t.Errorf("timezone not saved: %+v", upd)
	}
	if upd.ETag != "" || upd.LastModified != "" || upd.LastError != "" {
		t.Errorf("edit should clear validators/error: %+v", upd)
	}

	// Replace events and read them back across the trip, tagged with feed name.
	start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	if err := s.ReplaceFeedEvents(ctx, f1.ID, []TripFeedEvent{
		{UID: "x", Summary: "Keynote", Location: "Hall", StartsAt: start, EndsAt: &end, StartTZ: "UTC"},
	}); err != nil {
		t.Fatalf("ReplaceFeedEvents: %v", err)
	}
	events, err := s.TripFeedEventsForTrip(ctx, trip)
	if err != nil || len(events) != 1 {
		t.Fatalf("TripFeedEventsForTrip = %d, %v", len(events), err)
	}
	if events[0].Summary != "Keynote" || events[0].FeedName != "A2" {
		t.Errorf("event = %+v", events[0])
	}

	// Replacing again wipes the prior set (source-of-truth semantics).
	if err := s.ReplaceFeedEvents(ctx, f1.ID, nil); err != nil {
		t.Fatalf("ReplaceFeedEvents empty: %v", err)
	}
	events, _ = s.TripFeedEventsForTrip(ctx, trip)
	if len(events) != 0 {
		t.Fatalf("expected 0 events after empty replace, got %d", len(events))
	}

	// FeedsDueForRefresh surfaces never-fetched feeds.
	due, err := s.FeedsDueForRefresh(ctx, time.Now())
	if err != nil {
		t.Fatalf("FeedsDueForRefresh: %v", err)
	}
	if len(due) == 0 {
		t.Errorf("expected at least one feed due for refresh")
	}

	// Deleting a feed cascades its events away.
	if err := s.DeleteTripFeed(ctx, f1.ID); err != nil {
		t.Fatalf("DeleteTripFeed: %v", err)
	}
	feeds, _ = s.ListTripFeeds(ctx, trip)
	if len(feeds) != 1 {
		t.Errorf("after delete, ListTripFeeds = %d, want 1", len(feeds))
	}
}
