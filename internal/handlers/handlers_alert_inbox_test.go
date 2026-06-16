package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

// seedAlert inserts a flight_alert for a user via the store. It first creates a
// real trip → plan → plan_part chain so the alert's foreign keys (added in
// migration 0021) resolve.
func seedAlert(t *testing.T, e *testEnv, userID int64, msg string) {
	t.Helper()
	ctx := context.Background()
	var tripID, planID, partID int64
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('t', $1) RETURNING id`, userID).Scan(&tripID); err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`, tripID, userID).Scan(&planID); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if err := e.pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at) VALUES ($1, NOW()) RETURNING id`, planID).Scan(&partID); err != nil {
		t.Fatalf("seed plan_part: %v", err)
	}
	if _, err := e.store.InsertFlightAlert(ctx, store.FlightAlert{
		UserID: userID, PlanPartID: partID, PlanID: planID, TripID: tripID,
		Ident: "BA286", Kind: "gate", Status: "Scheduled", Message: msg,
	}); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
}

func TestNotificationsIncludesUnreadAlerts(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "alice", false)
	seedAlert(t, e, uid, "BA286 now departs gate B32")
	seedAlert(t, e, uid, "BA286 cancelled")

	w := e.req(t, http.MethodGet, "/api/notifications", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decodeBody[api.NotificationsDTO](t, w)
	if got.UnreadAlerts != 2 {
		t.Errorf("unread_alerts = %d, want 2", got.UnreadAlerts)
	}
}

func TestListAndMarkAlertsRead(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "bob", false)
	other := e.user(t, "carol", false)
	seedAlert(t, e, uid, "BA286 now departs gate B32")
	seedAlert(t, e, other, "not yours")

	// List: only the viewer's alert, in the merged inbox shape, still unread.
	w := e.req(t, http.MethodGet, "/api/alerts", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	list := decodeBody[[]api.NotificationItemDTO](t, w)
	if len(list) != 1 || list[0].Message != "BA286 now departs gate B32" {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Kind != "gate" || list[0].ReadAt != nil {
		t.Fatalf("inbox item kind/read_at = %+v", list[0])
	}

	// Mark read clears the unread count.
	w = e.req(t, http.MethodPost, "/api/alerts/read", nil, uid)
	if w.Code != http.StatusNoContent {
		t.Fatalf("mark-read status = %d", w.Code)
	}
	w = e.req(t, http.MethodGet, "/api/notifications", nil, uid)
	if decodeBody[api.NotificationsDTO](t, w).UnreadAlerts != 0 {
		t.Errorf("unread after mark-read != 0")
	}
}

func TestInboxIncludesShareNotifications(t *testing.T) {
	e := setup(t, nil, nil)
	actor := e.user(t, "inboxactor", false)
	uid := e.user(t, "inboxuser", false)
	tripID := newTrip(t, e, actor, "Trip")
	if _, err := e.store.InsertNotification(context.Background(), store.Notification{
		UserID: uid, Kind: "share", ActorID: &actor, TripID: &tripID,
		Message: "actor shared T with you",
	}); err != nil {
		t.Fatalf("InsertNotification: %v", err)
	}
	// Badge reflects the unread share.
	w := e.req(t, "GET", "/api/notifications", nil, uid)
	if !strings.Contains(w.Body.String(), `"unread_shares":1`) {
		t.Errorf("notifications DTO missing unread_shares: %s", w.Body.String())
	}
	// Inbox lists the share item.
	w = e.req(t, "GET", "/api/alerts", nil, uid)
	if !strings.Contains(w.Body.String(), "shared T with you") {
		t.Errorf("alert inbox missing share item: %s", w.Body.String())
	}
	// Marking read clears the unread share count.
	if w := e.req(t, "POST", "/api/alerts/read", nil, uid); w.Code != http.StatusNoContent {
		t.Fatalf("mark read code = %d", w.Code)
	}
	if n, _ := e.store.CountUnreadNotifications(context.Background(), uid); n != 0 {
		t.Errorf("unread notifications after read = %d, want 0", n)
	}
}

func TestAlertsRequireAuth(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, http.MethodGet, "/api/alerts", nil, 0)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauth list status = %d, want 401", w.Code)
	}
}

func TestDeleteFlightAlert(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "deleter", false)
	seedAlert(t, e, uid, "BA286 now departs gate B32")

	// Grab the alert's id from the inbox so we can delete it by source+id.
	w := e.req(t, http.MethodGet, "/api/alerts", nil, uid)
	list := decodeBody[[]api.NotificationItemDTO](t, w)
	if len(list) != 1 || list[0].Source != api.NotificationSourceFlight {
		t.Fatalf("inbox = %+v", list)
	}
	id := list[0].ID

	w = e.req(t, http.MethodDelete, "/api/alerts/flight/"+itoa(id), nil, uid)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", w.Code)
	}
	// Gone from the inbox, and the badge falls back to zero.
	w = e.req(t, http.MethodGet, "/api/alerts", nil, uid)
	if len(decodeBody[[]api.NotificationItemDTO](t, w)) != 0 {
		t.Errorf("alert still present after delete")
	}
	w = e.req(t, http.MethodGet, "/api/notifications", nil, uid)
	if decodeBody[api.NotificationsDTO](t, w).UnreadAlerts != 0 {
		t.Errorf("unread != 0 after delete")
	}
}

func TestDeleteAlertScopedToOwner(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	other := e.user(t, "other", false)
	seedAlert(t, e, owner, "owner's alert")

	w := e.req(t, http.MethodGet, "/api/alerts", nil, owner)
	id := decodeBody[[]api.NotificationItemDTO](t, w)[0].ID

	// A different user deleting by id is a no-op (the WHERE user_id guards it),
	// so the owner's alert survives.
	if w := e.req(t, http.MethodDelete, "/api/alerts/flight/"+itoa(id), nil, other); w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", w.Code)
	}
	w = e.req(t, http.MethodGet, "/api/alerts", nil, owner)
	if len(decodeBody[[]api.NotificationItemDTO](t, w)) != 1 {
		t.Errorf("owner's alert was deleted by another user")
	}
}

func TestDeleteAlertInvalidSource(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "baduser", false)
	w := e.req(t, http.MethodDelete, "/api/alerts/bogus/1", nil, uid)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid source status = %d, want 400", w.Code)
	}
}

func TestClearAlerts(t *testing.T) {
	e := setup(t, nil, nil)
	actor := e.user(t, "clearactor", false)
	uid := e.user(t, "clearer", false)
	seedAlert(t, e, uid, "flight alert one")
	seedAlert(t, e, uid, "flight alert two")
	tripID := newTrip(t, e, actor, "Trip")
	if _, err := e.store.InsertNotification(context.Background(), store.Notification{
		UserID: uid, Kind: "share", ActorID: &actor, TripID: &tripID,
		Message: "actor shared with you",
	}); err != nil {
		t.Fatalf("InsertNotification: %v", err)
	}

	if w := e.req(t, http.MethodDelete, "/api/alerts", nil, uid); w.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d", w.Code)
	}
	// Both flight alerts and share notifications are gone.
	w := e.req(t, http.MethodGet, "/api/alerts", nil, uid)
	if got := decodeBody[[]api.NotificationItemDTO](t, w); len(got) != 0 {
		t.Errorf("inbox not empty after clear: %+v", got)
	}
}
