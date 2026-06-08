package store

import "testing"

func TestShareAllFriendsFlagsRoundTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	tripID := mkTrip(t, s, owner)
	planID := mkPlan(t, s, tripID, owner)

	if err := s.SetTripShareAllFriends(ctx, tripID, "viewer"); err != nil {
		t.Fatalf("SetTripShareAllFriends: %v", err)
	}
	tr, err := s.TripByID(ctx, tripID)
	if err != nil || tr.ShareAllFriendsRole != "viewer" {
		t.Fatalf("trip role = %q, %v; want viewer", tr.ShareAllFriendsRole, err)
	}

	if err := s.SetPlanShareAllFriends(ctx, planID, true); err != nil {
		t.Fatalf("SetPlanShareAllFriends: %v", err)
	}
	pl, err := s.PlanByID(ctx, planID)
	if err != nil || !pl.ShareAllFriends {
		t.Fatalf("plan flag = %v, %v; want true", pl.ShareAllFriends, err)
	}
}
