// Package timing owns Aerly's time zone rules: which zones are valid, how a
// wall-clock time becomes an instant, and how a missing zone is inferred. It
// must not import internal/store, which depends on it.
package timing

import (
	"errors"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/geotz"
)

// Source records where a part end's zone came from (spec section 1).
type Source string

const (
	Confirmed Source = "confirmed" // the item's own place, a provider, a TZID or the user
	Inferred  Source = "inferred"  // borrowed from a neighbouring part or the trip
	Unknown   Source = "unknown"   // true instant, zone not known
	Floating  Source = "floating"  // zone not known; the UTC digits are local wall-clock time
)

func (s Source) Valid() bool {
	switch s {
	case Confirmed, Inferred, Unknown, Floating:
		return true
	}
	return false
}

var (
	ErrInvalidZone      = errors.New("invalid time zone")
	ErrNonexistentLocal = errors.New("local time does not exist in that zone")
)

// ValidZone reports whether tz is a loadable IANA zone name. "Local" is
// refused because it means the server's zone, never an item's.
func ValidZone(tz string) bool {
	if tz == "" || tz == "Local" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

// candidates returns every instant whose wall clock in loc reads the given
// digits: none in a spring-forward gap, two in a fall-back overlap, earliest
// first.
func candidates(y int, m time.Month, d, hh, mm, ss, ns int, loc *time.Location) []time.Time {
	naive := time.Date(y, m, d, hh, mm, ss, ns, time.UTC)
	var out []time.Time
	seen := map[int]bool{}
	// Offsets in force within a day either side cover every real transition.
	for _, probe := range []time.Duration{-24 * time.Hour, 0, 24 * time.Hour} {
		_, off := naive.Add(probe).In(loc).Zone()
		if seen[off] {
			continue
		}
		seen[off] = true
		c := naive.Add(-time.Duration(off) * time.Second)
		l := c.In(loc)
		if l.Year() == y && l.Month() == m && l.Day() == d && l.Hour() == hh && l.Minute() == mm {
			out = append(out, c)
		}
	}
	for i := 1; i < len(out); i++ { // at most two; keep earliest first
		if out[i].Before(out[0]) {
			out[0], out[i] = out[i], out[0]
		}
	}
	return out
}

// LocalInstant converts a wall-clock time at tz to an instant. A time skipped
// by a spring-forward change is an error; a repeated one takes the earlier.
func LocalInstant(y int, m time.Month, d, hh, mm int, tz string) (time.Time, error) {
	if !ValidZone(tz) {
		return time.Time{}, ErrInvalidZone
	}
	loc, _ := time.LoadLocation(tz)
	c := candidates(y, m, d, hh, mm, 0, 0, loc)
	if len(c) == 0 {
		return time.Time{}, ErrNonexistentLocal
	}
	return c[0], nil
}

// Anchor treats t's UTC digits as wall-clock time in tz (a floating time) and
// returns the instant. Unlike LocalInstant it never refuses a gap time: data
// already stored must anchor somewhere, so it takes Go's normalised instant.
func Anchor(t time.Time, tz string) (time.Time, bool) {
	if !ValidZone(tz) {
		return t, false
	}
	loc, _ := time.LoadLocation(tz)
	u := t.UTC()
	if c := candidates(u.Year(), u.Month(), u.Day(), u.Hour(), u.Minute(), u.Second(), u.Nanosecond(), loc); len(c) > 0 {
		return c[0], true
	}
	return time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), u.Minute(), u.Second(), u.Nanosecond(), loc), true
}

// Hints are what Infer may draw on, strongest first.
type Hints struct {
	Lat, Lon    *float64
	IATA        string
	NeighbourTZ string
	TripTZ      string
}

// Infer picks a zone from the hints: the item's own coordinates or airport
// are Confirmed; a neighbouring part's or the trip's zone is Inferred.
func Infer(h Hints) (string, Source) {
	if h.Lat != nil && h.Lon != nil {
		if tz, ok := geotz.Lookup(*h.Lat, *h.Lon); ok && ValidZone(tz) {
			return tz, Confirmed
		}
	}
	if h.IATA != "" {
		if tz, ok := airports.LookupTZ(h.IATA); ok {
			return tz, Confirmed
		}
	}
	for _, tz := range []string{h.NeighbourTZ, h.TripTZ} {
		if ValidZone(tz) {
			return tz, Inferred
		}
	}
	return "", Unknown
}

// AirportZone is the zone a flight end's clocks run on: the embedded airport
// table first, since it is authoritative for the codes it names, then the
// airport's own coordinates (a provider fix), which catch off-table airports.
// Both count as the item's own place, so a hit is Confirmed. Unlike Infer it
// tries the code before the coordinates, and it never borrows a neighbour's.
func AirportZone(iata string, lat, lon *float64) (string, bool) {
	if tz, ok := airports.LookupTZ(iata); ok {
		return tz, true
	}
	if lat != nil && lon != nil {
		if tz, ok := geotz.Lookup(*lat, *lon); ok && ValidZone(tz) {
			return tz, true
		}
	}
	return "", false
}
