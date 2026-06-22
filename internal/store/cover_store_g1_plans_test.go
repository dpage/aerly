package store

import (
	"testing"
	"time"
)

func g1ptr[T any](v T) *T { return &v }

// TestG1PlanHelperMethods covers the pure helpers: EffectiveAt, EffectiveOut,
// EffectiveIn, coalesceTime, and IsEmpty.
func TestG1PlanHelperMethods(t *testing.T) {
	now := time.Now()
	part := &PlanPart{StartsAt: now}
	if !part.EffectiveAt().Equal(now) {
		t.Fatalf("EffectiveAt should be StartsAt")
	}

	sched := now
	est := now.Add(time.Hour)
	act := now.Add(2 * time.Hour)

	// Scheduled-only: falls through to scheduled.
	d := &FlightDetail{ScheduledOut: sched, ScheduledIn: sched}
	if !d.EffectiveOut().Equal(sched) || !d.EffectiveIn().Equal(sched) {
		t.Fatalf("scheduled-only should use scheduled times")
	}
	// Estimated present: prefers estimated.
	d.EstimatedOut, d.EstimatedIn = &est, &est
	if !d.EffectiveOut().Equal(est) || !d.EffectiveIn().Equal(est) {
		t.Fatalf("should prefer estimated")
	}
	// Actual present: prefers actual.
	d.ActualOut, d.ActualIn = &act, &act
	if !d.EffectiveOut().Equal(act) || !d.EffectiveIn().Equal(act) {
		t.Fatalf("should prefer actual")
	}

	if !(UpdatePlanPartPayload{}).IsEmpty() {
		t.Fatalf("empty payload should report IsEmpty")
	}
	if (UpdatePlanPartPayload{Status: g1ptr("confirmed")}).IsEmpty() {
		t.Fatalf("payload with a field set should not be empty")
	}
}

// TestG1CoordHelpers covers coordOp and derefF directly (exercised here because
// not every branch is reached via UpdateFlightPartRoute alone).
func TestG1CoordHelpers(t *testing.T) {
	if coordOp(nil, nil, true) != 2 {
		t.Fatalf("clear should be op 2")
	}
	lat, lon := 1.0, 2.0
	if coordOp(&lat, &lon, false) != 1 {
		t.Fatalf("set should be op 1")
	}
	if coordOp(nil, nil, false) != 0 {
		t.Fatalf("leave should be op 0")
	}
	if derefF(nil) != 0 {
		t.Fatalf("derefF(nil) should be 0")
	}
	if derefF(&lat) != 1.0 {
		t.Fatalf("derefF should deref")
	}
}

