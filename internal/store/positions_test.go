package store

import (
	"math"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/geo"
)

// mkFlightPart seeds a trip + flight plan + one plan_part + its flight_details
// satellite, returning the plan_part_id (the key positions and the poller now
// use). The trip is owned by ownerID with the owner trip_members row, matching
// the helpers in plans_visibility_test.go. status is the flight_details
// flight_status enum value.
func mkFlightPart(t *testing.T, s *Store, ownerID int64, ident string, out, in time.Time) int64 {
	t.Helper()
	trip := mkTrip(t, s, ownerID)
	return mkFlightPartInTrip(t, s, trip, ownerID, ident, out, in, "Scheduled",
		51.4775, -0.4614, 40.6413, -73.7781)
}

// mkFlightPartInTrip is the fuller seeder: it lets a test place the part in a
// specific trip with chosen coords + status, for the convergence/visibility
// tests. Pass NaN-free coords; the start/end coords land on the plan_part and
// the schedule/status/airframe on flight_details.
func mkFlightPartInTrip(t *testing.T, s *Store, tripID, createdBy int64, ident string,
	out, in time.Time, status string,
	startLat, startLon, endLat, endLon float64) int64 {
	t.Helper()
	var planID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		tripID, createdBy,
	).Scan(&planID); err != nil {
		t.Fatalf("insert flight plan: %v", err)
	}
	var partID int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, end_lat, end_lon, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'confirmed') RETURNING id`,
		planID, out, in, startLat, startLon, endLat, endLon,
	).Scan(&partID); err != nil {
		t.Fatalf("insert flight plan_part: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in,
			origin_iata, dest_iata, flight_status)
		VALUES ($1, $2, $3, $4, 'LHR', 'JFK', $5)`,
		partID, ident, out, in, status); err != nil {
		t.Fatalf("insert flight_details: %v", err)
	}
	return partID
}

// TestPositions exercises the part-keyed position helpers (positions.go). The
// FlightID field on Position now carries a plan_part_id; the helpers key on it.
func TestPositions(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	part := mkFlightPart(t, s, owner, "POS1", now, now.Add(time.Hour))

	if p, err := s.LatestRealPosition(ctx, part); err != nil || p != nil {
		t.Fatalf("no positions yet → nil: %v %v", p, err)
	}
	if m, _ := s.LatestPartPositions(ctx, nil); len(m) != 0 {
		t.Error("empty ids → empty map")
	}
	if m, _ := s.PartTracks(ctx, nil, 0); len(m) != 0 {
		t.Error("empty ids → empty map")
	}

	t0 := now.Add(-30 * time.Minute)
	hdr := int16(90)
	for i := 0; i < 3; i++ {
		err := s.InsertPartPosition(ctx, Position{
			FlightID: part, Ts: t0.Add(time.Duration(i) * time.Minute),
			Lat: float64(i), Lon: float64(-i), HeadingDeg: &hdr, IsEstimated: i == 2,
		})
		if err != nil {
			t.Fatalf("InsertPosition: %v", err)
		}
	}

	real, err := s.LatestRealPosition(ctx, part)
	if err != nil || real == nil || real.IsEstimated {
		t.Fatalf("LatestRealPosition should skip estimated: %v %v", real, err)
	}
	if real.Lat != 1 {
		t.Errorf("expected latest real (i=1) lat=1, got %v", real.Lat)
	}

	latest, _ := s.LatestPartPositions(ctx, []int64{part})
	if latest[part] == nil || latest[part].Lat != 2 {
		t.Errorf("LatestPositions should pick newest (i=2): %+v", latest[part])
	}

	tracks, _ := s.PartTracks(ctx, []int64{part}, 0) // 0 → default limit
	if len(tracks[part]) != 3 {
		t.Errorf("RecentTracks count = %d, want 3", len(tracks[part]))
	}
	if tracks[part][0].Lat != 0 || tracks[part][2].Lat != 2 {
		t.Errorf("RecentTracks order wrong: %+v", tracks[part])
	}

	pf, _ := s.PositionsForFlight(ctx, part, 0) // 0 → default limit
	if len(pf) != 3 || pf[0].Lat != 2 {
		t.Errorf("PositionsForFlight newest-first wrong: %+v", pf)
	}
	pf2, _ := s.PositionsForFlight(ctx, part, 1)
	if len(pf2) != 1 {
		t.Errorf("PositionsForFlight limit not applied: %d", len(pf2))
	}

	any, err := s.LatestPosition(ctx, part)
	if err != nil || any == nil {
		t.Fatalf("LatestPosition should return the newest row regardless of is_estimated: %v %v", any, err)
	}
	if any.Lat != 2 || !any.IsEstimated {
		t.Errorf("LatestPosition expected estimated i=2 (lat=2), got %+v", any)
	}
}

// onArc asserts a smoothed estimate landed on the great-circle from anchor to
// fix at its time-share frac — i.e. equals Slerp(anchor, fix, frac).
func onArc(t *testing.T, got *Position, aLat, aLon, fLat, fLon, frac float64) {
	t.Helper()
	wantLat, wantLon := geo.Slerp(aLat, aLon, fLat, fLon, frac)
	if math.Abs(got.Lat-wantLat) > 1e-6 || math.Abs(got.Lon-wantLon) > 1e-6 {
		t.Errorf("estimate at ts=%s off the arc: got (%.6f,%.6f) want (%.6f,%.6f)",
			got.Ts.Format(time.RFC3339), got.Lat, got.Lon, wantLat, wantLon)
	}
	if !got.IsEstimated {
		t.Errorf("smoothing must keep the sample flagged is_estimated: %+v", got)
	}
}

