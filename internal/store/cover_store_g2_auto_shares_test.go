package store

import (
	"errors"
	"testing"
)

func TestG2AutoShareCRUD(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	a := mkUser(t, s)
	b := mkUser(t, s)

	if err := s.SetAutoShare(ctx, owner, a, "viewer"); err != nil {
		t.Fatalf("SetAutoShare a: %v", err)
	}
	if err := s.SetAutoShare(ctx, owner, b, "passenger"); err != nil {
		t.Fatalf("SetAutoShare b: %v", err)
	}
	// Upsert updates the role for an existing target.
	if err := s.SetAutoShare(ctx, owner, a, "editor"); err != nil {
		t.Fatalf("SetAutoShare a update: %v", err)
	}

	shares, err := s.ListAutoShares(ctx, owner)
	if err != nil {
		t.Fatalf("ListAutoShares: %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("ListAutoShares = %d, want 2", len(shares))
	}
	byUser := map[int64]string{}
	for _, sh := range shares {
		byUser[sh.ShareWithID] = sh.Role
	}
	if byUser[a] != "editor" || byUser[b] != "passenger" {
		t.Errorf("auto shares = %v, want a=editor b=passenger", byUser)
	}

	// Remove one; the other survives.
	if err := s.RemoveAutoShare(ctx, owner, a); err != nil {
		t.Fatalf("RemoveAutoShare: %v", err)
	}
	if err := s.RemoveAutoShare(ctx, owner, a); !errors.Is(err, ErrNotFound) {
		t.Errorf("double RemoveAutoShare = %v, want ErrNotFound", err)
	}
	shares, _ = s.ListAutoShares(ctx, owner)
	if len(shares) != 1 || shares[0].ShareWithID != b {
		t.Errorf("after remove = %v, want only b=%d", shares, b)
	}
}

// TestG2ApplyAutoSharesOnCreate exercises applyAutoSharesTx through CreateTrip:
// a viewer/editor default becomes a trip_members row; a passenger default
// becomes a trip_passengers row plus a viewer membership.
func TestG2ApplyAutoSharesOnCreate(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	viewerFriend := mkUser(t, s)
	paxFriend := mkUser(t, s)

	if err := s.SetAutoShare(ctx, owner, viewerFriend, "viewer"); err != nil {
		t.Fatalf("SetAutoShare viewer: %v", err)
	}
	if err := s.SetAutoShare(ctx, owner, paxFriend, "passenger"); err != nil {
		t.Fatalf("SetAutoShare passenger: %v", err)
	}

	trip, err := s.CreateTrip(ctx, CreateTripPayload{Name: "Auto"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}

	// viewerFriend got a viewer membership.
	role, err := s.TripRole(ctx, trip.ID, viewerFriend)
	if err != nil || role != "viewer" {
		t.Errorf("viewerFriend role = %q, %v; want viewer", role, err)
	}
	// paxFriend got a viewer membership and a trip_passengers row.
	role, err = s.TripRole(ctx, trip.ID, paxFriend)
	if err != nil || role != "viewer" {
		t.Errorf("paxFriend role = %q, %v; want viewer", role, err)
	}
	pax, _ := s.TripPassengers(ctx, trip.ID)
	if len(pax) != 1 || pax[0] != paxFriend {
		t.Errorf("trip passengers = %v, want [%d]", pax, paxFriend)
	}
}

func TestG2AutoShareErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	if _, err := s.ListAutoShares(cc, 1); err == nil {
		t.Error("ListAutoShares cancelled should error")
	}
	if err := s.SetAutoShare(cc, 1, 2, "viewer"); err == nil {
		t.Error("SetAutoShare cancelled should error")
	}
	if err := s.RemoveAutoShare(cc, 1, 2); err == nil {
		t.Error("RemoveAutoShare cancelled should error")
	}
}
