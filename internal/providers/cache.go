package providers

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// CachedResolver wraps an inner Resolver with an in-memory (ident, date) →
// ResolvedFlight cache. Hits return the cached entry without touching the
// upstream; ErrFlightNotFound is cached too (so we don't keep retrying a
// known-bad ident, which would burn quota and could trip a rate limit).
//
// Schedules are essentially static once published — the route, airports,
// times, and airframe don't change between a few hours after the flight is
// scheduled and a few hours after it's flown. A 24-hour TTL is a generous
// upper bound that lets us re-fetch overnight in case a flight was edited.
type CachedResolver struct {
	Inner Resolver
	TTL   time.Duration

	mu  sync.Mutex
	buf map[string]cacheEntry
}

type cacheEntry struct {
	flight  *ResolvedFlight
	notFnd  bool
	expires time.Time
}

func NewCachedResolver(inner Resolver, ttl time.Duration) *CachedResolver {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &CachedResolver{
		Inner: inner,
		TTL:   ttl,
		buf:   map[string]cacheEntry{},
	}
}

func (c *CachedResolver) Resolve(ctx context.Context, ident string, date time.Time) (*ResolvedFlight, error) {
	key := cacheKey(ident, date)
	now := time.Now()

	c.mu.Lock()
	entry, ok := c.buf[key]
	c.mu.Unlock()
	if ok && now.Before(entry.expires) {
		if entry.notFnd {
			slog.Debug("resolver cache hit (not found)", "key", key)
			return nil, ErrFlightNotFound
		}
		slog.Debug("resolver cache hit", "key", key)
		// Return a defensive copy so a caller can't mutate the cached value.
		rfCopy := *entry.flight
		return &rfCopy, nil
	}

	rf, err := c.Inner.Resolve(ctx, ident, date)
	// Only cache definitive answers — a hit (rf != nil) or a confirmed
	// not-found from the upstream. Network / rate-limit / 5xx errors are
	// intentionally NOT cached, so the next attempt gets a fresh try.
	switch {
	case err == nil && rf != nil:
		// Defensive copy at *store* time too, so a caller mutating the
		// returned pointer can't corrupt the cached value.
		stored := *rf
		c.store(key, cacheEntry{flight: &stored, expires: now.Add(c.TTL)})
	case errors.Is(err, ErrFlightNotFound):
		c.store(key, cacheEntry{notFnd: true, expires: now.Add(c.TTL)})
	}
	return rf, err
}

func (c *CachedResolver) store(key string, e cacheEntry) {
	c.mu.Lock()
	c.buf[key] = e
	c.mu.Unlock()
}

// Purge drops entries whose expiry has passed. Optional housekeeping —
// callers can drive it from a ticker, but it's also safe to skip; the
// natural traffic-driven re-fill in Resolve handles correctness.
func (c *CachedResolver) Purge(now time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	dropped := 0
	for k, e := range c.buf {
		if !now.Before(e.expires) {
			delete(c.buf, k)
			dropped++
		}
	}
	return dropped
}

func cacheKey(ident string, date time.Time) string {
	return ident + "|" + date.UTC().Format("2006-01-02")
}

// Compile-time check that CachedResolver satisfies Resolver.
var _ Resolver = (*CachedResolver)(nil)
