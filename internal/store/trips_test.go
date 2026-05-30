package store

import (
	"errors"
	"testing"
	"time"
)

func TestTripCRUD(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)

	starts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	trip, err := s.CreateTrip(ctx, CreateTripPayload{
		Name: "Italy", Destination: "Rome", StartsOn: &starts, EndsOn: &ends,
	}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	if trip.Name != "Italy" || trip.Destination != "Rome" {
		t.Errorf("unexpected trip: %+v", trip)
	}
	if trip.CreatedBy == nil || *trip.CreatedBy != owner {
		t.Errorf("created_by = %v, want %d", trip.CreatedBy, owner)
	}

	// Owner membership row was created.
	role, err := s.TripRole(ctx, trip.ID, owner)
	if err != nil || role != "owner" {
		t.Errorf("TripRole = %q, %v; want owner", role, err)
	}

	got, err := s.TripByID(ctx, trip.ID)
	if err != nil || got.ID != trip.ID {
		t.Fatalf("TripByID = %+v, %v", got, err)
	}

	// Update name, leave dates untouched (nil pointers).
	newName := "Italy 2026"
	upd, err := s.UpdateTrip(ctx, trip.ID, UpdateTripPayload{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateTrip: %v", err)
	}
	if upd.Name != newName {
		t.Errorf("name = %q, want %q", upd.Name, newName)
	}
	if upd.StartsOn == nil || !upd.StartsOn.Equal(starts) {
		t.Errorf("starts_on changed unexpectedly: %v", upd.StartsOn)
	}

	// List shows the trip for the owner, not for a stranger.
	stranger := mkUser(t, s)
	mine, err := s.ListTrips(ctx, owner)
	if err != nil || len(mine) != 1 {
		t.Fatalf("ListTrips owner = %d, %v", len(mine), err)
	}
	theirs, err := s.ListTrips(ctx, stranger)
	if err != nil || len(theirs) != 0 {
		t.Fatalf("ListTrips stranger = %d, %v", len(theirs), err)
	}

	if err := s.DeleteTrip(ctx, trip.ID); err != nil {
		t.Fatalf("DeleteTrip: %v", err)
	}
	if _, err := s.TripByID(ctx, trip.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("TripByID after delete = %v, want ErrNotFound", err)
	}
	if err := s.DeleteTrip(ctx, trip.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double delete = %v, want ErrNotFound", err)
	}
}

func TestTripMembersAndRoles(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	editor := mkUser(t, s)
	viewer := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	if err := s.AddTripMember(ctx, trip, editor, "editor"); err != nil {
		t.Fatalf("AddTripMember editor: %v", err)
	}
	if err := s.AddTripMember(ctx, trip, viewer, "viewer"); err != nil {
		t.Fatalf("AddTripMember viewer: %v", err)
	}

	members, err := s.TripMembers(ctx, trip)
	if err != nil || len(members) != 3 {
		t.Fatalf("TripMembers = %d, %v", len(members), err)
	}
	// Ordered owner, editor, viewer.
	if members[0].Role != "owner" || members[1].Role != "editor" || members[2].Role != "viewer" {
		t.Errorf("member order wrong: %+v", members)
	}

	// CanEditTrip: owner & editor yes, viewer no.
	for _, tc := range []struct {
		uid  int64
		want bool
	}{{owner, true}, {editor, true}, {viewer, false}} {
		ok, err := s.CanEditTrip(ctx, trip, tc.uid)
		if err != nil {
			t.Fatalf("CanEditTrip(%d): %v", tc.uid, err)
		}
		if ok != tc.want {
			t.Errorf("CanEditTrip(%d) = %v, want %v", tc.uid, ok, tc.want)
		}
	}

	// Role upsert: promote viewer to editor.
	if err := s.AddTripMember(ctx, trip, viewer, "editor"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	role, _ := s.TripRole(ctx, trip, viewer)
	if role != "editor" {
		t.Errorf("after promote role = %q, want editor", role)
	}

	// Remove.
	if err := s.RemoveTripMember(ctx, trip, viewer); err != nil {
		t.Fatalf("RemoveTripMember: %v", err)
	}
	if _, err := s.TripRole(ctx, trip, viewer); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed member role = %v, want ErrNotFound", err)
	}
	if err := s.RemoveTripMember(ctx, trip, viewer); !errors.Is(err, ErrNotFound) {
		t.Errorf("double remove = %v, want ErrNotFound", err)
	}

	// CanViewTrip: editor yes, stranger no.
	if ok, _ := s.CanViewTrip(ctx, trip, editor); !ok {
		t.Error("editor should view trip")
	}
	stranger := mkUser(t, s)
	if ok, _ := s.CanViewTrip(ctx, trip, stranger); ok {
		t.Error("stranger should not view trip")
	}
}

func TestTripQueryErrorPaths(t *testing.T) {
	s := newStore(t)
	cc := canceled()
	if _, err := s.ListTrips(cc, 1); err == nil {
		t.Error("ListTrips cancelled should error")
	}
	if _, err := s.TripByID(cc, 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("TripByID cancelled = %v, want non-NotFound error", err)
	}
	if _, err := s.CreateTrip(cc, CreateTripPayload{Name: "x"}, 1); err == nil {
		t.Error("CreateTrip cancelled should error")
	}
	if err := s.AddTripMember(cc, 1, 1, "viewer"); err == nil {
		t.Error("AddTripMember cancelled should error")
	}
}
