package store

import (
	"errors"
	"testing"
	"time"
)

// insertAlert seeds a flight_alerts row for a part so link/split can be checked
// to repoint its denormalised plan_id (the table has no FK).
func insertAlert(t *testing.T, s *Store, userID, partID, planID, tripID int64) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO flight_alerts (user_id, plan_part_id, plan_id, trip_id, ident, kind, status, message)
		VALUES ($1, $2, $3, $4, 'AY1', 'delayed', 'Delayed', 'msg') RETURNING id`,
		userID, partID, planID, tripID).Scan(&id); err != nil {
		t.Fatalf("insert alert: %v", err)
	}
	return id
}

func alertPlanID(t *testing.T, s *Store, alertID int64) int64 {
	t.Helper()
	var pid int64
	if err := s.pool.QueryRow(ctx, `SELECT plan_id FROM flight_alerts WHERE id = $1`, alertID).Scan(&pid); err != nil {
		t.Fatalf("read alert plan_id: %v", err)
	}
	return pid
}

func TestLinkPlans(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	primary := mkTypedPlan(t, s, trip, owner, "flight", "AY1", "PNR1", "")
	p1 := mkPart(t, s, primary, t0, nil, "", "", "LHR")
	absorbed := mkTypedPlan(t, s, trip, owner, "flight", "AY2", "PNR1", "")
	p2 := mkPart(t, s, absorbed, t0.Add(3*time.Hour), nil, "", "", "HEL")

	// Alerts on both parts; the absorbed one must be repointed to the primary.
	a1 := insertAlert(t, s, owner, p1, primary, trip)
	a2 := insertAlert(t, s, owner, p2, absorbed, trip)

	if err := s.LinkPlans(ctx, primary, []int64{absorbed}); err != nil {
		t.Fatalf("LinkPlans: %v", err)
	}

	parts, err := s.PartsByPlan(ctx, primary)
	if err != nil {
		t.Fatalf("PartsByPlan: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("want 2 parts on primary, got %d", len(parts))
	}
	if parts[0].Seq != 0 || parts[1].Seq != 1 {
		t.Fatalf("parts not re-sequenced: %d, %d", parts[0].Seq, parts[1].Seq)
	}
	if parts[0].ID != p1 || parts[1].ID != p2 {
		t.Fatalf("parts not ordered by start: %d, %d", parts[0].ID, parts[1].ID)
	}
	if _, err := s.PlanByID(ctx, absorbed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absorbed plan should be deleted, got %v", err)
	}
	if got := alertPlanID(t, s, a2); got != primary {
		t.Fatalf("absorbed alert plan_id = %d, want %d", got, primary)
	}
	if got := alertPlanID(t, s, a1); got != primary {
		t.Fatalf("primary alert plan_id = %d, want %d", got, primary)
	}
}

// TestLinkPlansGround covers the linkable type added beyond flight/train:
// ground transport (e.g. a transfer booking with a pickup and a drop-off leg).
func TestLinkPlansGround(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	primary := mkTypedPlan(t, s, trip, owner, "ground", "Pickup", "REF1", "")
	mkPart(t, s, primary, t0, nil, "", "", "Hotel")
	absorbed := mkTypedPlan(t, s, trip, owner, "ground", "Drop-off", "REF1", "")
	mkPart(t, s, absorbed, t0.Add(72*time.Hour), nil, "", "", "Airport")

	if err := s.LinkPlans(ctx, primary, []int64{absorbed}); err != nil {
		t.Fatalf("LinkPlans: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, primary)
	if err != nil {
		t.Fatalf("PartsByPlan: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("want 2 parts on primary, got %d", len(parts))
	}
	if _, err := s.PlanByID(ctx, absorbed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absorbed plan should be deleted, got %v", err)
	}

	// And the moved leg can be split back out into its own ground plan.
	newID, parentID, err := s.SplitPlanPart(ctx, parts[1].ID)
	if err != nil {
		t.Fatalf("SplitPlanPart: %v", err)
	}
	if parentID != primary {
		t.Fatalf("parent = %d, want %d", parentID, primary)
	}
	np, err := s.PlanByID(ctx, newID)
	if err != nil {
		t.Fatalf("PlanByID(new): %v", err)
	}
	if np.Type != "ground" {
		t.Fatalf("split plan type = %q, want ground", np.Type)
	}
}

func TestLinkPlansRejects(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	other := mkTrip(t, s, owner)

	flight := mkTypedPlan(t, s, trip, owner, "flight", "AY1", "", "")
	hotel := mkTypedPlan(t, s, trip, owner, "hotel", "Hotel", "", "")
	crossTrip := mkTypedPlan(t, s, other, owner, "flight", "AY9", "", "")
	wrongType := mkTypedPlan(t, s, trip, owner, "train", "TR1", "", "")

	cases := []struct {
		name    string
		primary int64
		absorb  []int64
	}{
		{"empty", flight, nil},
		{"self", flight, []int64{flight}},
		{"non-linkable primary", hotel, []int64{flight}},
		{"cross trip", flight, []int64{crossTrip}},
		{"cross type", flight, []int64{wrongType}},
		{"missing id", flight, []int64{99999}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.LinkPlans(ctx, tc.primary, tc.absorb); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestSplitPlanPart(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	viewer := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, viewer, "viewer")
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	plan := mkTypedPlan(t, s, trip, owner, "flight", "AY1", "PNR1", "notes")
	mkPart(t, s, plan, t0, nil, "", "", "LHR")
	leg2 := mkPart(t, s, plan, t0.Add(3*time.Hour), nil, "", "", "HEL")
	setVisibility(t, s, plan, "only_visible_to", viewer)
	addPlanPassenger(t, s, plan, viewer)
	alert := insertAlert(t, s, viewer, leg2, plan, trip)

	newID, parentID, err := s.SplitPlanPart(ctx, leg2)
	if err != nil {
		t.Fatalf("SplitPlanPart: %v", err)
	}
	if parentID != plan {
		t.Fatalf("parent = %d, want %d", parentID, plan)
	}

	parentParts, _ := s.PartsByPlan(ctx, parentID)
	if len(parentParts) != 1 {
		t.Fatalf("parent should keep 1 part, got %d", len(parentParts))
	}
	newParts, _ := s.PartsByPlan(ctx, newID)
	if len(newParts) != 1 || newParts[0].ID != leg2 {
		t.Fatalf("split plan should hold the moved leg, got %+v", newParts)
	}

	np, err := s.PlanByID(ctx, newID)
	if err != nil {
		t.Fatalf("PlanByID(new): %v", err)
	}
	if np.Type != "flight" || np.ConfirmationRef != "PNR1" {
		t.Fatalf("new plan identity not copied: %+v", np)
	}

	// Visibility and passengers must be copied so the audience is unchanged.
	vis, err := s.PlanVisibilityFor(ctx, newID)
	if err != nil {
		t.Fatalf("PlanVisibilityFor: %v", err)
	}
	if vis == nil || vis.Mode != "only_visible_to" || len(vis.UserIDs) != 1 || vis.UserIDs[0] != viewer {
		t.Fatalf("visibility not copied to split plan: %+v", vis)
	}
	pax, _ := s.PassengersByPlan(ctx, []int64{newID})
	if len(pax[newID]) != 1 || pax[newID][0] != viewer {
		t.Fatalf("passengers not copied to split plan: %+v", pax[newID])
	}
	if got := alertPlanID(t, s, alert); got != newID {
		t.Fatalf("moved-part alert plan_id = %d, want %d", got, newID)
	}
}

func TestSplitPlanPartRejectsSinglePart(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	plan := mkTypedPlan(t, s, trip, owner, "flight", "AY1", "", "")
	only := mkPart(t, s, plan, t0, nil, "", "", "LHR")
	if _, _, err := s.SplitPlanPart(ctx, only); !errors.Is(err, ErrNotSplittable) {
		t.Fatalf("want ErrNotSplittable for single-part plan, got %v", err)
	}

	hotel := mkTypedPlan(t, s, trip, owner, "hotel", "Hotel", "", "")
	hp1 := mkPart(t, s, hotel, t0, nil, "", "", "A")
	mkPart(t, s, hotel, t0.Add(time.Hour), nil, "", "", "B")
	if _, _, err := s.SplitPlanPart(ctx, hp1); !errors.Is(err, ErrNotSplittable) {
		t.Fatalf("want ErrNotSplittable for non-linkable type, got %v", err)
	}
}
