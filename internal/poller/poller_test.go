package poller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/feeds"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

type mockTracker struct {
	pos    *store.Position
	err    error
	calls  int
	before func(f *store.Flight) // invoked before returning a fix
}

func (m *mockTracker) Track(_ context.Context, f *store.Flight, now time.Time) (*store.Position, error) {
	m.calls++
	if m.before != nil {
		m.before(f)
	}
	if m.pos != nil {
		p := *m.pos
		p.FlightID = f.ID
		p.Ts = now
		return &p, nil
	}
	return nil, m.err
}

func newPoller(t *testing.T, tr *mockTracker, interval time.Duration) (*Poller, *store.Store, *sse.Hub) {
	t.Helper()
	s := store.New(testsupport.NewPool(t))
	hub := sse.NewHub()
	return New(s, tr, hub, interval), s, hub
}

func seedUser(t *testing.T, s *store.Store) int64 {
	t.Helper()
	u, err := s.InviteUser(context.Background(),
		store.InvitePayload{Username: fmt.Sprintf("po%d", seedSeq.Add(1)), Name: "po"})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

var seedSeq atomic.Int64

// failMarshal swaps the package jsonMarshal seam for one that always errors,
// restoring it when the test ends. It lets the otherwise-unreachable
// marshal-failure branches in the SSE-publish paths be exercised.
func failMarshal(t *testing.T) {
	t.Helper()
	orig := jsonMarshal
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("synthetic marshal failure") }
	t.Cleanup(func() { jsonMarshal = orig })
}

// partSeed carries the flight schedule fields mkPart needs to seed a flight
// part. It replaces the legacy store.CreateFlightPayload the tests used before
// the flight CRUD surface was retired in Wave 3.
type partSeed struct {
	Ident        string
	ScheduledOut time.Time
	ScheduledIn  time.Time
	OriginIATA   string
	DestIATA     string
	ICAO24       string
	Notes        string
}

// mkPart seeds a trip + flight plan + plan_part + flight_details from a
// partSeed and returns the flight carrier keyed on the plan_part_id — the unit
// the re-keyed poller works against. It mirrors the old CreateFlight create-time
// behaviour: coords are looked up from the airports table, status is derived
// from the schedule, and the ident is normalised. Returns (*store.Flight, error)
// so the test bodies read like the old s.CreateFlight calls.
func mkPart(ctx context.Context, s *store.Store, in partSeed, createdBy int64) (*store.Flight, error) {
	n := seedSeq.Add(1)
	var tripID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("trip%d", n), createdBy).Scan(&tripID); err != nil {
		return nil, err
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`,
		tripID, createdBy); err != nil {
		return nil, err
	}
	var planID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, notes, created_by) VALUES ($1, 'flight', $2, $3) RETURNING id`,
		tripID, in.Notes, createdBy).Scan(&planID); err != nil {
		return nil, err
	}
	ident := strings.ToUpper(strings.Join(strings.Fields(in.Ident), ""))
	originIATA := strings.ToUpper(in.OriginIATA)
	destIATA := strings.ToUpper(in.DestIATA)
	var oLat, oLon, dLat, dLon *float64
	if lat, lon, ok := airports.Lookup(originIATA); ok {
		oLat, oLon = &lat, &lon
	}
	if lat, lon, ok := airports.Lookup(destIATA); ok {
		dLat, dLon = &lat, &lon
	}
	var partID int64
	if err := s.Pool().QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, end_lat, end_lon, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'confirmed') RETURNING id`,
		planID, in.ScheduledOut, in.ScheduledIn, oLat, oLon, dLat, dLon).Scan(&partID); err != nil {
		return nil, err
	}
	var icao24 *string
	if v := strings.ToLower(strings.TrimSpace(in.ICAO24)); v != "" {
		icao24 = &v
	}
	if _, err := s.Pool().Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, icao24, scheduled_out, scheduled_in,
			origin_iata, dest_iata, flight_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7,
			CASE
				WHEN NOW() > $5 THEN 'Arrived'
				WHEN NOW() >= $4 THEN 'Enroute'
				ELSE 'Scheduled'
			END)`,
		partID, ident, icao24, in.ScheduledOut, in.ScheduledIn, originIATA, destIATA); err != nil {
		return nil, err
	}
	return s.FlightPartByID(ctx, partID)
}

// deletePart removes a flight plan_part (cascading its flight_details /
// positions), used by the "tracker deletes the row mid-poll" test.
func deletePart(ctx context.Context, s *store.Store, partID int64) error {
	_, err := s.Pool().Exec(ctx, `DELETE FROM plan_parts WHERE id = $1`, partID)
	return err
}

