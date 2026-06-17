package handlers

import (
	"errors"
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// listMyAutoShares returns the caller's "always share with" defaults — the
// people every new trip they create is automatically shared with.
func (a *API) listMyAutoShares(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	shares, err := a.Store.ListAutoShares(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ToAutoShareDTOs(shares))
}

type setAutoShareReq struct {
	Role string `json:"role"` // "viewer" | "editor" | "passenger"
}

// setMyAutoShare creates or updates an auto-share default for the caller: the
// {userId} target is granted the chosen role on every trip the caller creates
// from now on. Idempotent (re-PUT to change the role).
func (a *API) setMyAutoShare(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	targetID, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	if targetID == me.ID {
		writeError(w, http.StatusBadRequest, "Cannot auto-share with yourself.")
		return
	}
	var in setAutoShareReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Role != "viewer" && in.Role != "editor" && in.Role != "passenger" {
		writeError(w, http.StatusBadRequest, "Role must be viewer, editor, or passenger.")
		return
	}
	// The target must exist — guard against dangling references and surface a
	// clean 404 rather than a foreign-key error from the insert.
	if _, err := a.Store.UserByID(r.Context(), targetID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "User not found.")
			return
		}
		handleStoreErr(w, err)
		return
	}
	if err := a.Store.SetAutoShare(r.Context(), me.ID, targetID, in.Role); err != nil {
		handleStoreErr(w, err)
		return
	}
	shares, err := a.Store.ListAutoShares(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ToAutoShareDTOs(shares))
}

// deleteMyAutoShare removes an auto-share default for the caller. Trips already
// shared via this default keep their access — it only stops applying to future
// trips.
func (a *API) deleteMyAutoShare(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	targetID, err := pathID(r, "userId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	if err := a.Store.RemoveAutoShare(r.Context(), me.ID, targetID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
