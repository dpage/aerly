package poller

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
)

func TestSweep_TableFillsRow(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()

	// Seed a flight whose origin IATA (BRS) is in the embedded airports
	// table but whose dest IATA (ZZZ) is not — origin coords get filled
	// at create time, dest coords stay NULL.
	f, err := mkPart(ctx, s, partSeed{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate the "deploy added SID to the airports table" case by
	// switching dest_iata (flight_details) to "SID" and clearing the part's
	// end coords directly in SQL. The dest IATA lives on flight_details; the
	// coords live on the plan_part (keyed by f.ID, the plan_part_id).
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET dest_iata = 'SID' WHERE plan_part_id = $1`, f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plan_parts SET end_lat = NULL, end_lon = NULL WHERE id = $1`, f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Subscribe to SSE so we can assert the sweep publishes an update.
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	got, err := s.FlightPartByID(ctx, f.ID)
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
	if _, err := mkPart(ctx, s, partSeed{
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
	f, err := mkPart(ctx, s, partSeed{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ", // dest not in table
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	got, err := s.FlightPartByID(ctx, f.ID)
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
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "XX9999", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
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
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// Stamp last_resolved_at to "right now" so the throttle blocks the
	// resolver call on the next sweep. RefreshFlightAirframe with empty
	// strings bumps the timestamp without touching airframe columns.
	if err := s.RefreshFlightPartAirframe(ctx, f.ID, "", ""); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
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
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// Should not panic even with no resolver. Dest remains NULL (table
	// doesn't know ZZZ, resolver path skipped).
	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.DestLat != nil {
		t.Errorf("dest_lat should remain NULL with no resolver and unknown IATA; got %v", got.DestLat)
	}
}

func TestSweep_MixedBatchPerRowIsolation(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	// Resolver returns coords ONLY for ident "RESOLVE-ME"; everything
	// else gets ErrFlightNotFound. This lets one row depend on the
	// table, one on the resolver, and one on neither.
	resolver := &resolveByIdent{
		match: "RESOLVE-ME",
		rf: &providers.ResolvedFlight{
			Ident: "RESOLVE-ME", OriginIATA: "BRS", OriginLat: 51.3827, OriginLon: -2.7191,
			DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
		},
	}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()

	// (a) Table-fillable: BRS → SID (both in table); seeded with
	// dest_lat NULL via direct SQL to simulate the "deploy added SID"
	// case.
	a, _ := mkPart(ctx, s, partSeed{
		Ident: "TABLE-ME", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "SID",
	}, uid)
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plan_parts SET end_lat = NULL, end_lon = NULL WHERE id = $1`, a.ID); err != nil {
		t.Fatalf("setup a: %v", err)
	}

	// (b) Resolver-fillable: ident matches the fake resolver's match.
	b, _ := mkPart(ctx, s, partSeed{
		Ident: "RESOLVE-ME", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)

	// (c) Unfillable: ident the resolver returns ErrFlightNotFound for,
	// dest IATA not in the table.
	c, _ := mkPart(ctx, s, partSeed{
		Ident: "UNFILL-ME", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "QQQ",
	}, uid)

	_, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.Sweep(ctx)

	gotA, _ := s.FlightPartByID(ctx, a.ID)
	if gotA.DestLat == nil || *gotA.DestLat != 16.7414 {
		t.Errorf("table-fillable row: dest_lat = %v, want 16.7414", gotA.DestLat)
	}
	gotB, _ := s.FlightPartByID(ctx, b.ID)
	if gotB.DestLat == nil || *gotB.DestLat != 12.3456 {
		t.Errorf("resolver-fillable row: dest_lat = %v, want 12.3456", gotB.DestLat)
	}
	gotC, _ := s.FlightPartByID(ctx, c.ID)
	if gotC.DestLat != nil {
		t.Errorf("unfillable row: dest_lat = %v, want nil", gotC.DestLat)
	}
}

func TestSweep_PartiallyUnknownPreservesTableFilledLeg(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	// Resolver returns DELIBERATELY-WRONG origin coords (99.0) — if the
	// merge clobbered the table-derived BRS value (51.3827), we'd see
	// 99.0 in the result. The fix must skip overwriting the leg that
	// the table already satisfied.
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "EZY2823",
		OriginIATA: "BRS", OriginLat: 99.0, OriginLon: 99.0,
		DestIATA: "ZZZ", DestLat: 12.3456, DestLon: -34.5678,
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	// Seed with origin=BRS (in table) and dest=ZZZ (not in table). The
	// create-time helper fills origin coords, dest stays NULL.
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "EZY2823", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)
	// Wipe origin coords too so the sweep's table pass has to refill
	// them — this exercises the "table fills one leg, resolver fills
	// the other" code path.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plan_parts SET start_lat = NULL, start_lon = NULL WHERE id = $1`, f.ID); err != nil {
		t.Fatalf("setup: %v", err)
	}

	p.Sweep(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.OriginLat == nil {
		t.Fatalf("origin_lat should be table-filled (51.3827), got nil")
	}
	if *got.OriginLat != 51.3827 {
		t.Errorf("origin_lat = %v, want 51.3827 (BRS table value, NOT resolver's 99.0)", *got.OriginLat)
	}
	if got.DestLat == nil || *got.DestLat != 12.3456 {
		t.Errorf("dest_lat = %v, want 12.3456 (resolver-supplied)", got.DestLat)
	}
}

// A far-future unconfirmed flight (on-table, so the coord pass ignores it) is
// resolved-and-confirmed by the provisional sweep pass: resolved flips true and
// the provisional schedule is corrected.
func TestSweep_ProvisionalConfirmsFarFuture(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	now := time.Now()
	realOut, realIn := now.Add(60*24*time.Hour+1*time.Hour), now.Add(60*24*time.Hour+4*time.Hour)
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "TK1986", OriginIATA: "IST", DestIATA: "LHR",
		ScheduledOut: realOut, ScheduledIn: realIn, ICAO24: "4baa01",
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	// 60 days out, both airports on-table (coords filled → coord pass skips it).
	f, err := mkPart(ctx, s, partSeed{
		Ident: "TK1986", ScheduledOut: now.Add(60 * 24 * time.Hour),
		ScheduledIn: now.Add(60*24*time.Hour + 3*time.Hour),
		OriginIATA:  "IST", DestIATA: "LHR",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	p.Sweep(ctx)

	if resolver.calls != 1 {
		t.Fatalf("resolver.calls = %d, want 1 (provisional pass only)", resolver.calls)
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if !got.Resolved {
		t.Errorf("far-future provisional flight should be confirmed by the sweep")
	}
	if d := got.ScheduledOut.Sub(realOut); d > time.Second || d < -time.Second {
		t.Errorf("schedule not corrected: out=%v want≈%v", got.ScheduledOut, realOut)
	}
}

// Inside 12h the metadata pass owns re-resolution; the provisional sweep pass
// must defer (no resolver call, flight stays unconfirmed via the sweep).
func TestSweep_ProvisionalDefersInside12h(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{Ident: "TK1986", OriginIATA: "IST", DestIATA: "LHR"}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	// 6h out, on-table (coords filled so the coord pass also skips it).
	f, err := mkPart(ctx, s, partSeed{
		Ident: "TK1986", ScheduledOut: now.Add(6 * time.Hour), ScheduledIn: now.Add(9 * time.Hour),
		OriginIATA: "IST", DestIATA: "LHR",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	p.Sweep(ctx)

	if resolver.calls != 0 {
		t.Errorf("resolver.calls = %d, want 0 (inside 12h is the metadata pass's job)", resolver.calls)
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.Resolved {
		t.Errorf("flight inside 12h must not be confirmed by the sweep pass")
	}
}

// Throttle: a far-future flight resolved on one sweep is not re-resolved on the
// next (weekly cadence), so quota isn't burned.
func TestSweep_ProvisionalThrottlesFarFuture(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	now := time.Now()
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "TK1986", OriginIATA: "IST", DestIATA: "LHR",
		ScheduledOut: now.Add(60 * 24 * time.Hour), ScheduledIn: now.Add(60*24*time.Hour + 3*time.Hour),
	}}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	if _, err := mkPart(ctx, s, partSeed{
		Ident: "TK1986", ScheduledOut: now.Add(60 * 24 * time.Hour), ScheduledIn: now.Add(60*24*time.Hour + 3*time.Hour),
		OriginIATA: "IST", DestIATA: "LHR",
	}, uid); err != nil {
		t.Fatalf("create: %v", err)
	}

	p.Sweep(ctx) // resolves + confirms, bumps last_resolved_at
	p.Sweep(ctx) // second sweep: now resolved=true so excluded; the throttle would also hold it

	if resolver.calls != 1 {
		t.Errorf("resolver.calls = %d, want 1 (second sweep throttled/confirmed)", resolver.calls)
	}
}

// A flight the resolver can't find yet stays unconfirmed; the next sweep must
// skip it via the time-to-departure throttle (it was just attempted) rather
// than re-resolving and burning quota every 4h.
func TestSweep_ProvisionalThrottleSkipsAfterMiss(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{err: providers.ErrFlightNotFound}
	p.Resolver = resolver
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	// 60 days out, on-table (coords filled → coord pass skips it); resolver miss.
	f, err := mkPart(ctx, s, partSeed{
		Ident: "TK1986", ScheduledOut: now.Add(60 * 24 * time.Hour),
		ScheduledIn: now.Add(60*24*time.Hour + 3*time.Hour),
		OriginIATA:  "IST", DestIATA: "LHR",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	p.Sweep(ctx) // miss: stays resolved=false, bumps last_resolved_at
	p.Sweep(ctx) // throttled by the weekly interval (just attempted) → no re-resolve

	if resolver.calls != 1 {
		t.Errorf("resolver.calls = %d, want 1 (second sweep throttled by the weekly interval)", resolver.calls)
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.Resolved {
		t.Errorf("a missed resolve must leave the flight unconfirmed")
	}
}

// TestSweep_MissingCoordsListErrorReturns covers the FlightPartsWithMissingCoords
// error branch in Sweep: a cancelled context fails the candidate query, so Sweep
// logs and returns early (still running the deferred provisional pass, which
// also short-circuits on the cancelled context).
func TestSweep_MissingCoordsListErrorReturns(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{Ident: "X"}}
	p.Resolver = resolver
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p.Sweep(ctx) // must not panic; both passes bail on the cancelled context

	if resolver.calls != 0 {
		t.Errorf("a cancelled sweep should not call the resolver, got %d", resolver.calls)
	}
	_ = s
}

// cancelOnResolve is a Resolver double that cancels a context the first time it
// is asked to resolve, then returns not-found. It lets the sweep loops' per-row
// ctx.Err() guards be exercised deterministically: the first row triggers the
// cancel, and the loop bails before the second.
type cancelOnResolve struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancelOnResolve) Resolve(_ context.Context, _ string, _ time.Time) (*providers.ResolvedFlight, error) {
	c.calls++
	c.cancel()
	return nil, providers.ErrFlightNotFound
}

// TestSweep_ContextCancelledMidCoordLoop covers the per-row ctx.Err() guard in
// Sweep's coord loop (52-54): the resolver cancels the context whilst the first
// candidate is processed, so the loop bails before the second.
func TestSweep_ContextCancelledMidCoordLoop(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	// Two NULL-coord candidates (dest ZZZ/QQQ not in the table), so the coord
	// loop runs both unless cancelled mid-way.
	for _, id := range []string{"EZYA", "EZYB"} {
		dest := "ZZZ"
		if id == "EZYB" {
			dest = "QQQ"
		}
		if _, err := mkPart(context.Background(), s, partSeed{
			Ident: id, ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
			OriginIATA: "BRS", DestIATA: dest,
		}, uid); err != nil {
			t.Fatalf("mkPart %s: %v", id, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cr := &cancelOnResolve{cancel: cancel}
	p.Resolver = cr

	p.Sweep(ctx)

	if cr.calls != 1 {
		t.Errorf("coord loop should stop after the first row cancels, resolver calls = %d", cr.calls)
	}
}

// TestSweepProvisional_ContextCancelledMidLoop covers the per-row ctx.Err() guard
// in sweepProvisional's loop (108-110): the resolver cancels the context on the
// first provisional row, so the loop bails before the second.
func TestSweepProvisional_ContextCancelledMidLoop(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	// Two far-future on-table provisional flights (coords filled → the coord
	// pass skips them, the provisional pass owns them).
	for _, id := range []string{"TKAA", "TKBB"} {
		if _, err := mkPart(context.Background(), s, partSeed{
			Ident: id, ScheduledOut: now.Add(60 * 24 * time.Hour),
			ScheduledIn: now.Add(60*24*time.Hour + 3*time.Hour),
			OriginIATA:  "IST", DestIATA: "LHR",
		}, uid); err != nil {
			t.Fatalf("mkPart %s: %v", id, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cr := &cancelOnResolve{cancel: cancel}
	p.Resolver = cr

	// Call sweepProvisional directly with a non-cancelled context so the early
	// guard passes; the resolver cancels mid-loop.
	p.sweepProvisional(ctx, now)

	if cr.calls != 1 {
		t.Errorf("provisional loop should stop after the first row cancels, resolver calls = %d", cr.calls)
	}
}

// TestSweepOne_FillErrorIsIsolated covers the flightcoord.Fill error branch in
// sweepOne: a cancelled context makes the fill error, which is logged and
// isolated (no publish, no panic) so one bad row never unwinds the sweep.
func TestSweepOne_FillErrorIsIsolated(t *testing.T) {
	p, s, hub := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "EZY2", OriginIATA: "BRS", DestIATA: "ZZZ", DestLat: 1, DestLon: 2,
	}}
	p.Resolver = resolver
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(context.Background(), s, partSeed{
		Ident: "EZY2", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "BRS", DestIATA: "ZZZ",
	}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // flightcoord.Fill errors on the cancelled context

	p.sweepOne(ctx, f, now) // must not panic, must not publish

	select {
	case ev := <-events:
		t.Errorf("a failed fill must not publish, got %s", ev.Type)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestSweepProvisional_ListErrorReturns covers the ProvisionalFlightParts error
// branch: a context that is NOT yet cancelled when sweepProvisional's guard runs
// but fails the query. We use a context cancelled after the resolver-presence
// check by wrapping with a deadline already elapsed — simplest is a cancelled
// context, which short-circuits at the ctx.Err() guard, so instead we close the
// query path by cancelling just before the query. A cancelled context trips the
// early guard, so to reach the query error we keep the context live but point
// the pool at a cancelled child only for the query — not feasible without a
// seam. Cover the early-guard path here (resolver set, context cancelled).
func TestSweepProvisional_CancelledContextBails(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	resolver := &fakeResolver{rf: &providers.ResolvedFlight{Ident: "X"}}
	p.Resolver = resolver
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p.sweepProvisional(ctx, time.Now()) // ctx.Err() != nil → quiet bail

	if resolver.calls != 0 {
		t.Errorf("a cancelled provisional pass should not resolve, got %d", resolver.calls)
	}
	_ = s
}

// resolveByIdent is a Resolver double that only returns success for one
// specific ident. Used by the mixed-batch test.
type resolveByIdent struct {
	match string
	rf    *providers.ResolvedFlight
	calls int
}

func (r *resolveByIdent) Resolve(_ context.Context, ident string, _ time.Time) (*providers.ResolvedFlight, error) {
	r.calls++
	if ident == r.match {
		c := *r.rf
		return &c, nil
	}
	return nil, providers.ErrFlightNotFound
}
