package providers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver lets us drive the cache from the outside.
type fakeResolver struct {
	calls atomic.Int32
	resp  *ResolvedFlight
	err   error
}

func (f *fakeResolver) Resolve(_ context.Context, ident string, _ time.Time) (*ResolvedFlight, error) {
	f.calls.Add(1)
	if f.resp != nil {
		c := *f.resp
		c.Ident = ident
		return &c, f.err
	}
	return nil, f.err
}

func TestCachedResolverHit(t *testing.T) {
	inner := &fakeResolver{resp: &ResolvedFlight{OriginIATA: "LHR", DestIATA: "JFK"}}
	c := NewCachedResolver(inner, time.Hour)
	ctx := context.Background()
	date := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	// First call → miss, populates cache.
	if _, err := c.Resolve(ctx, "BA286", date); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if inner.calls.Load() != 1 {
		t.Errorf("expected 1 upstream call, got %d", inner.calls.Load())
	}

	// Second call with same key → hit, upstream untouched.
	if _, err := c.Resolve(ctx, "BA286", date); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("expected upstream count to stay at 1, got %d", got)
	}
}

// Defensive copy: a caller mutating the returned value must not corrupt the
// next cache hit for another caller.
func TestCachedResolverReturnsCopy(t *testing.T) {
	inner := &fakeResolver{resp: &ResolvedFlight{OriginIATA: "LHR"}}
	c := NewCachedResolver(inner, time.Hour)
	ctx := context.Background()
	date := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	r1, err := c.Resolve(ctx, "BA286", date)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	r1.OriginIATA = "TAMPERED"

	r2, err := c.Resolve(ctx, "BA286", date)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r2.OriginIATA != "LHR" {
		t.Errorf("cache was mutated through returned pointer: %q", r2.OriginIATA)
	}
}

// not-found responses are cached too — otherwise a wrong ident would keep
// hitting AeroDataBox and could trip a rate limit.
func TestCachedResolverCachesNotFound(t *testing.T) {
	inner := &fakeResolver{err: ErrFlightNotFound}
	c := NewCachedResolver(inner, time.Hour)
	ctx := context.Background()
	date := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		_, err := c.Resolve(ctx, "BA9999", date)
		if !errors.Is(err, ErrFlightNotFound) {
			t.Fatalf("call %d: want ErrFlightNotFound, got %v", i, err)
		}
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("expected 1 upstream call (rest cached), got %d", got)
	}
}

// Even when the inner wraps ErrFlightNotFound (which AeroDataBox.Resolve
// does to add context), isNotFound should unwrap and recognise it.
func TestCachedResolverCachesWrappedNotFound(t *testing.T) {
	wrapped := errors.Join(ErrFlightNotFound, errors.New("no flight found for BA9999 on 2026-05-19"))
	inner := &fakeResolver{err: wrapped}
	c := NewCachedResolver(inner, time.Hour)
	ctx := context.Background()
	date := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	_, err1 := c.Resolve(ctx, "BA9999", date)
	_, err2 := c.Resolve(ctx, "BA9999", date)
	if err1 == nil || err2 == nil {
		t.Fatal("expected errors")
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("wrapped not-found should be cached; got %d upstream calls", got)
	}
}

// Rate-limit / 5xx / network errors are NOT cached — the next attempt
// must hit the upstream so we recover automatically when it comes back.
func TestCachedResolverDoesNotCacheTransientErrors(t *testing.T) {
	inner := &fakeResolver{err: errors.New("aerodatabox rate limit hit")}
	c := NewCachedResolver(inner, time.Hour)
	ctx := context.Background()
	date := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		_, _ = c.Resolve(ctx, "BA286", date)
	}
	if got := inner.calls.Load(); got != 3 {
		t.Errorf("transient errors should NOT be cached; got %d upstream calls, want 3", got)
	}
}

func TestCachedResolverTTLExpiry(t *testing.T) {
	inner := &fakeResolver{resp: &ResolvedFlight{OriginIATA: "LHR"}}
	c := NewCachedResolver(inner, 50*time.Millisecond)
	ctx := context.Background()
	date := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	_, _ = c.Resolve(ctx, "BA286", date)
	time.Sleep(80 * time.Millisecond)
	_, _ = c.Resolve(ctx, "BA286", date)
	if got := inner.calls.Load(); got != 2 {
		t.Errorf("expected re-fetch after TTL; got %d upstream calls", got)
	}
}

func TestCachedResolverPurge(t *testing.T) {
	inner := &fakeResolver{resp: &ResolvedFlight{OriginIATA: "LHR"}}
	c := NewCachedResolver(inner, 50*time.Millisecond)
	ctx := context.Background()
	date := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)

	_, _ = c.Resolve(ctx, "BA286", date)
	_, _ = c.Resolve(ctx, "BA287", date)
	dropped := c.Purge(time.Now().Add(time.Hour)) // way past TTL
	if dropped != 2 {
		t.Errorf("Purge dropped %d, want 2", dropped)
	}
	// Both gone → next call goes to upstream.
	_, _ = c.Resolve(ctx, "BA286", date)
	if got := inner.calls.Load(); got != 3 {
		t.Errorf("expected 3 upstream calls after purge, got %d", got)
	}
}

func TestCacheKey(t *testing.T) {
	d := time.Date(2026, 5, 19, 18, 30, 0, 0, time.FixedZone("CET", 3600))
	k := cacheKey("BA286", d)
	want := "BA286|2026-05-19"
	if k != want {
		t.Errorf("cacheKey = %q, want %q (must normalise to UTC date)", k, want)
	}
}
