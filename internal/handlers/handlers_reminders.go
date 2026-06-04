package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/auth"
)

// Upcoming-plan reminders (issue #11): per-viewer opt-in at the trip level,
// overridable per plan. These endpoints mutate the requesting user's own
// opt-in; the per-viewer state is read back via the trip/plan DTOs.

// clampLead bounds a lead-hours value to a sane range (1h .. 1yr). Defaults to
// 24 when non-positive.
func clampLead(h int) int {
	if h <= 0 {
		return 24
	}
	if h > 8760 {
		return 8760
	}
	return h
}

// reminderInput is the PUT body for both the trip and plan reminder endpoints.
// Enabled is only meaningful for the per-plan override.
type reminderInput struct {
	LeadHours int  `json:"lead_hours"`
	Enabled   bool `json:"enabled"`
}

func (a *API) setTripReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid trip id")
		return
	}
	// Use the superuser-aware visibility helper (mirrors getTrip) so a superuser
	// isn't blocked on a trip they can otherwise view.
	ok, err := a.canViewTrip(r.Context(), tripID, me)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var in reminderInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := a.Store.SetTripReminder(r.Context(), tripID, me.ID, clampLead(in.LeadHours)); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteTripReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid trip id")
		return
	}
	if err := a.Store.RemoveTripReminder(r.Context(), tripID, me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setPlanReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	planID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	// A viewer may only set a reminder on a plan they can see (spec §4 gate);
	// otherwise it would leak the plan's existence.
	ok, err := a.Store.CanViewPlan(r.Context(), planID, me.ID, me.IsSuperuser)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var in reminderInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := a.Store.SetPlanReminder(r.Context(), planID, me.ID, in.Enabled, clampLead(in.LeadHours)); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deletePlanReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	planID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	if err := a.Store.RemovePlanReminder(r.Context(), planID, me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
