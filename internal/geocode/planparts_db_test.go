package geocode

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

// mkTripDB inserts a trip owned by ownerID directly via the pool (the store
// package's own mkTrip helper isn't reachable from here), returning its id.
func mkTripDB(t *testing.T, pool *pgxpool.Pool, ownerID int64) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('Test Trip', $1) RETURNING id`, ownerID,
	).Scan(&id); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, id, ownerID,
	); err != nil {
		t.Fatalf("insert owner member: %v", err)
	}
	return id
}

func TestPlanParts_NilGuards(t *testing.T) {
	g := stubGeo{resolves: map[string][2]float64{}}
	// Nil store, nil resolver, and a resolver with a nil Geo are all no-ops
	// returning (false, nil) rather than panicking.
	if changed, err := PlanParts(context.Background(), nil, testResolver(g, nil), 1); changed || err != nil {
		t.Errorf("nil store: got (%v,%v), want (false,nil)", changed, err)
	}
	if changed, err := PlanParts(context.Background(), &store.Store{}, nil, 1); changed || err != nil {
		t.Errorf("nil resolver: got (%v,%v), want (false,nil)", changed, err)
	}
	if changed, err := PlanParts(context.Background(), &store.Store{}, &Resolver{}, 1); changed || err != nil {
		t.Errorf("resolver with nil Geo: got (%v,%v), want (false,nil)", changed, err)
	}
}

// TestPlanParts_Backfills exercises the DB-backed core: a hotel part with an
// address but no coordinates gets geocoded and its floating local times anchored
// to the resolved zone, and the function reports that something changed.
func TestPlanParts_Backfills(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return // DB not available; skipped by NewPool.
	}
	st := store.New(pool)
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "planparts-owner", false, true)
	trip := mkTripDB(t, pool, owner)

	starts := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 6, 3, 11, 0, 0, 0, time.UTC)

	// Hotel part: addressed, no coords, no tz. Synthetic London address.
	hotelPlan, err := st.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Test Hotel",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: starts, EndsAt: &ends,
			StartLabel:    "Test Hotel",
			StartAddress:  "1 Example Street, London, United Kingdom",
			Hotel:         &store.HotelDetail{PropertyName: "Test Hotel"},
			StartTZSource: "floating",
			EndTZSource:   "floating",
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan(hotel): %v", err)
	}

	// A pinned part with no coords must NOT be geocoded over.
	pinnedPlan, err := st.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip, Type: "ground", Title: "Pinned Transfer",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt:      starts,
			StartLabel:    "Manual Pin",
			StartAddress:  "1 Example Street, London, United Kingdom",
			StartTZSource: "floating",
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan(pinned): %v", err)
	}
	// Pin the start coords (cleared) so the geocoder must skip it.
	pinnedParts, err := st.PartsByPlan(ctx, pinnedPlan.ID)
	if err != nil || len(pinnedParts) != 1 {
		t.Fatalf("PartsByPlan(pinned) = %d, %v", len(pinnedParts), err)
	}
	pinTrue := true
	if _, err := st.UpdatePlanPart(ctx, pinnedParts[0].ID, store.UpdatePlanPartPayload{
		StartCoordsPinned: &pinTrue, EndCoordsPinned: &pinTrue,
	}); err != nil {
		t.Fatalf("pin update: %v", err)
	}

	g := stubGeo{resolves: map[string][2]float64{
		"1 Example Street, London, United Kingdom": {londonLat, londonLon},
	}}

	changed, err := PlanParts(ctx, st, testResolver(g, nil), hotelPlan.ID)
	if err != nil {
		t.Fatalf("PlanParts(hotel): %v", err)
	}
	if !changed {
		t.Fatal("PlanParts reported no change, want changed=true")
	}

	parts, err := st.PartsByPlan(ctx, hotelPlan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	p := parts[0]
	if p.StartLat == nil || *p.StartLat != londonLat {
		t.Errorf("StartLat = %v, want %v", p.StartLat, londonLat)
	}
	if p.StartTZ != "Europe/London" {
		t.Errorf("StartTZ = %q, want Europe/London", p.StartTZ)
	}
	// The checkout (EndsAt, no own coords) inherits the start zone.
	if p.EndTZ != "Europe/London" {
		t.Errorf("EndTZ = %q, want inherited Europe/London", p.EndTZ)
	}

	// The pinned part is untouched: still no coordinates.
	pinnedParts, err = st.PartsByPlan(ctx, pinnedPlan.ID)
	if err != nil {
		t.Fatalf("PartsByPlan(pinned) reread: %v", err)
	}
	if _, err := PlanParts(ctx, st, testResolver(g, nil), pinnedPlan.ID); err != nil {
		t.Fatalf("PlanParts(pinned): %v", err)
	}
	pinnedParts, err = st.PartsByPlan(ctx, pinnedPlan.ID)
	if err != nil {
		t.Fatalf("PartsByPlan(pinned) reread2: %v", err)
	}
	if pinnedParts[0].StartLat != nil {
		t.Errorf("pinned part was geocoded over: StartLat = %v", pinnedParts[0].StartLat)
	}
}

// TestPlanParts_NoChange covers the path where nothing needs filling: a part
// that already has coordinates and a tz yields no update (changed=false).
func TestPlanParts_NoChange(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	st := store.New(pool)
	ctx := context.Background()
	owner := testsupport.InsertUser(t, pool, "planparts-nochange", false, true)
	trip := mkTripDB(t, pool, owner)

	starts := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	plan, err := st.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip, Type: "dining", Title: "Test Restaurant",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt:   starts,
			StartTZ:    "Europe/London",
			StartLabel: "Test Restaurant",
			StartLat:   fptr(londonLat), StartLon: fptr(londonLon),
			Dining:        &store.DiningDetail{},
			StartTZSource: "confirmed",
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	g := stubGeo{resolves: map[string][2]float64{}}
	changed, err := PlanParts(ctx, st, testResolver(g, nil), plan.ID)
	if err != nil {
		t.Fatalf("PlanParts: %v", err)
	}
	if changed {
		t.Error("PlanParts reported a change for an already-complete part")
	}
}

// TestPlanParts_PartsError surfaces a store error (a non-existent plan still
// returns no parts and no error, so we exercise the error path by closing the
// pool to force PartsByPlan to fail).
func TestPlanParts_PartsError(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	st := store.New(pool)
	pool.Close() // subsequent queries fail.
	g := stubGeo{resolves: map[string][2]float64{}}
	if _, err := PlanParts(context.Background(), st, testResolver(g, nil), 1); err == nil {
		t.Error("expected an error from PartsByPlan on a closed pool")
	}
}

// TestPlanParts_HomeSubstitution: when the plan owner has pinned home
// coordinates, an endpoint whose address matches their home address is filled
// from the pin (and flagged pinned) instead of being geocoded — even when the
// stored address differs in case/spacing/trailing punctuation.
func TestPlanParts_HomeSubstitution(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	st := store.New(pool)
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "home-owner", false, true)
	homeAddr := "Honeysuckle Cottage, Exampleton, ZZ9 9ZZ"
	hlat, hlon := 51.507, -0.128
	if _, err := st.UpdateUser(ctx, owner, store.UpdateUserPayload{
		HomeAddress: &homeAddr, SetHome: true, HomeLat: &hlat, HomeLon: &hlon,
	}); err != nil {
		t.Fatalf("pin home: %v", err)
	}

	trip := mkTripDB(t, pool, owner)
	starts := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	plan, err := st.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip, Type: "ground", Title: "Home to Airport",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt:   starts,
			StartLabel: "Home",
			// Differs from the saved home address in case, spacing and trailing
			// punctuation, to prove normalisation matches it.
			StartAddress:  "  honeysuckle cottage,  exampleton, zz9 9zz.  ",
			StartTZSource: "floating",
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// A geocoder that would send the home address to the WRONG place, proving the
	// pin is used instead of geocoding.
	g := stubGeo{resolves: map[string][2]float64{
		"honeysuckle cottage,  exampleton, zz9 9zz.": {99, 99},
	}}
	if _, err := PlanParts(ctx, st, testResolver(g, nil), plan.ID); err != nil {
		t.Fatalf("PlanParts: %v", err)
	}
	parts, err := st.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	p := parts[0]
	if p.StartLat == nil || *p.StartLat != hlat || p.StartLon == nil || *p.StartLon != hlon {
		t.Errorf("start coords = (%v,%v), want home (%v,%v)", p.StartLat, p.StartLon, hlat, hlon)
	}
	if !p.StartCoordsPinned {
		t.Error("home substitution should mark the endpoint pinned")
	}
}

// TestPlanParts_FlightOwnAirports: the post-save run routes a flight through
// ResolveFlightTZ, so a floating flight with coordinates on its departure only
// has each end anchored at its own airport (LHR, then JFK) rather than the
// arrival taking the departure's zone. A failed flight-detail lookup skips
// the part rather than falling back to the coordinate pass.
func TestPlanParts_FlightOwnAirports(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	st := store.New(pool)
	ctx := context.Background()
	owner := testsupport.InsertUser(t, pool, "planparts-flight", false, true)
	trip := mkTripDB(t, pool, owner)

	out := time.Date(2026, 6, 1, 18, 50, 0, 0, time.UTC)
	in := out.Add(8 * time.Hour)
	mk := func() int64 {
		t.Helper()
		plan, err := st.CreatePlan(ctx, store.CreatePlanPayload{
			TripID: trip, Type: "flight", Title: "XX100",
			Parts: []store.CreatePlanPartPayload{{
				StartsAt: out, EndsAt: &in,
				StartTZSource: "floating", EndTZSource: "floating",
				StartLabel: "LHR", EndLabel: "JFK",
				StartLat: fptr(londonLat), StartLon: fptr(londonLon),
				Flight: &store.FlightDetail{
					Ident: "XX100", ScheduledOut: out, ScheduledIn: in,
					OriginIATA: "LHR", DestIATA: "JFK",
				},
			}},
		}, owner)
		if err != nil {
			t.Fatalf("CreatePlan: %v", err)
		}
		return plan.ID
	}
	planID := mk()
	g := stubGeo{resolves: map[string][2]float64{}}
	changed, err := PlanParts(ctx, st, testResolver(g, nil), planID)
	if err != nil || !changed {
		t.Fatalf("PlanParts = %v, %v; want changed", changed, err)
	}
	parts, _ := st.PartsByPlan(ctx, planID)
	p := parts[0]
	if p.StartTZ != "Europe/London" || p.EndTZ != "America/New_York" ||
		p.StartTZSource != "confirmed" || p.EndTZSource != "confirmed" {
		t.Errorf("zones = %q/%q %q/%q, want each airport's zone confirmed",
			p.StartTZ, p.EndTZ, p.StartTZSource, p.EndTZSource)
	}
	if !p.StartsAt.Equal(out.Add(-time.Hour)) || p.EndsAt == nil || !p.EndsAt.Equal(in.Add(4*time.Hour)) {
		t.Errorf("anchored to %v/%v, want 17:50Z/06:50Z", p.StartsAt, p.EndsAt)
	}

	// Break the detail lookup: the part is skipped, not given a coordinate zone.
	broken := mk()
	if _, err := pool.Exec(ctx, `ALTER TABLE flight_details DROP COLUMN aircraft_type`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if changed, err := PlanParts(ctx, st, testResolver(g, nil), broken); err != nil || changed {
		t.Errorf("PlanParts with a failing detail lookup = %v, %v; want unchanged", changed, err)
	}
	parts, _ = st.PartsByPlan(ctx, broken)
	if parts[0].EndTZ != "" || parts[0].EndTZSource != "floating" {
		t.Errorf("arrival given %q/%q, want left floating", parts[0].EndTZ, parts[0].EndTZSource)
	}
}

// TestSyncFlightScheduleWrites: an anchored part's times are copied to the
// schedule, and an arrival before the departure (an off-table arrival left as
// digits behind an anchored departure) carries the old block time instead.
func TestSyncFlightScheduleWrites(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	st := store.New(pool)
	ctx := context.Background()
	owner := testsupport.InsertUser(t, pool, "planparts-sched", false, true)
	trip := mkTripDB(t, pool, owner)
	digits := time.Date(2026, 6, 1, 18, 50, 0, 0, time.UTC)
	block := 8 * time.Hour
	arr := digits.Add(block)
	plan, err := st.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "XX100",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: digits, EndsAt: &arr, StartTZSource: "floating", EndTZSource: "floating",
			StartLabel: "JFK", EndLabel: "ZZZ",
			Flight: &store.FlightDetail{
				Ident: "XX100", ScheduledOut: digits, ScheduledIn: arr, OriginIATA: "JFK", DestIATA: "ZZZ",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, _ := st.PartsByPlan(ctx, plan.ID)
	fd, err := st.FlightDetailFor(ctx, parts[0].ID)
	if err != nil || fd == nil {
		t.Fatalf("FlightDetailFor: %v", err)
	}
	// The departure is anchored in New York (22:50Z) and the arrival, which
	// could not be placed, is written as an instant before it.
	anchored := digits.Add(4 * time.Hour)
	early := digits.Add(-16 * time.Hour)
	updated := *parts[0]
	updated.StartsAt, updated.EndsAt = anchored, &early
	if err := SyncFlightSchedule(ctx, st, fd, store.UpdatePlanPartPayload{StartsAt: &anchored}, &updated); err != nil {
		t.Fatalf("SyncFlightSchedule: %v", err)
	}
	got, _ := st.FlightDetailFor(ctx, parts[0].ID)
	if !got.ScheduledOut.Equal(anchored) || !got.ScheduledIn.Equal(anchored.Add(block)) {
		t.Errorf("schedule = %v/%v, want %v/%v", got.ScheduledOut, got.ScheduledIn, anchored, anchored.Add(block))
	}
}
