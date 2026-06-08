package handlers

import (
	"net/http"
	"sort"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
)

// alertInboxLimit caps GET /api/alerts. The inbox is a recent-activity view,
// not full history (no pagination/pruning yet).
const alertInboxLimit = 50

func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	alerts, err := a.Store.ListFlightAlerts(r.Context(), me.ID, alertInboxLimit)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	notes, err := a.Store.ListNotifications(r.Context(), me.ID, alertInboxLimit)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.NotificationItemDTO, 0, len(alerts)+len(notes))
	for _, al := range alerts {
		tripID := al.TripID
		planID := al.PlanID
		out = append(out, api.NotificationItemDTO{
			ID:        al.ID,
			Kind:      al.Kind,
			TripID:    &tripID,
			PlanID:    &planID,
			Message:   al.Message,
			CreatedAt: al.CreatedAt,
			ReadAt:    al.ReadAt,
		})
	}
	for _, n := range notes {
		out = append(out, api.ToNotificationItemDTO(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	// Each sub-query caps at alertInboxLimit; cap the merged list too so the
	// inbox returns at most the most-recent alertInboxLimit items across both.
	if len(out) > alertInboxLimit {
		out = out[:alertInboxLimit]
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) markAlertsRead(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	if err := a.Store.MarkFlightAlertsRead(r.Context(), me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	if err := a.Store.MarkNotificationsRead(r.Context(), me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	// Recompute + push the badge so other tabs/devices clear too.
	a.publishNotifications(r.Context(), me.ID)
	w.WriteHeader(http.StatusNoContent)
}
