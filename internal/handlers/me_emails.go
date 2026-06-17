package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// emailIngestDisabledMsg is the error body returned by /api/me/emails
// endpoints when email ingest is turned off.
const emailIngestDisabledMsg = "email ingest is disabled"

func (a *API) listMyEmails(w http.ResponseWriter, r *http.Request) {
	if a.Config == nil || !a.Config.EmailIngestEnabled {
		writeError(w, http.StatusServiceUnavailable, emailIngestDisabledMsg)
		return
	}
	u := auth.UserFrom(r.Context())
	emails, err := a.Store.EmailsByUser(r.Context(), u.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.UserEmailDTO, 0, len(emails))
	for _, e := range emails {
		out = append(out, api.ToUserEmailDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

type addEmailReq struct {
	Address string `json:"address"`
}

func (a *API) addMyEmail(w http.ResponseWriter, r *http.Request) {
	if a.Config == nil || !a.Config.EmailIngestEnabled {
		writeError(w, http.StatusServiceUnavailable, emailIngestDisabledMsg)
		return
	}
	var in addEmailReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(in.Address) == "" {
		writeError(w, http.StatusBadRequest, "address required")
		return
	}
	u := auth.UserFrom(r.Context())
	row, token, err := a.Store.InsertUnverifiedEmail(r.Context(), u.ID, in.Address)
	switch {
	case errors.Is(err, store.ErrAddressTaken):
		// The address column is globally unique across accounts, so a conflict
		// means either the signed-in user is re-adding one of their own
		// addresses, or (the confusing case) it already belongs to a different
		// account and so never appears in this account's list. Distinguish the
		// two so the message actually explains what happened.
		msg := "That email address is already registered to another Aerly account."
		if mine, e := a.Store.EmailsByUser(r.Context(), u.ID); e == nil {
			for _, em := range mine {
				if strings.EqualFold(em.Address, strings.TrimSpace(in.Address)) {
					msg = "You've already added that address."
					break
				}
			}
		}
		writeError(w, http.StatusConflict, msg)
		return
	case err != nil:
		handleStoreErr(w, err)
		return
	}
	if err := a.SendVerifyEmail(r.Context(), row.Address, token); err != nil {
		// Drop the just-inserted row so the user can re-try cleanly.
		_ = a.Store.DeleteUserEmail(r.Context(), u.ID, row.ID)
		writeError(w, http.StatusBadGateway, "could not send verification email")
		return
	}
	writeJSON(w, http.StatusCreated, api.ToUserEmailDTO(row))
}

func (a *API) resendMyEmail(w http.ResponseWriter, r *http.Request) {
	if a.Config == nil || !a.Config.EmailIngestEnabled {
		writeError(w, http.StatusServiceUnavailable, emailIngestDisabledMsg)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	u := auth.UserFrom(r.Context())
	row, token, err := a.Store.ResendVerification(r.Context(), u.ID, id)
	switch {
	case errors.Is(err, store.ErrAlreadyVerified):
		writeError(w, http.StatusBadRequest, "address already verified")
		return
	case err != nil:
		handleStoreErr(w, err)
		return
	}
	if err := a.SendVerifyEmail(r.Context(), row.Address, token); err != nil {
		writeError(w, http.StatusBadGateway, "could not send verification email")
		return
	}
	writeJSON(w, http.StatusOK, api.ToUserEmailDTO(row))
}

func (a *API) deleteMyEmail(w http.ResponseWriter, r *http.Request) {
	if a.Config == nil || !a.Config.EmailIngestEnabled {
		writeError(w, http.StatusServiceUnavailable, emailIngestDisabledMsg)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	u := auth.UserFrom(r.Context())
	if err := a.Store.DeleteUserEmail(r.Context(), u.ID, id); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
