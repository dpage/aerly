package providers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

type fakeLatest struct {
	pos *store.Position
	err error
}

func (f fakeLatest) LatestPosition(context.Context, int64) (*store.Position, error) {
	return f.pos, f.err
}

func TestNewSpeedGateDefaults(t *testing.T) {
	g := NewSpeedGate(fakeTracker{}, fakeLatest{})
	if g.MaxSpeedKt != 1000 {
		t.Errorf("default MaxSpeedKt = %v, want 1000", g.MaxSpeedKt)
	}
	if g.Inner == nil || g.Anchor == nil {
		t.Error("Inner/Anchor not wired")
	}
}

func TestSpeedGatePassesNilFromInner(t *testing.T) {
	g := NewSpeedGate(fakeTracker{}, fakeLatest{pos: &store.Position{Lat: 0, Lon: 0, Ts: time.Now()}})
	got, err := g.Track(context.Background(), baseFlight(), time.Now())
	if got != nil || err != nil {
		t.Errorf("inner nil → (nil,nil); got %v %v", got, err)
	}
}

func TestSpeedGatePassesInnerError(t *testing.T) {
	boom := errors.New("boom")
	g := NewSpeedGate(fakeTracker{err: boom}, fakeLatest{})
	_, err := g.Track(context.Background(), baseFlight(), time.Now())
	if !errors.Is(err, boom) {
		t.Errorf("inner error should propagate, got %v", err)
	}
}

func TestSpeedGatePassesEstimatedFromInner(t *testing.T) {
	// Estimated fixes come from dead-reckoners (downstream of OpenSky in the
	// production wiring), so they should never be subject to plausibility
	// gating — they're already a hypothesis, not measured data.
	now := time.Now()
	innerPos := &store.Position{Lat: 51, Lon: -1, Ts: now, IsEstimated: true}
	g := NewSpeedGate(
		fakeTracker{pos: innerPos},
		fakeLatest{pos: &store.Position{Lat: 1, Lon: 101, Ts: now.Add(-2 * time.Minute)}},
	)
	got, _ := g.Track(context.Background(), baseFlight(), now)
	if got != innerPos {
		t.Errorf("estimated fix should pass through unchanged, got %+v", got)
	}
}

func TestSpeedGatePassesRealWhenNoAnchorYet(t *testing.T) {
	now := time.Now()
	innerPos := &store.Position{Lat: 51, Lon: -1, Ts: now}
	g := NewSpeedGate(fakeTracker{pos: innerPos}, fakeLatest{}) // anchor nil
	got, _ := g.Track(context.Background(), baseFlight(), now)
	if got != innerPos {
		t.Errorf("no anchor → pass-through; got %+v", got)
	}
}

func TestSpeedGatePassesRealWithinThreshold(t *testing.T) {
	// LHR area, ~30 nm apart over 2 minutes = ~900 kt ground speed. Plausible.
	now := time.Now()
	innerPos := &store.Position{Lat: 51.99, Lon: -0.46, Ts: now}
	anchorPos := &store.Position{Lat: 51.47, Lon: -0.46, Ts: now.Add(-2 * time.Minute)}
	g := NewSpeedGate(fakeTracker{pos: innerPos}, fakeLatest{pos: anchorPos})
	got, _ := g.Track(context.Background(), baseFlight(), now)
	if got != innerPos {
		t.Errorf("within threshold → pass; got %+v", got)
	}
}

func TestSpeedGateRejectsTeleport(t *testing.T) {
	// The actual bug: anchor over North Sea, new "real" fix near Singapore,
	// 2 minutes apart. Implied speed ~200,000 kt → reject.
	now := time.Now()
	innerPos := &store.Position{Lat: 4.28, Lon: 101.53, Ts: now}
	anchorPos := &store.Position{Lat: 56.4, Lon: 1.5, Ts: now.Add(-2 * time.Minute), IsEstimated: true}
	g := NewSpeedGate(fakeTracker{pos: innerPos}, fakeLatest{pos: anchorPos})
	got, err := g.Track(context.Background(), baseFlight(), now)
	if got != nil || err != nil {
		t.Errorf("implausible fix should be dropped (nil,nil); got %+v %v", got, err)
	}
}

func TestSpeedGateAllowsOnAnchorFetchError(t *testing.T) {
	// If LatestPosition errors (transient DB hiccup), don't block the fix —
	// the worse failure mode would be silently swallowing all updates.
	now := time.Now()
	innerPos := &store.Position{Lat: 51, Lon: -1, Ts: now}
	g := NewSpeedGate(fakeTracker{pos: innerPos}, fakeLatest{err: errors.New("db down")})
	got, _ := g.Track(context.Background(), baseFlight(), now)
	if got != innerPos {
		t.Errorf("anchor fetch error → pass-through; got %+v", got)
	}
}

func TestSpeedGateAllowsZeroOrNegativeDeltaT(t *testing.T) {
	// Fix older than (or equal to) the anchor: can't compute speed sensibly.
	// Pass through — out-of-order fixes are rare and the InsertPosition path
	// will keep the polyline sane via ts ordering anyway.
	now := time.Now()
	innerPos := &store.Position{Lat: 4, Lon: 101, Ts: now.Add(-time.Hour)}
	anchorPos := &store.Position{Lat: 51, Lon: -1, Ts: now}
	g := NewSpeedGate(fakeTracker{pos: innerPos}, fakeLatest{pos: anchorPos})
	got, _ := g.Track(context.Background(), baseFlight(), now)
	if got != innerPos {
		t.Errorf("dt<=0 → pass-through; got %+v", got)
	}
}
