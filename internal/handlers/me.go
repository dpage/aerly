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
	// HomeAddress is a self-editable profile field. Pointer so an absent field
	// leaves it unchanged; send "" to clear it.
	HomeAddress *string `json:"home_address"`
	// PaperSize is the PDF-itinerary page-size preference. Pointer so an absent
	// field leaves it unchanged; only "a4"/"letter" are accepted.
	PaperSize *string `json:"paper_size"`
	// HideExplore / HideMaps are the feature-hiding preferences. Pointer so an
	// absent field leaves it unchanged.
	HideExplore *bool `json:"hide_explore"`
	HideMaps    *bool `json:"hide_maps"`
}

func (a *API) updateMe(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var in updateMeReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.PaperSize != nil && *in.PaperSize != "a4" && *in.PaperSize != "letter" {
		writeError(w, http.StatusBadRequest, "Paper size must be \"a4\" or \"letter\".")
		return
	}
	u, err := a.Store.UpdateUser(r.Context(), me.ID, store.UpdateUserPayload{
		HomeAddress: in.HomeAddress,
		PaperSize:   in.PaperSize,
		HideExplore: in.HideExplore,
		HideMaps:    in.HideMaps,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ToSelfUserDTO(u))
}
