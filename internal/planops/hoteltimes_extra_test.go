package planops

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestSuggestHotelTimesNilStay: a nil stay yields an empty result (both nil).
func TestSuggestHotelTimesNilStay(t *testing.T) {
	res := SuggestHotelTimes(nil, &store.HotelDetail{}, HotelTimeFlights{})
	if res.Checkin != nil || res.Checkout != nil {
		t.Errorf("nil stay should give empty result, got %+v", res)
	}
}

// TestSuggestHotelTimesInboundNoEndsAt: an inbound flight with no EndsAt uses
// its StartsAt as the effective arrival (partEffectiveEnd fallback). Arrival
// 22:00 + 1h = 23:00 beats the 15:00 standard check-in.
func TestSuggestHotelTimesInboundNoEndsAt(t *testing.T) {
	arrival := time.Date(2026, 6, 1, 22, 0, 0, 0, time.UTC)
	stay := &store.PlanPart{StartsAt: time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)}
	inbound := &store.PlanPart{StartsAt: arrival} // no EndsAt

	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Inbound: inbound})
	want := arrival.Add(1 * time.Hour)
	if res.Checkin == nil || !res.Checkin.Equal(want) {
		t.Errorf("checkin = %v, want %v (arrival from StartsAt + 1h)", res.Checkin, want)
	}
}

// TestSuggestHotelTimesOutboundNoEndsAt: an outbound flight with no EndsAt is
// not long-haul (isLongHaul returns false on nil EndsAt → short-haul 2h lead).
// Departs 09:00 → 07:00, earlier than the 11:00 standard checkout.
func TestSuggestHotelTimesOutboundNoEndsAt(t *testing.T) {
	dep := time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC)
	outbound := &store.PlanPart{StartsAt: dep} // no EndsAt → short-haul
	stay := &store.PlanPart{
		StartsAt: time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC),
		EndsAt:   tp(time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)),
	}
	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Outbound: outbound})
	want := dep.Add(-leadShortHaul)
	if res.Checkout == nil || !res.Checkout.Equal(want) {
		t.Errorf("checkout = %v, want %v (short-haul 2h lead, no EndsAt)", res.Checkout, want)
	}
}

// TestSuggestHotelTimesCheckoutFallsBackToStartDate: an outbound-only stay with
// no EndsAt anchors the standard checkout to the stay's StartsAt date, and the
// stay's StartTZ is used when EndTZ is blank.
func TestSuggestHotelTimesCheckoutUsesStartTZWhenEndTZBlank(t *testing.T) {
	dep := time.Date(2026, 6, 5, 23, 0, 0, 0, time.UTC) // late departure
	outbound := &store.PlanPart{StartsAt: dep, EndsAt: tp(dep.Add(2 * time.Hour))}
	// Stay in Tokyo, no EndsAt and blank EndTZ → checkout anchors on StartsAt day
	// in StartTZ (Asia/Tokyo). Standard 11:00 JST = 02:00 UTC; the late departure
	// keeps the standard checkout.
	stay := &store.PlanPart{
		StartsAt: time.Date(2026, 6, 5, 1, 0, 0, 0, time.UTC), // 10:00 JST on the 5th
		StartTZ:  "Asia/Tokyo",
	}
	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Outbound: outbound})
	want := time.Date(2026, 6, 5, 2, 0, 0, 0, time.UTC) // 11:00 JST
	if res.Checkout == nil || !res.Checkout.Equal(want) {
		t.Errorf("checkout = %v, want %v (11:00 Tokyo on stay start day)", res.Checkout, want)
	}
}

// TestSuggestHotelTimesPropertyPublishedTimes: when the HotelDetail publishes
// its own standard check-in/out HH:MM, those drive the standard time instead of
// the 15:00/11:00 defaults (exercises detailCheckin/detailCheckout + parseHHMM).
func TestSuggestHotelTimesPropertyPublishedTimes(t *testing.T) {
	ci := "16:30"
	co := "10:00"
	detail := &store.HotelDetail{StandardCheckin: &ci, StandardCheckout: &co}

	// Early arrival → published 16:30 check-in wins.
	arrival := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	inbound := &store.PlanPart{StartsAt: arrival.Add(-time.Hour), EndsAt: tp(arrival)}
	// Late departure → published 10:00 checkout wins.
	dep := time.Date(2026, 6, 5, 22, 0, 0, 0, time.UTC)
	outbound := &store.PlanPart{StartsAt: dep, EndsAt: tp(dep.Add(2 * time.Hour))}
	stay := &store.PlanPart{
		StartsAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		EndsAt:   tp(time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)),
	}

	res := SuggestHotelTimes(stay, detail, HotelTimeFlights{Inbound: inbound, Outbound: outbound})
	wantCI := time.Date(2026, 6, 1, 16, 30, 0, 0, time.UTC)
	if res.Checkin == nil || !res.Checkin.Equal(wantCI) {
		t.Errorf("checkin = %v, want published 16:30 (%v)", res.Checkin, wantCI)
	}
	wantCO := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	if res.Checkout == nil || !res.Checkout.Equal(wantCO) {
		t.Errorf("checkout = %v, want published 10:00 (%v)", res.Checkout, wantCO)
	}
}

// TestStandardTimeOnDateMalformedPublishedTime: a malformed published HH:MM is
// ignored and the default hour is used (parseHHMM ok=false branch).
func TestStandardTimeOnDateMalformedPublishedTime(t *testing.T) {
	bad := "not-a-time"
	detail := &store.HotelDetail{StandardCheckin: &bad}
	arrival := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	inbound := &store.PlanPart{StartsAt: arrival.Add(-time.Hour), EndsAt: tp(arrival)}
	stay := &store.PlanPart{StartsAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}

	res := SuggestHotelTimes(stay, detail, HotelTimeFlights{Inbound: inbound})
	want := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC) // default 15:00
	if res.Checkin == nil || !res.Checkin.Equal(want) {
		t.Errorf("checkin = %v, want default 15:00 when published time malformed", res.Checkin)
	}
}

// TestAirportTravelZeroDistance: identical airport and hotel coordinates give a
// zero great-circle distance, so airportTravel contributes nothing (nm<=0).
func TestAirportTravelZeroDistance(t *testing.T) {
	arrival := time.Date(2026, 6, 1, 22, 0, 0, 0, time.UTC)
	lat, lon := 51.4700, -0.4543 // both endpoints at the same point
	inbound := &store.PlanPart{
		StartsAt: arrival.Add(-time.Hour), EndsAt: tp(arrival),
		EndLat: &lat, EndLon: &lon,
	}
	stay := &store.PlanPart{
		StartsAt: time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC),
		StartLat: &lat, StartLon: &lon,
	}
	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Inbound: inbound})
	want := arrival.Add(1 * time.Hour) // no travel term added
	if res.Checkin == nil || !res.Checkin.Equal(want) {
		t.Errorf("checkin = %v, want %v (zero travel for coincident points)", res.Checkin, want)
	}
}
