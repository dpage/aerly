package planops

import (
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/timing"
)

// transferArrivalBuffer is added to a flight's arrival to time an airport→hotel
// transfer's start — clearing immigration + baggage before the car leaves.
// Mirrors the 1h the §10 hotel check-in calc adds to a flight arrival.
const transferArrivalBuffer = 1 * time.Hour

// applyTransferTimes retimes airport↔accommodation ground transfers whose start
// time-of-day was defaulted (the source stated no time) off the flanking flight
// in the same batch: an airport→hotel transfer starts shortly after the inbound
// flight's arrival, and a hotel→airport transfer starts a lead time before the
// outbound flight's departure (spec §10). A transfer with an explicit time, or
// with no matching flight in the batch, is left at its default. The transfer is
// anchored to the airport's timezone (a true instant) so the downstream geocode
// tz-resolution leaves it alone rather than treating its digits as floating
// local time.
func applyTransferTimes(plans []ProposedPlan) {
	flights := collectFlights(plans)
	if len(flights) == 0 {
		return
	}
	for pi := range plans {
		if plans[pi].Type != "ground" {
			continue
		}
		for i := range plans[pi].Parts {
			retimeTransfer(&plans[pi].Parts[i], flights)
		}
	}
}

// flightInfo is the flanking-flight data the transfer timing needs: the airport
// codes, the scheduled out/in instants, their timezones, and whether the leg is
// long-haul (driving the airport-departure lead).
type flightInfo struct {
	originIATA string
	destIATA   string
	out        time.Time
	in         time.Time
	outTZ      string
	inTZ       string
	outSource  string // what a blank outTZ means: unknown or floating
	inSource   string
	longHaul   bool
}

func collectFlights(plans []ProposedPlan) []flightInfo {
	var out []flightInfo
	for _, pl := range plans {
		for _, part := range pl.Parts {
			if part.Type != "flight" || part.Flight == nil {
				continue
			}
			fd := part.Flight
			out = append(out, flightInfo{
				originIATA: strings.ToUpper(fd.OriginIATA),
				destIATA:   strings.ToUpper(fd.DestIATA),
				out:        fd.ScheduledOut,
				in:         fd.ScheduledIn,
				outTZ:      part.StartTZ,
				inTZ:       part.EndTZ,
				outSource:  part.StartTZSource,
				inSource:   part.EndTZSource,
				longHaul:   !fd.ScheduledOut.IsZero() && fd.ScheduledIn.Sub(fd.ScheduledOut) > longHaulThreshold,
			})
		}
	}
	return out
}

// retimeTransfer times one defaulted-time airport↔accommodation transfer off
// the matching flanking flight in place: an airport→hotel transfer starts
// shortly after the inbound flight's arrival; a hotel→airport transfer starts a
// lead time before the outbound flight's departure.
func retimeTransfer(part *ProposedPart, flights []flightInfo) {
	if !part.startTimeDefaulted || part.StartsAt.IsZero() {
		return
	}
	startCode := airports.CodeIn(part.StartLabel)
	endCode := airports.CodeIn(part.EndLabel)
	startIsAirport := startCode != "" || looksLikeAirport(part.StartLabel)
	endIsAirport := endCode != "" || looksLikeAirport(part.EndLabel)
	// Only the clear airport→place / place→airport shapes; an airport→airport or
	// place→place "transfer" isn't ours to retime.
	if startIsAirport == endIsAirport {
		return
	}
	transferDate := timing.Day(timing.Block{At: part.StartsAt, TZ: part.StartTZ, Source: timing.Source(part.StartTZSource)}, "")

	if startIsAirport {
		// Airport → accommodation: time off the inbound flight's arrival.
		if f, ok := findArrival(flights, startCode, transferDate); ok {
			retimeStart(part, f.in.Add(transferArrivalBuffer), f.inTZ, flightEndSource(f.inTZ, f.inSource))
		}
		return
	}
	// Accommodation → airport: time off the outbound flight's departure.
	if f, ok := findDeparture(flights, endCode, transferDate); ok {
		lead := leadShortHaul
		if f.longHaul {
			lead = leadLongHaul
		}
		retimeStart(part, f.out.Add(-lead), f.outTZ, flightEndSource(f.outTZ, f.outSource))
	}
}

