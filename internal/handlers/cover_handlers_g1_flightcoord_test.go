package handlers

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
)

// g1OffTableFlightPlan creates a flight plan whose origin/dest airports are not
// in the embedded coordinate table (NQY, FAO), so the part is left without
// coordinates at commit time and surfaces from FlightPartsByPlanMissingCoords.
//
// Plan creation itself fires the production resolveFlightCoordsAsync backfill;
// we disable the resolver for the duration of the POST so that goroutine is a
// no-op and the leg stays unresolved, leaving the explicit calls in each test
// fully in control. The resolver is restored before returning.
func g1OffTableFlightPlan(t *testing.T, e *testEnv, tripID, uid int64) int64 {
	t.Helper()
	saved := e.api.Resolver
	e.api.Resolver = nil
	defer func() { e.api.Resolver = saved }()
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(2 * time.Hour)
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tripID), map[string]any{
		"type": "flight", "title": "G1OFF", "confirmation_ref": "PNR",
		"parts": []map[string]any{{
			"type": "flight", "starts_at": out, "ends_at": in,
			"start_label": "NQY", "end_label": "FAO",
			"flight": map[string]any{
				"ident": "G1OFF", "scheduled_out": out, "scheduled_in": in,
				"origin_iata": "NQY", "dest_iata": "FAO",
			},
		}},
	}, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create off-table flight plan: %d %s", w.Code, w.Body.String())
	}
	return int64(decodeBody[map[string]any](t, w)["id"].(float64))
}

// TestResolveFlightCoordsAsyncNilResolverG1 covers the early return when no
// resolver is configured: the call is a no-op and must not panic.
func TestResolveFlightCoordsAsyncNilResolverG1(t *testing.T) {
	e := setup(t, nil, nil) // nil resolver
	owner := e.user(t, "g1fcnil", false)
	tripID := newTrip(t, e, owner, "Async nil trip")
	planID := g1OffTableFlightPlan(t, e, owner, tripID)

	// Should return immediately without spawning work.
	e.api.resolveFlightCoordsAsync(tripID, planID)
}

// TestResolveFlightCoordsAsyncFillsG1 covers the goroutine path: with a resolver
// configured and an off-table leg, Fill resolves coordinates, the plan is
// republished, and the part's coords are persisted.
func TestResolveFlightCoordsAsyncFillsG1(t *testing.T) {
	rf := &providers.ResolvedFlight{
		Ident:      "G1OFF",
		OriginIATA: "NQY", DestIATA: "FAO",
		OriginLat: 50.44, OriginLon: -4.99,
		DestLat: 37.01, DestLon: -7.97,
		ScheduledOut: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		ScheduledIn:  time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC),
	}
	e := setup(t, &fakeResolver{rf: rf}, nil)
	owner := e.user(t, "g1fcfill", false)
	tripID := newTrip(t, e, owner, "Async fill trip")
	planID := g1OffTableFlightPlan(t, e, owner, tripID)
	ctx := context.Background()

	// Sanity: the leg starts without coordinates.
	missing, err := e.store.FlightPartsByPlanMissingCoords(ctx, planID)
	if err != nil {
		t.Fatalf("FlightPartsByPlanMissingCoords: %v", err)
	}
	if len(missing) == 0 {
		t.Fatalf("expected an off-table leg missing coords, got none")
	}

	e.api.resolveFlightCoordsAsync(tripID, planID)

	// Wait for the background goroutine to fill the coords.
	deadline := time.Now().Add(5 * time.Second)
	for {
		left, err := e.store.FlightPartsByPlanMissingCoords(ctx, planID)
		if err != nil {
			t.Fatalf("poll FlightPartsByPlanMissingCoords: %v", err)
		}
		if len(left) == 0 {
			break // coords filled
		}
		if time.Now().After(deadline) {
			t.Fatalf("coords not filled within deadline; %d legs still missing", len(left))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// g1waitFlightResolved polls (via the DB, race-free) until the plan's flight
// part is marked resolved, which the async backfill does just before the write
// that fails in the fill-error case — confirming the goroutine reached
// flightcoord.Fill. Bounded by a timeout.
func g1waitFlightResolved(t *testing.T, e *testEnv, planID int64) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var resolved bool
		err := e.pool.QueryRow(ctx, `
			SELECT COALESCE(bool_or(fd.resolved), false)
			FROM plan_parts part
			JOIN flight_details fd ON fd.plan_part_id = part.id
			WHERE part.plan_id = $1`, planID).Scan(&resolved)
		if err == nil && resolved {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("flight part was never marked resolved within deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Small grace so the post-mark write attempt finishes before teardown.
	time.Sleep(100 * time.Millisecond)
}

// TestResolveFlightCoordsAsyncQueryErrG1 covers the goroutine's query-failed
// branch: FlightPartsByPlanMissingCoords errors (flight_details dropped), so the
// resolver is never reached and the goroutine logs and returns. The single
// failing query completes well within the wait below.
func TestResolveFlightCoordsAsyncQueryErrG1(t *testing.T) {
	// A non-nil ResolvedFlight so the plan-creation backfill (which also runs
	// resolveFlightCoordsAsync) doesn't nil-panic before we drop the table.
	rf := &providers.ResolvedFlight{
		Ident: "G1OFF", OriginIATA: "NQY", DestIATA: "FAO",
		OriginLat: 50.44, OriginLon: -4.99, DestLat: 37.01, DestLon: -7.97,
		ScheduledOut: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		ScheduledIn:  time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC),
	}
	e := setup(t, &fakeResolver{rf: rf}, nil)
	owner := e.user(t, "g1fcqerr", false)
	tripID := newTrip(t, e, owner, "Async query err trip")
	planID := g1OffTableFlightPlan(t, e, owner, tripID)

	g1dropTable(t, e, "flight_details")
	e.api.resolveFlightCoordsAsync(tripID, planID)
	// The query fails immediately; let the goroutine log and return before the
	// test database is torn down.
	time.Sleep(300 * time.Millisecond)
}

// TestResolveFlightCoordsAsyncFillErrG1 covers the goroutine's fill-failed
// branch: the resolver returns coordinates (so Fill attempts a write), but the
// backfill write fails because flight_details.aircraft_type is gone, making
// Fill return an error that the loop logs and continues past.
func TestResolveFlightCoordsAsyncFillErrG1(t *testing.T) {
	rf := &providers.ResolvedFlight{
		Ident:      "G1OFF",
		OriginIATA: "NQY", DestIATA: "FAO",
		OriginLat: 50.44, OriginLon: -4.99,
		DestLat: 37.01, DestLon: -7.97,
		ScheduledOut: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		ScheduledIn:  time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC),
	}
	e := setup(t, &fakeResolver{rf: rf}, nil)
	owner := e.user(t, "g1fcfillerr", false)
	tripID := newTrip(t, e, owner, "Async fill err trip")
	planID := g1OffTableFlightPlan(t, e, owner, tripID)

	// Drop a column the backfill write touches but the missing-coords query does
	// not, so the query succeeds, the resolver runs, and BackfillFlightPart fails.
	g1dropColumn(t, e, "flight_details", "aircraft_type")
	e.api.resolveFlightCoordsAsync(tripID, planID)
	g1waitFlightResolved(t, e, planID)
}