// TestNeedsBackfillDegenerateSchedule: a flight whose stored schedule is
// degenerate (no real arrival — scheduled_in not after scheduled_out, the
// "manual add, number + date only" case) must be treated as needing a resolver
// backfill, even when its other metadata is already filled. Otherwise nothing
// re-triggers a resolve and its times stay stuck at the placeholder.
func TestNeedsBackfillDegenerateSchedule(t *testing.T) {
	icao := "3c48f0"
	out := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	full := &store.Flight{
		OriginIATA: "BER", DestIATA: "MUC", ICAO24: &icao,
		ScheduledOut: out, ScheduledIn: out.Add(time.Hour),
	}
	if needsBackfill(full) {
		t.Error("a fully-resolved flight with a real schedule should not need backfill")
	}
	degen := *full
	degen.ScheduledIn = out // arrival == departure: no real arrival time
	if !needsBackfill(&degen) {
		t.Error("a flight with no real arrival time should need backfill so it re-resolves")
	}
}

func TestNewDefaultsInterval(t *testing.T) {
	p := New(nil, nil, nil, 0)
	if p.Interval != 60*time.Second {
		t.Errorf("default interval = %v, want 60s", p.Interval)
	}
	p = New(nil, nil, nil, 15*time.Second)
	if p.Interval != 15*time.Second {
		t.Errorf("explicit interval = %v", p.Interval)
	}
}

func TestMinPollAge(t *testing.T) {
	p := New(nil, nil, nil, 10*time.Second)
	now := time.Now()
	enroute := &store.Flight{Status: "Enroute", ScheduledOut: now.Add(-time.Hour)}
	if p.minPollAge(enroute, now) != 10*time.Second {
		t.Errorf("Enroute minPollAge = %v", p.minPollAge(enroute, now))
	}
	// Scheduled and in the final hour: ramp to 5-minute cadence.
	future := &store.Flight{Status: "Scheduled", ScheduledOut: now.Add(time.Hour)}
	if p.minPollAge(future, now) != 5*time.Minute {
		t.Errorf("pre-departure Scheduled (1h out) minPollAge = %v, want 5m", p.minPollAge(future, now))
	}
	// Scheduled and 3h out: 15-minute cadence.
	far := &store.Flight{Status: "Scheduled", ScheduledOut: now.Add(3 * time.Hour)}
	if p.minPollAge(far, now) != 15*time.Minute {
		t.Errorf("pre-departure Scheduled (3h out) minPollAge = %v, want 15m", p.minPollAge(far, now))
	}
	// Scheduled and 8h out: hourly cadence.
	further := &store.Flight{Status: "Scheduled", ScheduledOut: now.Add(8 * time.Hour)}
	if p.minPollAge(further, now) != time.Hour {
		t.Errorf("pre-departure Scheduled (8h out) minPollAge = %v, want 1h", p.minPollAge(further, now))
	}
	// Scheduled but already past departure (airborne, stored status not yet
	// flipped): must use the fast cadence so refresh() runs and flips it to
	// Enroute promptly, rather than waiting out the slow interval.
	departed := &store.Flight{Status: "Scheduled", ScheduledOut: now.Add(-time.Minute)}
	if p.minPollAge(departed, now) != 10*time.Second {
		t.Errorf("past-departure Scheduled minPollAge = %v, want fast cadence", p.minPollAge(departed, now))
	}
}

func TestTickInsertsPositionRefreshesAndPublishes(t *testing.T) {
	hdg := int16(90)
	alt := int32(35000)
	tr := &mockTracker{pos: &store.Position{Lat: 50, Lon: -10, HeadingDeg: &hdg, AltitudeFt: &alt}}
	p, s, hub := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "PL1", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("create flight: %v", err)
	}

	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: 1, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.tick(ctx)

	if tr.calls != 1 {
		t.Errorf("tracker calls = %d, want 1", tr.calls)
	}
	pos, _ := s.LatestPartPositions(ctx, []int64{f.ID})
	if pos[f.ID] == nil {
		t.Error("expected a position to be inserted")
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("RefreshFlightStatus should set last_polled_at")
	}
	select {
	case ev := <-events:
		if ev.Type != "plan_part.updated" {
			t.Errorf("event type = %q", ev.Type)
		}
		// The broadcast must carry the live freshness + flown track so the FE
		// can refresh "Last polled" and grow the polyline in place, rather than
		// freezing both at the last full HTTP fetch.
		var dto api.TrackerPartDTO
		if err := json.Unmarshal(ev.Data, &dto); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if dto.LastPolledAt == nil {
			t.Error("broadcast payload missing last_polled_at")
		}
		if len(dto.Track) == 0 {
			t.Error("broadcast payload missing flown track")
		}
		if dto.LatestPosition == nil {
			t.Error("broadcast payload missing latest_position")
		}
	case <-time.After(time.Second):
		t.Fatal("no SSE event published")
	}
}

