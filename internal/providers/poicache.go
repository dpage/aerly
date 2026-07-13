package providers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CachedPOIs wraps an inner POIResolver with an in-memory (lat, lon, radius,
// cats) → []POI cache. POI results around a given anchor are effectively
// static over the short term (OSM edits aside), so a generous TTL avoids
// hammering the upstream Overpass instance, which enforces its own fair-use
// rate limit.
type CachedPOIs struct {
	Inner POIResolver
	TTL   time.Duration
	Now   func() time.Time

	mu  sync.Mutex
	buf map[string]poiEntry
}

type poiEntry struct {
	pois    []POI
	expires time.Time
}

// NewCachedPOIs builds a CachedPOIs wrapping inner with the given TTL. A
// non-positive ttl falls back to a 7-day default.
func NewCachedPOIs(inner POIResolver, ttl time.Duration) *CachedPOIs {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &CachedPOIs{Inner: inner, TTL: ttl, Now: time.Now, buf: map[string]poiEntry{}}
}

func (c *CachedPOIs) Nearby(ctx context.Context, lat, lon float64, radiusM int, cats []string) ([]POI, error) {
	key := poiKey(lat, lon, radiusM, cats)
	now := c.Now()

	c.mu.Lock()
	e, ok := c.buf[key]
	c.mu.Unlock()
	if ok && now.Before(e.expires) {
		return append([]POI(nil), e.pois...), nil
	}

	pois, err := c.Inner.Nearby(ctx, lat, lon, radiusM, cats)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.buf[key] = poiEntry{pois: append([]POI(nil), pois...), expires: now.Add(c.TTL)}
	c.mu.Unlock()
	return pois, nil
}

// poiKey rounds coordinates to ~3dp (~110 m) so nearby anchors share a cache
// entry, and sorts cats so equivalent requests in a different order still
// hit the same entry.
func poiKey(lat, lon float64, radiusM int, cats []string) string {
	sorted := append([]string(nil), cats...)
	sort.Strings(sorted)
	return fmt.Sprintf("%.3f|%.3f|%d|%s", lat, lon, radiusM, strings.Join(sorted, ","))
}

// Compile-time check that CachedPOIs satisfies POIResolver.
var _ POIResolver = (*CachedPOIs)(nil)
