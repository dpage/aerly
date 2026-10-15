package weather

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Cached wraps a Forecaster with an in-memory cache. Several travellers on the
// same flight ask for the same destination and dates, and a forecast changes
// slowly, so one upstream call serves them all.
type Cached struct {
	Inner Forecaster
	TTL   time.Duration
	Now   func() time.Time

	mu  sync.Mutex
	buf map[string]cacheEntry
}

type cacheEntry struct {
	days    []Day
	expires time.Time
}

// maxCacheEntries bounds the cache. Keys derive from flight coordinates and
// dates, so without a bound the map could grow for the process lifetime. When
// the cap is hit the cache is cleared, as providers.CachedPOIs does.
const maxCacheEntries = 1024

// NewCached builds a Cached wrapping inner. A non-positive ttl falls back to
// one hour.
func NewCached(inner Forecaster, ttl time.Duration) *Cached {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &Cached{Inner: inner, TTL: ttl, Now: time.Now, buf: map[string]cacheEntry{}}
}

// Forecast implements Forecaster. Coordinates are rounded to two decimal
// places (about a kilometre) for the key, so nearby lookups share an entry.
func (c *Cached) Forecast(ctx context.Context, lat, lon float64, tz string, from time.Time, days int) ([]Day, error) {
	key := fmt.Sprintf("%.2f|%.2f|%s|%s|%d", lat, lon, tz, from.Format("2006-01-02"), days)
	now := c.Now()

	c.mu.Lock()
	e, ok := c.buf[key]
	c.mu.Unlock()
	if ok && now.Before(e.expires) {
		return append([]Day(nil), e.days...), nil
	}

	out, err := c.Inner.Forecast(ctx, lat, lon, tz, from, days)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if len(c.buf) >= maxCacheEntries {
		c.buf = map[string]cacheEntry{}
	}
	c.buf[key] = cacheEntry{days: append([]Day(nil), out...), expires: now.Add(c.TTL)}
	c.mu.Unlock()
	return out, nil
}

var _ Forecaster = (*Cached)(nil)
