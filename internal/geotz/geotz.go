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

	// newFinder constructs the timezone finder. It's a package var so tests can
	// substitute a failing constructor and exercise the init-failure path;
	// production always uses tzf's embedded-data finder.
	newFinder = func() (tzf.F, error) { return tzf.NewDefaultFinder() }
)

// tzFinder is the single method of tzf.F that resolve needs. Narrowing it to
// an interface lets the resolution logic be unit-tested with a stub finder.
type tzFinder interface {
	GetTimezoneName(lng, lat float64) string
}

// Lookup returns the IANA timezone name for a coordinate (e.g.
// "America/New_York"), or ("", false) when it can't be resolved (open ocean,
// or the finder failed to initialise). The finder embeds its boundary data, so
// the first call pays a one-off load cost; subsequent calls are cheap and the
// finder is safe for concurrent use.
func Lookup(lat, lon float64) (string, bool) {
	once.Do(initFinder)
	return resolve(finder, initEr, lat, lon)
}

// initFinder builds the shared finder once. A failure is logged here (guarded
// by once.Do) rather than on every Lookup: otherwise a silent ("", false)
// would mean timezone anchoring just never works, with no clue why.
func initFinder() {
	finder, initEr = newFinder()
	if initEr != nil {
		slog.Error("geotz: timezone finder init failed; tz lookups disabled", "err", initEr)
	}
}

// resolve is the pure lookup logic: it reports ("", false) when the finder
// failed to initialise or a coordinate has no zone, and otherwise the zone name.
func resolve(f tzFinder, initErr error, lat, lon float64) (string, bool) {
	if initErr != nil || f == nil {
		return "", false
	}
	name := f.GetTimezoneName(lon, lat)
	if name == "" {
		return "", false
	}
	return name, true
}
