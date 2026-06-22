package store

import (
	"testing"
	"time"
)

// TestG3LatestPositionEmpty covers the ErrNoRows → (nil, nil) branch of
// LatestPosition for a part with no samples.
func TestG3LatestPositionEmpty(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	part := mkFlightPart(t, s, owner, "G3LP1", now, now.Add(time.Hour))

	if p, err := s.LatestPosition(ctx, part); err != nil || p != nil {
		t.Fatalf("LatestPosition with no rows = %v, %v; want nil, nil", p, err)
	}
}

// TestG3SmoothEstimatedTrackNoOps covers the early-return branches of
// SmoothEstimatedTrack: an estimated fix, and a fix with no usable anchor.
func TestG3SmoothEstimatedTrackNoOps(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now().UTC()
	owner := mkUser(t, s)

	// An estimated "fix" is a no-op (line guard fix.IsEstimated).
	t.Run("estimated fix is a no-op", func(t *testing.T) {
		out := now.Add(-time.Hour)
		part := mkFlightPart(t, s, owner, "G3SM-EST", out, now.Add(time.Hour))
		f := &Flight{ID: part, ScheduledOut: out, OriginLat: ptr(51.0), OriginLon: ptr(0.0)}
		if err := s.SmoothEstimatedTrack(ctx, f, Position{FlightID: part, Ts: now, IsEstimated: true}); err != nil {
			t.Fatalf("SmoothEstimatedTrack estimated fix: %v", err)
		}
	})

	// No origin coords and no prior real fix → no anchor → no-op.
	t.Run("no anchor available", func(t *testing.T) {
		out := now.Add(-time.Hour)
		part := mkFlightPart(t, s, owner, "G3SM-NOA", out, now.Add(time.Hour))
		f := &Flight{ID: part, ScheduledOut: out} // OriginLat/Lon nil
		fix := Position{FlightID: part, Ts: now.Add(-10 * time.Minute), Lat: 44.0, Lon: -22.0}
		if err := s.SmoothEstimatedTrack(ctx, f, fix); err != nil {
			t.Fatalf("SmoothEstimatedTrack no anchor: %v", err)
		}
	})
}

// TestG3SmoothEstimatedTrackDepTimeOverrides covers the ActualOut and
// EstimatedOut departure-time override branches when the origin anchor wins.
func TestG3SmoothEstimatedTrackDepTimeOverrides(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	oLat, oLon := 51.0, 0.0

	run := func(name string, setDep func(f *Flight, dep time.Time)) {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			sched := now.Add(-2 * time.Hour)
			dep := now.Add(-time.Hour) // actual/estimated departure later than scheduled
			part := mkFlightPart(t, s, owner, "G3SM-"+name, sched, now.Add(time.Hour))
			f := &Flight{ID: part, ScheduledOut: sched, OriginLat: ptr(oLat), OriginLon: ptr(oLon)}
			setDep(f, dep)

			ts := func(min int) time.Time { return now.Add(time.Duration(min) * time.Minute) }
			h := int16(200)
			ins := func(at time.Time, lat, lon float64, est bool) {
				if err := s.InsertPartPosition(ctx, Position{FlightID: part, Ts: at,
					Lat: lat, Lon: lon, HeadingDeg: &h, IsEstimated: est}); err != nil {
					t.Fatalf("insert: %v", err)
				}
			}
			// Estimates after the departure time, then a real fix off the line.
			ins(ts(-40), 49.0, -8.0, true)
			ins(ts(-20), 47.0, -12.0, true)
			fix := Position{FlightID: part, Ts: ts(-10), Lat: 48.0, Lon: -6.0}
			ins(fix.Ts, fix.Lat, fix.Lon, false)

			if err := s.SmoothEstimatedTrack(ctx, f, fix); err != nil {
				t.Fatalf("SmoothEstimatedTrack: %v", err)
			}
			// The estimate at ts(-40) should have moved onto the great-circle
			// from the origin to the fix (proving the origin anchor was used
			// with the overridden departure time).
			all, _ := s.PositionsForFlight(ctx, part, 0)
			moved := false
			for _, p := range all {
				if p.Ts.Truncate(time.Second).Equal(ts(-40).Truncate(time.Second)) {
					if p.Lat != 49.0 || p.Lon != -8.0 {
						moved = true
					}
				}
			}
			if !moved {
				t.Errorf("estimate was not smoothed onto the origin arc")
			}
		})
	}

	run("ActualOut", func(f *Flight, dep time.Time) { f.ActualOut = &dep })
	run("EstimatedOut", func(f *Flight, dep time.Time) { f.EstimatedOut = &dep })
}
