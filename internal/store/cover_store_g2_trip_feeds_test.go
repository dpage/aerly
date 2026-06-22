package store

import (
	"errors"
	"testing"
	"time"
)

func TestG2TripFeedCRUD(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	feed, err := s.AddTripFeed(ctx, trip, "https://example.com/cal.ics", "Work", "Europe/London")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}
	if feed.URL != "https://example.com/cal.ics" || feed.Name != "Work" || feed.Timezone != "Europe/London" {
		t.Errorf("unexpected feed: %+v", feed)
	}

	// TripFeedByID resolves it; unknown id → ErrNotFound.
	got, err := s.TripFeedByID(ctx, feed.ID)
	if err != nil || got.ID != feed.ID {
		t.Fatalf("TripFeedByID = %+v, %v", got, err)
	}
	if _, err := s.TripFeedByID(ctx, 99999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("TripFeedByID unknown = %v, want ErrNotFound", err)
	}

	// ListTripFeeds returns it.
	feeds, err := s.ListTripFeeds(ctx, trip)
	if err != nil || len(feeds) != 1 || feeds[0].ID != feed.ID {
		t.Fatalf("ListTripFeeds = %+v, %v", feeds, err)
	}

	// Update changes the fields and clears validators/error.
	upd, err := s.UpdateTripFeed(ctx, feed.ID, "https://example.com/new.ics", "Holiday", "UTC")
	if err != nil {
		t.Fatalf("UpdateTripFeed: %v", err)
	}
	if upd.URL != "https://example.com/new.ics" || upd.Name != "Holiday" || upd.Timezone != "UTC" {
		t.Errorf("unexpected updated feed: %+v", upd)
	}

	// Delete; second delete → ErrNotFound.
	if err := s.DeleteTripFeed(ctx, feed.ID); err != nil {
		t.Fatalf("DeleteTripFeed: %v", err)
	}
	if err := s.DeleteTripFeed(ctx, feed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double DeleteTripFeed = %v, want ErrNotFound", err)
	}
}

func TestG2FeedRefreshAndEvents(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	feed, err := s.AddTripFeed(ctx, trip, "https://example.com/f.ics", "Feed", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}

	// A never-fetched feed is due for refresh.
	due, err := s.FeedsDueForRefresh(ctx, time.Now())
	if err != nil {
		t.Fatalf("FeedsDueForRefresh: %v", err)
	}
	if !feedInList(due, feed.ID) {
		t.Errorf("never-fetched feed %d should be due", feed.ID)
	}

	// Mark it fetched with validators; it drops out of the due list for a cutoff
	// in the past.
	if err := s.MarkFeedFetched(ctx, feed.ID, "etag-1", "Mon, 01 Jun 2026 00:00:00 GMT", ""); err != nil {
		t.Fatalf("MarkFeedFetched: %v", err)
	}
	due, _ = s.FeedsDueForRefresh(ctx, time.Now().Add(-time.Hour))
	if feedInList(due, feed.ID) {
		t.Errorf("recently-fetched feed %d should not be due", feed.ID)
	}

	// Replace events (twice, to exercise the delete-then-insert path).
	start := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	events := []TripFeedEvent{
		{UID: "evt-1", Summary: "Dinner", Description: "with friends", Location: "Lisbon",
			StartsAt: start, EndsAt: &end, StartTZ: "Europe/Lisbon", AllDay: false},
		{UID: "evt-2", Summary: "Conference", StartsAt: start.Add(48 * time.Hour), AllDay: true},
	}
	if err := s.ReplaceFeedEvents(ctx, feed.ID, events); err != nil {
		t.Fatalf("ReplaceFeedEvents: %v", err)
	}
	// Replace again with a single event; the old two must be gone.
	if err := s.ReplaceFeedEvents(ctx, feed.ID, events[:1]); err != nil {
		t.Fatalf("ReplaceFeedEvents 2: %v", err)
	}

	out, err := s.TripFeedEventsForTrip(ctx, trip)
	if err != nil {
		t.Fatalf("TripFeedEventsForTrip: %v", err)
	}
	if len(out) != 1 || out[0].UID != "evt-1" || out[0].FeedName != "Feed" {
		t.Fatalf("events = %+v, want one evt-1 tagged Feed", out)
	}
	if out[0].EndsAt == nil || !out[0].EndsAt.Equal(end) {
		t.Errorf("event end = %v, want %v", out[0].EndsAt, end)
	}

	// An empty replace clears everything.
	if err := s.ReplaceFeedEvents(ctx, feed.ID, nil); err != nil {
		t.Fatalf("ReplaceFeedEvents empty: %v", err)
	}
	out, _ = s.TripFeedEventsForTrip(ctx, trip)
	if len(out) != 0 {
		t.Errorf("events after empty replace = %d, want 0", len(out))
	}
}

func TestG2TripFeedsErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	if _, err := s.ListTripFeeds(cc, 1); err == nil {
		t.Error("ListTripFeeds cancelled should error")
	}
	if _, err := s.FeedsDueForRefresh(cc, time.Now()); err == nil {
		t.Error("FeedsDueForRefresh cancelled should error")
	}
	if _, err := s.TripFeedEventsForTrip(cc, 1); err == nil {
		t.Error("TripFeedEventsForTrip cancelled should error")
	}
	if err := s.MarkFeedFetched(cc, 1, "", "", ""); err == nil {
		t.Error("MarkFeedFetched cancelled should error")
	}
	if err := s.DeleteTripFeed(cc, 1); err == nil {
		t.Error("DeleteTripFeed cancelled should error")
	}
	if err := s.ReplaceFeedEvents(cc, 1, nil); err == nil {
		t.Error("ReplaceFeedEvents cancelled should error")
	}
	// ReplaceFeedEvents with a row whose insert fails (unknown feed_id → FK
	// violation) exercises the insert error branch.
	if err := s.ReplaceFeedEvents(ctx, 99999999, []TripFeedEvent{{UID: "x", StartsAt: time.Now()}}); err == nil {
		t.Error("ReplaceFeedEvents with bad feed_id should error on insert")
	}
}

func feedInList(feeds []*TripFeed, id int64) bool {
	for _, f := range feeds {
		if f.ID == id {
			return true
		}
	}
	return false
}
