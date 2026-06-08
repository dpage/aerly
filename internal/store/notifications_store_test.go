package store

import "testing"

func TestNotificationsInboxRoundTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	actor := mkUser(t, s)
	recipient := mkUser(t, s)
	tripID := mkTrip(t, s, actor)

	got, err := s.InsertNotification(ctx, Notification{
		UserID: recipient, Kind: "share", ActorID: &actor,
		TripID: &tripID, Message: "Alice shared Paris 2026 with you",
	})
	if err != nil || got.ID == 0 {
		t.Fatalf("InsertNotification: %+v, %v", got, err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be populated by the DB")
	}
	if n, _ := s.CountUnreadNotifications(ctx, recipient); n != 1 {
		t.Errorf("unread = %d, want 1", n)
	}
	list, err := s.ListNotifications(ctx, recipient, 50)
	if err != nil || len(list) != 1 || list[0].Message == "" {
		t.Fatalf("ListNotifications = %+v, %v", list, err)
	}
	if list[0].ActorID == nil || *list[0].ActorID != actor {
		t.Errorf("ActorID round-trip failed: %+v", list[0].ActorID)
	}
	if err := s.MarkNotificationsRead(ctx, recipient); err != nil {
		t.Fatalf("MarkNotificationsRead: %v", err)
	}
	if n, _ := s.CountUnreadNotifications(ctx, recipient); n != 0 {
		t.Errorf("unread after read = %d, want 0", n)
	}
}