// TestG1CreatePlanAllTypes drives CreatePlan through every satellite branch of
// insertDetailTx (flight/hotel/train/ground/dining/excursion/ice_cream/meeting/
// event) and verifies each detail loader reads the row back.
func TestG1CreatePlanAllTypes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()

	mkPart := func(extra CreatePlanPartPayload) CreatePlanPartPayload {
		extra.Seq = 0
		extra.StartsAt = now
		extra.EndsAt = g1ptr(now.Add(time.Hour))
		return extra
	}

	// flight (with a non-nil detail, exercising the populated branch).
	flightPlan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "Test flight",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Flight: &FlightDetail{Ident: "TST100", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
				OriginIATA: "LHR", DestIATA: "JFK", AircraftType: "Boeing 777", Resolved: true},
		})},
	}, owner)
	if err != nil {
		t.Fatalf("create flight plan: %v", err)
	}
	fparts, _ := s.PartsByPlan(ctx, flightPlan.ID)
	if fd, err := s.FlightDetailFor(ctx, fparts[0].ID); err != nil || fd == nil || fd.Ident != "TST100" {
		t.Fatalf("flight detail not stored: %+v %v", fd, err)
	}

	// hotel (nil detail → default branch).
	hotelPlan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Test hotel",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Hotel: &HotelDetail{PropertyName: "Test Hotel", Address: "1 Example St",
				Phone: "555-0100", RoomType: "Double", Guests: g1ptr(2),
				StandardCheckin: g1ptr("15:00"), StandardCheckout: g1ptr("11:00")},
		})},
	}, owner)
	if err != nil {
		t.Fatalf("create hotel plan: %v", err)
	}
	hparts, _ := s.PartsByPlan(ctx, hotelPlan.ID)
	hd, err := s.HotelDetailFor(ctx, hparts[0].ID)
	if err != nil || hd == nil || hd.PropertyName != "Test Hotel" {
		t.Fatalf("hotel detail not stored: %+v %v", hd, err)
	}
	if hd.StandardCheckin == nil || *hd.StandardCheckin != "15:00" {
		t.Fatalf("standard checkin not formatted: %+v", hd.StandardCheckin)
	}

	// train.
	trainPlan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "train", Title: "Test train",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Train: &TrainDetail{Operator: "Eurostar", ServiceNo: "9004", Coach: "12", Seat: "34", Class: "Std", Platform: "5"},
		})},
	}, owner)
	tparts, _ := s.PartsByPlan(ctx, trainPlan.ID)
	if td, err := s.TrainDetailFor(ctx, tparts[0].ID); err != nil || td == nil || td.Operator != "Eurostar" {
		t.Fatalf("train detail not stored: %+v %v", td, err)
	}

	// ground.
	groundPlan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "ground", Title: "Test taxi",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Ground: &GroundDetail{Provider: "Cabs", Phone: "555-0101", Vehicle: "Saloon", Driver: "Test Driver", Pax: g1ptr(3)},
		})},
	}, owner)
	gparts, _ := s.PartsByPlan(ctx, groundPlan.ID)
	if gd, err := s.GroundDetailFor(ctx, gparts[0].ID); err != nil || gd == nil || gd.Provider != "Cabs" {
		t.Fatalf("ground detail not stored: %+v %v", gd, err)
	}

	// dining.
	diningPlan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "dining", Title: "Test dinner",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Dining: &DiningDetail{PartySize: g1ptr(4), ReservationName: "Test User", Phone: "555-0102"},
		})},
	}, owner)
	dparts, _ := s.PartsByPlan(ctx, diningPlan.ID)
	if dd, err := s.DiningDetailFor(ctx, dparts[0].ID); err != nil || dd == nil || dd.ReservationName != "Test User" {
		t.Fatalf("dining detail not stored: %+v %v", dd, err)
	}

	// excursion.
	excPlan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "excursion", Title: "Test tour",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Excursion: &ExcursionDetail{Provider: "Tours Ltd", TicketCount: g1ptr(2)},
		})},
	}, owner)
	eparts, _ := s.PartsByPlan(ctx, excPlan.ID)
	if ed, err := s.ExcursionDetailFor(ctx, eparts[0].ID); err != nil || ed == nil || ed.Provider != "Tours Ltd" {
		t.Fatalf("excursion detail not stored: %+v %v", ed, err)
	}

	// ice_cream.
	icPlan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "ice_cream", Title: "Test gelato",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			IceCream: &IceCreamDetail{Rating: 5, WhatOrdered: "Pistachio"},
		})},
	}, owner)
	icparts, _ := s.PartsByPlan(ctx, icPlan.ID)
	if id, err := s.IceCreamDetailFor(ctx, icparts[0].ID); err != nil || id == nil || id.Rating != 5 {
		t.Fatalf("ice cream detail not stored: %+v %v", id, err)
	}

	// meeting.
	meetPlan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "meeting", Title: "Test standup",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Meeting: &MeetingDetail{Location: "Room 1", Organiser: "Test User", Platform: "Zoom"},
		})},
	}, owner)
	mparts, _ := s.PartsByPlan(ctx, meetPlan.ID)
	if md, err := s.MeetingDetailFor(ctx, mparts[0].ID); err != nil || md == nil || md.Platform != "Zoom" {
		t.Fatalf("meeting detail not stored: %+v %v", md, err)
	}

	// event.
	evPlan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "event", Title: "Test gig",
		Parts: []CreatePlanPartPayload{mkPart(CreatePlanPartPayload{
			Event: &EventDetail{Performer: "The Band", Category: "Concert", VenueArea: "Stage A", URL: "https://example.com/gig"},
		})},
	}, owner)
	evparts, _ := s.PartsByPlan(ctx, evPlan.ID)
	if evd, err := s.EventDetailFor(ctx, evparts[0].ID); err != nil || evd == nil || evd.Performer != "The Band" {
		t.Fatalf("event detail not stored: %+v %v", evd, err)
	}
}

// TestG1CreatePlanNilDetails exercises the nil-detail default branches of
// insertDetailTx for each type (the d == nil → &XDetail{} path) plus the
// unknown-type error.
func TestG1CreatePlanNilDetails(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()
	for _, typ := range []string{"flight", "hotel", "train", "ground", "dining", "excursion", "ice_cream", "meeting", "event"} {
		if _, err := s.CreatePlan(ctx, CreatePlanPayload{
			TripID: trip, Type: typ, Title: "Nil " + typ,
			Parts: []CreatePlanPartPayload{{Seq: 0, StartsAt: now}},
		}, owner); err != nil {
			t.Fatalf("create %s with nil detail: %v", typ, err)
		}
	}
	if _, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "spaceship", Title: "Nope",
		Parts: []CreatePlanPartPayload{{Seq: 0, StartsAt: now}},
	}, owner); err == nil {
		t.Fatalf("unknown plan type should error")
	}
}

