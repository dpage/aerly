package store

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// roleOnTrip returns the target's trip_members role, or "" if none. A real
// query failure fails the test rather than being masked as "no role".
func roleOnTrip(t *testing.T, s *Store, tripID, userID int64) string {
	t.Helper()
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM trip_members WHERE trip_id = $1 AND user_id = $2`,
		tripID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("roleOnTrip query failed: %v", err)
	}
	return role
}

func TestAutoShareAppliedOnCreateTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	wife := mkUser(t, s)
	pa := mkUser(t, s)
	befriendStore(t, s, owner, wife)
	befriendStore(t, s, owner, pa)

	if err := s.SetAutoShare(ctx, owner, wife, "viewer"); err != nil {
		t.Fatalf("SetAutoShare wife: %v", err)
	}
	if err := s.SetAutoShare(ctx, owner, pa, "editor"); err != nil {
		t.Fatalf("SetAutoShare pa: %v", err)
	}

	trip, err := s.CreateTrip(ctx, CreateTripPayload{Name: "Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}

	if got := roleOnTrip(t, s, trip.ID, wife); got != "viewer" {
		t.Errorf("wife role = %q, want viewer", got)
	}
	if got := roleOnTrip(t, s, trip.ID, pa); got != "editor" {
		t.Errorf("pa role = %q, want editor", got)
	}
	// The grant makes the trip visible to a friend.
	if !tripVisible(t, s, wife, trip.ID) {
		t.Error("auto-shared viewer should see the new trip")
	}
}

func TestAutoSharePassengerAppliedOnCreateTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	partner := mkUser(t, s)
	befriendStore(t, s, owner, partner)

	if err := s.SetAutoShare(ctx, owner, partner, "passenger"); err != nil {
		t.Fatalf("SetAutoShare: %v", err)
	}
	trip, err := s.CreateTrip(ctx, CreateTripPayload{Name: "Holiday"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}

	// A passenger gets a viewer membership plus a trip_passengers row.
	if got := roleOnTrip(t, s, trip.ID, partner); got != "viewer" {
		t.Errorf("partner role = %q, want viewer", got)
	}
	pax, err := s.TripPassengers(ctx, trip.ID)
	if err != nil {
		t.Fatalf("TripPassengers: %v", err)
	}
	if !containsID(pax, partner) {
		t.Errorf("partner should be a trip passenger, got %v", pax)
	}

	// A plan added afterwards (via CreatePlan, which materialises trip
	// passengers) inherits the trip passenger.
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{TripID: trip.ID, Type: "flight"}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	var onPlan bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM plan_passengers WHERE plan_id = $1 AND user_id = $2)`,
		plan.ID, partner).Scan(&onPlan); err != nil {
		t.Fatalf("query plan_passengers: %v", err)
	}
	if !onPlan {
		t.Error("trip passenger should be materialised onto a later-added plan")
	}
}

func TestSetAutoShareUpsertAndList(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	friend := mkUser(t, s)

	if err := s.SetAutoShare(ctx, owner, friend, "viewer"); err != nil {
		t.Fatalf("SetAutoShare: %v", err)
	}
	// Re-PUT changes the role rather than erroring on the PK.
	if err := s.SetAutoShare(ctx, owner, friend, "editor"); err != nil {
		t.Fatalf("SetAutoShare (update): %v", err)
	}
	shares, err := s.ListAutoShares(ctx, owner)
	if err != nil {
		t.Fatalf("ListAutoShares: %v", err)
	}
	if len(shares) != 1 || shares[0].ShareWithID != friend || shares[0].Role != "editor" {
		t.Fatalf("unexpected shares: %+v", shares)
	}
}

func TestRemoveAutoShare(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	friend := mkUser(t, s)

	if err := s.RemoveAutoShare(ctx, owner, friend); !errors.Is(err, ErrNotFound) {
		t.Errorf("RemoveAutoShare on missing = %v, want ErrNotFound", err)
	}
	if err := s.SetAutoShare(ctx, owner, friend, "viewer"); err != nil {
		t.Fatalf("SetAutoShare: %v", err)
	}
	if err := s.RemoveAutoShare(ctx, owner, friend); err != nil {
		t.Fatalf("RemoveAutoShare: %v", err)
	}
	shares, err := s.ListAutoShares(ctx, owner)
	if err != nil {
		t.Fatalf("ListAutoShares: %v", err)
	}
	if len(shares) != 0 {
		t.Errorf("expected no shares after removal, got %+v", shares)
	}
}

// TestRemoveAutoShareKeepsExistingTrips: removing a default does not revoke
// access on trips already shared via it.
func TestRemoveAutoShareKeepsExistingTrips(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	friend := mkUser(t, s)
	befriendStore(t, s, owner, friend)

	if err := s.SetAutoShare(ctx, owner, friend, "viewer"); err != nil {
		t.Fatalf("SetAutoShare: %v", err)
	}
	trip, err := s.CreateTrip(ctx, CreateTripPayload{Name: "Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	if err := s.RemoveAutoShare(ctx, owner, friend); err != nil {
		t.Fatalf("RemoveAutoShare: %v", err)
	}
	if got := roleOnTrip(t, s, trip.ID, friend); got != "viewer" {
		t.Errorf("removing the default should not revoke the existing grant; role = %q", got)
	}
}
