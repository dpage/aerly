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

// befriendStore makes a and b accepted friends via the canonical request+accept
// path (a requests, b accepts).
func befriendStore(t *testing.T, s *Store, a, b int64) {
	t.Helper()
	if _, err := s.RequestFriendship(ctx, a, b, ""); err != nil {
		t.Fatalf("request friendship: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("accept friendship: %v", err)
	}
}

func canView(t *testing.T, s *Store, planID, viewer int64) bool {
	t.Helper()
	ok, err := s.CanViewPlan(ctx, planID, viewer, false)
	if err != nil {
		t.Fatalf("CanViewPlan: %v", err)
	}
	return ok
}

// TestPlanGrantIsScoped: a plan passenger sees ONLY the plan they're on, not
// the trip's other default-visible plans.
func TestPlanGrantIsScoped(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	claire := mkUser(t, s)
	befriendStore(t, s, owner, claire)
	trip := mkTrip(t, s, owner)
	flight := mkPlan(t, s, trip, owner)
	hotel := mkPlan(t, s, trip, owner)

	if err := s.AddPlanPassenger(ctx, flight, claire); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	if !canView(t, s, flight, claire) {
		t.Error("passenger should see their own plan")
	}
	if canView(t, s, hotel, claire) {
		t.Error("passenger must NOT see the trip's other default plans (plan-scoped grant)")
	}
}

// TestTripMemberSeesAllNonHidden: an accepted-friend trip member sees every
// non-restricted plan, but not one they're named in via hidden_from.
func TestTripMemberSeesAllNonHidden(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	bob := mkUser(t, s)
	befriendStore(t, s, owner, bob)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, bob, "viewer")
	p1 := mkPlan(t, s, trip, owner)
	p2 := mkPlan(t, s, trip, owner)

	if !canView(t, s, p1, bob) || !canView(t, s, p2, bob) {
		t.Fatal("trip member should see both default plans before hiding")
	}
	if err := s.SetPlanVisibility(ctx, p2, "hidden_from", []int64{bob}); err != nil {
		t.Fatalf("SetPlanVisibility: %v", err)
	}
	if canView(t, s, p2, bob) {
		t.Error("member named in hidden_from must not see p2")
	}
	if !canView(t, s, p1, bob) {
		t.Error("member should still see the non-hidden p1")
	}
}

// TestTripShareAllFriendsGrantsFullAccess: turning on the trip-level all-friends
// flag grants every accepted friend full (trip-member-equivalent) access; a
// non-friend stranger still sees nothing.
func TestTripShareAllFriendsGrantsFullAccess(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	fran := mkUser(t, s)
	stranger := mkUser(t, s)
	befriendStore(t, s, owner, fran)
	trip := mkTrip(t, s, owner)
	p := mkPlan(t, s, trip, owner)

	if canView(t, s, p, fran) {
		t.Error("friend should not see the plan before the flag is set")
	}
	if err := s.SetTripShareAllFriends(ctx, trip, "viewer"); err != nil {
		t.Fatalf("SetTripShareAllFriends: %v", err)
	}
	if !canView(t, s, p, fran) {
		t.Error("accepted friend should see the plan once the trip flag is set")
	}
	if canView(t, s, p, stranger) {
		t.Error("a non-friend stranger must not see the plan even with the trip flag")
	}
}

// TestPlanShareAllFriendsScoped: the plan-level all-friends flag grants only the
// flagged plan to accepted friends, not the trip's other plans.
func TestPlanShareAllFriendsScoped(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	gus := mkUser(t, s)
	befriendStore(t, s, owner, gus)
	trip := mkTrip(t, s, owner)
	shared := mkPlan(t, s, trip, owner)
	other := mkPlan(t, s, trip, owner)

	if err := s.SetPlanShareAllFriends(ctx, shared, true); err != nil {
		t.Fatalf("SetPlanShareAllFriends: %v", err)
	}
	if !canView(t, s, shared, gus) {
		t.Error("friend should see the plan flagged share_all_friends")
	}
	if canView(t, s, other, gus) {
		t.Error("plan-level all-friends grant must be scoped to the flagged plan only")
	}
}

// TestFriendGateActivationAndRevocation: a trip member sees nothing until they
// are an accepted friend of the owner; unfriending revokes access.
func TestFriendGateActivationAndRevocation(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pat := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, pat, "viewer")
	p := mkPlan(t, s, trip, owner)

	// Pending (not yet accepted) friendship: the gate is closed.
	if _, err := s.RequestFriendship(ctx, owner, pat, ""); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if canView(t, s, p, pat) {
		t.Error("a pending friendship must not open the gate")
	}
	// Accept: the gate opens.
	if _, err := s.AcceptFriendship(ctx, pat, owner); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	if !canView(t, s, p, pat) {
		t.Error("accepting the friendship should open the gate for the trip member")
	}
	// Unfriend: the gate closes again.
	if err := s.RemoveFriendship(ctx, owner, pat); err != nil {
		t.Fatalf("RemoveFriendship: %v", err)
	}
	if canView(t, s, p, pat) {
		t.Error("unfriending must revoke access")
	}
}

func tripVisible(t *testing.T, s *Store, viewer, tripID int64) bool {
	t.Helper()
	trips, err := s.ListTrips(ctx, viewer)
	if err != nil {
		t.Fatalf("ListTrips: %v", err)
	}
	for _, tr := range trips {
		if tr.ID == tripID {
			return true
		}
	}
	return false
}

func TestTileVisibleForPlanScopedViewer(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	claire := mkUser(t, s)
	befriendStore(t, s, owner, claire)
	tripID := mkTrip(t, s, owner)
	flight := mkPlan(t, s, tripID, owner)
	if err := s.AddPlanPassenger(ctx, flight, claire); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	if !tripVisible(t, s, claire, tripID) {
		t.Error("plan-scoped viewer should see the trip tile")
	}
	stranger := mkUser(t, s)
	befriendStore(t, s, owner, stranger)
	if tripVisible(t, s, stranger, tripID) {
		t.Error("friend with no grant must not see the tile")
	}
}

func TestTileVisibleForTripAllFriends(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	fran := mkUser(t, s)
	befriendStore(t, s, owner, fran)
	tripID := mkTrip(t, s, owner)
	_ = mkPlan(t, s, tripID, owner)
	if err := s.SetTripShareAllFriends(ctx, tripID, "viewer"); err != nil {
		t.Fatalf("SetTripShareAllFriends: %v", err)
	}
	if !tripVisible(t, s, fran, tripID) {
		t.Error("all-friends trip should be visible to a friend")
	}
}
