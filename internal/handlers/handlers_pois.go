package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/providers"
)

const (
	poiDefaultRadius = 2000
	poiMaxRadius     = 10000
)

// getTripPOIs answers GET /api/trips/{id}/pois: nearby sightseeing suggestions
// around an explicit lat/lon or a geocoded place, restricted to viewers of the
// trip.
func (a *API) getTripPOIs(w http.ResponseWriter, r *http.Request) {
	if a.POIs == nil {
		writeError(w, http.StatusNotImplemented, "POI lookups aren't available.")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	ok, err := a.canViewTrip(r.Context(), id, me)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}

	q := r.URL.Query()
	radius := atoiDefault(q.Get("radius"), poiDefaultRadius)
	if radius <= 0 {
		radius = poiDefaultRadius
	} else if radius > poiMaxRadius {
		radius = poiMaxRadius
	}
	cats := splitCats(q.Get("cats"))
	if len(cats) == 0 {
		cats = []string{"sights", "museum", "landmark", "park"}
	}

	lat, lon, label, err := a.resolvePOICenter(r, q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't work out where to search.")
		return
	}

	pois, err := a.POIs.Nearby(r.Context(), lat, lon, radius, cats)
	if err != nil {
		if errors.Is(err, providers.ErrPOIUnavailable) {
			// The upstream OSM instances are rate-limiting or timing out; this
			// is transient, so tell the user to try again rather than 500.
			slog.Warn("overpass unavailable", "err", err)
			writeError(w, http.StatusServiceUnavailable,
				"Nearby places are temporarily unavailable — please try again in a moment.")
			return
		}
		serverError(w, err)
		return
	}
	if nameQ := strings.TrimSpace(q.Get("q")); nameQ != "" {
		pois = filterByName(pois, nameQ)
	}

	writeJSON(w, http.StatusOK, api.PoiResponseDTO{
		Center:      api.CoordsDTO{Lat: lat, Lon: lon},
		CenterLabel: label,
		Pois:        toPoiDTOs(pois),
	})
}

// resolvePOICenter uses explicit lat/lon query params when supplied,
// otherwise geocodes the `place` param. It returns a plain error (not a typed
// sentinel) since the handler always maps failure to a single 400.
func (a *API) resolvePOICenter(r *http.Request, q url.Values) (lat, lon float64, label string, err error) {
	latS, lonS := q.Get("lat"), q.Get("lon")
	if latS != "" && lonS != "" {
		parsedLat, latErr := strconv.ParseFloat(latS, 64)
		parsedLon, lonErr := strconv.ParseFloat(lonS, 64)
		if latErr != nil || lonErr != nil {
			return 0, 0, "", fmt.Errorf("bad coords")
		}
		return parsedLat, parsedLon, "", nil
	}
	place := strings.TrimSpace(q.Get("place"))
	if place == "" || a.Geocoder == nil {
		return 0, 0, "", fmt.Errorf("no location")
	}
	glat, glon, ok, err := a.Geocoder.Geocode(r.Context(), place, "")
	if err != nil || !ok {
		return 0, 0, "", fmt.Errorf("geocode failed")
	}
	return glat, glon, place, nil
}

func toPoiDTOs(pois []providers.POI) []api.PoiDTO {
	out := make([]api.PoiDTO, 0, len(pois))
	for _, p := range pois {
		out = append(out, api.PoiDTO{
			ID:        p.ID,
			Name:      p.Name,
			Category:  p.Category,
			Lat:       p.Lat,
			Lon:       p.Lon,
			DistanceM: p.DistanceM,
			Address:   p.Address,
			Wikidata:  p.Wikidata,
			Wikipedia: p.Wikipedia,
			Website:   p.Website,
		})
	}
	return out
}

func splitCats(s string) []string {
	out := []string{}
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func filterByName(pois []providers.POI, q string) []providers.POI {
	q = strings.ToLower(q)
	out := pois[:0]
	for _, p := range pois {
		if strings.Contains(strings.ToLower(p.Name), q) {
			out = append(out, p)
		}
	}
	return out
}

func atoiDefault(s string, d int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return d
}
