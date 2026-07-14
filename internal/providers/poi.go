package providers

import (
	"context"
	"errors"
	"net/http"
	"sort"
)

// ErrPOIUnavailable signals that the upstream POI service answered with a
// transient failure (rate limit or gateway timeout) rather than a result. It's
// distinct from a hard error so the handler can return a try-again-later
// response instead of a blunt 500.
var ErrPOIUnavailable = errors.New("poi: upstream temporarily unavailable")

// POI is a normalised point of interest, independent of which provider
// resolved it.
type POI struct {
	ID        string // stable, provider-scoped id (used as a UI key), e.g. a Geoapify place_id
	Name      string
	Category  string
	Lat, Lon  float64
	DistanceM int
	Address   string
	// Description is a short free-text blurb from the OSM `description` tag,
	// where present. It's sparse (most POIs have none), so callers should treat
	// it as optional and omit any UI line when it's empty.
	Description string
	Wikidata    string
	Wikipedia   string
	Website     string
}

// POIResolver looks up points of interest around a coordinate.
type POIResolver interface {
	Nearby(ctx context.Context, lat, lon float64, radiusM int, cats []string) ([]POI, error)
}

// sortByDistance orders POIs nearest-first, a stable contract every resolver
// returns.
func sortByDistance(pois []POI) {
	sort.SliceStable(pois, func(i, j int) bool { return pois[i].DistanceM < pois[j].DistanceM })
}

// isTransientStatus reports whether an upstream HTTP status is worth treating
// as temporary (and mapping to ErrPOIUnavailable) rather than a hard failure.
func isTransientStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, // 429
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}
