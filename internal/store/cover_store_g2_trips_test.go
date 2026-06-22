package store

import (
	"errors"
	"testing"
	"time"
)

// g2TripWithDest inserts a trip directly with an explicit destination and a
// geocoded plan endpoint so the backfill candidate queries match it. Returns
// the trip id.
func g2TripWithGeoPart(t *testing.T, s *Store, ownerID int64, dest string) int64 {
	t.Helper()
	tripID := mkTrip(t, s, ownerID)
	if dest != "" {
		if _, err := s.pool.Exec(ctx, `UPDATE trips SET destination = $2 WHERE id = $1`, tripID, dest); err != nil {
			t.Fatalf("set destination: %v", err)
		}
	}
	planID := mkPlan(t, s, tripID, ownerID)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, start_lat, start_lon)
		 VALUES ($1, NOW(), 51.5, -0.12)`, planID); err != nil {
		t.Fatalf("insert geo part: %v", err)
	}
	return tripID
}

func TestG2TripByTripItID(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)

	trip, err := s.CreateTrip(ctx, CreateTripPayload{
		Name: "Imported", Destination: "Porto", TripItID: "tripit-12345",
	}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}

	got, err := s.TripByTripItID(ctx, owner, "tripit-12345")
	if err != nil || got.ID != trip.ID {
		t.Fatalf("TripByTripItID = %+v, %v; want trip %d", got, err, trip.ID)
	}

	// Unknown tripit id → ErrNotFound.
	if _, err := s.TripByTripItID(ctx, owner, "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("TripByTripItID unknown = %v, want ErrNotFound", err)
	}
	// Right id but wrong owner → ErrNotFound.
	other := mkUser(t, s)
	if _, err := s.TripByTripItID(ctx, other, "tripit-12345"); !errors.Is(err, ErrNotFound) {
		t.Errorf("TripByTripItID wrong owner = %v, want ErrNotFound", err)
	}
}

func TestG2SetAndOverwriteDestination(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner) // destination starts blank

	// SetTripDestination fills a blank destination and reports it changed.
	ok, err := s.SetTripDestination(ctx, trip, "Reykjavik")
	if err != nil || !ok {
		t.Fatalf("SetTripDestination first = %v, %v; want true", ok, err)
	}
	got, _ := s.TripByID(ctx, trip)
	if got.Destination != "Reykjavik" {
		t.Errorf("destination = %q, want Reykjavik", got.Destination)
	}
	// Second call is a no-op (destination no longer blank) and reports false.
	ok, err = s.SetTripDestination(ctx, trip, "Oslo")
	if err != nil || ok {
		t.Errorf("SetTripDestination second = %v, %v; want false", ok, err)
	}

	// OverwriteTripDestination replaces unconditionally.
	if err := s.OverwriteTripDestination(ctx, trip, "Bergen"); err != nil {
		t.Fatalf("OverwriteTripDestination: %v", err)
	}
	got, _ = s.TripByID(ctx, trip)
	if got.Destination != "Bergen" {
		t.Errorf("after overwrite destination = %q, want Bergen", got.Destination)
	}
}

func TestG2TripsNeedingDestinationAndReconcile(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)

	// A trip with no destination but a geocoded part is a destination candidate
	// and a reconcile candidate.
	noDest := g2TripWithGeoPart(t, s, owner, "")
	// A trip with a destination and a geocoded part is a reconcile candidate but
	// not a destination candidate.
	withDest := g2TripWithGeoPart(t, s, owner, "Madrid")

	needDest, err := s.TripsNeedingDestination(ctx)
	if err != nil {
		t.Fatalf("TripsNeedingDestination: %v", err)
	}
	if !containsTrip(needDest, noDest) || containsTrip(needDest, withDest) {
		t.Errorf("TripsNeedingDestination want only %d, got %v", noDest, tripIDs(needDest))
	}

	needRec, err := s.TripsNeedingPlaceReconcile(ctx)
	if err != nil {
		t.Fatalf("TripsNeedingPlaceReconcile: %v", err)
	}
	if !containsTrip(needRec, noDest) || !containsTrip(needRec, withDest) {
		t.Errorf("TripsNeedingPlaceReconcile want %d and %d, got %v", noDest, withDest, tripIDs(needRec))
	}

	// Mark one reconciled; it drops out of the reconcile candidate set.
	if err := s.MarkTripPlaceReconciled(ctx, noDest); err != nil {
		t.Fatalf("MarkTripPlaceReconciled: %v", err)
	}
	needRec, _ = s.TripsNeedingPlaceReconcile(ctx)
	if containsTrip(needRec, noDest) {
		t.Errorf("reconciled trip %d should no longer be a candidate", noDest)
	}
}

func TestG2TripPartSpans(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	planID := mkPlan(t, s, trip, owner)

	start := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	later := time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
	mkPart(t, s, planID, start, &end, "UTC", "UTC", "LHR")
	mkPart(t, s, planID, later, nil, "UTC", "", "CDG")

	spans, err := s.TripPartSpans(ctx, owner)
	if err != nil {
		t.Fatalf("TripPartSpans: %v", err)
	}
	sp, ok := spans[trip]
	if !ok {
		t.Fatalf("TripPartSpans missing trip %d: %v", trip, spans)
	}
	if !sp.Start.Equal(start) || !sp.End.Equal(later) {
		t.Errorf("span = %v..%v, want %v..%v", sp.Start, sp.End, start, later)
	}

	// A user who is not a member sees no spans for this trip.
	stranger := mkUser(t, s)
	other, _ := s.TripPartSpans(ctx, stranger)
	if _, ok := other[trip]; ok {
		t.Errorf("stranger should not see span for trip %d", trip)
	}
}

func TestG2TripPassengerLifecycle(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	planID := mkPlan(t, s, trip, owner)

	// Add a trip passenger: they become a viewer and get materialised onto the
	// visible plan.
	if err := s.AddTripPassenger(ctx, trip, pax); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	// Idempotent re-add.
	if err := s.AddTripPassenger(ctx, trip, pax); err != nil {
		t.Fatalf("AddTripPassenger repeat: %v", err)
	}
	paxIDs, err := s.TripPassengers(ctx, trip)
	if err != nil || len(paxIDs) != 1 || paxIDs[0] != pax {
		t.Fatalf("TripPassengers = %v, %v; want [%d]", paxIDs, err, pax)
	}
	role, _ := s.TripRole(ctx, trip, pax)
	if role != "viewer" {
		t.Errorf("passenger role = %q, want viewer", role)
	}
	var onPlan bool
	s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM plan_passengers WHERE plan_id=$1 AND user_id=$2 AND via_trip)`,
		planID, pax).Scan(&onPlan)
	if !onPlan {
		t.Error("passenger should be materialised onto the visible plan")
	}

	// Remove the passenger: trip_passengers row, materialised plan row, and the
	// auto viewer membership all go.
	if err := s.RemoveTripPassenger(ctx, trip, pax); err != nil {
		t.Fatalf("RemoveTripPassenger: %v", err)
	}
	if _, err := s.TripRole(ctx, trip, pax); !errors.Is(err, ErrNotFound) {
		t.Errorf("after remove role = %v, want ErrNotFound", err)
	}
	paxIDs, _ = s.TripPassengers(ctx, trip)
	if len(paxIDs) != 0 {
		t.Errorf("TripPassengers after remove = %v, want empty", paxIDs)
	}
	// Removing again returns ErrNotFound (no trip_passengers row).
	if err := s.RemoveTripPassenger(ctx, trip, pax); !errors.Is(err, ErrNotFound) {
		t.Errorf("double RemoveTripPassenger = %v, want ErrNotFound", err)
	}
}

