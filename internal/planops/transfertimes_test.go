package planops

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// flightPlan builds a one-part flight ProposedPlan with the given schedule + tz.
func flightPlan(origin, dest string, out, in time.Time, outTZ, inTZ string) ProposedPlan {
	return ProposedPlan{
		Type: "flight",
		Parts: []ProposedPart{{
			Type:    "flight",
			StartTZ: outTZ,
			EndTZ:   inTZ,
			Flight: &store.FlightDetail{
				OriginIATA:   origin,
				DestIATA:     dest,
				ScheduledOut: out,
				ScheduledIn:  in,
			},
		}},
	}
}

// transferPlan builds a one-part ground ProposedPlan as proposePart would for a
// transfer with no stated time: start_at defaulted to 09:00 UTC on `date`,
// startTimeDefaulted=true, no tz.
func transferPlan(startLabel, endLabel, date string) ProposedPlan {
	d, _ := time.Parse("2006-01-02T15:04", date+"T09:00")
	return ProposedPlan{
		Type: "ground",
		Parts: []ProposedPart{{
			Type:               "ground",
			StartLabel:         startLabel,
			EndLabel:           endLabel,
			StartsAt:           d.UTC(),
			startTimeDefaulted: true,
		}},
	}
}

func TestApplyTransferTimes_AirportToHotel(t *testing.T) {
	// Inbound short-haul arriving SID 16:20 local (Cape Verde, UTC-1) on 01-15.
	arr := time.Date(2026, 1, 15, 17, 20, 0, 0, time.UTC) // 16:20 local
	dep := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	plans := []ProposedPlan{
		flightPlan("BRS", "SID", dep, arr, "Europe/London", "Atlantic/Cape_Verde"),
		transferPlan("Sal Airport", "Melia Tortuga Beach Resort", "2026-01-15"),
	}
	applyTransferTimes(plans)

	got := plans[1].Parts[0]
	want := arr.Add(transferArrivalBuffer) // arrival + 1h
	if !got.StartsAt.Equal(want) {
		t.Errorf("transfer start = %s, want %s (arrival + buffer)", got.StartsAt, want)
	}
	if got.StartTZ != "Atlantic/Cape_Verde" {
		t.Errorf("transfer tz = %q, want the arrival airport's zone", got.StartTZ)
	}
}

func TestApplyTransferTimes_HotelToAirport(t *testing.T) {
	// Outbound short-haul departing SID 17:00 local on 01-25.
	dep := time.Date(2026, 1, 25, 18, 0, 0, 0, time.UTC) // 17:00 local
	arr := time.Date(2026, 1, 25, 23, 0, 0, 0, time.UTC)
	plans := []ProposedPlan{
		flightPlan("SID", "BRS", dep, arr, "Atlantic/Cape_Verde", "Europe/London"),
		transferPlan("Melia Tortuga Beach Resort", "Sal Airport", "2026-01-25"),
	}
	applyTransferTimes(plans)

	got := plans[1].Parts[0]
	want := dep.Add(-leadShortHaul) // departure − short-haul lead
	if !got.StartsAt.Equal(want) {
		t.Errorf("transfer start = %s, want %s (departure − lead)", got.StartsAt, want)
	}
	if got.StartTZ != "Atlantic/Cape_Verde" {
		t.Errorf("transfer tz = %q, want the departure airport's zone", got.StartTZ)
	}
}

func TestApplyTransferTimes_LongHaulUsesLongerLead(t *testing.T) {
	// 9h block time → long-haul → 3h lead.
	dep := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)
	arr := time.Date(2026, 3, 10, 19, 0, 0, 0, time.UTC)
	plans := []ProposedPlan{
		flightPlan("JFK", "LHR", dep, arr, "America/New_York", "Europe/London"),
		transferPlan("Manhattan Hotel", "JFK Airport", "2026-03-10"),
	}
	applyTransferTimes(plans)
	want := dep.Add(-leadLongHaul)
	if !plans[1].Parts[0].StartsAt.Equal(want) {
		t.Errorf("long-haul transfer = %s, want %s (departure − long lead)", plans[1].Parts[0].StartsAt, want)
	}
}

func TestApplyTransferTimes_ExplicitTimePreserved(t *testing.T) {
	arr := time.Date(2026, 1, 15, 17, 20, 0, 0, time.UTC)
	plans := []ProposedPlan{
		flightPlan("BRS", "SID", arr.Add(-3*time.Hour), arr, "Europe/London", "Atlantic/Cape_Verde"),
		transferPlan("Sal Airport", "Melia Tortuga", "2026-01-15"),
	}
	// Caller stated an explicit time → not defaulted; must be left untouched.
	plans[1].Parts[0].startTimeDefaulted = false
	explicit := plans[1].Parts[0].StartsAt
	applyTransferTimes(plans)
	if !plans[1].Parts[0].StartsAt.Equal(explicit) {
		t.Errorf("explicit transfer time was overwritten: %s", plans[1].Parts[0].StartsAt)
	}
}

func TestApplyTransferTimes_NoFlightLeavesDefault(t *testing.T) {
	plans := []ProposedPlan{transferPlan("Sal Airport", "Melia Tortuga", "2026-01-15")}
	def := plans[0].Parts[0].StartsAt
	applyTransferTimes(plans)
	if !plans[0].Parts[0].StartsAt.Equal(def) {
		t.Errorf("transfer with no flanking flight should keep its default, got %s", plans[0].Parts[0].StartsAt)
	}
}

func TestApplyTransferTimes_PlaceToPlaceIgnored(t *testing.T) {
	arr := time.Date(2026, 1, 15, 17, 20, 0, 0, time.UTC)
	plans := []ProposedPlan{
		flightPlan("BRS", "SID", arr.Add(-3*time.Hour), arr, "Europe/London", "Atlantic/Cape_Verde"),
		transferPlan("Hotel A", "Hotel B", "2026-01-15"), // neither end is an airport
	}
	def := plans[1].Parts[0].StartsAt
	applyTransferTimes(plans)
	if !plans[1].Parts[0].StartsAt.Equal(def) {
		t.Errorf("place→place transfer should be left alone, got %s", plans[1].Parts[0].StartsAt)
	}
}
