package handlers

import (
	"context"
	"net/http"
	"testing"
)

// TestNotificationsFriendCountErrG2 covers getNotifications' error path and
// buildNotificationsDTO's first count error (CountIncomingFriendRequests):
// dropping the friendships table makes the first count query fail. Auth uses
// sessions, not friendships, so the request still authenticates.
func TestNotificationsFriendCountErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2notiffr", false)
	g1dropTable(t, e, "friendships")
	if w := e.req(t, "GET", "/api/notifications", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("friend count err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestNotificationsAlertCountErrG2 covers buildNotificationsDTO's second count
// error (CountUnreadFlightAlerts): the friendships count succeeds, then dropping
// flight_alerts makes the alert count fail.
func TestNotificationsAlertCountErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2notifal", false)
	g1dropTable(t, e, "flight_alerts")
	if w := e.req(t, "GET", "/api/notifications", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("alert count err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestNotificationsShareCountErrG2 covers buildNotificationsDTO's third count
// error (CountUnreadNotifications): the first two counts succeed, then dropping
// notifications makes the share count fail.
func TestNotificationsShareCountErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2notifsh", false)
	g1dropTable(t, e, "notifications")
	if w := e.req(t, "GET", "/api/notifications", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("share count err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestPublishNotificationsBuildErrG2 covers publishNotifications' build-dto
// error branch: it logs and returns without publishing when the DTO build
// fails. We assert it doesn't panic and emits nothing on the hub.
func TestPublishNotificationsBuildErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g2puberr", false)
	ch, unsub := subscribe(e, uid)
	defer unsub()
	g1dropTable(t, e, "friendships")
	e.api.publishNotifications(context.Background(), uid)
	if evs := drainEvents(ch); len(evs) != 0 {
		t.Errorf("publish on build error emitted %d events, want 0", len(evs))
	}
}
