package poller

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

func TestSweep_TableFillsRow(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()

	// Seed a flight whose origin IATA (BRS) is in the embedded airports
	// table but whose dest IATA (ZZZ) is not — origin coords get filled
	// at create time, dest coords stay NULL.
	f, err := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate the "deploy added SID to the airports table" case by
	// switching dest_iata to "SID" and clearing the coords directly
	// in SQL — the public UpdateFlight would re-run lookupCoords and
	// fill them immediately, defeating the test.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flights SET dest_iata = 'SID', dest_lat = NULL, dest_lon = NULL WHERE id = $1`,
		f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Subscribe to SSE so we can assert the sweep publishes an update.
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	got, err := s.FlightByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if got.DestLat == nil || *got.DestLat == 0 {
		t.Fatalf("dest_lat should be table-filled (SID = 16.7414), got %v", got.DestLat)
	}
	if *got.DestLat != 16.7414 {
		t.Errorf("dest_lat = %v, want 16.7414 (SID table value)", *got.DestLat)
	}

	select {
	case <-events:
		// good — SSE published.
	case <-time.After(500 * time.Millisecond):
		t.Errorf("expected SSE flight.updated event after sweep, got none")
	}
}

func TestSweep_NoNullRowsIsNoOp(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()

	// Single flight, both IATAs known → all four coord columns populated
	// at create time. Sweep should find zero candidates.
	if _, err := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "LH400", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "FRA", DestIATA: "JFK",
	}, uid); err != nil {
		t.Fatalf("create: %v", err)
	}

	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	select {
	case e := <-events:
		t.Errorf("no-op sweep should not publish; got %s", e.Type)
	case <-time.After(100 * time.Millisecond):
		// good — no event.
	}
}

func TestSweep_ResolverFillsUnknownIATA(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	// Resolver returns SID coords; sweep should pick them up.
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "EZY2823",
		OriginIATA: "BRS", OriginLat: 51.3827, OriginLon: -2.7191,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ", // dest not in table
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	got, err := s.FlightByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if resolver.calls != 1 {
		t.Errorf("resolver.calls = %d, want 1", resolver.calls)
	}
	if got.DestLat == nil || *got.DestLat != 12.3456 {
		t.Errorf("dest_lat = %v, want 12.3456 (resolver-supplied)", got.DestLat)
	}
	if got.LastResolvedAt == nil {
		t.Errorf("last_resolved_at should be bumped after resolver call")
	}
	select {
	case <-events:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Errorf("expected SSE event")
	}
}

func TestSweep_ResolverNotFoundLeavesNull(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{err: providers.ErrFlightNotFound}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "XX9999", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	p.Sweep(ctx)

	got, _ := s.FlightByID(ctx, f.ID)
	if got.DestLat != nil {
		t.Errorf("dest_lat should remain NULL on resolver-not-found; got %v", got.DestLat)
	}
	if resolver.calls != 1 {
		t.Errorf("resolver.calls = %d, want 1", resolver.calls)
	}
	if got.LastResolvedAt == nil {
		t.Errorf("last_resolved_at should be bumped even on not-found")
	}
}

func TestSweep_ThrottleHoldsRecentRow(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "EZY2823", OriginIATA: "BRS", DestIATA: "ZZZ",
		DestLat: 12.3456, DestLon: -34.5678,
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// Stamp last_resolved_at to "right now" so the throttle blocks the
	// resolver call on the next sweep. RefreshFlightAirframe with empty
	// strings bumps the timestamp without touching airframe columns.
	if err := s.RefreshFlightAirframe(ctx, f.ID, "", ""); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	p.Sweep(ctx)

	got, _ := s.FlightByID(ctx, f.ID)
	if resolver.calls != 0 {
		t.Errorf("resolver should not have been called (throttled); calls = %d", resolver.calls)
	}
	if got.DestLat != nil {
		t.Errorf("dest_lat should remain NULL (resolver throttled); got %v", got.DestLat)
	}
}

func TestSweep_NoResolverConfiguredTableOnly(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.Resolver = nil // explicit
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := s.CreateFlight(ctx, store.CreateFlightPayload{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// Should not panic even with no resolver. Dest remains NULL (table
	// doesn't know ZZZ, resolver path skipped).
	p.Sweep(ctx)

	got, _ := s.FlightByID(ctx, f.ID)
	if got.DestLat != nil {
		t.Errorf("dest_lat should remain NULL with no resolver and unknown IATA; got %v", got.DestLat)
	}
}

