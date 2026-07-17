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
// the resolver's host allowlist.
//
// A link that names a place but carries no coordinates (the iOS "Share" link,
// which identifies its place by a feature ID Google exposes through no API) is
// geocoded from the text it does carry, and returned with needs_confirmation so
// the user can check it. We never plot a geocoded link silently: the geocode is
// a good lead, not the pin the user chose. 400 for a missing/unsupported URL,
// 422 when the link carries no coordinates and either there is no geocoder
// configured (GeoResolver nil, e.g. GEOAPIFY_API_KEY unset) or the geocode
// itself found nothing confident enough to suggest.
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
	lat, lon, ok, hint, err := a.Maps.ResolveURLOrHint(r.Context(), rawURL)
	if errors.Is(err, aerlymaps.ErrNotAllowed) {
		writeError(w, http.StatusBadRequest, "Not a supported Google Maps URL.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, api.ResolvedLocationDTO{Lat: lat, Lon: lon})
		return
	}
	if hint != "" && a.GeoResolver != nil {
		if c, found := a.GeoResolver.Suggest(r.Context(), hint); found {
			writeJSON(w, http.StatusOK, api.ResolvedLocationDTO{
				Lat: c.Lat, Lon: c.Lon, Label: c.Formatted, NeedsConfirmation: true,
			})
			return
		}
	}
	writeError(w, http.StatusUnprocessableEntity,
		"That Google Maps link doesn't contain a location. In Google Maps, long-press the pin and paste the coordinates it copies.")
}
