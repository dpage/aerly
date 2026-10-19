package api

import (
	"time"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// tripEdgeDate is the local date the instant at falls on at one end of a
// trip, and the zone its midnight is read in. The date is timing.Day's, so a
// floating end reads in its UTC digits and an unknown one in UTC (a trip has
// no viewer's home zone here); the midnight uses the zone when it has one.
func tripEdgeDate(at time.Time, tz, source string) (date time.Time, loc *time.Location) {
	b := timing.Block{At: at, TZ: tz, Source: timing.Source(source)}
	loc = time.UTC
	if b.TZ != "" && timing.ValidZone(b.TZ) {
		if l, err := time.LoadLocation(b.TZ); err == nil {
			loc = l
		}
	}
	return timing.Day(b, ""), loc
}

// tripSpanEdges returns the instants the trip runs between: midnight starting
// its first day in its first item's zone, and midnight ending its last day in
// its last item's zone. The days are the explicit dates when set, else the
// inferred ones read in those zones. Nil where the trip has no such day.
func tripSpanEdges(t *store.Trip) (start, end *time.Time) {
	if date, loc, ok := tripEdge(t.StartsOn, t.EffectiveStart, t.FirstTZ, t.FirstTZSource); ok {
		s := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
		start = &s
	}
	if date, loc, ok := tripEdge(t.EndsOn, t.EffectiveEnd, t.LastTZ, t.LastTZSource); ok {
		e := time.Date(date.Year(), date.Month(), date.Day()+1, 0, 0, 0, 0, loc)
		end = &e
	}
	return start, end
}

// tripEdge is the day at one end of a trip: the explicit date when set, else
// the inferred instant's local date. ok is false where the trip has neither.
func tripEdge(date, inferred *time.Time, tz, source string) (day time.Time, loc *time.Location, ok bool) {
	switch {
	case date != nil:
		_, loc = tripEdgeDate(*date, tz, source)
		return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC), loc, true
	case inferred != nil:
		day, loc = tripEdgeDate(*inferred, tz, source)
		return day, loc, true
	}
	return time.Time{}, nil, false
}
