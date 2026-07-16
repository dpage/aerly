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
// the resolver's host allowlist. 400 for a missing/unsupported URL, 422 when the
// link carries no coordinates (e.g. an iOS "Share" link, which names the place
// by feature ID only — Google won't hand its pin to a key-less server request,
// so we decline rather than guess).
func (a *API) resolveMapsURL(w http.ResponseWriter, r *http.Request) {
	var in resolveMapsURLInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		writeError(w, http.StatusBadRequest, "A URL is required.")
		return
	}
	lat, lon, ok, err := a.Maps.ResolveURL(r.Context(), rawURL)
	if errors.Is(err, aerlymaps.ErrNotAllowed) {
		writeError(w, http.StatusBadRequest, "Not a supported Google Maps URL.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusUnprocessableEntity,
			"That Google Maps link doesn't contain a location. In Google Maps, long-press the pin and paste the coordinates it copies.")
		return
	}
	writeJSON(w, http.StatusOK, api.CoordsDTO{Lat: lat, Lon: lon})
}