// retimeStart moves part.StartsAt to newStart. If the part also carries a
// stated EndsAt (a booked drop-off: a ground part could not carry one before
// the ground/EndsAt fix in propose.go, so this combination was previously
// unreachable), EndsAt is shifted by the same delta rather than left in
// place, preserving the transfer's stated duration instead of risking an
// EndsAt that lands at or before the retimed StartsAt.
func retimeStart(part *ProposedPart, newStart time.Time, tz, source string) {
	if part.EndsAt != nil {
		delta := newStart.Sub(part.StartsAt)
		newEnd := part.EndsAt.Add(delta)
		part.EndsAt = &newEnd
		// Shifted off a flight's time, a blank end zone is as known as the
		// flight end it follows: still floating digits after a floating end,
		// else a true instant whose zone is not known.
		if part.EndTZ == "" {
			part.EndTZSource = string(timing.Unknown)
			if source == string(timing.Floating) {
				part.EndTZSource = source
			}
		}
	}
	part.StartsAt = newStart
	part.StartTZ, part.StartTZSource = tz, source
}

// findArrival returns the flight arriving on transferDate (in its arrival-local
// calendar). A flight whose destination matches wantCode is preferred; if the
// code matches none (e.g. wantCode was a spurious 3-letter token, or the label
// names the airport in words), it falls back to any same-date arrival. Among
// ties the latest arrival wins.
func findArrival(flights []flightInfo, wantCode string, transferDate time.Time) (flightInfo, bool) {
	var coded, dated flightInfo
	var haveCoded, haveDated bool
	for _, f := range flights {
		if f.in.IsZero() || !localDate(f.in, f.inTZ, f.inSource).Equal(transferDate) {
			continue
		}
		if !haveDated || f.in.After(dated.in) {
			dated, haveDated = f, true
		}
		if wantCode != "" && f.destIATA == wantCode && (!haveCoded || f.in.After(coded.in)) {
			coded, haveCoded = f, true
		}
	}
	if haveCoded {
		return coded, true
	}
	return dated, haveDated
}

// findDeparture returns the flight departing on transferDate (in its
// departure-local calendar), with the same code-preferred / date-fallback logic
// as findArrival; among ties the earliest departure wins.
func findDeparture(flights []flightInfo, wantCode string, transferDate time.Time) (flightInfo, bool) {
	var coded, dated flightInfo
	var haveCoded, haveDated bool
	for _, f := range flights {
		if f.out.IsZero() || !localDate(f.out, f.outTZ, f.outSource).Equal(transferDate) {
			continue
		}
		if !haveDated || f.out.Before(dated.out) {
			dated, haveDated = f, true
		}
		if wantCode != "" && f.originIATA == wantCode && (!haveCoded || f.out.Before(coded.out)) {
			coded, haveCoded = f, true
		}
	}
	if haveCoded {
		return coded, true
	}
	return dated, haveDated
}

// localDate is the calendar date (midnight UTC) of a flight end's instant, by
// the end's own zone and source: its UTC digits when floating, and UTC when
// unknown (an email has no viewer's home zone).
func localDate(t time.Time, tzName, source string) time.Time {
	return timing.Day(timing.Block{At: t, TZ: tzName, Source: timing.Source(source)}, "")
}

// flightEndSource is the source a transfer takes from the flight end it was
// timed off: confirmed in the end's zone, else whatever the end's own source
// says (unknown or floating).
func flightEndSource(tz, source string) string {
	if tz != "" {
		return string(timing.Confirmed)
	}
	if source == string(timing.Floating) {
		return source
	}
	return string(timing.Unknown)
}

// looksLikeAirport reports whether a free-text label names an airport.
func looksLikeAirport(label string) bool {
	return strings.Contains(strings.ToLower(label), "airport")
}
