package planops

import (
	"strings"

	"github.com/dpage/aerly/internal/airports"
)

// Trip naming for auto-created trips (issue #21). When an import has to create a
// trip — email ingest finds no existing trip to attach to, or an .ics has no
// calendar name — the trip should be named for where it goes, not after the
// first flight's ident. These helpers derive "Trip to <city>" from the first
// flight leg's destination; callers fall back to other names when they return
// "" (no flight to name the trip after).

// TripNameForProposedPlan derives a destination-based name for a trip created
// from a single proposed plan (the email-ingest path). Returns "" when the plan
// contains no flight.
func TripNameForProposedPlan(p ProposedPlan) string {
	for _, part := range p.Parts {
		if part.Type == "flight" && part.Flight != nil {
			if name := destinationTripName(part.Flight.DestIATA); name != "" {
				return name
			}
		}
	}
	return ""
}

// TripNameForConfirmPlans derives a destination-based name from the first flight
// leg across a set of confirmed plans (the .ics import path, which builds a trip
// from many plans at once). Returns "" when none contain a flight.
func TripNameForConfirmPlans(plans []ConfirmPlanInput) string {
	for _, p := range plans {
		for _, part := range p.Parts {
			if part.Type == "flight" && part.Flight != nil {
				if name := destinationTripName(part.Flight.DestIATA); name != "" {
					return name
				}
			}
		}
	}
	return ""
}

// destinationTripName turns a destination IATA code into "Trip to <city>",
// falling back to the upper-cased code when the airport isn't in the table
// (e.g. "Trip to JFK"). Returns "" for a blank code.
func destinationTripName(destIATA string) string {
	code := strings.ToUpper(strings.TrimSpace(destIATA))
	if code == "" {
		return ""
	}
	if city, ok := airports.LookupCity(code); ok {
		return "Trip to " + city
	}
	return "Trip to " + code
}
