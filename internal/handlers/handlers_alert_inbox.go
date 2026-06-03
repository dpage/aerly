package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
)

// alertInboxLimit caps GET /api/alerts. The inbox is a recent-activity view,
// not full history (no pagination/pruning yet).
const alertInboxLimit = 50

func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	rows, err := a.Store.ListFlightAlerts(r.Context(), me.ID, alertInboxLimit)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FlightAlertDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, api.ToFlightAlertDTO(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) markAlertsRead(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	if err := a.Store.MarkFlightAlertsRead(r.Context(), me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	// Recompute + push the badge so other tabs/devices clear too.
	a.publishNotifications(r.Context(), me.ID)
	w.WriteHeader(http.StatusNoContent)
}