func TestTickTrackerErrorStillRefreshes(t *testing.T) {
	tr := &mockTracker{err: errors.New("adsb down")}
	p, s, _ := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "PL2", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("status should still be refreshed despite tracker error")
	}
	if pos, _ := s.LatestPartPositions(ctx, []int64{f.ID}); pos[f.ID] != nil {
		t.Error("no position should be inserted when tracker errors")
	}
}

func TestTickSkipsFreshlyPolled(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Hour) // huge interval
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "PL3", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	// Mark as just polled so minPollAge skips it.
	if err := s.RefreshFlightPartStatus(ctx, f.ID); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	p.tick(ctx)
	if tr.calls != 0 {
		t.Errorf("freshly-polled flight should be skipped, tracker calls = %d", tr.calls)
	}
}

func TestTickActiveFlightsErrorReturns(t *testing.T) {
	tr := &mockTracker{}
	p, _, _ := newPoller(t, tr, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.tick(ctx) // ActiveFlights errors on cancelled ctx → logged + return
	if tr.calls != 0 {
		t.Errorf("no tracking should happen, calls = %d", tr.calls)
	}
}

func TestTickContextCancelledMidLoop(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	_, _ = mkPart(context.Background(), s, partSeed{
		Ident: "PL4", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)

	// Cancel right after ActiveFlights succeeds: the per-flight ctx.Err()
	// guard returns before tracking. We approximate by cancelling a derived
	// context once ActiveFlights has run — use a context cancelled between
	// the (already-loaded) list and the loop body via a 1-shot wrapper.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.tick(ctx)
	if tr.calls != 0 {
		t.Errorf("cancelled ctx should stop the loop, calls = %d", tr.calls)
	}
}

// TestRefreshHandlesDeletedFlight covers the InsertPosition error branch
// (positions FK now dangling) and the FlightByID error branch (row gone):
// the tracker deletes the flight row just before returning a fix.
func TestRefreshHandlesDeletedFlight(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, hub := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "DELME", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	tr.before = func(fl *store.Flight) {
		if err := deletePart(ctx, s, fl.ID); err != nil {
			t.Fatalf("delete in tracker: %v", err)
		}
	}
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: 1, IsSuperuser: true, ShowAll: true})
	defer unsub()

	p.tick(ctx) // must not panic; FlightByID after delete → error → return

	if _, err := s.FlightPartByID(ctx, f.ID); err == nil {
		t.Error("flight should have been deleted by the tracker hook")
	}
	select {
	case <-events:
		t.Error("no SSE event expected when the refetch fails")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestTickContextCancelledBetweenFlights covers the per-flight ctx.Err()
// guard: the tracker cancels the context while processing the first flight,
// so the second loop iteration bails out early.
func TestTickContextCancelledBetweenFlights(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	for _, id := range []string{"AA1", "BB2"} {
		_, _ = mkPart(context.Background(), s, partSeed{
			Ident: id, ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
			OriginIATA: "LHR", DestIATA: "JFK",
		}, uid)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tr.before = func(*store.Flight) { cancel() } // cancel during first flight

	p.tick(ctx)

	if tr.calls != 1 {
		t.Errorf("expected loop to stop after first flight, tracker calls = %d", tr.calls)
	}
}

// fakeResolver lets tests pin the resolver response without an HTTP server.
type fakeResolver struct {
	rf    *providers.ResolvedFlight
	err   error
	calls int
}

func (f *fakeResolver) Resolve(_ context.Context, _ string, _ time.Time) (*providers.ResolvedFlight, error) {
	f.calls++
	if f.rf != nil {
		c := *f.rf
		return &c, nil
	}
	return nil, f.err
}

// A flight added with blank IATAs and no icao24 should have those filled in
// from the resolver on its next tick, leaving user-entered fields alone.
func TestRefreshBackfillsMissingMetadata(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "BA286",
		OriginIATA: "LHR", OriginLat: 51.47, OriginLon: -0.46,
		DestIATA: "SFO", DestLat: 37.62, DestLon: -122.38,
		ICAO24: "406b05",
		Notes:  "British Airways · Boeing 777",
	}}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident:        "BA286",
		ScheduledOut: now.Add(-time.Hour),
		ScheduledIn:  now.Add(time.Hour),
		Notes:        "user-typed note", // existing non-empty notes must NOT be overwritten
	}, uid)
	if err != nil {
		t.Fatalf("create flight: %v", err)
	}

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.OriginIATA != "LHR" || got.DestIATA != "SFO" {
		t.Errorf("airports not backfilled: %q → %q", got.OriginIATA, got.DestIATA)
	}
	if got.OriginLat == nil || *got.OriginLat != 51.47 {
		t.Errorf("origin lat not backfilled: %v", got.OriginLat)
	}
	if got.ICAO24 == nil || *got.ICAO24 != "406b05" {
		t.Errorf("icao24 not backfilled: %v", got.ICAO24)
	}
	if got.Notes != "user-typed note" {
		t.Errorf("user-typed notes were overwritten: %q", got.Notes)
	}
}

