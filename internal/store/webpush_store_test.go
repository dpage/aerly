package store

import (
	"errors"
	"testing"
)

func mkSub(userID int64, endpoint string) WebPushSubscription {
	return WebPushSubscription{
		UserID:    userID,
		Endpoint:  endpoint,
		P256dh:    "p256-" + endpoint,
		Auth:      "auth-" + endpoint,
		UserAgent: "test-agent",
	}
}

func TestWebPushUpsertAndList(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)

	id1, err := s.UpsertWebPushSubscription(ctx, mkSub(uid, "https://push.example/a"))
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if _, err := s.UpsertWebPushSubscription(ctx, mkSub(uid, "https://push.example/b")); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	subs, err := s.WebPushSubscriptionsFor(ctx, uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("got %d subs, want 2", len(subs))
	}
	if subs[0].ID != id1 || subs[0].Endpoint != "https://push.example/a" {
		t.Errorf("first sub = %+v", subs[0])
	}
	if subs[0].P256dh != "p256-https://push.example/a" || subs[0].Auth != "auth-https://push.example/a" {
		t.Errorf("keys not stored: %+v", subs[0])
	}
}

func TestWebPushUpsertConflictReassignsAndResets(t *testing.T) {
	s := newStore(t)
	u1 := mkUser(t, s)
	u2 := mkUser(t, s)
	const ep = "https://push.example/shared-device"

	id, err := s.UpsertWebPushSubscription(ctx, mkSub(u1, ep))
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Accumulate a failure so we can prove the conflict path resets it.
	if _, err := s.BumpWebPushFailure(ctx, id); err != nil {
		t.Fatalf("bump: %v", err)
	}

	// Same endpoint, different user (the device's new signed-in user).
	again := mkSub(u2, ep)
	again.P256dh = "rotated"
	id2, err := s.UpsertWebPushSubscription(ctx, again)
	if err != nil {
		t.Fatalf("conflict upsert: %v", err)
	}
	if id2 != id {
		t.Errorf("conflict created a new row (%d != %d)", id2, id)
	}

	// u1 no longer owns it; u2 does, with rotated keys and a reset counter.
	if subs, _ := s.WebPushSubscriptionsFor(ctx, u1); len(subs) != 0 {
		t.Errorf("u1 still has %d subs after reassignment", len(subs))
	}
	subs, err := s.WebPushSubscriptionsFor(ctx, u2)
	if err != nil {
		t.Fatalf("list u2: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("u2 has %d subs, want 1", len(subs))
	}
	if subs[0].P256dh != "rotated" {
		t.Errorf("keys not rotated: %q", subs[0].P256dh)
	}
	if subs[0].FailureCount != 0 {
		t.Errorf("failure_count not reset: %d", subs[0].FailureCount)
	}
}

func TestWebPushDeleteByEndpointScopedToOwner(t *testing.T) {
	s := newStore(t)
	u1 := mkUser(t, s)
	u2 := mkUser(t, s)
	if _, err := s.UpsertWebPushSubscription(ctx, mkSub(u1, "https://push.example/u1")); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// A different user can't delete it.
	if err := s.DeleteWebPushSubscriptionByEndpoint(ctx, u2, "https://push.example/u1"); err != nil {
		t.Fatalf("delete (wrong owner): %v", err)
	}
	if subs, _ := s.WebPushSubscriptionsFor(ctx, u1); len(subs) != 1 {
		t.Fatalf("sub deleted by non-owner")
	}

	// The owner can.
	if err := s.DeleteWebPushSubscriptionByEndpoint(ctx, u1, "https://push.example/u1"); err != nil {
		t.Fatalf("delete (owner): %v", err)
	}
	if subs, _ := s.WebPushSubscriptionsFor(ctx, u1); len(subs) != 0 {
		t.Fatalf("sub not deleted by owner")
	}
}

func TestWebPushDeleteByID(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)
	id, err := s.UpsertWebPushSubscription(ctx, mkSub(uid, "https://push.example/x"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DeleteWebPushSubscription(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if subs, _ := s.WebPushSubscriptionsFor(ctx, uid); len(subs) != 0 {
		t.Fatalf("sub not deleted")
	}
}

func TestWebPushSuccessAndFailureBookkeeping(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)
	id, err := s.UpsertWebPushSubscription(ctx, mkSub(uid, "https://push.example/y"))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	n, err := s.BumpWebPushFailure(ctx, id)
	if err != nil || n != 1 {
		t.Fatalf("bump 1: n=%d err=%v", n, err)
	}
	n, err = s.BumpWebPushFailure(ctx, id)
	if err != nil || n != 2 {
		t.Fatalf("bump 2: n=%d err=%v", n, err)
	}

	// A success clears the counter.
	if err := s.MarkWebPushSuccess(ctx, id); err != nil {
		t.Fatalf("mark success: %v", err)
	}
	subs, _ := s.WebPushSubscriptionsFor(ctx, uid)
	if len(subs) != 1 || subs[0].FailureCount != 0 {
		t.Fatalf("failure_count not cleared on success: %+v", subs)
	}
}

func TestWebPushBumpMissingReturnsNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.BumpWebPushFailure(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bump missing: want ErrNotFound, got %v", err)
	}
}

func TestPushKindPrefsDefaultEnabled(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)

	// No rows yet: every kind defaults to enabled.
	on, err := s.PushKindEnabled(ctx, uid, "alert")
	if err != nil || !on {
		t.Fatalf("default alert enabled: on=%v err=%v", on, err)
	}

	if err := s.SetPushKindPref(ctx, uid, "alert", false); err != nil {
		t.Fatalf("set alert off: %v", err)
	}
	on, err = s.PushKindEnabled(ctx, uid, "alert")
	if err != nil || on {
		t.Fatalf("alert should be off: on=%v err=%v", on, err)
	}
	// Other kinds remain default-enabled.
	if on, _ := s.PushKindEnabled(ctx, uid, "share"); !on {
		t.Errorf("share should still default enabled")
	}

	// Upsert flips it back on.
	if err := s.SetPushKindPref(ctx, uid, "alert", true); err != nil {
		t.Fatalf("set alert on: %v", err)
	}
	if on, _ := s.PushKindEnabled(ctx, uid, "alert"); !on {
		t.Errorf("alert should be on after re-enable")
	}
}

func TestPushKindPrefsFor(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)
	if err := s.SetPushKindPref(ctx, uid, "alert", false); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetPushKindPref(ctx, uid, "share", true); err != nil {
		t.Fatalf("set: %v", err)
	}
	prefs, err := s.PushKindPrefsFor(ctx, uid)
	if err != nil {
		t.Fatalf("prefs: %v", err)
	}
	if len(prefs) != 2 || prefs["alert"] != false || prefs["share"] != true {
		t.Fatalf("prefs = %+v", prefs)
	}
}
