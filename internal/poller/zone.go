package poller

import (
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// Timezone resolution for the notifications the poller sends (issue #117).
//
// A plan part usually carries its own IANA zone, but flight parts often don't:
// the airline-data provider hands us a UTC instant and an IATA code. These
// helpers label such a time by its airport's zone, and leave a floating or
// unknown one to the label rules in the timing package.

// airportZone returns the IANA zone an airport's clocks run on: the embedded
// airport table first, since it covers the codes the provider quotes, then the
// coordinate the row carries, which catches the off-table airports the table
// doesn't name. It returns "" when neither resolves, and the caller labels the
// time as unknown rather than inventing a zone.
func airportZone(iata string, lat, lon *float64) string {
	tz, _ := timing.AirportZone(iata, lat, lon)
	return tz
}

// partBlock is the label block for a plan part's start, following the zone
// rulings of the web: a stored zone is used as its source says; a floating
// part keeps its bare digits and is never moved into the zone its airport or
// coordinates would give; and a part with no stored zone borrows its
// airport's or coordinate's zone, which makes that zone inferred. With
// nothing to go on the start stays an unknown true instant.
func partBlock(at time.Time, storedTZ, source, iata string, lat, lon *float64) timing.Block {
	src := timing.Source(source)
	if src == timing.Floating {
		return timing.Block{At: at, Source: timing.Floating}
	}
	if storedTZ != "" {
		return timing.Block{At: at, TZ: storedTZ, Source: src, Place: timing.PlaceName(storedTZ, iata)}
	}
	if tz := airportZone(iata, lat, lon); tz != "" {
		return timing.Block{At: at, TZ: tz, Source: timing.Inferred, Place: timing.PlaceName(tz, iata)}
	}
	return timing.Block{At: at, Source: timing.Unknown}
}

// endZone is the zone a flight end is labelled in, with the source to label
// it by.
type endZone struct {
	TZ     string
	Source timing.Source
}

// flightZone is an airport's zone for a flight end. timing.AirportZone counts
// a table or coordinate hit as the airport's own place, so it is Confirmed;
// with neither the end is Unknown.
func flightZone(iata string, lat, lon *float64) endZone {
	if tz := airportZone(iata, lat, lon); tz != "" {
		return endZone{TZ: tz, Source: timing.Confirmed}
	}
	return endZone{Source: timing.Unknown}
}

// startZone is the zone for a flight's departure: the part's stored zone with
// the source it was stored with. A floating start has no zone and is not given
// its airport's at read time, since its digits are wall-clock time; it is
// labelled as floating. Otherwise, with no stored zone, the departure
// airport's.
func startZone(storedTZ, storedSource, iata string, lat, lon *float64) endZone {
	if timing.Source(storedSource) == timing.Floating {
		return endZone{Source: timing.Floating}
	}
	if storedTZ == "" {
		return flightZone(iata, lat, lon)
	}
	return endZone{TZ: storedTZ, Source: timing.Source(storedSource)}
}

// instantBlock is the label block for a flight time: in its zone, with that
// zone's source, when one is known, else without a zone and with the source
// given (floating, or unknown so the label falls back to the recipient's home
// zone or UTC).
func instantBlock(at time.Time, kind timing.Kind, z endZone, iata string) timing.Block {
	if z.TZ == "" {
		src := z.Source
		if src != timing.Floating {
			src = timing.Unknown
		}
		return timing.Block{At: at, Source: src, Kind: kind}
	}
	return timing.Block{At: at, TZ: z.TZ, Source: z.Source, Place: timing.PlaceName(z.TZ, iata), Kind: kind}
}

// labelDay is the calendar date t falls on as the block b is labelled: its own
// zone, UTC digits for a floating block, and the home zone (UTC without one)
// for an unknown one.
func labelDay(t time.Time, b timing.Block, home string) string {
	b.At = t
	return timing.Day(b, home).Format("2006-01-02")
}