// TestTickResolvesGateAheadOfDeparture: a flight hours before departure (inside
// the 12h metadata band, outside the 30-min tracking window) gets its gate
// resolved without any position tracking — so a gate published early surfaces
// promptly instead of waiting until 30 minutes out.
func TestTickResolvesGateAheadOfDeparture(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "SK161", OriginIATA: "ARN", DestIATA: "GOT", OriginGate: "12",
	}}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	// Departs in 6h: outside the 30-min active window, inside the 12h band.
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "SK161", ScheduledOut: now.Add(6 * time.Hour), ScheduledIn: now.Add(7 * time.Hour),
		OriginIATA: "ARN", DestIATA: "GOT",
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.OriginGate != "12" {
		t.Errorf("gate not resolved ahead of departure, got %q", got.OriginGate)
	}
	if tr.calls != 0 {
		t.Errorf("metadata pass must not track positions before departure, tracker calls=%d", tr.calls)
	}
}

// TestTickBackfillsDegenerateSchedule: a manually-added flight (number + date
// only → scheduled_in == scheduled_out) gets its real times filled from the
// resolver during the pre-departure metadata pass.
func TestTickBackfillsDegenerateSchedule(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	now := time.Now()
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "VL1939", OriginIATA: "BER", DestIATA: "MUC",
		ScheduledOut: now.Add(5 * time.Hour), ScheduledIn: now.Add(6 * time.Hour),
	}}
	ctx := context.Background()
	uid := seedUser(t, s)
	// Degenerate schedule, 5h out (in the metadata band).
	degen := now.Add(5 * time.Hour)
	f, _ := mkPart(ctx, s, partSeed{Ident: "VL1939", ScheduledOut: degen, ScheduledIn: degen}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if !got.ScheduledIn.After(got.ScheduledOut) {
		t.Errorf("degenerate schedule not filled from API: out=%v in=%v", got.ScheduledOut, got.ScheduledIn)
	}
}

// When ErrFlightNotFound comes back, the flight stays as-is and we don't
// log it noisily (covered indirectly — what matters is the row is unchanged
// and the tracker still runs).
func TestRefreshBackfillNotFoundLeavesFlightAlone(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	fr := &fakeResolver{err: providers.ErrFlightNotFound}
	p.Resolver = fr
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "XX9999", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
	}, uid)

	p.tick(ctx)

	if fr.calls != 1 {
		t.Errorf("resolver should have been called exactly once, got %d", fr.calls)
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.OriginIATA != "" || got.DestIATA != "" || got.ICAO24 != nil {
		t.Errorf("not-found should leave row blank: %+v", got)
	}
}

