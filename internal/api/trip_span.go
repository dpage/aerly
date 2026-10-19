package api

import (
	"time"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// tripZone loads tz, or UTC for a blank or unusable zone: a trip whose edge
// item has no zone reads its dates as stored (floating digits are local time).
func tripZone(tz string) *time.Location {
	if timing.ValidZone(tz) {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}

// tripSpanEdges returns the instants the trip runs between: midnight starting
// its first day in its first item's zone, and midnight ending its last day in
// its last item's zone. The days are the explicit dates when set, else the
// inferred ones read in those zones. Nil where the trip has no such day.
func tripSpanEdges(t *store.Trip) (start, end *time.Time) {
	first, last := tripZone(t.FirstTZ), tripZone(t.LastTZ)
	day := func(date, inferred *time.Time, loc *time.Location) (y int, m time.Month, d int, ok bool) {
		switch {
		case date != nil:
			y, m, d = date.Date()
			return y, m, d, true
		case inferred != nil:
			y, m, d = inferred.In(loc).Date()
			return y, m, d, true
		}
		return 0, 0, 0, false
	}
	if y, m, d, ok := day(t.StartsOn, t.EffectiveStart, first); ok {
		s := time.Date(y, m, d, 0, 0, 0, 0, first)
		start = &s
	}
	if y, m, d, ok := day(t.EndsOn, t.EffectiveEnd, last); ok {
		e := time.Date(y, m, d+1, 0, 0, 0, 0, last)
		end = &e
	}
	return start, end
}
