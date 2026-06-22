package store

import (
	"testing"
	"time"
)

// TestG1FlightPartsWithMissingCoords covers the periodic sweep query that
// returns every flight part with at least one NULL coord column.
func TestG1FlightPartsWithMissingCoords(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Fully-coordinated part: excluded.
	mkFlightPartInTrip(t, s, trip, owner, "FULL1",
		now.Add(time.Hour), now.Add(2*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)

	// A part missing its end coords. Seed a plan + part + flight_details with
	// NULL end_lat/end_lon directly.
	var planID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		trip, owner).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	var partID int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, status)
		VALUES ($1, $2, $3, 51.47, -0.46, 'confirmed') RETURNING id`,
		planID, now.Add(time.Hour), now.Add(2*time.Hour)).Scan(&partID); err != nil {
		t.Fatalf("insert part: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in, origin_iata, dest_iata, flight_status)
		VALUES ($1, 'MISS1', $2, $3, 'LHR', 'JFK', 'Scheduled')`,
		partID, now.Add(time.Hour), now.Add(2*time.Hour)); err != nil {
		t.Fatalf("insert flight_details: %v", err)
	}

	parts, err := s.FlightPartsWithMissingCoords(ctx)
	if err != nil {
		t.Fatalf("FlightPartsWithMissingCoords: %v", err)
	}
	var found bool
	for _, p := range parts {
		if p.ID == partID {
			found = true
		}
		if p.OriginLat == nil {
			t.Fatalf("scanned part should still expose populated coords: %+v", p)
		}
	}
	if !found {
		t.Fatalf("missing-coords part %d not returned: %+v", partID, parts)
	}
}