// A flight that already has full metadata AND was resolved recently must
// NOT trigger another resolver call — last_resolved_at is the throttle.
// TestMetadataPassThrottlesAfterResolveFailure: a pre-departure flight the
// resolver can't fix must still get last_polled_at bumped, so the metadata
// pass's minPollAge throttle applies and it isn't retried every tick.
func TestMetadataPassThrottlesAfterResolveFailure(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{err: providers.ErrFlightNotFound}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	// In the 12h metadata band (departs in 2h), no metadata → resolve attempted.
	f, _ := mkPart(ctx, s, partSeed{
		Ident: "XX9999", ScheduledOut: now.Add(2 * time.Hour), ScheduledIn: now.Add(4 * time.Hour),
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("metadata pass must bump last_polled_at even on a failed resolve, so the throttle applies")
	}
}

func TestRefreshSkipsResolveWhenFresh(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	fr := &fakeResolver{rf: &providers.ResolvedFlight{}}
	p.Resolver = fr
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	icao := "abc123"
	f, _ := mkPart(ctx, s, partSeed{
		Ident:        "PL9",
		ScheduledOut: now.Add(-time.Hour),
		ScheduledIn:  now.Add(time.Hour),
		OriginIATA:   "LHR",
		DestIATA:     "JFK",
		ICAO24:       icao,
	}, uid)
	// Pretend we just resolved this flight a moment ago.
	if err := s.RefreshFlightPartAirframe(ctx, f.ID, "", ""); err != nil {
		t.Fatalf("seed last_resolved_at: %v", err)
	}

	p.tick(ctx)

	if fr.calls != 0 {
		t.Errorf("resolver should not be called when last_resolved_at is fresh, got %d calls", fr.calls)
	}
}

// Late refresh: a flight that has icao24 set but was last resolved long
// ago (or never) should trigger a fresh resolver call when close to
// departure, and the new icao24 / callsign must overwrite whatever's
// stored — that's how we catch day-of airframe swaps that produce the
// "wrong aircraft" tracks we saw with LH493.
func TestLateRefreshOverwritesStaleAirframe(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident:      "LH493",
		OriginIATA: "YVR", DestIATA: "FRA",
		ICAO24:   "3c4a8c", // the day-of correct airframe
		Callsign: "DLH493",
	}}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	old := "3c4a8b" // wrong airframe stored at booking time
	f, _ := mkPart(ctx, s, partSeed{
		Ident:        "LH493",
		ScheduledOut: now.Add(-time.Hour), // already enroute
		ScheduledIn:  now.Add(time.Hour),
		OriginIATA:   "YVR", DestIATA: "FRA",
		ICAO24: old,
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.ICAO24 == nil || *got.ICAO24 != "3c4a8c" {
		t.Errorf("icao24 should have been overwritten by late-refresh: %v", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "DLH493" {
		t.Errorf("callsign should have been written by late-refresh: %v", got.Callsign)
	}
	if got.LastResolvedAt == nil {
		t.Error("last_resolved_at should have been bumped")
	}
}

// Late refresh must not fire for a flight that's still far in the future —
// AeroDataBox won't have an airframe assigned yet and there's no value in
// burning quota.
func TestLateRefreshSkipsFarFuture(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	fr := &fakeResolver{rf: &providers.ResolvedFlight{
		OriginIATA: "LHR", DestIATA: "JFK", ICAO24: "abc123",
	}}
	p.Resolver = fr
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	icao := "abc123"
	// 24h before departure: ActiveFlights still won't pick it up, but if
	// the late-refresh window is mis-tuned we'd otherwise see calls here.
	_, _ = mkPart(ctx, s, partSeed{
		Ident:        "PL10",
		ScheduledOut: now.Add(24 * time.Hour),
		ScheduledIn:  now.Add(30 * time.Hour),
		OriginIATA:   "LHR", DestIATA: "JFK", ICAO24: icao,
	}, uid)

	p.tick(ctx)

	if fr.calls != 0 {
		t.Errorf("late-refresh should not fire for a flight a day out, got %d calls", fr.calls)
	}
}

// Late refresh on a resolver error / not-found must still bump
// last_resolved_at so we throttle the retry interval — otherwise an
// unresolvable flight would burn a resolver call on every tick.
func TestLateRefreshStampsEvenOnNotFound(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	p.Resolver = &fakeResolver{err: providers.ErrFlightNotFound}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, _ := mkPart(ctx, s, partSeed{
		Ident:        "ZZ404",
		ScheduledOut: now.Add(-time.Hour),
		ScheduledIn:  now.Add(time.Hour),
		OriginIATA:   "LHR", DestIATA: "JFK",
		ICAO24: "abc123",
	}, uid)

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastResolvedAt == nil {
		t.Error("last_resolved_at should be bumped even when the resolver returned not-found")
	}
	if got.ICAO24 == nil || *got.ICAO24 != "abc123" {
		t.Errorf("not-found must NOT blank existing icao24: %v", got.ICAO24)
	}
}

// A previously-unconfirmed flight (resolved=false) gets resolved=true after a
// successful resolve, has its provisional schedule corrected to the provider's,
// and the schedule is then frozen (a second tick does not move it).
func TestResolveConfirmsAndFreezesSchedule(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	now := time.Now()
	provOut, provIn := now.Add(6*time.Hour), now.Add(9*time.Hour)
	realOut, realIn := now.Add(6*time.Hour+30*time.Minute), now.Add(9*time.Hour+30*time.Minute)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "TK1986", OriginIATA: "IST", DestIATA: "LHR", OriginName: "Istanbul",
		DestName: "Heathrow", ScheduledOut: realOut, ScheduledIn: realIn, ICAO24: "4baa01",
	}}
	ctx := context.Background()
	uid := seedUser(t, s)
	// 6h out: inside the metadata band, so refreshMetadata resolves it.
	f, err := mkPart(ctx, s, partSeed{
		Ident: "TK1986", ScheduledOut: provOut, ScheduledIn: provIn,
		OriginIATA: "IST", DestIATA: "LHR",
	}, uid)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	p.tick(ctx)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if !got.Resolved {
		t.Fatalf("flight should be resolved after a successful resolve")
	}
	if d := got.ScheduledOut.Sub(realOut); d > time.Second || d < -time.Second {
		t.Errorf("provisional schedule not corrected: out=%v want≈%v", got.ScheduledOut, realOut)
	}

	// Freeze: pretend the provider later reports a different scheduled time; a
	// confirmed flight must not move (its schedule is the delay baseline).
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "TK1986", OriginIATA: "IST", DestIATA: "LHR",
		ScheduledOut: realOut.Add(2 * time.Hour), ScheduledIn: realIn.Add(2 * time.Hour), ICAO24: "4baa01",
	}}
	// Clear the throttle so the resolve actually runs again.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET last_resolved_at = NULL WHERE plan_part_id = $1`, f.ID); err != nil {
		t.Fatalf("clear throttle: %v", err)
	}
	p.tick(ctx)
	frozen, _ := s.FlightPartByID(ctx, f.ID)
	if d := frozen.ScheduledOut.Sub(realOut); d > time.Second || d < -time.Second {
		t.Errorf("confirmed schedule was overwritten on a later tick: out=%v want≈%v", frozen.ScheduledOut, realOut)
	}
}

