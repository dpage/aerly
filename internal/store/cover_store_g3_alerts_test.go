package store

import (
	"testing"
	"time"
)

// TestG3PlanAlertOptedIn covers PlanAlertOptedIn for both the opted-in and
// not-opted-in cases (the function was wholly uncovered).
func TestG3PlanAlertOptedIn(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	viewer := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	if in, err := s.PlanAlertOptedIn(ctx, plan, viewer); err != nil || in {
		t.Fatalf("PlanAlertOptedIn before optin = %v, %v; want false, nil", in, err)
	}
	if err := s.AddPlanAlertOptin(ctx, plan, viewer); err != nil {
		t.Fatalf("AddPlanAlertOptin: %v", err)
	}
	if in, err := s.PlanAlertOptedIn(ctx, plan, viewer); err != nil || !in {
		t.Fatalf("PlanAlertOptedIn after optin = %v, %v; want true, nil", in, err)
	}
}

// TestG3FlightAlertDeletes covers DeleteFlightAlert (owner-scoped, with a
// foreign no-op) and DeleteAllFlightAlerts.
func TestG3FlightAlertDeletes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	other := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	part := mkFlightPartInTrip(t, s, trip, owner, "G3AL1",
		now, now.Add(time.Hour), "Scheduled", 51.4775, -0.4614, 40.6413, -73.7781)

	mk := func() FlightAlert {
		a, err := s.InsertFlightAlert(ctx, FlightAlert{
			UserID:     owner,
			PlanPartID: part,
			PlanID:     plan,
			TripID:     trip,
			Ident:      "G3AL1",
			Kind:       "delayed",
			Status:     "Delayed",
			Message:    "Delayed by 30 minutes",
		})
		if err != nil {
			t.Fatalf("InsertFlightAlert: %v", err)
		}
		return a
	}

	a1 := mk()
	mk()

	// A different user can't delete this user's alert (no-op, no error).
	if err := s.DeleteFlightAlert(ctx, other, a1.ID); err != nil {
		t.Fatalf("DeleteFlightAlert foreign: %v", err)
	}
	if list, _ := s.ListFlightAlerts(ctx, owner, 10); len(list) != 2 {
		t.Fatalf("after foreign delete = %d, want 2 (no-op)", len(list))
	}
	// The owner can delete their own.
	if err := s.DeleteFlightAlert(ctx, owner, a1.ID); err != nil {
		t.Fatalf("DeleteFlightAlert owner: %v", err)
	}
	if list, _ := s.ListFlightAlerts(ctx, owner, 10); len(list) != 1 {
		t.Fatalf("after owner delete = %d, want 1", len(list))
	}
	// Clear-all wipes the rest.
	if err := s.DeleteAllFlightAlerts(ctx, owner); err != nil {
		t.Fatalf("DeleteAllFlightAlerts: %v", err)
	}
	if list, _ := s.ListFlightAlerts(ctx, owner, 10); len(list) != 0 {
		t.Fatalf("after clear-all = %d, want 0", len(list))
	}
}
