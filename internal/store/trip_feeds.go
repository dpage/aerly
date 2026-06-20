package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// TripFeed is a registered iCalendar feed URL on a trip. The server refreshes
// it periodically and caches its events in trip_feed_events; the feed itself
// carries only the source URL and the bookkeeping needed to poll it politely.
type TripFeed struct {
	ID            int64
	TripID        int64
	URL           string
	Name          string
	ETag          string
	LastModified  string
	LastFetchedAt *time.Time
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TripFeedEvent is one cached event parsed from a feed. Events are read-only
// and replaced wholesale on each refresh — the feed is the source of truth.
type TripFeedEvent struct {
	ID          int64
	FeedID      int64
	UID         string
	Summary     string
	Description string
	Location    string
	StartsAt    time.Time
	EndsAt      *time.Time
	StartTZ     string
	AllDay      bool
	// FeedName is the owning feed's display name. Populated only on the read
	// path (TripFeedEventsForTrip); empty when parsing/storing events.
	FeedName string
}

const tripFeedColumns = `id, trip_id, url, name, etag, last_modified, last_fetched_at, last_error, created_at, updated_at`

func scanTripFeed(row pgx.Row) (*TripFeed, error) {
	var f TripFeed
	err := row.Scan(&f.ID, &f.TripID, &f.URL, &f.Name, &f.ETag, &f.LastModified,
		&f.LastFetchedAt, &f.LastError, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListTripFeeds returns the trip's feeds, oldest first (stable add order).
func (s *Store) ListTripFeeds(ctx context.Context, tripID int64) ([]*TripFeed, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tripFeedColumns+` FROM trip_feeds WHERE trip_id = $1 ORDER BY id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TripFeed
	for rows.Next() {
		var f TripFeed
		if err := rows.Scan(&f.ID, &f.TripID, &f.URL, &f.Name, &f.ETag, &f.LastModified,
			&f.LastFetchedAt, &f.LastError, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

// TripFeedByID returns one feed by id (used to resolve its trip for auth).
func (s *Store) TripFeedByID(ctx context.Context, id int64) (*TripFeed, error) {
	return scanTripFeed(s.pool.QueryRow(ctx,
		`SELECT `+tripFeedColumns+` FROM trip_feeds WHERE id = $1`, id))
}

// AddTripFeed registers a new feed URL on a trip with an optional friendly name.
func (s *Store) AddTripFeed(ctx context.Context, tripID int64, url, name string) (*TripFeed, error) {
	return scanTripFeed(s.pool.QueryRow(ctx, `
		INSERT INTO trip_feeds (trip_id, url, name)
		VALUES ($1, $2, $3)
		RETURNING `+tripFeedColumns, tripID, url, name))
}

// UpdateTripFeed changes a feed's URL and/or name. A changed URL clears the
// cached validators and error so the next poll re-fetches from scratch (the old
// events are dropped by ReplaceFeedEvents on that refresh).
func (s *Store) UpdateTripFeed(ctx context.Context, id int64, url, name string) (*TripFeed, error) {
	return scanTripFeed(s.pool.QueryRow(ctx, `
		UPDATE trip_feeds SET
			url           = $2,
			name          = $3,
			etag          = '',
			last_modified = '',
			last_error    = '',
			updated_at    = NOW()
		WHERE id = $1
		RETURNING `+tripFeedColumns, id, url, name))
}

// DeleteTripFeed removes a feed and (via cascade) its cached events.
func (s *Store) DeleteTripFeed(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM trip_feeds WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FeedsDueForRefresh returns feeds never fetched, or last fetched before
// `cutoff`, oldest-fetched first so a backlog drains fairly.
func (s *Store) FeedsDueForRefresh(ctx context.Context, cutoff time.Time) ([]*TripFeed, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tripFeedColumns+` FROM trip_feeds
		  WHERE last_fetched_at IS NULL OR last_fetched_at < $1
		  ORDER BY last_fetched_at ASC NULLS FIRST, id`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TripFeed
	for rows.Next() {
		var f TripFeed
		if err := rows.Scan(&f.ID, &f.TripID, &f.URL, &f.Name, &f.ETag, &f.LastModified,
			&f.LastFetchedAt, &f.LastError, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

// MarkFeedFetched records the outcome of a poll: it stamps last_fetched_at,
// updates the conditional-GET validators, and sets (or clears) last_error.
func (s *Store) MarkFeedFetched(ctx context.Context, id int64, etag, lastModified, fetchErr string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE trip_feeds SET
			etag            = $2,
			last_modified   = $3,
			last_error      = $4,
			last_fetched_at = NOW()
		WHERE id = $1`, id, etag, lastModified, fetchErr)
	return err
}

// ReplaceFeedEvents swaps a feed's cached events for a freshly-parsed set, in
// one transaction (delete-all then insert). The feed is the source of truth, so
// a wholesale replace is simpler — and safer against deletions — than a
// per-UID reconcile.
func (s *Store) ReplaceFeedEvents(ctx context.Context, feedID int64, events []TripFeedEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM trip_feed_events WHERE feed_id = $1`, feedID); err != nil {
		return err
	}
	for _, e := range events {
		if _, err := tx.Exec(ctx, `
			INSERT INTO trip_feed_events
				(feed_id, uid, summary, description, location, starts_at, ends_at, start_tz, all_day)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			feedID, e.UID, e.Summary, e.Description, e.Location,
			e.StartsAt, e.EndsAt, e.StartTZ, e.AllDay); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// TripFeedEventsForTrip returns every cached event across all of a trip's
// feeds, in start-time order, each tagged with its source feed id.
func (s *Store) TripFeedEventsForTrip(ctx context.Context, tripID int64) ([]*TripFeedEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.feed_id, e.uid, e.summary, e.description, e.location,
		       e.starts_at, e.ends_at, e.start_tz, e.all_day, f.name
		  FROM trip_feed_events e
		  JOIN trip_feeds f ON f.id = e.feed_id
		 WHERE f.trip_id = $1
		 ORDER BY e.starts_at, e.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TripFeedEvent
	for rows.Next() {
		var e TripFeedEvent
		if err := rows.Scan(&e.ID, &e.FeedID, &e.UID, &e.Summary, &e.Description, &e.Location,
			&e.StartsAt, &e.EndsAt, &e.StartTZ, &e.AllDay, &e.FeedName); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
