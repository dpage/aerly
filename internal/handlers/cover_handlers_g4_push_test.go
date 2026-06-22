package handlers

import (
	"net/http"
	"testing"
)

// TestG4PushShareKindPrefErr covers pushShare's error branch when reading the
// recipient's 'share' push-kind pref fails: the push is silently skipped (the
// share flow still succeeds with 204) rather than erroring out.
func TestG4PushShareKindPrefErr(t *testing.T) {
	e, fp, owner, bob, tripID := shareSetup(t)
	// Drop the prefs table so PushKindEnabled errors inside pushShare.
	g1dropTable(t, e, "push_kind_prefs")
	notifyTrip(t, e, tripID, bob, owner)
	if fp.count() != 0 {
		t.Fatalf("expected no push when kind-pref lookup errors, got %d", fp.count())
	}
}

// TestG4SubscribePushBadBody covers the decode-error 400 branch of subscribePush.
func TestG4SubscribePushBadBody(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "g4subbad", false)
	if w := e.req(t, "POST", "/api/push/subscriptions", map[string]any{"bogus": 1}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("subscribe bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4SubscribePushStoreErr covers the store-error 500 branch of subscribePush.
func TestG4SubscribePushStoreErr(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "g4substore", false)
	g1dropTable(t, e, "webpush_subscriptions")
	if w := e.req(t, "POST", "/api/push/subscriptions", validSubBody(), uid); w.Code != http.StatusInternalServerError {
		t.Errorf("subscribe store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4UnsubscribePushBadBody covers the decode-error 400 branch of
// unsubscribePush.
func TestG4UnsubscribePushBadBody(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "g4unsubbad", false)
	if w := e.req(t, "DELETE", "/api/push/subscriptions", map[string]any{"bogus": 1}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("unsubscribe bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4UnsubscribePushStoreErr covers the store-error 500 branch of
// unsubscribePush.
func TestG4UnsubscribePushStoreErr(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "g4unsubstore", false)
	g1dropTable(t, e, "webpush_subscriptions")
	body := map[string]any{"endpoint": "https://push.example.com/device-1"}
	if w := e.req(t, "DELETE", "/api/push/subscriptions", body, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("unsubscribe store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4GetPushPrefsStoreErr covers the store-error 500 branch of getPushPrefs.
func TestG4GetPushPrefsStoreErr(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "g4prefstore", false)
	g1dropTable(t, e, "push_kind_prefs")
	if w := e.req(t, "GET", "/api/push/prefs", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("get prefs store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4SetPushPrefBadBody covers the decode-error 400 branch of setPushPref.
func TestG4SetPushPrefBadBody(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "g4setbad", false)
	if w := e.req(t, "PATCH", "/api/push/prefs", map[string]any{"bogus": 1}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("set pref bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4SetPushPrefStoreErr covers the store-error 500 branch of setPushPref.
func TestG4SetPushPrefStoreErr(t *testing.T) {
	e := setup(t, nil, enabledPushConfig())
	uid := e.user(t, "g4setstore", false)
	g1dropTable(t, e, "push_kind_prefs")
	body := map[string]any{"kind": "alert", "enabled": false}
	if w := e.req(t, "PATCH", "/api/push/prefs", body, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("set pref store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
