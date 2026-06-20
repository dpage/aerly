package feeds

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// Service ties the Fetcher to the store: it refreshes feeds on a schedule (via
// the poller) and on demand (when a feed is added or its URL edited). It's
// shared by the poller and the HTTP handlers.
type Service struct {
	Store   *store.Store
	Fetcher *Fetcher
	// Interval is how stale a feed may get before the periodic sweep refetches
	// it. Defaults to 15 minutes.
	Interval time.Duration
}

// NewService builds a feed refresh service with sane defaults.
func NewService(s *store.Store, userAgent string, interval time.Duration) *Service {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return &Service{Store: s, Fetcher: NewFetcher(userAgent), Interval: interval}
}

// RefreshDue refetches every feed older than the configured interval. Best
// effort: one feed's failure is recorded and doesn't stop the rest.
func (s *Service) RefreshDue(ctx context.Context) {
	due, err := s.Store.FeedsDueForRefresh(ctx, time.Now().Add(-s.Interval))
	if err != nil {
		slog.Error("feeds: list due", "err", err)
		return
	}
	for _, f := range due {
		if ctx.Err() != nil {
			return
		}
		if err := s.RefreshFeed(ctx, f); err != nil {
			slog.Warn("feeds: refresh failed", "feed", f.ID, "url", f.URL, "err", err)
		}
	}
}

// RefreshFeed fetches one feed and updates its cached events and bookkeeping. A
// 304 (ErrNotModified) just bumps last_fetched_at; a transport/parse error is
// recorded on the feed (and returned) without disturbing the cached events.
func (s *Service) RefreshFeed(ctx context.Context, f *store.TripFeed) error {
	res, err := s.Fetcher.Fetch(ctx, f.URL, f.ETag, f.LastModified)
	if errors.Is(err, ErrNotModified) {
		return s.Store.MarkFeedFetched(ctx, f.ID, f.ETag, f.LastModified, "")
	}
	if err != nil {
		// Keep the prior validators so a transient blip still allows a future
		// conditional GET; record the error for the UI.
		if markErr := s.Store.MarkFeedFetched(ctx, f.ID, f.ETag, f.LastModified, err.Error()); markErr != nil {
			slog.Error("feeds: mark error", "feed", f.ID, "err", markErr)
		}
		return err
	}
	if err := s.Store.ReplaceFeedEvents(ctx, f.ID, res.Events); err != nil {
		return err
	}
	return s.Store.MarkFeedFetched(ctx, f.ID, res.ETag, res.LastModified, "")
}

// RefreshFeedAsync refetches one feed in the background, detached from the
// request that triggered it (so adding a feed returns immediately while its
// events populate shortly after). Failures are recorded on the feed row.
func (s *Service) RefreshFeedAsync(feedID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		f, err := s.Store.TripFeedByID(ctx, feedID)
		if err != nil {
			slog.Warn("feeds: async load", "feed", feedID, "err", err)
			return
		}
		if err := s.RefreshFeed(ctx, f); err != nil {
			slog.Warn("feeds: async refresh", "feed", feedID, "err", err)
		}
	}()
}