// TestG1FlightPartsByPlanMissingCoords covers the plan-scoped variant.
func TestG1FlightPartsByPlanMissingCoords(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	var planID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		trip, owner).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	var partID int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, status)
		VALUES ($1, $2, $3, 'confirmed') RETURNING id`,
		planID, now.Add(time.Hour), now.Add(2*time.Hour)).Scan(&partID); err != nil {
		t.Fatalf("insert part: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in, origin_iata, dest_iata, flight_status)
		VALUES ($1, 'NOCO1', $2, $3, 'LHR', 'JFK', 'Scheduled')`,
		partID, now.Add(time.Hour), now.Add(2*time.Hour)); err != nil {
		t.Fatalf("insert flight_details: %v", err)
	}

	parts, err := s.FlightPartsByPlanMissingCoords(ctx, planID)
	if err != nil {
		t.Fatalf("FlightPartsByPlanMissingCoords: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != partID {
		t.Fatalf("expected only part %d, got %+v", partID, parts)
	}

	// A different plan with full coords yields nothing.
	other := mkFlightPartInTrip(t, s, trip, owner, "OTH1",
		now.Add(time.Hour), now.Add(2*time.Hour), "Scheduled", 1, 1, 2, 2)
	otherPlan, _, err := s.PlanIDForPart(ctx, other)
	if err != nil {
		t.Fatalf("PlanIDForPart: %v", err)
	}
	empty, err := s.FlightPartsByPlanMissingCoords(ctx, otherPlan)
	if err != nil {
		t.Fatalf("FlightPartsByPlanMissingCoords (other): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("fully-coordinated plan should return nothing, got %+v", empty)
	}
}

// TestG1MarkFlightPartResolved covers the resolve-and-relabel transaction,
// including the guarded label upgrade (only replaces a still-bare label).
func TestG1MarkFlightPartResolved(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	part := mkFlightPart(t, s, owner, "RES1", now.Add(time.Hour), now.Add(2*time.Hour))
	// Pre-set the start_label to the bare origin code so the guard upgrades it;
	// leave end_label empty so the empty-branch upgrades too.
	if _, err := s.pool.Exec(ctx,
		`UPDATE plan_parts SET start_label = 'LHR', end_label = '' WHERE id = $1`, part); err != nil {
		t.Fatalf("seed labels: %v", err)
	}

	if err := s.MarkFlightPartResolved(ctx, part, "LHR", "London Heathrow (LHR)", "JFK", "New York JFK (JFK)"); err != nil {
		t.Fatalf("MarkFlightPartResolved: %v", err)
	}
	fd, err := s.FlightDetailFor(ctx, part)
	if err != nil {
		t.Fatalf("FlightDetailFor: %v", err)
	}
	if !fd.Resolved {
		t.Fatalf("flight should be resolved")
	}
	pp, err := s.PlanPartByID(ctx, part)
	if err != nil {
		t.Fatalf("PlanPartByID: %v", err)
	}
	if pp.StartLabel != "London Heathrow (LHR)" || pp.EndLabel != "New York JFK (JFK)" {
		t.Fatalf("labels not upgraded: start=%q end=%q", pp.StartLabel, pp.EndLabel)
	}

	// A hand-edited label is left untouched.
	part2 := mkFlightPart(t, s, owner, "RES2", now.Add(time.Hour), now.Add(2*time.Hour))
	if _, err := s.pool.Exec(ctx,
		`UPDATE plan_parts SET start_label = 'My custom origin' WHERE id = $1`, part2); err != nil {
		t.Fatalf("seed custom label: %v", err)
	}
	if err := s.MarkFlightPartResolved(ctx, part2, "LHR", "London Heathrow (LHR)", "JFK", "New York JFK (JFK)"); err != nil {
		t.Fatalf("MarkFlightPartResolved (custom): %v", err)
	}
	pp2, err := s.PlanPartByID(ctx, part2)
	if err != nil {
		t.Fatalf("PlanPartByID (custom): %v", err)
	}
	if pp2.StartLabel != "My custom origin" {
		t.Fatalf("hand-edited label should be preserved, got %q", pp2.StartLabel)
	}
}

// TestG1ConvergencePartsAll covers the unified map+list query: flight parts
// windowed by effective arrival, non-flight parts by their span, with the tag
// scoping branch and the visibility gate.
func TestG1ConvergencePartsAll(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	from := now.Add(-time.Hour)
	to := now.Add(6 * time.Hour)

	// A flight whose effective arrival falls in [from, to].
	flightPart := mkFlightPartInTrip(t, s, trip, owner, "CVA1",
		now.Add(time.Hour), now.Add(2*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)

	// A non-flight (hotel) part with coords whose span overlaps the window.
	hotelPlan := mkPlanType(t, s, trip, owner, "hotel")
	hotelPart := addCoordPart(t, s, hotelPlan, now.Add(time.Hour), now.Add(3*time.Hour), 48.85, 2.35)

	parts, err := s.ConvergencePartsAll(ctx, owner, from, to, "")
	if err != nil {
		t.Fatalf("ConvergencePartsAll: %v", err)
	}
	var sawFlight, sawHotel bool
	for _, p := range parts {
		if p.ID == flightPart {
			sawFlight = true
		}
		if p.ID == hotelPart {
			sawHotel = true
		}
	}
	if !sawFlight || !sawHotel {
		t.Fatalf("expected both flight (%d) and hotel (%d) parts, got %+v", flightPart, hotelPart, parts)
	}

	// Tag scoping: tag the trip and request that tag → still returned; request
	// an unknown tag → empty.
	tagTrip(t, s, trip, "ski2026")
	tagged, err := s.ConvergencePartsAll(ctx, owner, from, to, "ski2026")
	if err != nil {
		t.Fatalf("ConvergencePartsAll (tagged): %v", err)
	}
	if len(tagged) == 0 {
		t.Fatalf("tagged query should return the trip's parts")
	}
	none, err := s.ConvergencePartsAll(ctx, owner, from, to, "no-such-tag")
	if err != nil {
		t.Fatalf("ConvergencePartsAll (no tag): %v", err)
	}
	for _, p := range none {
		if p.ID == flightPart || p.ID == hotelPart {
			t.Fatalf("unknown tag should exclude this trip's parts")
		}
	}
}

// TestG1ConvergenceMarkers covers the non-flight marker overlay query and its
// tag-scoping branch.
func TestG1ConvergenceMarkers(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	dinPlan := mkPlanType(t, s, trip, owner, "dining")
	dinPart := addCoordPart(t, s, dinPlan, now, now.Add(time.Hour), 40.71, -74.0)

	from := now.Add(-time.Hour)
	to := now.Add(2 * time.Hour)

	markers, err := s.ConvergenceMarkers(ctx, owner, from, to, "")
	if err != nil {
		t.Fatalf("ConvergenceMarkers: %v", err)
	}
	var found bool
	for _, m := range markers {
		if m.PlanPartID == dinPart {
			found = true
			if m.Type != "dining" || m.StartLat == nil {
				t.Fatalf("marker not populated: %+v", m)
			}
		}
	}
	if !found {
		t.Fatalf("dining marker %d not returned: %+v", dinPart, markers)
	}

	tagTrip(t, s, trip, "foodie")
	tagged, err := s.ConvergenceMarkers(ctx, owner, from, to, "foodie")
	if err != nil {
		t.Fatalf("ConvergenceMarkers (tagged): %v", err)
	}
	if len(tagged) == 0 {
		t.Fatalf("tagged marker query should return the dining marker")
	}
}

// TestG1TrackerPartRow covers the ungated single-part tracker lookup and its
// ErrNotFound path.
func TestG1TrackerPartRow(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	part := mkFlightPart(t, s, owner, "ROW1", now.Add(time.Hour), now.Add(2*time.Hour))

	tp, err := s.TrackerPartRow(ctx, part)
	if err != nil {
		t.Fatalf("TrackerPartRow: %v", err)
	}
	if tp.PlanPartID != part || tp.Ident != "ROW1" {
		t.Fatalf("unexpected row: %+v", tp)
	}

	if _, err := s.TrackerPartRow(ctx, 999999999); err != ErrNotFound {
		t.Fatalf("missing part should be ErrNotFound, got %v", err)
	}
}

// TestG1FlightPartByIDNotFound covers scanFlightPart's ErrNotFound path.
func TestG1FlightPartByIDNotFound(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	if _, err := s.FlightPartByID(ctx, 999999999); err != ErrNotFound {
		t.Fatalf("missing flight part should be ErrNotFound, got %v", err)
	}
	if _, err := s.TrackerPartByID(ctx, mkUser(t, s), 999999999); err != ErrNotFound {
		t.Fatalf("missing tracker part should be ErrNotFound, got %v", err)
	}
}

// TestG1BackfillFlightPartCoords covers the coord-setting branches of
// BackfillFlightPart (both origin and dest coords non-zero).
func TestG1BackfillFlightPartCoords(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	now := time.Now()
	// Seed a part with NULL coords + empty IATA so the only-fill-empty branches
	// actually write.
	trip := mkTrip(t, s, owner)
	var planID, partID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		trip, owner).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, status)
		VALUES ($1, $2, $3, 'confirmed') RETURNING id`,
		planID, now, now.Add(time.Hour)).Scan(&partID); err != nil {
		t.Fatalf("insert part: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in, origin_iata, dest_iata, flight_status)
		VALUES ($1, 'BF1', $2, $3, '', '', 'Scheduled')`,
		partID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("insert flight_details: %v", err)
	}

	if err := s.BackfillFlightPart(ctx, partID, BackfillPayload{
		OriginIATA: "lhr", OriginLat: 51.47, OriginLon: -0.46,
		DestIATA: "jfk", DestLat: 40.64, DestLon: -73.78,
		ICAO24: " ABC123 ", Callsign: " baw1 ",
		Notes: "Backfilled note", AircraftType: "A320",
		OriginTerminal: "5", DestTerminal: "4",
	}); err != nil {
		t.Fatalf("BackfillFlightPart: %v", err)
	}
	fd, _ := s.FlightDetailFor(ctx, partID)
	if fd.OriginIATA != "LHR" || fd.DestIATA != "JFK" || fd.AircraftType != "A320" {
		t.Fatalf("flight_details not backfilled: %+v", fd)
	}
	pp, _ := s.PlanPartByID(ctx, partID)
	if pp.StartLat == nil || pp.EndLat == nil {
		t.Fatalf("coords not backfilled: %+v", pp)
	}
}

