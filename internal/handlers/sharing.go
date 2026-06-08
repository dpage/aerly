package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/auth"
)

type shareAllFriendsTripReq struct {
	Role string `json:"role"` // "viewer"|"editor"|"" (clear)
}

// setTripShareAllFriends toggles the persistent trip-level "share with all
// friends" grant: every accepted friend gets the chosen role until it's cleared
// with an empty role. Owner-only, mirroring member management.
func (a *API) setTripShareAllFriends(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripOwner(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareAllFriendsTripReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Role != "" && in.Role != "viewer" && in.Role != "editor" {
		writeError(w, http.StatusBadRequest, "role must be viewer, editor, or empty")
		return
	}
	if err := a.Store.SetTripShareAllFriends(r.Context(), id, in.Role); err != nil {
		handleStoreErr(w, err)
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), id)
	writeJSON(w, http.StatusOK, dto)
}

type shareAllFriendsPlanReq struct {
	Enabled bool `json:"enabled"`
}

// setPlanShareAllFriends toggles the per-plan "share with all friends" grant.
// Requires editor rights on the plan's trip.
func (a *API) setPlanShareAllFriends(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareAllFriendsPlanReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SetPlanShareAllFriends(r.Context(), id, in.Enabled); err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}
