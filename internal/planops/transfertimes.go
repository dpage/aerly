package planops

import (
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
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
				longHaul:   !fd.ScheduledOut.IsZero() && fd.ScheduledIn.Sub(fd.ScheduledOut) > longHaulThreshold,
			})
		}
	}
	return out
}

// retimeTransfer adjusts one ground part in place when it's a defaulted-time
// airport↔accommodation transfer with a matching flanking flight.
func retimeTransfer(part *ProposedPart, flights []flightInfo) {
	if !part.startTimeDefaulted || part.StartsAt.IsZero() {
		return
	}
	startCode := airportCode(part.StartLabel)
	endCode := airportCode(part.EndLabel)
	startIsAirport := startCode != "" || looksLikeAirport(part.StartLabel)
	endIsAirport := endCode != "" || looksLikeAirport(part.EndLabel)
	// Only the clear airport→place / place→airport shapes; an airport→airport or
	// place→place "transfer" isn't ours to retime.
	if startIsAirport == endIsAirport {
		return
	}
	transferDate := part.StartsAt.UTC().Format("2006-01-02")

	if startIsAirport {
		// Airport → accommodation: time off the inbound flight's arrival.
		if f, ok := findArrival(flights, startCode, transferDate); ok {
			part.StartsAt = f.in.Add(transferArrivalBuffer)
			part.StartTZ = f.inTZ
		}
		return
	}
	// Accommodation → airport: time off the outbound flight's departure.
	if f, ok := findDeparture(flights, endCode, transferDate); ok {
		lead := leadShortHaul
		if f.longHaul {
			lead = leadLongHaul
		}
		part.StartsAt = f.out.Add(-lead)
		part.StartTZ = f.outTZ
	}
}

// findArrival returns the flight arriving on transferDate (in its arrival-local
// calendar). A flight whose destination matches wantCode is preferred; if the
// code matches none (e.g. wantCode was a spurious 3-letter token, or the label
// names the airport in words), it falls back to any same-date arrival. Among
// ties the latest arrival wins.
func findArrival(flights []flightInfo, wantCode, transferDate string) (flightInfo, bool) {
	var coded, dated flightInfo
	var haveCoded, haveDated bool
	for _, f := range flights {
		if f.in.IsZero() || localDate(f.in, f.inTZ) != transferDate {
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
func findDeparture(flights []flightInfo, wantCode, transferDate string) (flightInfo, bool) {
	var coded, dated flightInfo
	var haveCoded, haveDated bool
	for _, f := range flights {
		if f.out.IsZero() || localDate(f.out, f.outTZ) != transferDate {
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

// localDate renders the instant's calendar date in tzName, falling back to UTC
// when the zone is empty or won't load.
func localDate(t time.Time, tzName string) string {
	if tzName != "" {
		if loc, err := time.LoadLocation(tzName); err == nil {
			return t.In(loc).Format("2006-01-02")
		}
	}
	return t.UTC().Format("2006-01-02")
}

// looksLikeAirport reports whether a free-text label names an airport.
func looksLikeAirport(label string) bool {
	return strings.Contains(strings.ToLower(label), "airport")
}

// airportCode extracts an IATA code from a label when one is present and known
// (e.g. "SID", "Sal Airport (SID)"), else "". Used to match a transfer endpoint
// to a specific flight; matching falls back to date-only when no code is found.
func airportCode(label string) string {
	for _, tok := range strings.FieldsFunc(label, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z')
	}) {
		if len(tok) != 3 {
			continue
		}
		code := strings.ToUpper(tok)
		if _, ok := airports.LookupTZ(code); ok {
			return code
		}
	}
	return ""
}