// TestG1PlanExistsByTripItUID covers both the present and absent paths.
func TestG1PlanExistsByTripItUID(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()

	if _, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "UID hotel", TripItUID: "uid-g1-123",
		Parts: []CreatePlanPartPayload{{Seq: 0, StartsAt: now}},
	}, owner); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	exists, err := s.PlanExistsByTripItUID(ctx, trip, "uid-g1-123")
	if err != nil || !exists {
		t.Fatalf("expected existing uid, got %v %v", exists, err)
	}
	exists, err = s.PlanExistsByTripItUID(ctx, trip, "uid-not-there")
	if err != nil || exists {
		t.Fatalf("expected absent uid, got %v %v", exists, err)
	}
}

// TestG1TripPartDwells covers the dwell aggregation: a spanned part with both
// endpoints, plus a no-coords part that is skipped.
func TestG1TripPartDwells(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()

	plan := mkPlanType(t, s, trip, owner, "hotel")
	// Part with start + end coords and a positive span.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_lat, start_lon, end_lat, end_lon, status)
		VALUES ($1, $2, $3, 48.85, 2.35, 48.86, 2.36, 'confirmed')`,
		plan, now, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("insert spanned part: %v", err)
	}
	// Part with no coords → skipped.
	addPlanPart(t, s, plan, now.Add(3*time.Hour))

	dwells, err := s.TripPartDwells(ctx, trip)
	if err != nil {
		t.Fatalf("TripPartDwells: %v", err)
	}
	if len(dwells) != 1 {
		t.Fatalf("expected 1 dwell (coords-bearing only), got %d: %+v", len(dwells), dwells)
	}
	if dwells[0].Seconds <= 0 || len(dwells[0].Coords) != 2 {
		t.Fatalf("dwell should carry a span and two coords: %+v", dwells[0])
	}
}

// TestG1PartsNeedingTZ covers the candidate query for tz anchoring.
func TestG1PartsNeedingTZ(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()
	plan := mkPlanType(t, s, trip, owner, "hotel")

	var partID int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, start_lat, start_lon, start_tz, status)
		VALUES ($1, $2, 48.85, 2.35, '', 'confirmed') RETURNING id`,
		plan, now).Scan(&partID); err != nil {
		t.Fatalf("insert part needing tz: %v", err)
	}
	parts, err := s.PartsNeedingTZ(ctx)
	if err != nil {
		t.Fatalf("PartsNeedingTZ: %v", err)
	}
	var found bool
	for _, p := range parts {
		if p.ID == partID {
			found = true
		}
	}
	if !found {
		t.Fatalf("part %d needing tz not returned", partID)
	}
}

