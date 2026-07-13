package providers

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakePOIs struct {
	calls atomic.Int32
	out   []POI
}

func (f *fakePOIs) Nearby(ctx context.Context, lat, lon float64, radiusM int, cats []string) ([]POI, error) {
	f.calls.Add(1)
	return f.out, nil
}

func TestCachedPOIsHitsAvoidSecondCall(t *testing.T) {
	inner := &fakePOIs{out: []POI{{Name: "X"}}}
	c := NewCachedPOIs(inner, time.Hour)

	if _, err := c.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != 1 {
		t.Errorf("expected 1 upstream call, got %d", inner.calls.Load())
	}
}

func TestCachedPOIsKeyVariesByCats(t *testing.T) {
	inner := &fakePOIs{out: []POI{{Name: "X"}}}
	c := NewCachedPOIs(inner, time.Hour)
	_, _ = c.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights"})
	_, _ = c.Nearby(context.Background(), 51.5, -0.12, 2000, []string{"sights", "food"})
	if inner.calls.Load() != 2 {
		t.Errorf("different cats must miss cache, got %d calls", inner.calls.Load())
	}
}

func TestCachedPOIsIsBounded(t *testing.T) {
	inner := &fakePOIs{out: []POI{{Name: "X"}}}
	c := NewCachedPOIs(inner, time.Hour)

	// Insert well past the cap, varying the coordinates each call so every
	// request is a distinct cache key, and confirm the map never grows
	// beyond maxPOICacheEntries (the clear-on-cap eviction kicks in instead
	// of accumulating entries forever).
	for i := 0; i < maxPOICacheEntries*2; i++ {
		lat := 51.0 + float64(i)*0.01
		lon := -0.1 + float64(i)*0.01
		if _, err := c.Nearby(context.Background(), lat, lon, 2000, []string{"sights"}); err != nil {
			t.Fatal(err)
		}
		if len(c.buf) > maxPOICacheEntries {
			t.Fatalf("cache grew to %d entries, want <= %d", len(c.buf), maxPOICacheEntries)
		}
	}
}
