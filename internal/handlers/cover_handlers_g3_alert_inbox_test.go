package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

// TestG3AlertInboxSortAndTruncate seeds more than alertInboxLimit items across
// both sources so the merged list exercises the sort comparator (47) and the
// final truncation to alertInboxLimit (50-52).
func TestG3AlertInboxSortAndTruncate(t *testing.T) {
	e := setup(t, nil, nil)
	actor := e.user(t, "g3sortactor", false)
	uid := e.user(t, "g3sortuser", false)
	tripID := newTrip(t, e, actor, "Trip")

	// 30 flight alerts + 30 share notifications = 60 merged, capped to 50.
	for i := 0; i < 30; i++ {
		seedAlert(t, e, uid, "flight alert")
		if _, err := e.store.InsertNotification(context.Background(), store.Notification{
			UserID: uid, Kind: "share", ActorID: &actor, TripID: &tripID,
			Message: "shared with you",
		}); err != nil {
			t.Fatalf("InsertNotification: %v", err)
		}
	}

	w := e.req(t, http.MethodGet, "/api/alerts", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	list := decodeBody[[]api.NotificationItemDTO](t, w)
	if len(list) != alertInboxLimit {
		t.Fatalf("inbox length = %d, want %d (capped)", len(list), alertInboxLimit)
	}
	// Confirm descending CreatedAt order (the sort comparator).
	for i := 1; i < len(list); i++ {
		if list[i].CreatedAt.After(list[i-1].CreatedAt) {
			t.Errorf("inbox not sorted newest-first at %d", i)
		}
	}
}

// TestG3ListAlertsFlightStoreErr drives the first store-error branch in
// listAlerts (ListFlightAlerts fails).
func TestG3ListAlertsFlightStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3lafa", false)
	g1dropTable(t, e, "flight_alerts")
	if w := e.req(t, http.MethodGet, "/api/alerts", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("list flight-alert store err = %d, want 500", w.Code)
	}
}

// TestG3ListAlertsNotificationsStoreErr drives the second branch
// (ListNotifications fails after ListFlightAlerts succeeds): only notifications
// is dropped, so the flight-alert query still works.
func TestG3ListAlertsNotificationsStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3lano", false)
	g1dropTable(t, e, "notifications")
	if w := e.req(t, http.MethodGet, "/api/alerts", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("list notifications store err = %d, want 500", w.Code)
	}
}

// TestG3MarkAlertsReadStoreErrs drives both store-error branches in
// markAlertsRead.
func TestG3MarkAlertsReadStoreErrs(t *testing.T) {
	// Flight branch fails.
	e := setup(t, nil, nil)
	uid := e.user(t, "g3mrfa", false)
	g1dropTable(t, e, "flight_alerts")
	if w := e.req(t, http.MethodPost, "/api/alerts/read", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("mark-read flight store err = %d, want 500", w.Code)
	}

	// Notifications branch fails (flight alerts table intact).
	e2 := setup(t, nil, nil)
	uid2 := e2.user(t, "g3mrno", false)
	g1dropTable(t, e2, "notifications")
	if w := e2.req(t, http.MethodPost, "/api/alerts/read", nil, uid2); w.Code != http.StatusInternalServerError {
		t.Errorf("mark-read notifications store err = %d, want 500", w.Code)
	}
}

// TestG3DeleteAlertBadID drives the invalid-id 400 in deleteAlert.
func TestG3DeleteAlertBadID(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3dabad", false)
	if w := e.req(t, http.MethodDelete, "/api/alerts/flight/notanumber", nil, uid); w.Code != http.StatusBadRequest {
		t.Errorf("delete bad id = %d, want 400", w.Code)
	}
}

// TestG3DeleteAlertStoreErrs drives the store-error branch for each source.
func TestG3DeleteAlertStoreErrs(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3dafa", false)
	g1dropTable(t, e, "flight_alerts")
	if w := e.req(t, http.MethodDelete, "/api/alerts/flight/1", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("delete flight store err = %d, want 500", w.Code)
	}

	e2 := setup(t, nil, nil)
	uid2 := e2.user(t, "g3dano", false)
	g1dropTable(t, e2, "notifications")
	if w := e2.req(t, http.MethodDelete, "/api/alerts/notification/1", nil, uid2); w.Code != http.StatusInternalServerError {
		t.Errorf("delete share store err = %d, want 500", w.Code)
	}
}

// TestG3ClearAlertsStoreErrs drives both store-error branches in clearAlerts.
func TestG3ClearAlertsStoreErrs(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3clfa", false)
	g1dropTable(t, e, "flight_alerts")
	if w := e.req(t, http.MethodDelete, "/api/alerts", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("clear flight store err = %d, want 500", w.Code)
	}

	e2 := setup(t, nil, nil)
	uid2 := e2.user(t, "g3clno", false)
	g1dropTable(t, e2, "notifications")
	if w := e2.req(t, http.MethodDelete, "/api/alerts", nil, uid2); w.Code != http.StatusInternalServerError {
		t.Errorf("clear notifications store err = %d, want 500", w.Code)
	}
}
