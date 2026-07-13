package store

import (
	"errors"
	"testing"
	"time"
)

func TestCreatePlanWritesSatellite(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "BA286",
		ConfirmationRef: "ABC123",
		Parts: []CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "LHR", EndLabel: "SFO",
			Flight: &FlightDetail{
				Ident: "BA286", ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "SFO",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.Source != "manual" {
		t.Errorf("default source = %q, want manual", plan.Source)
	}

	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	if parts[0].Type != "flight" {
		t.Errorf("part type = %q, want flight", parts[0].Type)
	}
	fd, err := s.FlightDetailFor(ctx, parts[0].ID)
	if err != nil || fd == nil {
		t.Fatalf("FlightDetailFor = %v, %v", fd, err)
	}
	if fd.Ident != "BA286" || fd.OriginIATA != "LHR" || fd.DestIATA != "SFO" {
		t.Errorf("flight detail wrong: %+v", fd)
	}
	if fd.FlightStatus != "Scheduled" {
		t.Errorf("default flight_status = %q, want Scheduled", fd.FlightStatus)
	}
	// No other satellite leaked.
	if hd, _ := s.HotelDetailFor(ctx, parts[0].ID); hd != nil {
		t.Error("flight part should not have a hotel satellite")
	}
}

func TestCreatePlanPersistsTicketAndCost(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	cost := 250.50

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "BA286",
		ConfirmationRef: "ABC123",
		TicketNumber:    "1252300000001",
		CostAmount:      &cost,
		CostCurrency:    "GBP",
		Parts: []CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "LHR", EndLabel: "SFO",
			Flight: &FlightDetail{
				Ident: "BA286", ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "SFO",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	reread, err := s.PlanByID(ctx, plan.ID)
	if err != nil {
		t.Fatalf("PlanByID: %v", err)
	}
	// Round-trips through both the RETURNING scan and a fresh read (this also
	// exercises pgx scanning a NUMERIC column into *float64).
	for _, p := range []*Plan{plan, reread} {
		if p.TicketNumber != "1252300000001" {
			t.Errorf("ticket_number = %q, want 1252300000001", p.TicketNumber)
		}
		if p.CostAmount == nil || *p.CostAmount != cost {
			t.Errorf("cost_amount = %v, want %v", p.CostAmount, cost)
		}
		if p.CostCurrency != "GBP" {
			t.Errorf("cost_currency = %q, want GBP", p.CostCurrency)
		}
	}

	// A plan with no cost reads back as nil (unknown), not zero.
	bare, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Hotel",
		Parts: []CreatePlanPartPayload{{StartsAt: out, Hotel: &HotelDetail{PropertyName: "Plaza"}}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan (bare): %v", err)
	}
	if bare.CostAmount != nil {
		t.Errorf("bare cost_amount = %v, want nil", bare.CostAmount)
	}

	// UpdatePlan sets the ticket + cost; a nil pointer leaves a field unchanged.
	newCost := 999.99
	tn, cur := "9990000000001", "USD"
	updated, err := s.UpdatePlan(ctx, bare.ID, UpdatePlanPayload{
		TicketNumber: &tn, CostAmount: &newCost, CostCurrency: &cur,
	})
	if err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if updated.TicketNumber != tn || updated.CostCurrency != cur ||
		updated.CostAmount == nil || *updated.CostAmount != newCost {
		t.Errorf("after update: ticket=%q cost=%v cur=%q", updated.TicketNumber, updated.CostAmount, updated.CostCurrency)
	}
	if updated.Title != "Hotel" {
		t.Errorf("UpdatePlan with nil Title clobbered it: %q", updated.Title)
	}
}

func TestCreatePlanPinsCoords(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	lat, lon := 51.5010, -0.1245

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "excursion", Title: "Example Tower",
		Parts: []CreatePlanPartPayload{{
			StartsAt:          out,
			StartLabel:        "Example Tower",
			StartLat:          &lat,
			StartLon:          &lon,
			StartCoordsPinned: true,
			Excursion:         &ExcursionDetail{},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	if !parts[0].StartCoordsPinned {
		t.Errorf("StartCoordsPinned = false, want true")
	}
	if parts[0].EndCoordsPinned {
		t.Errorf("EndCoordsPinned = true, want false (not set)")
	}
	if parts[0].StartLat == nil || *parts[0].StartLat != lat {
		t.Errorf("StartLat = %v, want %v", parts[0].StartLat, lat)
	}
}

func TestFlightDetailForReturnsGateAndTerminal(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "BA286",
		Parts: []CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "LHR", EndLabel: "SFO",
			Flight: &FlightDetail{
				Ident: "BA286", ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "SFO",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	// The poller fills gate/terminal post-creation; simulate that.
	if _, err := s.pool.Exec(ctx,
		`UPDATE flight_details SET origin_gate=$2, dest_gate=$3, origin_terminal=$4, dest_terminal=$5
		 WHERE plan_part_id=$1`,
		parts[0].ID, "B32", "", "5", ""); err != nil {
		t.Fatalf("set gate: %v", err)
	}

	fd, err := s.FlightDetailFor(ctx, parts[0].ID)
	if err != nil || fd == nil {
		t.Fatalf("FlightDetailFor = %v, %v", fd, err)
	}
	if fd.OriginGate != "B32" || fd.OriginTerminal != "5" {
		t.Errorf("origin gate/terminal = %q/%q, want B32/5", fd.OriginGate, fd.OriginTerminal)
	}
	if fd.DestGate != "" || fd.DestTerminal != "" {
		t.Errorf("dest gate/terminal = %q/%q, want empty", fd.DestGate, fd.DestTerminal)
	}

	// Aircraft type is backfilled only-fill-empty (like terminal) and surfaced
	// on the flight tile via FlightDetailFor.
	if err := s.BackfillFlightPart(ctx, parts[0].ID, BackfillPayload{
		AircraftType: "Boeing 777-300ER",
	}); err != nil {
		t.Fatalf("BackfillFlightPart aircraft type: %v", err)
	}
	fd, _ = s.FlightDetailFor(ctx, parts[0].ID)
	if fd.AircraftType != "Boeing 777-300ER" {
		t.Errorf("aircraft type = %q, want Boeing 777-300ER", fd.AircraftType)
	}
	// A second backfill must NOT overwrite the captured type.
	if err := s.BackfillFlightPart(ctx, parts[0].ID, BackfillPayload{
		AircraftType: "Airbus A320",
	}); err != nil {
		t.Fatalf("BackfillFlightPart aircraft type again: %v", err)
	}
	fd, _ = s.FlightDetailFor(ctx, parts[0].ID)
	if fd.AircraftType != "Boeing 777-300ER" {
		t.Errorf("aircraft type should be only-fill-empty, got %q", fd.AircraftType)
	}

	// Arrival baggage belt is updatable (overwrite-when-non-empty), like gate,
	// and surfaced on the flight tile via FlightDetailFor.
	if err := s.RefreshFlightPartBelt(ctx, parts[0].ID, "34"); err != nil {
		t.Fatalf("RefreshFlightPartBelt: %v", err)
	}
	fd, _ = s.FlightDetailFor(ctx, parts[0].ID)
	if fd.DestBaggageBelt != "34" {
		t.Errorf("baggage belt = %q, want 34", fd.DestBaggageBelt)
	}
	// A non-empty value overwrites; an empty value preserves the known belt.
	if err := s.RefreshFlightPartBelt(ctx, parts[0].ID, "12"); err != nil {
		t.Fatalf("RefreshFlightPartBelt update: %v", err)
	}
	if err := s.RefreshFlightPartBelt(ctx, parts[0].ID, ""); err != nil {
		t.Fatalf("RefreshFlightPartBelt empty: %v", err)
	}
	fd, _ = s.FlightDetailFor(ctx, parts[0].ID)
	if fd.DestBaggageBelt != "12" {
		t.Errorf("baggage belt = %q, want 12 (overwritten, empty preserves)", fd.DestBaggageBelt)
	}
}

func TestPlanPartAddressesRoundTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	start := time.Date(2026, 4, 7, 9, 0, 0, 0, time.UTC)

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "ground", Title: "Kev's taxi",
		Parts: []CreatePlanPartPayload{{
			StartsAt:     start,
			StartLabel:   "Honeysuckle Cottage",
			StartAddress: "Honeysuckle Cottage, Somewhere Lane",
			EndLabel:     "LHR T5",
			EndAddress:   "Heathrow Terminal 5, Longford TW6 2GA",
			Ground:       &GroundDetail{Provider: "Kev's taxi"},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	if parts[0].StartAddress != "Honeysuckle Cottage, Somewhere Lane" {
		t.Errorf("start_address = %q", parts[0].StartAddress)
	}
	if parts[0].EndAddress != "Heathrow Terminal 5, Longford TW6 2GA" {
		t.Errorf("end_address = %q", parts[0].EndAddress)
	}

	// And it can be edited.
	newStart := "12 Somewhere Street"
	if _, err := s.UpdatePlanPart(ctx, parts[0].ID, UpdatePlanPartPayload{StartAddress: &newStart}); err != nil {
		t.Fatalf("UpdatePlanPart: %v", err)
	}
	got, err := s.PlanPartByID(ctx, parts[0].ID)
	if err != nil {
		t.Fatalf("PlanPartByID: %v", err)
	}
	if got.StartAddress != newStart {
		t.Errorf("after edit start_address = %q, want %q", got.StartAddress, newStart)
	}
	if got.EndAddress != "Heathrow Terminal 5, Longford TW6 2GA" {
		t.Errorf("end_address should be unchanged, got %q", got.EndAddress)
	}
}

func TestCreatePlanHotelSatellite(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	ci := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	co := time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)
	hhmm := "14:00"

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Grand Hotel",
		Parts: []CreatePlanPartPayload{{
			StartsAt: ci, EndsAt: &co, StartLabel: "Grand Hotel",
			Hotel: &HotelDetail{PropertyName: "Grand Hotel", StandardCheckin: &hhmm},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, _ := s.PartsByPlan(ctx, plan.ID)
	hd, err := s.HotelDetailFor(ctx, parts[0].ID)
	if err != nil || hd == nil {
		t.Fatalf("HotelDetailFor = %v, %v", hd, err)
	}
	if hd.PropertyName != "Grand Hotel" {
		t.Errorf("property = %q", hd.PropertyName)
	}
	if hd.StandardCheckin == nil || *hd.StandardCheckin != "14:00" {
		t.Errorf("standard_checkin = %v, want 14:00", hd.StandardCheckin)
	}
}

func TestPlanCRUDEdit(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "dining", Title: "Dinner",
		Parts: []CreatePlanPartPayload{{
			StartsAt: time.Now(), Dining: &DiningDetail{ReservationName: "Page"},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	newTitle := "Late Dinner"
	upd, err := s.UpdatePlan(ctx, plan.ID, UpdatePlanPayload{Title: &newTitle})
	if err != nil || upd.Title != newTitle {
		t.Fatalf("UpdatePlan = %+v, %v", upd, err)
	}

	plans, err := s.PlansByTrip(ctx, trip)
	if err != nil || len(plans) != 1 {
		t.Fatalf("PlansByTrip = %d, %v", len(plans), err)
	}

	if err := s.DeletePlan(ctx, plan.ID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	if _, err := s.PlanByID(ctx, plan.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("PlanByID after delete = %v, want ErrNotFound", err)
	}
}

func TestPlanPartEditAndDismiss(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan, _ := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "ground", Title: "Taxi",
		Parts: []CreatePlanPartPayload{{
			StartsAt: time.Now(), Status: "planned", Ground: &GroundDetail{Provider: "Uber"},
		}},
	}, owner)
	parts, _ := s.PartsByPlan(ctx, plan.ID)
	pid := parts[0].ID

	newLabel := "Hotel lobby"
	confirmed := "confirmed"
	upd, err := s.UpdatePlanPart(ctx, pid, UpdatePlanPartPayload{
		StartLabel: &newLabel, Status: &confirmed,
	})
	if err != nil {
		t.Fatalf("UpdatePlanPart: %v", err)
	}
	if upd.StartLabel != newLabel || upd.Status != "confirmed" {
		t.Errorf("update wrong: %+v", upd)
	}

	// Dismiss drops it from non-dismissed listings.
	if err := s.DismissPlanPart(ctx, pid); err != nil {
		t.Fatalf("DismissPlanPart: %v", err)
	}
	got, _ := s.PlanPartByID(ctx, pid)
	if got.DismissedAt == nil {
		t.Error("dismissed_at should be set")
	}
	visible, _ := s.ListVisiblePlanParts(ctx, owner, ListVisiblePlanPartsOpts{TripID: trip})
	if len(visible) != 0 {
		t.Errorf("dismissed part should be hidden, got %d", len(visible))
	}
	withDismissed, _ := s.ListVisiblePlanParts(ctx, owner, ListVisiblePlanPartsOpts{TripID: trip, IncludeDismissed: true})
	if len(withDismissed) != 1 {
		t.Errorf("IncludeDismissed should show it, got %d", len(withDismissed))
	}

	if _, err := s.UpdatePlanPart(ctx, 999999, UpdatePlanPartPayload{Status: &confirmed}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing part = %v, want ErrNotFound", err)
	}
}

// TestPlanPassengerTriggerMakesViewer was removed: migration 0030 dropped the
// plan_passengers_ensure_member trigger, so adding a plan passenger no longer
// creates a trip_members viewer row — passengers are now plan-scoped. The
// passenger's plan-scoped visibility is covered by TestPlanGrantIsScoped.

func TestPlanPassengerTriggerDoesNotDemoteOwner(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	// Owner is already an owner member; adding them as a passenger must not
	// demote them to viewer.
	if err := s.AddPlanPassenger(ctx, plan, owner); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	if role, _ := s.TripRole(ctx, trip, owner); role != "owner" {
		t.Errorf("owner role after passenger add = %q, want owner (no demote)", role)
	}
}

func TestSetAndGetPlanVisibility(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	a := mkUser(t, s)
	b := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	// Default: no row.
	if _, err := s.PlanVisibilityFor(ctx, plan); !errors.Is(err, ErrNotFound) {
		t.Errorf("default visibility = %v, want ErrNotFound", err)
	}

	if err := s.SetPlanVisibility(ctx, plan, "only_visible_to", []int64{a, b}); err != nil {
		t.Fatalf("SetPlanVisibility: %v", err)
	}
	v, err := s.PlanVisibilityFor(ctx, plan)
	if err != nil {
		t.Fatalf("PlanVisibilityFor: %v", err)
	}
	if v.Mode != "only_visible_to" || len(v.UserIDs) != 2 {
		t.Errorf("visibility = %+v", v)
	}

	// Switch mode and member set (replace semantics).
	if err := s.SetPlanVisibility(ctx, plan, "hidden_from", []int64{a}); err != nil {
		t.Fatalf("SetPlanVisibility replace: %v", err)
	}
	v, _ = s.PlanVisibilityFor(ctx, plan)
	if v.Mode != "hidden_from" || len(v.UserIDs) != 1 || v.UserIDs[0] != a {
		t.Errorf("after replace = %+v", v)
	}

	// Clearing (empty mode) removes the override.
	if err := s.SetPlanVisibility(ctx, plan, "", nil); err != nil {
		t.Fatalf("clear visibility: %v", err)
	}
	if _, err := s.PlanVisibilityFor(ctx, plan); !errors.Is(err, ErrNotFound) {
		t.Errorf("after clear = %v, want ErrNotFound", err)
	}
}

// TestMovePlanRecomputesVisibility: a member who saw a default-visibility plan
// through the source trip's membership loses access after the plan is moved to
// a trip they aren't on — the trip-default grant is recomputed against the
// destination trip's membership. (A friend of the owner; only the trip_members
// gate, evaluated against the new trip, decides the trip-default tier.)
func TestMovePlanRecomputesVisibility(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	befriendStore(t, s, owner, member) // friend gate

	src := mkTrip(t, s, owner)
	dst := mkTrip(t, s, owner)
	// `member` is a member of the source trip only.
	addMember(t, s, src, member, "viewer")

	plan := mkPlan(t, s, src, owner)

	// On the source trip, member sees the default-visibility plan.
	if !mustCanView(t, s, plan, member) {
		t.Fatal("member should see default-visibility plan on source trip")
	}

	// Move to the destination trip, where member is NOT a member.
	if err := s.MovePlan(ctx, plan, dst); err != nil {
		t.Fatalf("MovePlan: %v", err)
	}
	got, _ := s.PlanByID(ctx, plan)
	if got.TripID != dst {
		t.Fatalf("plan trip_id = %d, want %d", got.TripID, dst)
	}
	// Visibility now evaluated against dst: member isn't on it → can't see it.
	if mustCanView(t, s, plan, member) {
		t.Error("after move to a trip member isn't on, the plan must be invisible to them")
	}
	// The owner of both trips still sees it.
	if !mustCanView(t, s, plan, owner) {
		t.Error("owner should still see the moved plan")
	}
}

func TestPlanQueryErrorPaths(t *testing.T) {
	s := newStore(t)
	cc := canceled()
	if _, err := s.CreatePlan(cc, CreatePlanPayload{TripID: 1, Type: "flight"}, 1); err == nil {
		t.Error("CreatePlan cancelled should error")
	}
	if _, err := s.PlanByID(cc, 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("PlanByID cancelled = %v", err)
	}
	if err := s.MovePlan(cc, 1, 2); err == nil {
		t.Error("MovePlan cancelled should error")
	}
	if err := s.SetPlanVisibility(cc, 1, "hidden_from", []int64{2}); err == nil {
		t.Error("SetPlanVisibility cancelled should error")
	}
	if err := s.AddPlanPassenger(cc, 1, 2); err == nil {
		t.Error("AddPlanPassenger cancelled should error")
	}
	// A genuine query error (not pgx.ErrNoRows) must surface as-is, not get
	// mapped to ErrNotFound.
	if _, err := s.TripCountryByPlan(cc, 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("TripCountryByPlan cancelled = %v, want a plain error", err)
	}
}

func TestTripCountryByPlan(t *testing.T) {
	s := newStore(t)
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	if err := s.SetTripCountry(ctx, trip, "cv"); err != nil {
		t.Fatalf("SetTripCountry: %v", err)
	}
	plan := mkPlan(t, s, trip, owner)

	code, err := s.TripCountryByPlan(ctx, plan)
	if err != nil {
		t.Fatalf("TripCountryByPlan: %v", err)
	}
	if code != "cv" {
		t.Errorf("country = %q, want \"cv\"", code)
	}

	// An unknown plan is ErrNotFound, not a bare empty string.
	if _, err := s.TripCountryByPlan(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("TripCountryByPlan(unknown) = %v, want ErrNotFound", err)
	}

	// A trip with no country set reads back as "", not an error.
	trip2 := mkTrip(t, s, owner)
	plan2 := mkPlan(t, s, trip2, owner)
	code2, err := s.TripCountryByPlan(ctx, plan2)
	if err != nil {
		t.Fatalf("TripCountryByPlan (no country): %v", err)
	}
	if code2 != "" {
		t.Errorf("country = %q, want \"\" (no country set)", code2)
	}
}

// TestCoalesceTimeAllNil covers the fallback branch of coalesceTime: when
// every candidate is nil (no actual, no estimated, no scheduled), it must
// return the zero time rather than panic or dereference a nil pointer.
func TestCoalesceTimeAllNil(t *testing.T) {
	if got := coalesceTime(nil, nil, nil); !got.IsZero() {
		t.Errorf("coalesceTime(nil...) = %v, want zero time", got)
	}
	if got := coalesceTime(); !got.IsZero() {
		t.Errorf("coalesceTime() = %v, want zero time", got)
	}
}

// TestListVisiblePlanPartsFiltersByType covers ListVisiblePlanPartsOpts.Type:
// a caller wanting only one plan type must not see another type's live part
// bleed through.
func TestListVisiblePlanPartsFiltersByType(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	start := time.Now()

	flight, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "BA1",
		Parts: []CreatePlanPartPayload{{StartsAt: start, Flight: &FlightDetail{Ident: "BA1", ScheduledOut: start, ScheduledIn: start}}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan flight: %v", err)
	}
	hotel, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Hotel",
		Parts: []CreatePlanPartPayload{{StartsAt: start, Hotel: &HotelDetail{PropertyName: "Grand"}}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan hotel: %v", err)
	}

	parts, err := s.ListVisiblePlanParts(ctx, owner, ListVisiblePlanPartsOpts{TripID: trip, Type: "flight"})
	if err != nil {
		t.Fatalf("ListVisiblePlanParts: %v", err)
	}
	if len(parts) != 1 || parts[0].PlanID != flight.ID {
		t.Fatalf("parts = %+v, want just the flight plan's part (%d)", parts, flight.ID)
	}
	if parts[0].PlanID == hotel.ID {
		t.Error("hotel part should not appear when filtering Type=flight")
	}
}
