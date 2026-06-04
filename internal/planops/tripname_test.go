package planops

import (
	"testing"

	"github.com/dpage/aerly/internal/store"
)

func flightPart(origin, dest string) ProposedPart {
	return ProposedPart{Type: "flight", Flight: &store.FlightDetail{OriginIATA: origin, DestIATA: dest}}
}

func confirmFlightPart(origin, dest string) ConfirmPartInput {
	return ConfirmPartInput{Type: "flight", Flight: &store.FlightDetail{OriginIATA: origin, DestIATA: dest}}
}

func TestTripNameForProposedPlanOneWay(t *testing.T) {
	p := ProposedPlan{Type: "flight", Parts: []ProposedPart{flightPart("LHR", "CDG")}}
	if got := TripNameForProposedPlan(p); got != "Trip to Paris" {
		t.Errorf("one-way LHR→CDG: got %q, want %q", got, "Trip to Paris")
	}
}

func TestTripNameForProposedPlanRoundTripUsesFirstLegDestination(t *testing.T) {
	// The away city is where the first leg lands, not home on the return leg.
	p := ProposedPlan{Type: "flight", Parts: []ProposedPart{
		flightPart("LHR", "JFK"),
		flightPart("JFK", "LHR"),
	}}
	if got := TripNameForProposedPlan(p); got != "Trip to New York" {
		t.Errorf("round-trip LHR↔JFK: got %q, want %q", got, "Trip to New York")
	}
}

func TestTripNameForProposedPlanUnknownAirportFallsBackToCode(t *testing.T) {
	p := ProposedPlan{Type: "flight", Parts: []ProposedPart{flightPart("LHR", "xyz")}}
	if got := TripNameForProposedPlan(p); got != "Trip to XYZ" {
		t.Errorf("unknown dest: got %q, want %q", got, "Trip to XYZ")
	}
}

func TestTripNameForProposedPlanNonFlightIsEmpty(t *testing.T) {
	p := ProposedPlan{Type: "hotel", Parts: []ProposedPart{{Type: "hotel", Hotel: &store.HotelDetail{}}}}
	if got := TripNameForProposedPlan(p); got != "" {
		t.Errorf("non-flight plan should yield no name, got %q", got)
	}
}

func TestTripNameForProposedPlanFlightWithoutDestIsEmpty(t *testing.T) {
	p := ProposedPlan{Type: "flight", Parts: []ProposedPart{flightPart("LHR", "")}}
	if got := TripNameForProposedPlan(p); got != "" {
		t.Errorf("flight with no destination should yield no name, got %q", got)
	}
}

func TestTripNameForConfirmPlansFindsFirstFlight(t *testing.T) {
	// A hotel plan precedes the flight; the trip name still comes from the flight.
	plans := []ConfirmPlanInput{
		{Type: "hotel", Parts: []ConfirmPartInput{{Type: "hotel", Hotel: &store.HotelDetail{}}}},
		{Type: "flight", Parts: []ConfirmPartInput{confirmFlightPart("LHR", "FCO")}},
	}
	if got := TripNameForConfirmPlans(plans); got != "Trip to Rome" {
		t.Errorf("got %q, want %q", got, "Trip to Rome")
	}
}

func TestTripNameForConfirmPlansNoFlightIsEmpty(t *testing.T) {
	plans := []ConfirmPlanInput{
		{Type: "hotel", Parts: []ConfirmPartInput{{Type: "hotel", Hotel: &store.HotelDetail{}}}},
	}
	if got := TripNameForConfirmPlans(plans); got != "" {
		t.Errorf("plans without a flight should yield no name, got %q", got)
	}
}