// posByTs indexes positions by their (whole-minute-unique) timestamp.
func posByTs(ps []*Position) map[time.Time]*Position {
	m := map[time.Time]*Position{}
	for _, p := range ps {
		m[p.Ts.UTC().Truncate(time.Second)] = p
	}
	return m
}

// TestSmoothEstimatedTrack covers the dog-leg removal: when a real fix lands,
// the dead-reckoned estimates that preceded it are re-laid onto a smooth
// great-circle from the nearest solid anchor (origin or last real fix) to the
// new position.
func TestSmoothEstimatedTrack(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)

	// --- Case A: a prior real fix is the closer anchor ----------------------
	// Origin is far away, so the last real fix wins the "whichever is closer".
	t.Run("anchors on last real fix", func(t *testing.T) {
		now := time.Now().UTC()
		out := now.Add(-time.Hour)
		part := mkFlightPart(t, s, owner, "SMTH-A", out, now.Add(time.Hour))
		f := &Flight{ID: part, ScheduledOut: out,
			OriginLat: ptr(51.0), OriginLon: ptr(0.0)}

		ts := func(min int) time.Time { return now.Add(time.Duration(min) * time.Minute) }
		ins := func(at time.Time, lat, lon float64, est bool) {
			t.Helper()
			h := int16(200)
			if err := s.InsertPartPosition(ctx, Position{FlightID: part, Ts: at,
				Lat: lat, Lon: lon, HeadingDeg: &h, IsEstimated: est}); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}

		// Real anchor near the new fix; origin (51,0) is far.
		ins(ts(-40), 45.0, -20.0, false)
		// Dead-reckoned dog-leg veering off toward the destination.
		ins(ts(-30), 44.0, -25.0, true)
		ins(ts(-20), 43.0, -30.0, true)
		// The new real fix, off the extrapolated line.
		fix := Position{FlightID: part, Ts: ts(-10), Lat: 44.0, Lon: -22.0}
		ins(fix.Ts, fix.Lat, fix.Lon, false)

		if err := s.SmoothEstimatedTrack(ctx, f, fix); err != nil {
			t.Fatalf("SmoothEstimatedTrack: %v", err)
		}

		all, _ := s.PositionsForFlight(ctx, part, 0)
		by := posByTs(all)
		// span anchor(-40) → fix(-10) is 30m; estimate at -30 → 1/3, -20 → 2/3.
		onArc(t, by[ts(-30).Truncate(time.Second)], 45.0, -20.0, 44.0, -22.0, 1.0/3)
		onArc(t, by[ts(-20).Truncate(time.Second)], 45.0, -20.0, 44.0, -22.0, 2.0/3)
		// Real fixes are never moved.
		if r := by[ts(-40).Truncate(time.Second)]; r.Lat != 45.0 || r.Lon != -20.0 {
			t.Errorf("anchor real fix moved: %+v", r)
		}
		if r := by[ts(-10).Truncate(time.Second)]; r.Lat != 44.0 || r.Lon != -22.0 {
			t.Errorf("new real fix moved: %+v", r)
		}
	})

	// --- Case B: no real fix yet → anchor on the origin ---------------------
	t.Run("anchors on origin when no real fix precedes", func(t *testing.T) {
		now := time.Now().UTC()
		out := now.Add(-time.Hour)
		part := mkFlightPart(t, s, owner, "SMTH-B", out, now.Add(time.Hour))
		oLat, oLon := 51.0, 0.0
		f := &Flight{ID: part, ScheduledOut: out, OriginLat: ptr(oLat), OriginLon: ptr(oLon)}

		ts := func(min int) time.Time { return now.Add(time.Duration(min) * time.Minute) }
		ins := func(at time.Time, lat, lon float64, est bool) {
			t.Helper()
			h := int16(200)
			if err := s.InsertPartPosition(ctx, Position{FlightID: part, Ts: at,
				Lat: lat, Lon: lon, HeadingDeg: &h, IsEstimated: est}); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}

		// Only schedule-based estimates so far, then a real fix.
		ins(ts(-40), 49.0, -8.0, true)
		ins(ts(-20), 47.0, -12.0, true)
		fix := Position{FlightID: part, Ts: ts(-10), Lat: 48.0, Lon: -6.0}
		ins(fix.Ts, fix.Lat, fix.Lon, false)

		if err := s.SmoothEstimatedTrack(ctx, f, fix); err != nil {
			t.Fatalf("SmoothEstimatedTrack: %v", err)
		}

		all, _ := s.PositionsForFlight(ctx, part, 0)
		by := posByTs(all)
		// span origin(scheduled_out=-60) → fix(-10) is 50m; -40 → 20/50, -20 → 40/50.
		onArc(t, by[ts(-40).Truncate(time.Second)], oLat, oLon, fix.Lat, fix.Lon, 20.0/50)
		onArc(t, by[ts(-20).Truncate(time.Second)], oLat, oLon, fix.Lat, fix.Lon, 40.0/50)
	})
}

func ptr[T any](v T) *T { return &v }
