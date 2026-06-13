package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dpage/aerly/internal/api"
	aerlymaps "github.com/dpage/aerly/internal/maps"
)

type resolveMapsURLInput struct {
	URL string `json:"url"`
}

// resolveMapsURL turns a pasted Google Maps URL into coordinates: full URLs are
// parsed directly, short maps.app.goo.gl links are followed (server-side) under
// the resolver's host allowlist. 400 for a missing/unsupported URL, 422 when no
// coordinates can be read (e.g. a place-only link).
func (a *API) resolveMapsURL(w http.ResponseWriter, r *http.Request) {
	var in resolveMapsURLInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	lat, lon, ok, err := a.Maps.ResolveURL(r.Context(), rawURL)
	if errors.Is(err, aerlymaps.ErrNotAllowed) {
		writeError(w, http.StatusBadRequest, "not a supported Google Maps URL")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "couldn't read a location from that link")
		return
	}
	writeJSON(w, http.StatusOK, api.CoordsDTO{Lat: lat, Lon: lon})
}
