package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	writeJSON(w, http.StatusOK, api.ToSelfUserDTO(u))
}

type updateMeReq struct {
	// HomeAddress is the only self-editable profile field for now. Pointer so an
	// absent field leaves it unchanged; send "" to clear it.
	HomeAddress *string `json:"home_address"`
}

func (a *API) updateMe(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var in updateMeReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := a.Store.UpdateUser(r.Context(), me.ID, store.UpdateUserPayload{HomeAddress: in.HomeAddress})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ToSelfUserDTO(u))
}
