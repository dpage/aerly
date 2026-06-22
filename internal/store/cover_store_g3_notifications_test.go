package store

import "testing"

// TestG3NotificationsLifecycle drives the notification helpers that earlier
// rounds left uncovered: insert, list (with optional actor/trip/plan fields),
// mark-read, count, single-row delete (owner-scoped) and clear-all.
func TestG3NotificationsLifecycle(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	actor := mkUser(t, s)
	other := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)

	// Insert one fully-populated row (actor/trip/plan all set) and one bare row
	// (all optional FKs nil) so ListNotifications scans both shapes.
	full, err := s.InsertNotification(ctx, Notification{
		UserID:  owner,
		Kind:    "share",
		ActorID: &actor,
		TripID:  &trip,
		PlanID:  &plan,
		Message: "Test User shared a trip with you",
	})
	if err != nil {
		t.Fatalf("InsertNotification full: %v", err)
	}
	if full.ID == 0 || full.CreatedAt.IsZero() {
		t.Fatalf("InsertNotification did not populate ID/CreatedAt: %+v", full)
	}
	if _, err := s.InsertNotification(ctx, Notification{
		UserID:  owner,
		Kind:    "share",
		Message: "bare notification",
	}); err != nil {
		t.Fatalf("InsertNotification bare: %v", err)
	}

	list, err := s.ListNotifications(ctx, owner, 10)
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListNotifications = %d rows, want 2", len(list))
	}

	if n, err := s.CountUnreadNotifications(ctx, owner); err != nil || n != 2 {
		t.Fatalf("CountUnreadNotifications = %d, %v; want 2, nil", n, err)
	}
	if err := s.MarkNotificationsRead(ctx, owner); err != nil {
		t.Fatalf("MarkNotificationsRead: %v", err)
	}
	if n, _ := s.CountUnreadNotifications(ctx, owner); n != 0 {
		t.Fatalf("CountUnreadNotifications after read = %d, want 0", n)
	}

	// Owner-scoping: another user can't delete this user's row (no-op, no error).
	if err := s.DeleteNotification(ctx, other, full.ID); err != nil {
		t.Fatalf("DeleteNotification wrong owner: %v", err)
	}
	if l, _ := s.ListNotifications(ctx, owner, 10); len(l) != 2 {
		t.Fatalf("after foreign delete = %d rows, want 2 (no-op)", len(l))
	}
	// The owner can delete their own.
	if err := s.DeleteNotification(ctx, owner, full.ID); err != nil {
		t.Fatalf("DeleteNotification owner: %v", err)
	}
	if l, _ := s.ListNotifications(ctx, owner, 10); len(l) != 1 {
		t.Fatalf("after owner delete = %d rows, want 1", len(l))
	}

	// Clear-all wipes the rest.
	if err := s.DeleteAllNotifications(ctx, owner); err != nil {
		t.Fatalf("DeleteAllNotifications: %v", err)
	}
	if l, _ := s.ListNotifications(ctx, owner, 10); len(l) != 0 {
		t.Fatalf("after clear-all = %d rows, want 0", len(l))
	}
}
