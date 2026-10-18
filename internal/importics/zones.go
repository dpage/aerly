package importics

import (
	"time"

	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/timing"
)

// settleZones is the last step of every mapper: it records what each end's time
// means, given the DateTime the mapper took it from. start must be the value
// behind StartsAt; end is the value behind EndsAt, or nil when the end time came
// from elsewhere, in which case it is taken to have start's form. An end with no
// EndsAt is left for the store to settle.
//
// A zone the mapper already set came from the event's own airport or GEO, so it
// is confirmed, and a floating time is anchored in it; this is what stops a
// GEO-zoned floating hotel time from being stored as though it were UTC.
func settleZones(part *planops.ConfirmPartInput, start, end *DateTime) {
	part.StartsAt, part.StartTZ, part.StartTZSource = settleEnd(part.StartsAt, part.StartTZ, start)
	if part.EndsAt == nil {
		return
	}
	if end == nil {
		end = start
	}
	t, tz, src := settleEnd(*part.EndsAt, part.EndTZ, end)
	part.EndsAt, part.EndTZ, part.EndTZSource = &t, tz, src
}

// settleFlightZones is settleZones for a flight part, keeping the schedule on
// its flight detail in step with the part's times once they are anchored.
func settleFlightZones(part *planops.ConfirmPartInput, start, end *DateTime) {
	settleZones(part, start, end)
	part.Flight.ScheduledOut = part.StartsAt
	if part.EndsAt != nil {
		part.Flight.ScheduledIn = *part.EndsAt
	}
}

// settleEnd returns one end's instant, zone and source.
func settleEnd(t time.Time, tz string, dt *DateTime) (time.Time, string, string) {
	if !dt.HasTime {
		// Date-only: the UTC-midnight digits are a calendar date, not an
		// instant, so no zone can apply.
		return t, "", string(timing.Floating)
	}
	if timing.ValidZone(tz) {
		if dt.Floating {
			t, _ = timing.Anchor(t, tz)
		}
		return t, tz, string(timing.Confirmed)
	}
	if !dt.Floating && timing.ValidZone(dt.TZID) {
		// parseDateTime already resolved the time in this zone.
		return t, dt.TZID, string(timing.Confirmed)
	}
	if dt.Floating {
		return t, "", string(timing.Floating)
	}
	return t, "", string(timing.Unknown)
}