// TestG1TrackerQueryErrorBranches drives the cancelled-context error returns of
// the tracker read queries.
func TestG1TrackerQueryErrorBranches(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	now := time.Now()
	checks := []struct {
		name string
		fn   func() error
	}{
		{"ActiveFlightParts", func() error { _, e := s.ActiveFlightParts(cc, now); return e }},
		{"FlightPartsNeedingMetadata", func() error { _, e := s.FlightPartsNeedingMetadata(cc, now); return e }},
		{"FlightPartByID", func() error { _, e := s.FlightPartByID(cc, 1); return e }},
		{"FlightPartsWithMissingCoords", func() error { _, e := s.FlightPartsWithMissingCoords(cc); return e }},
		{"ProvisionalFlightParts", func() error { _, e := s.ProvisionalFlightParts(cc); return e }},
		{"FlightPartsByPlanMissingCoords", func() error { _, e := s.FlightPartsByPlanMissingCoords(cc, 1); return e }},
		{"BackfillFlightPart", func() error { return s.BackfillFlightPart(cc, 1, BackfillPayload{}) }},
		{"RefreshFlightPartAirframe", func() error { return s.RefreshFlightPartAirframe(cc, 1, "a", "b") }},
		{"MarkFlightPartResolved", func() error { return s.MarkFlightPartResolved(cc, 1, "a", "b", "c", "d") }},
		{"RefreshFlightPartGate", func() error { return s.RefreshFlightPartGate(cc, 1, "a", "b") }},
		{"RefreshFlightPartTerminal", func() error { return s.RefreshFlightPartTerminal(cc, 1, "a", "b") }},
		{"RefreshFlightPartBelt", func() error { return s.RefreshFlightPartBelt(cc, 1, "a") }},
		{"RefreshFlightPartSchedule", func() error { return s.RefreshFlightPartSchedule(cc, 1, now, now) }},
		{"RefreshFlightPartStatus", func() error { return s.RefreshFlightPartStatus(cc, 1) }},
		{"ConvergenceParts", func() error { _, e := s.ConvergenceParts(cc, 1, now, now, ""); return e }},
		{"ConvergencePartsAll", func() error { _, e := s.ConvergencePartsAll(cc, 1, now, now, ""); return e }},
		{"ConvergenceMarkers", func() error { _, e := s.ConvergenceMarkers(cc, 1, now, now, ""); return e }},
		{"TrackerPartByID", func() error { _, e := s.TrackerPartByID(cc, 1, 2); return e }},
		{"TrackerPartRow", func() error { _, e := s.TrackerPartRow(cc, 1); return e }},
		{"TaggedTripSpan", func() error { _, _, _, e := s.TaggedTripSpan(cc, 1, "tag"); return e }},
	}
	for _, c := range checks {
		if err := c.fn(); err == nil {
			t.Errorf("%s with cancelled context should error", c.name)
		}
	}
}

