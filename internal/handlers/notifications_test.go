package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/sse"
)

func TestGetNotificationsRequiresAuth(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, "GET", "/api/notifications", nil, 0)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestGetNotificationsReportsCount(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "me", false)
	a := e.user(t, "a", false)
	b := e.user(t, "b", false)
	if _, err := e.store.RequestFriendship(context.Background(), a, me, ""); err != nil {
		t.Fatalf("seed a→me: %v", err)
	}
	if _, err := e.store.RequestFriendship(context.Background(), b, me, ""); err != nil {
		t.Fatalf("seed b→me: %v", err)
	}

	w := e.req(t, "GET", "/api/notifications", nil, me)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[api.NotificationsDTO](t, w)
	if got.FriendRequestsPending != 2 {
		t.Errorf("count = %d, want 2", got.FriendRequestsPending)
	}
}

func TestPublishNotificationsPushesToUser(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "me", false)
	inviter := e.user(t, "inv", false)

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: me})
	defer unsub()

	if _, err := e.store.RequestFriendship(context.Background(), inviter, me, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	e.api.publishNotifications(context.Background(), me)

	events := drainEvents(ch)
	if len(events) != 1 || events[0].Type != "notifications.updated" {
		t.Fatalf("events = %+v", events)
	}
	var got api.NotificationsDTO
	if err := json.Unmarshal(events[0].Data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.FriendRequestsPending != 1 {
		t.Errorf("payload count = %d, want 1", got.FriendRequestsPending)
	}
}

func TestPublishNotificationsScopesToTargetUser(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "me", false)
	other := e.user(t, "other", false)

	myCh, myUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: me})
	defer myUnsub()
	otherCh, otherUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: other})
	defer otherUnsub()

	e.api.publishNotifications(context.Background(), me)

	if got := drainEvents(myCh); len(got) != 1 {
		t.Errorf("my events = %d, want 1", len(got))
	}
	if got := drainEvents(otherCh); len(got) != 0 {
		t.Errorf("other events = %d, want 0 (publish should be scoped)", len(got))
	}
}