func TestG2RemoveTripPassengerKeepsManualMembership(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Make pax an editor (a manual membership, not the auto viewer one).
	addMember(t, s, trip, pax, "editor")
	if err := s.AddTripPassenger(ctx, trip, pax); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	if err := s.RemoveTripPassenger(ctx, trip, pax); err != nil {
		t.Fatalf("RemoveTripPassenger: %v", err)
	}
	// The editor membership survives (only the auto viewer one is dropped).
	role, err := s.TripRole(ctx, trip, pax)
	if err != nil || role != "editor" {
		t.Errorf("editor membership should survive: role=%q err=%v", role, err)
	}
}

func TestG2VisibleTripUserIDsAndFriendsTrips(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	befriendStore(t, s, owner, friend)

	trip, err := s.CreateTrip(ctx, CreateTripPayload{Name: "Shared"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	// Share with all friends so the friend sees it without a member row.
	if err := s.SetTripShareAllFriends(ctx, trip.ID, "viewer"); err != nil {
		t.Fatalf("SetTripShareAllFriends: %v", err)
	}

	ids, err := s.VisibleTripUserIDs(ctx, trip.ID)
	if err != nil {
		t.Fatalf("VisibleTripUserIDs: %v", err)
	}
	if !containsID(ids, owner) || !containsID(ids, friend) {
		t.Errorf("VisibleTripUserIDs = %v, want owner %d and friend %d", ids, owner, friend)
	}
	if containsID(ids, stranger) {
		t.Errorf("stranger %d should not be in VisibleTripUserIDs %v", stranger, ids)
	}

	// ListFriendsTrips from the friend's perspective sees the owner's trip.
	fr, err := s.ListFriendsTrips(ctx, friend)
	if err != nil {
		t.Fatalf("ListFriendsTrips: %v", err)
	}
	if !containsTrip(fr, trip.ID) {
		t.Errorf("ListFriendsTrips = %v, want trip %d", tripIDs(fr), trip.ID)
	}
}

func TestG2TripErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	if _, err := s.ListFriendsTrips(cc, 1); err == nil {
		t.Error("ListFriendsTrips cancelled should error")
	}
	if _, err := s.ListAllTrips(cc); err == nil {
		t.Error("ListAllTrips cancelled should error")
	}
	if _, err := s.TripsNeedingDestination(cc); err == nil {
		t.Error("TripsNeedingDestination cancelled should error")
	}
	if _, err := s.TripsNeedingCountry(cc); err == nil {
		t.Error("TripsNeedingCountry cancelled should error")
	}
	if _, err := s.TripsNeedingPlaceReconcile(cc); err == nil {
		t.Error("TripsNeedingPlaceReconcile cancelled should error")
	}
	if _, err := s.TripPartSpans(cc, 1); err == nil {
		t.Error("TripPartSpans cancelled should error")
	}
	if _, err := s.TripMembers(cc, 1); err == nil {
		t.Error("TripMembers cancelled should error")
	}
	if _, err := s.TripPassengers(cc, 1); err == nil {
		t.Error("TripPassengers cancelled should error")
	}
	if _, err := s.VisibleTripUserIDs(cc, 1); err == nil {
		t.Error("VisibleTripUserIDs cancelled should error")
	}
	if err := s.AddTripPassenger(cc, 1, 1); err == nil {
		t.Error("AddTripPassenger cancelled should error")
	}
	if err := s.RemoveTripPassenger(cc, 1, 1); err == nil {
		t.Error("RemoveTripPassenger cancelled should error")
	}
	if err := s.RemoveTripMember(cc, 1, 1); err == nil {
		t.Error("RemoveTripMember cancelled should error")
	}
	if _, err := s.TripRole(cc, 1, 1); err == nil {
		t.Error("TripRole cancelled should error")
	}
	if _, err := s.UpdateTrip(cc, 1, UpdateTripPayload{}); err == nil {
		t.Error("UpdateTrip cancelled should error")
	}
	if err := s.DeleteTrip(cc, 1); err == nil {
		t.Error("DeleteTrip cancelled should error")
	}
	if err := s.SetTripCountry(cc, 1, "gb"); err == nil {
		t.Error("SetTripCountry cancelled should error")
	}
	if _, err := s.SetTripDestination(cc, 1, "x"); err == nil {
		t.Error("SetTripDestination cancelled should error")
	}
	if err := s.OverwriteTripDestination(cc, 1, "x"); err == nil {
		t.Error("OverwriteTripDestination cancelled should error")
	}
	if err := s.MarkTripPlaceReconciled(cc, 1); err == nil {
		t.Error("MarkTripPlaceReconciled cancelled should error")
	}
}

func containsTrip(trips []*Trip, want int64) bool {
	for _, tr := range trips {
		if tr.ID == want {
			return true
		}
	}
	return false
}

func tripIDs(trips []*Trip) []int64 {
	out := make([]int64, 0, len(trips))
	for _, tr := range trips {
		out = append(out, tr.ID)
	}
	return out
}