// TestG1PlanIDsNeedingGeocode covers the geocode-candidate query.
func TestG1PlanIDsNeedingGeocode(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()
	plan := mkPlanType(t, s, trip, owner, "ground")

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, start_address, status)
		VALUES ($1, $2, '10 Example Avenue', 'confirmed')`,
		plan, now); err != nil {
		t.Fatalf("insert geocode part: %v", err)
	}
	ids, err := s.PlanIDsNeedingGeocode(ctx)
	if err != nil {
		t.Fatalf("PlanIDsNeedingGeocode: %v", err)
	}
	if !containsID(ids, plan) {
		t.Fatalf("plan %d needing geocode not returned: %+v", plan, ids)
	}
}

// TestG1PlanIDForPart covers the lookup and the ErrNotFound branch.
func TestG1PlanIDForPart(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	part := addPlanPart(t, s, plan, time.Now())

	gotPlan, gotTrip, err := s.PlanIDForPart(ctx, part)
	if err != nil || gotPlan != plan || gotTrip != trip {
		t.Fatalf("PlanIDForPart: got %d/%d err %v, want %d/%d", gotPlan, gotTrip, err, plan, trip)
	}
	if _, _, err := s.PlanIDForPart(ctx, 999999999); err != ErrNotFound {
		t.Fatalf("missing part should be ErrNotFound, got %v", err)
	}
}

// TestG1ListUnresolvedFlightParts covers the relabel-backfill candidate query.
func TestG1ListUnresolvedFlightParts(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	now := time.Now()
	// mkFlightPart seeds with resolved defaulting to false.
	part := mkFlightPart(t, s, owner, "UNRES1", now.Add(time.Hour), now.Add(2*time.Hour))

	rows, err := s.ListUnresolvedFlightParts(ctx)
	if err != nil {
		t.Fatalf("ListUnresolvedFlightParts: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.PartID == part {
			found = true
			if r.Ident != "UNRES1" {
				t.Fatalf("ident not populated: %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("unresolved part %d not returned", part)
	}
}

// TestG1UpdateFlightPartRoute covers the cross-table flight route edit (set,
// clear, and ErrNotFound branches).
func TestG1UpdateFlightPartRoute(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	now := time.Now()
	part := mkFlightPart(t, s, owner, "RT1", now.Add(time.Hour), now.Add(2*time.Hour))

	// Set new coords + identity fields.
	err := s.UpdateFlightPartRoute(ctx, part, FlightRouteUpdate{
		Ident: g1ptr("RT1X"), OriginIATA: g1ptr("CDG"), DestIATA: g1ptr("LAX"),
		Resolved: g1ptr(true), StartLabel: g1ptr("Paris"), EndLabel: g1ptr("Los Angeles"),
		StartTZ: g1ptr("Europe/Paris"), EndTZ: g1ptr("America/Los_Angeles"),
		StartsAt: g1ptr(now.Add(90 * time.Minute)), EndsAt: g1ptr(now.Add(3 * time.Hour)),
		StartLat: g1ptr(49.0), StartLon: g1ptr(2.5),
		EndLat: g1ptr(33.9), EndLon: g1ptr(-118.4),
	})
	if err != nil {
		t.Fatalf("UpdateFlightPartRoute (set): %v", err)
	}
	fd, _ := s.FlightDetailFor(ctx, part)
	if fd.Ident != "RT1X" || fd.OriginIATA != "CDG" || !fd.Resolved {
		t.Fatalf("flight_details not updated: %+v", fd)
	}
	pp, _ := s.PlanPartByID(ctx, part)
	if pp.StartLabel != "Paris" || pp.StartLat == nil || *pp.StartLat != 49.0 {
		t.Fatalf("plan_part not updated: %+v", pp)
	}

	// Clear coords.
	if err := s.UpdateFlightPartRoute(ctx, part, FlightRouteUpdate{
		ClearStartCoords: true, ClearEndCoords: true,
	}); err != nil {
		t.Fatalf("UpdateFlightPartRoute (clear): %v", err)
	}
	pp, _ = s.PlanPartByID(ctx, part)
	if pp.StartLat != nil || pp.EndLat != nil {
		t.Fatalf("coords should be cleared: %+v", pp)
	}

	// Missing part → ErrNotFound (flight_details update affects 0 rows).
	if err := s.UpdateFlightPartRoute(ctx, 999999999, FlightRouteUpdate{Ident: g1ptr("X")}); err != ErrNotFound {
		t.Fatalf("missing part should be ErrNotFound, got %v", err)
	}
}

// TestG1DetailUpdaters covers each per-type detail updater's upsert path (both
// the insert-on-conflict and the COALESCE update) and the absent loaders.
func TestG1DetailUpdaters(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()

	mk := func(typ string) int64 {
		plan := mkPlanType(t, s, trip, owner, typ)
		return addPlanPart(t, s, plan, now)
	}

	// ice_cream
	ic := mk("ice_cream")
	if err := s.UpdateIceCreamDetail(ctx, ic, IceCreamDetailUpdate{Rating: g1ptr(4), WhatOrdered: g1ptr("Vanilla")}); err != nil {
		t.Fatalf("UpdateIceCreamDetail insert: %v", err)
	}
	if err := s.UpdateIceCreamDetail(ctx, ic, IceCreamDetailUpdate{Rating: g1ptr(5)}); err != nil {
		t.Fatalf("UpdateIceCreamDetail update: %v", err)
	}
	if d, _ := s.IceCreamDetailFor(ctx, ic); d == nil || d.Rating != 5 || d.WhatOrdered != "Vanilla" {
		t.Fatalf("ice cream upsert wrong: %+v", d)
	}

	// hotel
	h := mk("hotel")
	if err := s.UpdateHotelDetail(ctx, h, HotelDetailUpdate{PropertyName: g1ptr("HotelOne"), Guests: g1ptr(2)}); err != nil {
		t.Fatalf("UpdateHotelDetail insert: %v", err)
	}
	if err := s.UpdateHotelDetail(ctx, h, HotelDetailUpdate{Phone: g1ptr("555-0103")}); err != nil {
		t.Fatalf("UpdateHotelDetail update: %v", err)
	}
	if d, _ := s.HotelDetailFor(ctx, h); d == nil || d.PropertyName != "HotelOne" || d.Phone != "555-0103" {
		t.Fatalf("hotel upsert wrong: %+v", d)
	}

	// train
	tr := mk("train")
	if err := s.UpdateTrainDetail(ctx, tr, TrainDetailUpdate{Operator: g1ptr("Op"), Seat: g1ptr("1A")}); err != nil {
		t.Fatalf("UpdateTrainDetail insert: %v", err)
	}
	if err := s.UpdateTrainDetail(ctx, tr, TrainDetailUpdate{Platform: g1ptr("9")}); err != nil {
		t.Fatalf("UpdateTrainDetail update: %v", err)
	}
	if d, _ := s.TrainDetailFor(ctx, tr); d == nil || d.Operator != "Op" || d.Platform != "9" {
		t.Fatalf("train upsert wrong: %+v", d)
	}

	// ground
	gr := mk("ground")
	if err := s.UpdateGroundDetail(ctx, gr, GroundDetailUpdate{Provider: g1ptr("Prov"), Pax: g1ptr(2)}); err != nil {
		t.Fatalf("UpdateGroundDetail insert: %v", err)
	}
	if err := s.UpdateGroundDetail(ctx, gr, GroundDetailUpdate{Driver: g1ptr("Driver")}); err != nil {
		t.Fatalf("UpdateGroundDetail update: %v", err)
	}
	if d, _ := s.GroundDetailFor(ctx, gr); d == nil || d.Provider != "Prov" || d.Driver != "Driver" {
		t.Fatalf("ground upsert wrong: %+v", d)
	}

	// dining
	di := mk("dining")
	if err := s.UpdateDiningDetail(ctx, di, DiningDetailUpdate{PartySize: g1ptr(2), ReservationName: g1ptr("Test User")}); err != nil {
		t.Fatalf("UpdateDiningDetail insert: %v", err)
	}
	if err := s.UpdateDiningDetail(ctx, di, DiningDetailUpdate{Phone: g1ptr("555-0104")}); err != nil {
		t.Fatalf("UpdateDiningDetail update: %v", err)
	}
	if d, _ := s.DiningDetailFor(ctx, di); d == nil || d.ReservationName != "Test User" || d.Phone != "555-0104" {
		t.Fatalf("dining upsert wrong: %+v", d)
	}

	// excursion
	ex := mk("excursion")
	if err := s.UpdateExcursionDetail(ctx, ex, ExcursionDetailUpdate{Provider: g1ptr("ExcProv"), TicketCount: g1ptr(2)}); err != nil {
		t.Fatalf("UpdateExcursionDetail insert: %v", err)
	}
	if err := s.UpdateExcursionDetail(ctx, ex, ExcursionDetailUpdate{TicketCount: g1ptr(3)}); err != nil {
		t.Fatalf("UpdateExcursionDetail update: %v", err)
	}
	if d, _ := s.ExcursionDetailFor(ctx, ex); d == nil || d.Provider != "ExcProv" || d.TicketCount == nil || *d.TicketCount != 3 {
		t.Fatalf("excursion upsert wrong: %+v", d)
	}
}

// TestG1DetailLoadersAbsent covers the (nil, nil) no-satellite path of every
// detail loader (a part of one type asked for another type's satellite).
func TestG1DetailLoadersAbsent(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlanType(t, s, trip, owner, "hotel")
	part := addPlanPart(t, s, plan, time.Now())

	if d, err := s.FlightDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("FlightDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.HotelDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("HotelDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.TrainDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("TrainDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.GroundDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("GroundDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.DiningDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("DiningDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.ExcursionDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("ExcursionDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.IceCreamDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("IceCreamDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.MeetingDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("MeetingDetailFor absent: %+v %v", d, err)
	}
	if d, err := s.EventDetailFor(ctx, part); err != nil || d != nil {
		t.Fatalf("EventDetailFor absent: %+v %v", d, err)
	}
}

// TestG1NotFoundPaths covers the ErrNotFound / zero-rows-affected branches of
// the mutators.
func TestG1NotFoundPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	const missing = int64(999999999)
	if err := s.DeletePlan(ctx, missing); err != ErrNotFound {
		t.Fatalf("DeletePlan missing: %v", err)
	}
	if err := s.MovePlan(ctx, missing, missing); err != ErrNotFound {
		t.Fatalf("MovePlan missing: %v", err)
	}
	if err := s.DismissPlanPart(ctx, missing); err != ErrNotFound {
		t.Fatalf("DismissPlanPart missing: %v", err)
	}
	if _, err := s.UpdatePlanPart(ctx, missing, UpdatePlanPartPayload{Status: g1ptr("confirmed")}); err != ErrNotFound {
		t.Fatalf("UpdatePlanPart missing: %v", err)
	}
	if err := s.RemovePlanPassenger(ctx, missing, missing); err != ErrNotFound {
		t.Fatalf("RemovePlanPassenger missing: %v", err)
	}
	// LinkPlans / SplitPlanPart ErrNotFound on a missing primary / part.
	if err := s.LinkPlans(ctx, missing, []int64{missing - 1}); err != ErrNotFound {
		t.Fatalf("LinkPlans missing primary: %v", err)
	}
	if _, _, err := s.SplitPlanPart(ctx, missing); err != ErrNotFound {
		t.Fatalf("SplitPlanPart missing part: %v", err)
	}
}

// TestG1DismissAndUpdatePart covers the happy-path dismiss + the empty-set
// PassengersByPlan early return.
func TestG1DismissAndUpdatePart(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	part := addPlanPart(t, s, plan, time.Now())

	if err := s.DismissPlanPart(ctx, part); err != nil {
		t.Fatalf("DismissPlanPart: %v", err)
	}
	pp, err := s.PlanPartByID(ctx, part)
	if err != nil || pp.DismissedAt == nil {
		t.Fatalf("part should be dismissed: %+v %v", pp, err)
	}

	if m, err := s.PassengersByPlan(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("PassengersByPlan(nil) should be empty: %+v %v", m, err)
	}
}

// TestG1PlanQueryErrorBranches drives the cancelled-context error returns of
// the read queries so the post-Query / post-Scan error branches are exercised.
func TestG1PlanQueryErrorBranches(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	checks := []struct {
		name string
		fn   func() error
	}{
		{"PlansByTrip", func() error { _, e := s.PlansByTrip(cc, 1); return e }},
		{"TripPartDwells", func() error { _, e := s.TripPartDwells(cc, 1); return e }},
		{"PartsByPlan", func() error { _, e := s.PartsByPlan(cc, 1); return e }},
		{"PartsNeedingTZ", func() error { _, e := s.PartsNeedingTZ(cc); return e }},
		{"PlanIDsNeedingGeocode", func() error { _, e := s.PlanIDsNeedingGeocode(cc); return e }},
		{"ListUnresolvedFlightParts", func() error { _, e := s.ListUnresolvedFlightParts(cc); return e }},
		{"UpdatePlanPart", func() error { _, e := s.UpdatePlanPart(cc, 1, UpdatePlanPartPayload{Status: g1ptr("x")}); return e }},
		{"UpdateFlightPartRoute", func() error { return s.UpdateFlightPartRoute(cc, 1, FlightRouteUpdate{Ident: g1ptr("x")}) }},
		{"DismissPlanPart", func() error { return s.DismissPlanPart(cc, 1) }},
		{"DeletePlan", func() error { return s.DeletePlan(cc, 1) }},
		{"LinkPlans", func() error { return s.LinkPlans(cc, 1, []int64{2}) }},
		{"SplitPlanPart", func() error { _, _, e := s.SplitPlanPart(cc, 1); return e }},
		{"FlightDetailFor", func() error { _, e := s.FlightDetailFor(cc, 1); return e }},
		{"HotelDetailFor", func() error { _, e := s.HotelDetailFor(cc, 1); return e }},
		{"TrainDetailFor", func() error { _, e := s.TrainDetailFor(cc, 1); return e }},
		{"GroundDetailFor", func() error { _, e := s.GroundDetailFor(cc, 1); return e }},
		{"DiningDetailFor", func() error { _, e := s.DiningDetailFor(cc, 1); return e }},
		{"ExcursionDetailFor", func() error { _, e := s.ExcursionDetailFor(cc, 1); return e }},
		{"IceCreamDetailFor", func() error { _, e := s.IceCreamDetailFor(cc, 1); return e }},
		{"MeetingDetailFor", func() error { _, e := s.MeetingDetailFor(cc, 1); return e }},
		{"EventDetailFor", func() error { _, e := s.EventDetailFor(cc, 1); return e }},
		{"UpdateIceCreamDetail", func() error { return s.UpdateIceCreamDetail(cc, 1, IceCreamDetailUpdate{Rating: g1ptr(1)}) }},
		{"UpdateHotelDetail", func() error { return s.UpdateHotelDetail(cc, 1, HotelDetailUpdate{Phone: g1ptr("x")}) }},
		{"UpdateTrainDetail", func() error { return s.UpdateTrainDetail(cc, 1, TrainDetailUpdate{Seat: g1ptr("x")}) }},
		{"UpdateGroundDetail", func() error { return s.UpdateGroundDetail(cc, 1, GroundDetailUpdate{Driver: g1ptr("x")}) }},
		{"UpdateDiningDetail", func() error { return s.UpdateDiningDetail(cc, 1, DiningDetailUpdate{Phone: g1ptr("x")}) }},
		{"UpdateExcursionDetail", func() error { return s.UpdateExcursionDetail(cc, 1, ExcursionDetailUpdate{Provider: g1ptr("x")}) }},
		{"RemovePlanPassenger", func() error { return s.RemovePlanPassenger(cc, 1, 2) }},
		{"PassengersByPlan", func() error { _, e := s.PassengersByPlan(cc, []int64{1}); return e }},
		{"IsTripPassenger", func() error { _, e := s.IsTripPassenger(cc, 1, 2); return e }},
		{"PlanOwners", func() error { _, e := s.PlanOwners(cc, []int64{1}); return e }},
		{"SuppliersByPlan", func() error { _, e := s.SuppliersByPlan(cc, []int64{1}); return e }},
		{"TitlesByPlan", func() error { _, e := s.TitlesByPlan(cc, []int64{1}); return e }},
		{"TripOwnersByPlan", func() error { _, e := s.TripOwnersByPlan(cc, []int64{1}); return e }},
		{"PlanVisibilityFor", func() error { _, e := s.PlanVisibilityFor(cc, 1); return e }},
		{"CanViewPlan", func() error { _, e := s.CanViewPlan(cc, 1, 2, false); return e }},
		{"CanViewPlanSuper", func() error { _, e := s.CanViewPlan(cc, 1, 2, true); return e }},
		{"ListVisiblePlanParts", func() error { _, e := s.ListVisiblePlanParts(cc, 1, ListVisiblePlanPartsOpts{}); return e }},
		{"VisiblePlanUserIDs", func() error { _, e := s.VisiblePlanUserIDs(cc, 1); return e }},
		{"PlanExistsByTripItUID", func() error { _, e := s.PlanExistsByTripItUID(cc, 1, "x"); return e }},
	}
	for _, c := range checks {
		if err := c.fn(); err == nil {
			t.Errorf("%s with cancelled context should error", c.name)
		}
	}
}

// TestG1RemovePlanPassengerKeepsTrip covers the via_trip re-derivation branch:
// a trip-level passenger on a visible plan keeps the row (downgraded) rather
// than being deleted.
func TestG1RemovePlanPassengerKeepsTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	befriendStore(t, s, owner, partner)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	// Make partner a trip-level passenger (so the keep-branch fires), then add a
	// manual per-plan override.
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_passengers (trip_id, user_id) VALUES ($1, $2)`, trip, partner); err != nil {
		t.Fatalf("insert trip passenger: %v", err)
	}
	if err := s.AddPlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	// Remove the manual override: still a trip passenger on a visible plan, so
	// the row is kept as via_trip = true (no error, no delete).
	if err := s.RemovePlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("RemovePlanPassenger keep-branch: %v", err)
	}
	var viaTrip bool
	if err := s.pool.QueryRow(ctx,
		`SELECT via_trip FROM plan_passengers WHERE plan_id = $1 AND user_id = $2`,
		plan, partner).Scan(&viaTrip); err != nil {
		t.Fatalf("row should still exist: %v", err)
	}
	if !viaTrip {
		t.Fatalf("row should be downgraded to via_trip = true")
	}
}

