package handlers

import (
	"net/http"

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/auth"
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
