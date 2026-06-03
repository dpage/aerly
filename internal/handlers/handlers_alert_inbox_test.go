package handlers

import (
	"context"
	"net/http"
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

	// List: only the viewer's alert.
	w := e.req(t, http.MethodGet, "/api/alerts", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	list := decodeBody[[]api.FlightAlertDTO](t, w)
	if len(list) != 1 || list[0].Message != "BA286 now departs gate B32" {
		t.Fatalf("list = %+v", list)
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

func TestAlertsRequireAuth(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, http.MethodGet, "/api/alerts", nil, 0)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauth list status = %d, want 401", w.Code)
	}
}
