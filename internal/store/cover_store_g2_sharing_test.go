package store

import (
	"errors"
	"testing"
)

func TestG2SetShareAllFriendsRoundTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	planID := mkPlan(t, s, trip, owner)

	// Trip-level: set to editor, then clear with "".
	if err := s.SetTripShareAllFriends(ctx, trip, "editor"); err != nil {
		t.Fatalf("SetTripShareAllFriends editor: %v", err)
	}
	got, _ := s.TripByID(ctx, trip)
	if got.ShareAllFriendsRole != "editor" {
		t.Errorf("share role = %q, want editor", got.ShareAllFriendsRole)
	}
	if err := s.SetTripShareAllFriends(ctx, trip, ""); err != nil {
		t.Fatalf("SetTripShareAllFriends clear: %v", err)
	}
	got, _ = s.TripByID(ctx, trip)
	if got.ShareAllFriendsRole != "" {
		t.Errorf("share role after clear = %q, want empty", got.ShareAllFriendsRole)
	}

	// Unknown trip → ErrNotFound.
	if err := s.SetTripShareAllFriends(ctx, 99999999, "viewer"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTripShareAllFriends unknown trip = %v, want ErrNotFound", err)
	}

	// Plan-level: enable, then disable.
	if err := s.SetPlanShareAllFriends(ctx, planID, true); err != nil {
		t.Fatalf("SetPlanShareAllFriends enable: %v", err)
	}
	if err := s.SetPlanShareAllFriends(ctx, planID, false); err != nil {
		t.Fatalf("SetPlanShareAllFriends disable: %v", err)
	}
	// Unknown plan → ErrNotFound.
	if err := s.SetPlanShareAllFriends(ctx, 99999999, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPlanShareAllFriends unknown plan = %v, want ErrNotFound", err)
	}
}

func TestG2InsertPendingShareUpsert(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	ps := PendingShare{EmailLower: "g2joiner@example.com", Kind: "trip", TargetID: trip, Role: "viewer", InviterID: owner}
	if err := s.InsertPendingShare(ctx, ps); err != nil {
		t.Fatalf("InsertPendingShare: %v", err)
	}
	// Upsert (same key, new role) is idempotent and updates the role.
	ps.Role = "editor"
	if err := s.InsertPendingShare(ctx, ps); err != nil {
		t.Fatalf("InsertPendingShare upsert: %v", err)
	}
	var role string
	if err := s.pool.QueryRow(ctx,
		`SELECT role FROM pending_shares WHERE email_lower=$1 AND kind='trip' AND target_id=$2`,
		ps.EmailLower, trip).Scan(&role); err != nil {
		t.Fatalf("read pending_share: %v", err)
	}
	if role != "editor" {
		t.Errorf("upserted role = %q, want editor", role)
	}
}

func TestG2SharingErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	if err := s.SetTripShareAllFriends(cc, 1, "viewer"); err == nil {
		t.Error("SetTripShareAllFriends cancelled should error")
	}
	if err := s.SetPlanShareAllFriends(cc, 1, true); err == nil {
		t.Error("SetPlanShareAllFriends cancelled should error")
	}
	if err := s.InsertPendingShare(cc, PendingShare{EmailLower: "x@example.com", Kind: "trip", TargetID: 1}); err == nil {
		t.Error("InsertPendingShare cancelled should error")
	}
}
