package importics

import (
	"time"

	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/timing"
)

// settleZones is the last step of every mapper: it records what each end's time
// means, given the DateTime the mapper took it from, through Resolve. start must
// be the value behind StartsAt; end is the value behind EndsAt, or nil when the
// end time came from elsewhere, in which case it is taken to have start's form.
// An end with no EndsAt is left for the store to settle. calTZ is the
// calendar's X-WR-TIMEZONE.
//
// A zone the mapper already set came from the event's own airport or GEO, so
// it is the location zone; this is what stops a GEO-zoned floating hotel time
// from being stored as though it were UTC.
func settleZones(part *planops.ConfirmPartInput, start, end *DateTime, calTZ string) {
	part.StartsAt, part.StartTZ, part.StartTZSource = settleEnd(part.StartsAt, part.StartTZ, start, calTZ)
	if part.EndsAt == nil {
		return
	}
	if end == nil {
		end = start
	}
	t, tz, src := settleEnd(*part.EndsAt, part.EndTZ, end, calTZ)
	part.EndsAt, part.EndTZ, part.EndTZSource = &t, tz, src
}

// settleFlightZones is settleZones for a flight part, keeping the schedule on
// its flight detail in step with the part's times once they are anchored.
func settleFlightZones(part *planops.ConfirmPartInput, start, end *DateTime, calTZ string) {
	settleZones(part, start, end, calTZ)
	part.Flight.ScheduledOut = part.StartsAt
	if part.EndsAt != nil {
		part.Flight.ScheduledIn = *part.EndsAt
	}
}

// settleEnd returns one end's instant, zone and source. t is the end's time,
// read in dt's form; locTZ is the zone the mapper found at its location.
func settleEnd(t time.Time, locTZ string, dt *DateTime, calTZ string) (time.Time, string, string) {
	d := *dt
	d.Time = t
	r, ok := Resolve(d, Context{LocationTZ: locTZ, CalendarTZ: calTZ})
	if !ok {
		// No time to place: keep it as it is, with no zone, by its form.
		if dt.Floating || !dt.HasTime {
			return t, "", string(timing.Floating)
		}
		return t, "", string(timing.Unknown)
	}
	return r.At, r.TZ, string(r.Source)
}