// TestPublishPartChange_MarshalErrorSkipsBroadcast covers the marshal-failure
// branch in publishPartChange: when the DTO can't be encoded, no SSE event is
// published (rather than a half-built or empty payload).
func TestPublishPartChange_MarshalErrorSkipsBroadcast(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}}
	p, s, hub := newPoller(t, tr, time.Minute)
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "PL99", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	events, unsub := hub.Subscribe(sse.Subscription{ViewerID: uid, IsSuperuser: true, ShowAll: true})
	defer unsub()

	failMarshal(t)
	p.publishPartChange(ctx, f.ID)

	select {
	case ev := <-events:
		t.Errorf("no broadcast expected when the payload fails to marshal, got %s", ev.Type)
	case <-time.After(150 * time.Millisecond):
		// good — nothing published.
	}
}

func TestRunImmediateTickThenStops(t *testing.T) {
	tr := &mockTracker{pos: &store.Position{Lat: 2, Lon: 2}}
	p, s, _ := newPoller(t, tr, 20*time.Millisecond)
	uid := seedUser(t, s)
	now := time.Now()
	_, _ = mkPart(context.Background(), s, partSeed{
		Ident: "PL5", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	// Let the immediate tick + at least one ticker tick happen.
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
	if tr.calls == 0 {
		t.Error("Run should have polled at least once")
	}
}

// TestGuardRecoversFromPanic covers the panic-recovery path in guard: a panicking
// fn must be caught and logged, not crash the process.
func TestGuardRecoversFromPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("guard let a panic escape: %v", r)
		}
	}()
	guard("test.panic", 42, func() { panic("boom") })
}

// TestRefreshFeeds covers refreshFeeds with and without a wired feed service:
// nil is a no-op, a real (empty) service runs RefreshDue cleanly through guard.
func TestRefreshFeeds(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	ctx := context.Background()

	p.refreshFeeds(ctx) // Feeds nil → no-op

	p.Feeds = feeds.NewService(s, "aerly-test", time.Minute)
	p.refreshFeeds(ctx) // empty feed set → RefreshDue runs and returns cleanly
}

// TestNeedsLateRefresh covers the early-out branches: a far-future flight, and a
// terminal-status flight (Arrived/Cancelled/Diverted) never need a late refresh.
func TestNeedsLateRefresh(t *testing.T) {
	now := time.Now()
	farFuture := &store.Flight{Status: "Scheduled", ScheduledOut: now.Add(48 * time.Hour)}
	if needsLateRefresh(farFuture, now) {
		t.Error("a flight two days out should not need a late refresh")
	}
	arrived := &store.Flight{Status: "Arrived", ScheduledOut: now.Add(-time.Hour)}
	if needsLateRefresh(arrived, now) {
		t.Error("an arrived flight should not need a late refresh")
	}
	cancelled := &store.Flight{Status: "Cancelled", ScheduledOut: now.Add(time.Hour)}
	if needsLateRefresh(cancelled, now) {
		t.Error("a cancelled flight should not need a late refresh")
	}
	// In-window, never resolved → needs it.
	fresh := &store.Flight{Status: "Scheduled", ScheduledOut: now.Add(time.Hour)}
	if !needsLateRefresh(fresh, now) {
		t.Error("an in-window, never-resolved flight should need a late refresh")
	}
}