// TestG1RemovePlanPassengerDeletes covers the delete-and-commit branch: a
// manual-only passenger (not a trip passenger) is removed outright.
func TestG1RemovePlanPassengerDeletes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	befriendStore(t, s, owner, partner)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	if err := s.AddPlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	// Not a trip passenger → the keep-branch UPDATE affects 0 rows and we fall
	// through to a successful DELETE + commit.
	if err := s.RemovePlanPassenger(ctx, plan, partner); err != nil {
		t.Fatalf("RemovePlanPassenger delete-branch: %v", err)
	}
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM plan_passengers WHERE plan_id = $1 AND user_id = $2`,
		plan, partner).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("passenger should be deleted, found %d rows", n)
	}
}

// TestG1SetPlanVisibilityWithMembers exercises SetPlanVisibility's member-insert
// loop and the clear (empty mode) path on an existing plan.
func TestG1SetPlanVisibilityWithMembers(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	a := mkUser(t, s)
	b := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	if err := s.SetPlanVisibility(ctx, plan, "only_visible_to", []int64{a, b}); err != nil {
		t.Fatalf("SetPlanVisibility set: %v", err)
	}
	v, err := s.PlanVisibilityFor(ctx, plan)
	if err != nil {
		t.Fatalf("PlanVisibilityFor: %v", err)
	}
	if v.Mode != "only_visible_to" || len(v.UserIDs) != 2 {
		t.Fatalf("visibility wrong: %+v", v)
	}

	// Clear it (empty mode).
	if err := s.SetPlanVisibility(ctx, plan, "", nil); err != nil {
		t.Fatalf("SetPlanVisibility clear: %v", err)
	}
	if _, err := s.PlanVisibilityFor(ctx, plan); err != ErrNotFound {
		t.Fatalf("cleared visibility should be ErrNotFound, got %v", err)
	}
}

// TestG1SplitPlanPartCopiesAudience drives SplitPlanPart with visibility +
// passenger rows present, exercising the copy-audience tx.Exec chain.
func TestG1SplitPlanPartCopiesAudience(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	befriendStore(t, s, owner, partner)
	trip := mkTrip(t, s, owner)
	now := time.Now()

	// A 2-leg flight plan with an only_visible_to restriction + a passenger.
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "Multi-leg",
		Parts: []CreatePlanPartPayload{
			{Seq: 0, StartsAt: now, EndsAt: g1ptr(now.Add(time.Hour)),
				Flight: &FlightDetail{Ident: "LEG1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour), OriginIATA: "LHR", DestIATA: "CDG"}},
			{Seq: 1, StartsAt: now.Add(2 * time.Hour), EndsAt: g1ptr(now.Add(3 * time.Hour)),
				Flight: &FlightDetail{Ident: "LEG2", ScheduledOut: now.Add(2 * time.Hour), ScheduledIn: now.Add(3 * time.Hour), OriginIATA: "CDG", DestIATA: "JFK"}},
		},
	}, owner)
	if err != nil {
		t.Fatalf("create multi-leg plan: %v", err)
	}
	if err := s.AddPlanPassenger(ctx, plan.ID, partner); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	if err := s.SetPlanVisibility(ctx, plan.ID, "only_visible_to", []int64{partner}); err != nil {
		t.Fatalf("SetPlanVisibility: %v", err)
	}

	parts, _ := s.PartsByPlan(ctx, plan.ID)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	newPlanID, parentID, err := s.SplitPlanPart(ctx, parts[1].ID)
	if err != nil {
		t.Fatalf("SplitPlanPart: %v", err)
	}
	if parentID != plan.ID || newPlanID == 0 {
		t.Fatalf("unexpected ids: new=%d parent=%d", newPlanID, parentID)
	}
	// The split-out plan must carry the same visibility + passenger.
	nv, err := s.PlanVisibilityFor(ctx, newPlanID)
	if err != nil || nv.Mode != "only_visible_to" {
		t.Fatalf("split plan visibility not copied: %+v %v", nv, err)
	}
	pax, _ := s.PassengersByPlan(ctx, []int64{newPlanID})
	if !containsID(pax[newPlanID], partner) {
		t.Fatalf("split plan passenger not copied: %+v", pax)
	}
}

// TestG1PlanLookupMaps covers PlanOwners and TitlesByPlan (including the
// empty-input early return and the empty-value omission).
func TestG1PlanLookupMaps(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	now := time.Now()

	titled, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Named Plan",
		Parts: []CreatePlanPartPayload{{Seq: 0, StartsAt: now}},
	}, owner)
	if err != nil {
		t.Fatalf("create titled plan: %v", err)
	}
	untitled := mkPlanType(t, s, trip, owner, "hotel") // empty title

	// PlanOwners
	owners, err := s.PlanOwners(ctx, []int64{titled.ID, untitled})
	if err != nil {
		t.Fatalf("PlanOwners: %v", err)
	}
	if owners[titled.ID] != owner {
		t.Fatalf("PlanOwners missing owner for %d: %+v", titled.ID, owners)
	}
	if m, _ := s.PlanOwners(ctx, nil); len(m) != 0 {
		t.Fatalf("PlanOwners(nil) should be empty")
	}

	// TitlesByPlan
	titles, err := s.TitlesByPlan(ctx, []int64{titled.ID, untitled})
	if err != nil {
		t.Fatalf("TitlesByPlan: %v", err)
	}
	if titles[titled.ID] != "Named Plan" {
		t.Fatalf("TitlesByPlan wrong: %+v", titles)
	}
	if _, ok := titles[untitled]; ok {
		t.Fatalf("empty-title plan should be omitted: %+v", titles)
	}
	if m, _ := s.TitlesByPlan(ctx, nil); len(m) != 0 {
		t.Fatalf("TitlesByPlan(nil) should be empty")
	}
}
