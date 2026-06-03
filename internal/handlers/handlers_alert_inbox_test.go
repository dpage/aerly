package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

// seedAlert inserts a flight_alert for a user via the store.
func seedAlert(t *testing.T, e *testEnv, userID int64, msg string) {
	t.Helper()
	if _, err := e.store.InsertFlightAlert(context.Background(), store.FlightAlert{
		UserID: userID, PlanPartID: 1, PlanID: 1, TripID: 1,
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