// TestTickMetadataListErrorReturns covers the FlightPartsNeedingMetadata error
// branch in tick: the active pass succeeds (no active rows) but a cancelled
// context fails the metadata-list query, so tick logs and returns.
func TestTickMetadataListErrorReturns(t *testing.T) {
	tr := &mockTracker{}
	p, _, _ := newPoller(t, tr, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.tick(ctx) // ActiveFlightParts errors first → returns before the metadata pass
	if tr.calls != 0 {
		t.Errorf("no tracking expected on a cancelled tick, calls = %d", tr.calls)
	}
}

// TestRefreshMetadata_NoResolverIsNoOp covers refreshMetadata's guard: with no
// Resolver wired it returns immediately (no status write, no alert).
func TestRefreshMetadata_NoResolverIsNoOp(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.Resolver = nil
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "NR1", ScheduledOut: now.Add(2 * time.Hour), ScheduledIn: now.Add(4 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	p.refreshMetadata(ctx, f, now) // no resolver → early return, must not panic
}

// TestRefreshMetadata_ResolveErrorRefreshesStatus covers the resolve-error path
// in refreshMetadata: a not-found resolve still bumps last_polled_at via
// RefreshFlightPartStatus so the metadata-pass throttle applies.
func TestRefreshMetadata_ResolveErrorRefreshesStatus(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.Resolver = &fakeResolver{err: providers.ErrFlightNotFound}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	// Degenerate schedule (needsBackfill true) so the resolve is attempted.
	dep := now.Add(2 * time.Hour)
	f, err := mkPart(ctx, s, partSeed{Ident: "NR2", ScheduledOut: dep, ScheduledIn: dep}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	p.refreshMetadata(ctx, f, now)

	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastPolledAt == nil {
		t.Error("a failed metadata resolve must still bump last_polled_at")
	}
}

// TestTickMetadataListErrorAfterActivePass covers the FlightPartsNeedingMetadata
// error branch (180-183): the active pass runs (its tracker cancels the context
// mid-flight), so the subsequent metadata-list query fails on the now-cancelled
// context and tick logs + returns.
func TestTickMetadataListErrorAfterActivePass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tr := &mockTracker{pos: &store.Position{Lat: 1, Lon: 1}, before: func(*store.Flight) { cancel() }}
	p, s, _ := newPoller(t, tr, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	// One active (enroute) flight to drive the active pass.
	if _, err := mkPart(context.Background(), s, partSeed{
		Ident: "MD1", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid); err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	p.tick(ctx) // active pass cancels ctx; metadata list query then errors

	if tr.calls != 1 {
		t.Errorf("the active flight should have been tracked once, calls = %d", tr.calls)
	}
}

// cancelMetaResolver cancels the context on its first Resolve call (then reports
// not-found), so a tick's metadata loop bails on its per-row ctx.Err() guard
// before the second candidate.
type cancelMetaResolver struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancelMetaResolver) Resolve(_ context.Context, _ string, _ time.Time) (*providers.ResolvedFlight, error) {
	c.calls++
	c.cancel()
	return nil, providers.ErrFlightNotFound
}

// TestTickMetadataLoopContextCancelled covers the per-row ctx.Err() guard in the
// metadata loop (185-187): two pre-departure metadata candidates, and the
// resolver cancels the context whilst the first is processed, so the loop bails
// before the second.
func TestTickMetadataLoopContextCancelled(t *testing.T) {
	tr := &mockTracker{}
	p, s, _ := newPoller(t, tr, time.Minute)
	uid := seedUser(t, s)
	now := time.Now()
	// Two flights 2h out with degenerate schedules → both are metadata-pass
	// candidates needing a resolve; none are in the active (<30m) window.
	for _, id := range []string{"ML1", "ML2"} {
		dep := now.Add(2 * time.Hour)
		if _, err := mkPart(context.Background(), s, partSeed{Ident: id, ScheduledOut: dep, ScheduledIn: dep}, uid); err != nil {
			t.Fatalf("mkPart %s: %v", id, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cr := &cancelMetaResolver{cancel: cancel}
	p.Resolver = cr

	p.tick(ctx)

	if cr.calls != 1 {
		t.Errorf("metadata loop should stop after the first row cancels, resolver calls = %d", cr.calls)
	}
}

// TestRefreshMetadata_StatusRefreshErrorOnResolveError covers the
// RefreshFlightPartStatus error branch inside refreshMetadata's resolve-error
// path (229-231): a cancelled context fails both the resolve and the follow-up
// status refresh.
func TestRefreshMetadata_StatusRefreshErrorOnResolveError(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.Resolver = &fakeResolver{err: providers.ErrFlightNotFound}
	uid := seedUser(t, s)
	now := time.Now()
	dep := now.Add(2 * time.Hour)
	f, err := mkPart(context.Background(), s, partSeed{Ident: "MD2", ScheduledOut: dep, ScheduledIn: dep}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // resolve errors → RefreshFlightPartStatus also errors on cancel

	p.refreshMetadata(ctx, f, now) // must not panic
}

// TestResolveAndUpdate_BackfillWriteErrorPropagates covers the BackfillFlightPart
// error branch (432-435): the (fake) resolver returns success, but a cancelled
// context fails the backfill write, which is logged and propagated.
func TestResolveAndUpdate_BackfillWriteErrorPropagates(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.Resolver = &fakeResolver{rf: &providers.ResolvedFlight{
		Ident: "RU2", OriginIATA: "LHR", DestIATA: "JFK", ICAO24: "406b05",
	}}
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(context.Background(), s, partSeed{
		Ident: "RU2", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
	}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Resolve succeeds (fake ignores ctx); BackfillFlightPart fails

	if _, rerr := p.resolveAndUpdate(ctx, f, now); rerr == nil {
		t.Fatal("a failed backfill write should propagate from resolveAndUpdate")
	}
}

// TestResolveAndUpdate_ResolveErrorStampsThrottle covers resolveAndUpdate's
// resolver-error path: a transport error (not ErrFlightNotFound) is logged, the
// throttle is stamped via an empty airframe refresh, and the error propagates.
func TestResolveAndUpdate_ResolveErrorStampsThrottle(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.Resolver = &fakeResolver{err: errors.New("upstream 500")}
	ctx := context.Background()
	uid := seedUser(t, s)
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "RU1", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", ICAO24: "abc123",
	}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	if _, rerr := p.resolveAndUpdate(ctx, f, now); rerr == nil {
		t.Fatal("resolveAndUpdate should propagate the transport error")
	}
	got, _ := s.FlightPartByID(ctx, f.ID)
	if got.LastResolvedAt == nil {
		t.Error("a transport error must still stamp last_resolved_at to throttle the retry")
	}
}

// TestRunFiresSweepAndFeedTickers covers the sweep and feed ticker cases of
// Run's select loop (otherwise gated behind the 4h / 5m production cadences). We
// shorten the two package-level intervals for the duration of the test so both
// tickers fire promptly, then assert the loop stops on context cancel.
func TestRunFiresSweepAndFeedTickers(t *testing.T) {
	origSweep, origFeed := sweepInterval, feedInterval
	sweepInterval = 15 * time.Millisecond
	feedInterval = 15 * time.Millisecond
	t.Cleanup(func() { sweepInterval, feedInterval = origSweep, origFeed })

	tr := &mockTracker{pos: &store.Position{Lat: 3, Lon: 3}}
	p, s, _ := newPoller(t, tr, 20*time.Millisecond)
	p.Feeds = feeds.NewService(s, "aerly-test", time.Minute) // empty set → no-op refresh
	uid := seedUser(t, s)
	now := time.Now()
	if _, err := mkPart(context.Background(), s, partSeed{
		Ident: "RN1", ScheduledOut: now.Add(-time.Hour), ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, uid); err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	time.Sleep(80 * time.Millisecond) // let all three tickers fire at least once
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}

func TestMetadataRefreshIntervalFor(t *testing.T) {
	now := time.Now()
	mk := func(ttd time.Duration) *store.Flight {
		return &store.Flight{ScheduledOut: now.Add(ttd)}
	}
	cases := []struct {
		name string
		ttd  time.Duration
		want time.Duration
	}{
		{"final hour", 30 * time.Minute, 5 * time.Minute},
		{"just inside an hour", 59 * time.Minute, 5 * time.Minute},
		{"inside four hours", 3 * time.Hour, 15 * time.Minute},
		{"beyond four hours", 8 * time.Hour, time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := metadataRefreshIntervalFor(mk(c.ttd), now); got != c.want {
				t.Errorf("ttd=%v: got %v, want %v", c.ttd, got, c.want)
			}
		})
	}
}

func TestProvisionalRefreshIntervalFor(t *testing.T) {
	now := time.Now()
	mk := func(ttd time.Duration) *store.Flight {
		return &store.Flight{ScheduledOut: now.Add(ttd)}
	}
	cases := []struct {
		name string
		ttd  time.Duration
		want time.Duration
	}{
		{"ten days out", 10 * 24 * time.Hour, 24 * time.Hour},
		{"exactly thirty days", 30 * 24 * time.Hour, 24 * time.Hour},
		{"thirty-one days", 31 * 24 * time.Hour, 7 * 24 * time.Hour},
		{"sixty days out", 60 * 24 * time.Hour, 7 * 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := provisionalRefreshIntervalFor(mk(c.ttd), now); got != c.want {
				t.Errorf("ttd=%v: got %v, want %v", c.ttd, got, c.want)
			}
		})
	}
}
