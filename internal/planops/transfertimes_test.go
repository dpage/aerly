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
	// Inbound short-haul arriving ALC 16:20 on 01-15 (real airport so the coord
	// lookup resolves).
	arr := time.Date(2026, 1, 15, 17, 20, 0, 0, time.UTC) // 16:20 local
	dep := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	plans := []ProposedPlan{
		flightPlan("LGW", "ALC", dep, arr, "Europe/London", "Europe/Madrid"),
		transferPlan("Alicante Airport", "Melia Benidorm", "2026-01-15"),
	}
	applyTransferTimes(plans)

	got := plans[1].Parts[0]
	want := arr.Add(transferArrivalBuffer) // arrival + 1h
	if !got.StartsAt.Equal(want) {
		t.Errorf("transfer start = %s, want %s (arrival + buffer)", got.StartsAt, want)
	}
	if got.StartTZ != "Europe/Madrid" {
		t.Errorf("transfer tz = %q, want the arrival airport's zone", got.StartTZ)
	}
}

func TestApplyTransferTimes_HotelToAirport(t *testing.T) {
	// Outbound short-haul departing ALC 17:00 on 01-25.
	dep := time.Date(2026, 1, 25, 18, 0, 0, 0, time.UTC) // 17:00 local
	arr := time.Date(2026, 1, 25, 23, 0, 0, 0, time.UTC)
	plans := []ProposedPlan{
		flightPlan("ALC", "LGW", dep, arr, "Europe/Madrid", "Europe/London"),
		transferPlan("Melia Benidorm", "Alicante Airport", "2026-01-25"),
	}
	applyTransferTimes(plans)

	got := plans[1].Parts[0]
	want := dep.Add(-leadShortHaul) // departure − short-haul lead
	if !got.StartsAt.Equal(want) {
		t.Errorf("transfer start = %s, want %s (departure − lead)", got.StartsAt, want)
	}
	if got.StartTZ != "Europe/Madrid" {
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

// TestApplyTransferTimes_ShiftsEndsAtWithStart is the regression guard for a
// bug the ground/EndsAt fix in propose.go made reachable: retimeTransfer
// rewrites StartsAt off the flanking flight but, before this fix, left a
// stated EndsAt untouched. A ground part could not previously carry an
// EndsAt at all, so a defaulted StartsAt (retimed) alongside a stated EndsAt
// was impossible; it now is (an airport→hotel transfer stating a drop-off
// time but no pickup time). Retiming must shift EndsAt by the same delta as
// StartsAt so the transfer's stated duration survives and it never ends
// before it starts.
func TestApplyTransferTimes_ShiftsEndsAtWithStart(t *testing.T) {
	arr := time.Date(2026, 1, 15, 17, 20, 0, 0, time.UTC)
	dep := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	// As proposePart would build it for StartDate 2026-01-15 with no
	// StartTime (defaulted 09:00, startTimeDefaulted=true) and EndTime
	// "11:00" with no EndDate (falls back to the start date, per the ground
	// case's train-shaped fallback).
	defaultStart, _ := time.Parse("2006-01-02T15:04", "2026-01-15T09:00")
	defaultStart = defaultStart.UTC()
	statedEnd, _ := time.Parse("2006-01-02T15:04", "2026-01-15T11:00")
	statedEnd = statedEnd.UTC()
	origDuration := statedEnd.Sub(defaultStart)

	plans := []ProposedPlan{
		flightPlan("LGW", "ALC", dep, arr, "Europe/London", "Europe/Madrid"),
		{
			Type: "ground",
			Parts: []ProposedPart{{
				Type:               "ground",
				StartLabel:         "Alicante Airport",
				EndLabel:           "Melia Benidorm",
				StartsAt:           defaultStart,
				EndsAt:             &statedEnd,
				startTimeDefaulted: true,
			}},
		},
	}
	applyTransferTimes(plans)

	got := plans[1].Parts[0]
	if got.EndsAt == nil {
		t.Fatal("EndsAt was dropped by retiming")
	}
	if !got.EndsAt.After(got.StartsAt) {
		t.Fatalf("retimed transfer ends (%s) at or before it starts (%s), a persisted part that ends before it starts",
			got.EndsAt, got.StartsAt)
	}
	if gotDuration := got.EndsAt.Sub(got.StartsAt); gotDuration != origDuration {
		t.Errorf("duration = %s, want the originally stated %s preserved through the retime", gotDuration, origDuration)
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

// TestApplyTransferTimes_RetimedSources: a retimed start is a true instant, so
// it is confirmed in the flight's zone, or unknown when the flight has none; an
// end moved by the same delta is no longer a wall-clock reading either.
func TestApplyTransferTimes_RetimedSources(t *testing.T) {
	arr := time.Date(2026, 1, 15, 17, 20, 0, 0, time.UTC)
	dep := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC)
	withEnd := func() ProposedPlan {
		p := transferPlan("Alicante Airport", "Test Hotel", "2026-01-15")
		end := p.Parts[0].StartsAt.Add(time.Hour)
		p.Parts[0].EndsAt = &end
		p.Parts[0].StartTZSource, p.Parts[0].EndTZSource = "floating", "floating"
		return p
	}

	plans := []ProposedPlan{flightPlan("LGW", "ALC", dep, arr, "Europe/London", "Europe/Madrid"), withEnd()}
	applyTransferTimes(plans)
	got := plans[1].Parts[0]
	if got.StartTZ != "Europe/Madrid" || got.StartTZSource != "confirmed" {
		t.Errorf("start = %q %q, want Europe/Madrid confirmed", got.StartTZ, got.StartTZSource)
	}
	if got.EndTZ != "" || got.EndTZSource != "unknown" {
		t.Errorf("end = %q %q, want '' unknown", got.EndTZ, got.EndTZSource)
	}

	plans = []ProposedPlan{flightPlan("LGW", "ALC", dep, arr, "", ""), withEnd()}
	applyTransferTimes(plans)
	if got := plans[1].Parts[0]; got.StartTZ != "" || got.StartTZSource != "unknown" {
		t.Errorf("zoneless flight: start = %q %q, want '' unknown", got.StartTZ, got.StartTZSource)
	}

	// Hotel to airport retimes off the departure, and an end with a zone of its
	// own keeps its source.
	out := withEnd()
	out.Parts[0].StartLabel, out.Parts[0].EndLabel = "Test Hotel", "Gatwick Airport (LGW)"
	out.Parts[0].EndTZ, out.Parts[0].EndTZSource = "Europe/London", "confirmed"
	plans = []ProposedPlan{flightPlan("LGW", "ALC", time.Date(2026, 1, 15, 18, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 15, 21, 0, 0, 0, time.UTC), "Europe/London", "Europe/Madrid"), out}
	applyTransferTimes(plans)
	if got := plans[1].Parts[0]; got.StartTZSource != "confirmed" || got.EndTZSource != "confirmed" {
		t.Errorf("hotel to airport sources = %q/%q, want confirmed/confirmed", got.StartTZSource, got.EndTZSource)
	}
}
