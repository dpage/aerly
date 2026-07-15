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
	// HomeCoords pins the user's exact home location. Absent (nil) leaves the
	// pin unchanged; present with both lat/lon sets it; present with null lat/lon
	// clears it. Both coordinates must be supplied together to pin.
	HomeCoords *homeCoordsReq `json:"home_coords"`
}

type homeCoordsReq struct {
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
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
	var homeLat, homeLon *float64
	if in.HomeCoords != nil {
		homeLat, homeLon = in.HomeCoords.Lat, in.HomeCoords.Lon
		// Pin both or clear both — a lone coordinate is meaningless.
		if (homeLat == nil) != (homeLon == nil) {
			writeError(w, http.StatusBadRequest, "Home coordinates need both a latitude and a longitude.")
			return
		}
		if homeLat != nil && (*homeLat < -90 || *homeLat > 90 || *homeLon < -180 || *homeLon > 180) {
			writeError(w, http.StatusBadRequest, "Home coordinates are out of range.")
			return
		}
	}
	u, err := a.Store.UpdateUser(r.Context(), me.ID, store.UpdateUserPayload{
		HomeAddress: in.HomeAddress,
		PaperSize:   in.PaperSize,
		HideExplore: in.HideExplore,
		HideMaps:    in.HideMaps,
		SetHome:     in.HomeCoords != nil,
		HomeLat:     homeLat,
		HomeLon:     homeLon,
	})
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ToSelfUserDTO(u))
}
