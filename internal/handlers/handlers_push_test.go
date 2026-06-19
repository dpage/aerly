package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/config"
)

// enabledPushConfig is a Config with Web Push switched on.
func enabledPushConfig() *config.Config {
	return &config.Config{
		WebPushVAPIDPublic:  "test-public-key",
		WebPushVAPIDPrivate: "test-private-key",
		WebPushSubject:      "mailto:ops@aerly.test",
	}
}

func validSubBody() map[string]any {
	return map[string]any{
		"endpoint": "https://push.example/device-1",
		"keys":     map[string]any{"p256dh": "p256", "auth": "auth"},
	}
}

func TestPushVAPIDKey_DisabledByDefault(t *testing.T) {
	e := setup(t, nil, nil) // no VAPID keys
	uid := e.user(t, "alice", false)

	w := e.req(t, "GET", "/api/push/vapid-key", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[vapidKeyDTO](t, w)
	if got.Enabled || got.PublicKey != "" {
		t.Fatalf("expected disabled with no key, got %+v", got)
	}
}

func TestPushVAPIDKey_ExposesPublicKeyWhenEnabled(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)

	w := e.req(t, "GET", "/api/push/vapid-key", nil, uid)
	got := decodeBody[vapidKeyDTO](t, w)
	if !got.Enabled || got.PublicKey != "test-public-key" {
		t.Fatalf("expected enabled with public key, got %+v", got)
	}
}

func TestPushSubscribe_DisabledReturns503(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "alice", false)

	w := e.req(t, "POST", "/api/push/subscriptions", validSubBody(), uid)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestPushSubscribe_StoresSubscription(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)

	w := e.req(t, "POST", "/api/push/subscriptions", validSubBody(), uid)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	subs, err := e.store.WebPushSubscriptionsFor(context.Background(), uid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example/device-1" {
		t.Fatalf("subscription not stored: %+v", subs)
	}
	if subs[0].P256dh != "p256" || subs[0].Auth != "auth" {
		t.Errorf("keys not stored: %+v", subs[0])
	}
}

func TestPushSubscribe_RejectsIncompleteBody(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)

	// Missing keys.
	w := e.req(t, "POST", "/api/push/subscriptions", map[string]any{"endpoint": "https://push/x"}, uid)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing keys status = %d, want 400", w.Code)
	}
	// Missing endpoint.
	w = e.req(t, "POST", "/api/push/subscriptions",
		map[string]any{"keys": map[string]any{"p256dh": "p", "auth": "a"}}, uid)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing endpoint status = %d, want 400", w.Code)
	}
}

func TestPushUnsubscribe_IdempotentDelete(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)

	// Subscribe, then unsubscribe twice — both succeed.
	if w := e.req(t, "POST", "/api/push/subscriptions", validSubBody(), uid); w.Code != http.StatusNoContent {
		t.Fatalf("subscribe status = %d", w.Code)
	}
	body := map[string]any{"endpoint": "https://push.example/device-1"}
	if w := e.req(t, "DELETE", "/api/push/subscriptions", body, uid); w.Code != http.StatusNoContent {
		t.Fatalf("first unsubscribe status = %d", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/push/subscriptions", body, uid); w.Code != http.StatusNoContent {
		t.Fatalf("repeat unsubscribe status = %d", w.Code)
	}
	subs, _ := e.store.WebPushSubscriptionsFor(context.Background(), uid)
	if len(subs) != 0 {
		t.Fatalf("subscription not removed: %+v", subs)
	}
}

func TestPushUnsubscribe_RejectsMissingEndpoint(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)
	w := e.req(t, "DELETE", "/api/push/subscriptions", map[string]any{}, uid)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPushPrefs_DefaultsAllEnabled(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)

	w := e.req(t, "GET", "/api/push/prefs", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decodeBody[pushPrefsDTO](t, w)
	if !got.Kinds["alert"] || !got.Kinds["share"] {
		t.Fatalf("expected all kinds default enabled, got %+v", got.Kinds)
	}
}

func TestPushPrefs_SetAndPersist(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)

	w := e.req(t, "PATCH", "/api/push/prefs", map[string]any{"kind": "alert", "enabled": false}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[pushPrefsDTO](t, w)
	if got.Kinds["alert"] || !got.Kinds["share"] {
		t.Fatalf("after patch = %+v", got.Kinds)
	}
	// Re-fetch to confirm persistence.
	w = e.req(t, "GET", "/api/push/prefs", nil, uid)
	got = decodeBody[pushPrefsDTO](t, w)
	if got.Kinds["alert"] {
		t.Fatalf("alert pref did not persist: %+v", got.Kinds)
	}
}

func TestPushPrefs_RejectsUnknownKindOrMissingFlag(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "alice", false)

	w := e.req(t, "PATCH", "/api/push/prefs", map[string]any{"kind": "bogus", "enabled": true}, uid)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind status = %d, want 400", w.Code)
	}
	w = e.req(t, "PATCH", "/api/push/prefs", map[string]any{"kind": "alert"}, uid)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled status = %d, want 400", w.Code)
	}
}
