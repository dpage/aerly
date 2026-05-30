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

func TestPlanPassengerTriggerMakesViewer(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	// pax is not yet a trip member.
	if _, err := s.TripRole(ctx, trip, pax); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pax should not be a member yet: %v", err)
	}
	if err := s.AddPlanPassenger(ctx, plan, pax); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	// The DB trigger should have made them a viewer.
	role, err := s.TripRole(ctx, trip, pax)
	if err != nil || role != "viewer" {
		t.Errorf("after add passenger role = %q, %v; want viewer (trigger)", role, err)
	}

	got, _ := s.PassengersByPlan(ctx, []int64{plan})
	if len(got[plan]) != 1 || got[plan][0] != pax {
		t.Errorf("PassengersByPlan = %v", got)
	}

	// Removing the passenger leaves the trip membership intact.
	if err := s.RemovePlanPassenger(ctx, plan, pax); err != nil {
		t.Fatalf("RemovePlanPassenger: %v", err)
	}
	if role, _ := s.TripRole(ctx, trip, pax); role != "viewer" {
		t.Errorf("after remove passenger role = %q, want viewer (kept)", role)
	}
}

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

// TestMovePlanRecomputesVisibility: a plan only_visible_to a user on the source
// trip becomes invisible to that user after a move to a trip they aren't on —
// the §4 predicate's trip_members gate fails against the destination trip.
func TestMovePlanRecomputesVisibility(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	named := mkUser(t, s)

	src := mkTrip(t, s, owner)
	dst := mkTrip(t, s, owner)
	// `named` is a member of the source trip only.
	addMember(t, s, src, named, "viewer")

	plan := mkPlan(t, s, src, owner)
	setVisibility(t, s, plan, "only_visible_to", named)

	// On the source trip, named can see it.
	if !mustCanView(t, s, plan, named) {
		t.Fatal("named should see only_visible_to plan on source trip")
	}

	// Move to the destination trip, where named is NOT a member.
	if err := s.MovePlan(ctx, plan, dst); err != nil {
		t.Fatalf("MovePlan: %v", err)
	}
	got, _ := s.PlanByID(ctx, plan)
	if got.TripID != dst {
		t.Fatalf("plan trip_id = %d, want %d", got.TripID, dst)
	}
	// Visibility now evaluated against dst: named isn't a member → can't see it,
	// even though the only_visible_to row still names them (it's gone inert).
	if mustCanView(t, s, plan, named) {
		t.Error("after move to a trip named isn't on, the plan must be invisible to them")
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
}
