package poller

import (
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/geotz"
	"github.com/dpage/aerly/internal/store"
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
	if tz, ok := airports.LookupTZ(iata); ok {
		return tz
	}
	if lat != nil && lon != nil {
		if tz, ok := geotz.Lookup(*lat, *lon); ok {
			return tz
		}
	}
	return ""
}

// reminderZone picks the zone an upcoming-plan reminder should be written in:
// the part's own zone when it stored one, else the departure airport's for a
// flight, else the zone under whatever coordinate the part was geocoded to,
// which covers hotels and transfers ingested before coordinate-based anchoring
// existed. "" leaves the mailer on its UTC fallback.
func reminderZone(d store.DueReminder) string {
	if d.StartTZ != "" {
		return d.StartTZ
	}
	return airportZone(d.OriginIATA, d.StartLat, d.StartLon)
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
