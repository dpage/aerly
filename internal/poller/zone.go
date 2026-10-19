package poller

import (
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// Timezone resolution for the notifications the poller sends (issue #117).
//
// A plan part usually carries its own IANA zone, but flight parts often don't:
// the airline-data provider hands us a UTC instant and an IATA code, and the
// zone is only filled in when the ingest path happened to recognise the
// airport. The API layer already papers over that for the client (see
// api.ToPlanPartDTO), so the timeline renders airport-local times whilst the
// reminder email and the delay alert fell back to UTC. These helpers give the
// notification paths the same resolution the client gets.

// airportZone returns the IANA zone an airport's clocks run on: the embedded
// airport table first, since it covers the codes the provider quotes, then the
// coordinate the row carries, which catches the off-table airports the table
// doesn't name. It returns "" when neither resolves, leaving the caller to fall
// back to UTC rather than invent a zone.
func airportZone(iata string, lat, lon *float64) string {
	tz, _ := timing.AirportZone(iata, lat, lon)
	return tz
}

// partZone picks the zone a notification about a plan part should be written
// in: the part's own zone when it stored one, else the departure airport's for
// a flight, else the zone under whatever coordinate the part was geocoded to,
// which covers hotels and transfers ingested before coordinate-based anchoring
// existed. "" leaves the mailer on its UTC fallback. Shared by the upcoming-plan
// reminders and the check-in reminders, which want the same answer.
func partZone(storedTZ, iata string, lat, lon *float64) string {
	if storedTZ != "" {
		return storedTZ
	}
	return airportZone(iata, lat, lon)
}

// inZone renders t against the named IANA zone, falling back to UTC when the
// name is empty or the zone database doesn't know it. Mirrors the mailer's own
// fallback so an alert and a reminder for the same flight agree.
func inZone(t time.Time, tz string) time.Time {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return t.In(loc)
		}
	}
	return t.UTC()
}

// partBlock is the label block for a plan part's start, following the zone
// rulings of the web: a stored zone is used as its source says; a floating
// part keeps its bare digits and is never moved into the zone its airport or
// coordinates would give; and a part whose zone is unknown borrows its
// airport's or coordinate's zone, which makes that zone inferred. With
// nothing to go on the start stays an unknown true instant.
func partBlock(at time.Time, storedTZ, source, iata string, lat, lon *float64) timing.Block {
	src := timing.Source(source)
	if src == timing.Floating {
		return timing.Block{At: at, Source: timing.Floating}
	}
	if storedTZ != "" {
		if !src.Valid() {
			// A zone stored before sources were recorded counts as given.
			src = timing.Confirmed
		}
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
// the source it was stored with (a zone stored before sources were recorded
// counts as given), else the departure airport's.
func startZone(storedTZ, storedSource, iata string, lat, lon *float64) endZone {
	if storedTZ == "" {
		return flightZone(iata, lat, lon)
	}
	src := timing.Source(storedSource)
	if !src.Valid() {
		src = timing.Confirmed
	}
	return endZone{TZ: storedTZ, Source: src}
}

// instantBlock is the label block for a flight time, which is always a true
// instant: in its zone, with that zone's source, when one is known, else
// unknown, so the label falls back to the recipient's home zone or UTC.
func instantBlock(at time.Time, kind timing.Kind, z endZone, iata string) timing.Block {
	if z.TZ == "" {
		return timing.Block{At: at, Source: timing.Unknown, Kind: kind}
	}
	return timing.Block{At: at, TZ: z.TZ, Source: z.Source, Place: timing.PlaceName(z.TZ, iata), Kind: kind}
}

// labelDay is the calendar date a block is shown on, judged in the zone its
// label is written in: its own zone, UTC for a floating block, and the home
// zone (UTC without one) for an unknown one.
func labelDay(t time.Time, b timing.Block, home string) string {
	tz := b.TZ
	if tz == "" && b.Source != timing.Floating {
		tz = home
	}
	return inZone(t, tz).Format("2006-01-02")
}
