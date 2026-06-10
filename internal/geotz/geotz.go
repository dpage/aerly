// Package geotz resolves an IANA timezone name from coordinates, so geocoded
// plan venues (hotels, restaurants, taxi pickups…) that arrive with only a
// floating local wall-clock can be anchored to their real zone — and rendered
// in correct local time with a proper abbreviation (PRD §6.2).
package geotz

import (
	"log/slog"
	"sync"

	"github.com/ringsaturn/tzf"
)

var (
	once   sync.Once
	finder tzf.F
	initEr error
)

// Lookup returns the IANA timezone name for a coordinate (e.g.
// "America/New_York"), or ("", false) when it can't be resolved (open ocean,
// or the finder failed to initialise). The finder embeds its boundary data, so
// the first call pays a one-off load cost; subsequent calls are cheap and the
// finder is safe for concurrent use.
func Lookup(lat, lon float64) (string, bool) {
	once.Do(func() {
		finder, initEr = tzf.NewDefaultFinder()
		if initEr != nil {
			// Logged once (guarded by once.Do): otherwise every Lookup silently
			// returns ("", false) forever and timezone anchoring just never works.
			slog.Error("geotz: timezone finder init failed; tz lookups disabled", "err", initEr)
		}
	})
	if initEr != nil || finder == nil {
		return "", false
	}
	name := finder.GetTimezoneName(lon, lat)
	if name == "" {
		return "", false
	}
	return name, true
}
