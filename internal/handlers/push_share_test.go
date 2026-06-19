package handlers

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/dpage/aerly/internal/push"
)

// fakePusher records pushes the handlers make so the share-notification tests
// can assert the push channel without real VAPID/crypto/network.
type fakePusher struct {
	mu       sync.Mutex
	enabled  bool
	users    [][]int64
	payloads []push.Payload
}

func (f *fakePusher) Enabled() bool { return f.enabled }

func (f *fakePusher) Send(_ context.Context, userIDs []int64, p push.Payload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = append(f.users, userIDs)
	f.payloads = append(f.payloads, p)
}

func (f *fakePusher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.payloads)
}

// shareSetup wires an env whose API has a fake (enabled) pusher, an owner who
// has shared a trip with bob (a member), and returns them.
func shareSetup(t *testing.T) (*testEnv, *fakePusher, int64, int64, int64) {
	t.Helper()
	e := setup(t, nil, enabledPushConfig())
	fp := &fakePusher{enabled: true}
	e.api.Push = fp
	owner := e.user(t, "shareowner", false)
	bob := e.user(t, "sharebob", false)
	e.befriend(t, owner, bob)
	tripID := newTrip(t, e, owner, "Trip")
	e.req(t, "POST", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner)
	return e, fp, owner, bob, tripID
}

func notifyTrip(t *testing.T, e *testEnv, tripID, sharee, actor int64) {
	t.Helper()
	w := e.req(t, "POST", "/api/trips/"+strconv.FormatInt(tripID, 10)+"/notify-shares",
		map[string]any{"user_ids": []int64{sharee}, "emails": []string{}}, actor)
	if w.Code != http.StatusNoContent {
		t.Fatalf("notify-shares code = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestShareNotify_PushesWhenKindEnabled(t *testing.T) {
	e, fp, owner, bob, tripID := shareSetup(t)
	notifyTrip(t, e, tripID, bob, owner)

	if fp.count() != 1 {
		t.Fatalf("expected 1 share push, got %d", fp.count())
	}
	if got := fp.payloads[0]; got.Kind != "share" || got.Body == "" || got.URL == "" {
		t.Fatalf("unexpected share payload: %+v", got)
	}
	if len(fp.users[0]) != 1 || fp.users[0][0] != bob {
		t.Errorf("push targeted %v, want [%d]", fp.users[0], bob)
	}
}

func TestShareNotify_SkippedWhenKindDisabled(t *testing.T) {
	e, fp, owner, bob, tripID := shareSetup(t)
	if err := e.store.SetPushKindPref(context.Background(), bob, "share", false); err != nil {
		t.Fatalf("disable share push: %v", err)
	}
	notifyTrip(t, e, tripID, bob, owner)

	if fp.count() != 0 {
		t.Fatalf("expected no share push when kind disabled, got %d", fp.count())
	}
	// The in-app notification still lands — push being off doesn't suppress it.
	n, _ := e.store.CountUnreadNotifications(context.Background(), bob)
	if n != 1 {
		t.Errorf("bob in-app notifications = %d, want 1", n)
	}
}

func TestShareNotify_DisabledSenderIsNoOp(t *testing.T) {
	e, fp, owner, bob, tripID := shareSetup(t)
	fp.enabled = false // sender present but unconfigured
	notifyTrip(t, e, tripID, bob, owner)

	if fp.count() != 0 {
		t.Fatalf("disabled sender should not be asked to send, got %d", fp.count())
	}
}