// TestG1TaggedTripSpanEmptyTag covers the empty-normalised-tag early return.
func TestG1TaggedTripSpanEmptyTag(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	_, _, ok, err := s.TaggedTripSpan(ctx, mkUser(t, s), "")
	if err != nil || ok {
		t.Fatalf("empty tag should return ok=false, nil err: %v %v", ok, err)
	}
	// A tag the viewer has no visible parts for → ok=false (MIN/MAX NULL branch).
	_, _, ok, err = s.TaggedTripSpan(ctx, mkUser(t, s), "nonexistent-tag-xyz")
	if err != nil || ok {
		t.Fatalf("unseen tag should return ok=false: %v %v", ok, err)
	}
}

// TestG1ConvergencePartsTagged exercises the tag-scoping branch of
// ConvergenceParts (the other branches are already covered elsewhere).
func TestG1ConvergencePartsTagged(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	mkFlightPartInTrip(t, s, trip, owner, "CVT1",
		now.Add(time.Hour), now.Add(2*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)
	tagTrip(t, s, trip, "alpine")

	parts, err := s.ConvergenceParts(ctx, owner, now.Add(-time.Hour), now.Add(6*time.Hour), "alpine")
	if err != nil {
		t.Fatalf("ConvergenceParts tagged: %v", err)
	}
	if len(parts) == 0 {
		t.Fatalf("tagged convergence should return the flight part")
	}

	from, to, ok, err := s.TaggedTripSpan(ctx, owner, "alpine")
	if err != nil || !ok {
		t.Fatalf("TaggedTripSpan should find the tagged span: %v %v", ok, err)
	}
	if !to.After(from) && !to.Equal(from) {
		t.Fatalf("span looks wrong: %v..%v", from, to)
	}
}

// mkPlanType inserts a plan of the given type and returns its id.
func mkPlanType(t *testing.T, s *Store, tripID, createdBy int64, typ string) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, $2, $3) RETURNING id`,
		tripID, typ, createdBy).Scan(&id); err != nil {
		t.Fatalf("insert %s plan: %v", typ, err)
	}
	return id
}

// addCoordPart inserts a plan_part with start coordinates and returns its id.
func addCoordPart(t *testing.T, s *Store, planID int64, startsAt, endsAt time.Time, lat, lon float64) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, start_label, status)
		VALUES ($1, $2, $3, $4, $5, 'Somewhere', 'confirmed') RETURNING id`,
		planID, startsAt, endsAt, lat, lon).Scan(&id); err != nil {
		t.Fatalf("insert coord part: %v", err)
	}
	return id
}
