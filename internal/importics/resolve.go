package importics

import (
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// Context is what the caller knows beyond the DateTime itself.
type Context struct {
	LocationTZ string // from the item's own airport or GEO; "" if none
	CalendarTZ string // X-WR-TIMEZONE; "" if none or invalid
	FallbackTZ string // feeds only: trip_feeds.timezone
}

// Resolved is one imported time settled: its instant, its zone and where the
// zone came from. AllDay marks a date with no time of day, whose At is that
// date at UTC midnight.
type Resolved struct {
	At     time.Time
	TZ     string
	Source timing.Source
	AllDay bool
}

// Resolve decides an imported time's instant, zone and zone source, for the
// importers' mappers and the calendar feed reader alike. The first rule that
// applies wins:
//
//  1. A date with no time of day: all day, no zone, floating.
//  2. The time's own TZID (an IANA name, or a mapped Windows one): confirmed.
//  3. An instant with the item's own location zone: confirmed.
//  4. An instant with a calendar or fallback zone: inferred.
//  5. An instant and nothing else: no zone, unknown.
//  6. Floating digits with a location zone: anchored there, confirmed.
//  7. Floating digits with a calendar, then fallback, zone: anchored, inferred.
//  8. Floating digits and nothing else: untouched, no zone, floating.
//
// Zone names that do not load are skipped. ok is false when dt has no time
// at all, as for a missing or unparseable value.
func Resolve(dt DateTime, c Context) (Resolved, bool) {
	if dt.Time.IsZero() {
		return Resolved{}, false
	}
	if !dt.HasTime {
		d := dt.Time
		return Resolved{
			At:     time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC),
			Source: timing.Floating,
			AllDay: true,
		}, true
	}
	if timing.ValidZone(dt.TZID) {
		// The parser never leaves a valid TZID floating, but a hand-built
		// value might, and its own zone is still the one to anchor it in.
		return settle(dt, dt.TZID, timing.Confirmed), true
	}
	if timing.ValidZone(c.LocationTZ) {
		return settle(dt, c.LocationTZ, timing.Confirmed), true
	}
	for _, tz := range []string{c.CalendarTZ, c.FallbackTZ} {
		if timing.ValidZone(tz) {
			return settle(dt, tz, timing.Inferred), true
		}
	}
	if dt.Floating {
		return Resolved{At: dt.Time.UTC(), Source: timing.Floating}, true
	}
	return Resolved{At: dt.Time.UTC(), Source: timing.Unknown}, true
}

// settle gives dt the zone tz: a true instant keeps its moment, and floating
// digits are anchored in tz.
func settle(dt DateTime, tz string, src timing.Source) Resolved {
	at := dt.Time
	if dt.Floating {
		at, _ = timing.Anchor(at, tz)
	}
	return Resolved{At: at.UTC(), TZ: tz, Source: src}
}
