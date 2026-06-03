package planops

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func tp(t time.Time) *time.Time { return &t }

func TestSuggestHotelTimesNoFlights(t *testing.T) {
	ci := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	co := time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)
	stay := &store.PlanPart{StartsAt: ci, EndsAt: tp(co)}

	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{})
	if res.Checkin != nil || res.Checkout != nil {
		t.Errorf("with no flanking flights both suggestions should be nil, got %+v", res)
	}
}

func TestSuggestHotelTimesLateArrivalPushesCheckin(t *testing.T) {
	// Inbound flight arrives 22:00; standard check-in is 15:00. The arrival
	// + 1h (23:00) is later, so the suggested check-in is 23:00.
	arrival := time.Date(2026, 6, 1, 22, 0, 0, 0, time.UTC)
	stayStart := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	stay := &store.PlanPart{StartsAt: stayStart}
	inbound := &store.PlanPart{
		StartsAt: arrival.Add(-2 * time.Hour), EndsAt: tp(arrival),
	}

	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Inbound: inbound})
	if res.Checkin == nil {
		t.Fatal("expected a check-in suggestion")
	}
	want := arrival.Add(1 * time.Hour) // 23:00
	if !res.Checkin.Equal(want) {
		t.Errorf("checkin = %v, want %v (arrival+1h)", res.Checkin, want)
	}
}

func TestSuggestHotelTimesStandardUsesPropertyTimezone(t *testing.T) {
	// The property is in Tokyo (+09:00, no DST). An early arrival means the
	// standard 15:00 check-in wins — and it must be 15:00 *Tokyo* time
	// (06:00 UTC), not 15:00 UTC, even though the stored instant is UTC.
	stayStart := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	stay := &store.PlanPart{StartsAt: stayStart, StartTZ: "Asia/Tokyo"}
	arrival := time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC) // arrival+1h = 03:00Z < 06:00Z standard
	inbound := &store.PlanPart{StartsAt: arrival.Add(-time.Hour), EndsAt: tp(arrival)}

	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Inbound: inbound})
	want := time.Date(2026, 6, 1, 6, 0, 0, 0, time.UTC) // 15:00 JST
	if res.Checkin == nil || !res.Checkin.Equal(want) {
		t.Errorf("checkin = %v, want 15:00 Tokyo (%v)", res.Checkin, want)
	}
}

func TestSuggestHotelTimesEarlyArrivalKeepsStandard(t *testing.T) {
	// Inbound arrives 09:00; arrival+1h = 10:00 is before standard 15:00, so
	// the suggested check-in stays at 15:00.
	arrival := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	stayStart := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	stay := &store.PlanPart{StartsAt: stayStart}
	inbound := &store.PlanPart{StartsAt: arrival.Add(-time.Hour), EndsAt: tp(arrival)}

	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Inbound: inbound})
	want := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	if res.Checkin == nil || !res.Checkin.Equal(want) {
		t.Errorf("checkin = %v, want standard 15:00", res.Checkin)
	}
}

func TestSuggestHotelTimesShortHaulCheckout(t *testing.T) {
	// Outbound departs 09:00 (short-haul, 2h block). Lead = 2h → 07:00. That's
	// earlier than standard checkout 11:00, so the suggestion is 07:00.
	dep := time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC)
	outbound := &store.PlanPart{StartsAt: dep, EndsAt: tp(dep.Add(2 * time.Hour))}
	stay := &store.PlanPart{
		StartsAt: time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC),
		EndsAt:   tp(time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)),
	}

	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Outbound: outbound})
	if res.Checkout == nil {
		t.Fatal("expected a check-out suggestion")
	}
	want := dep.Add(-2 * time.Hour) // 07:00
	if !res.Checkout.Equal(want) {
		t.Errorf("checkout = %v, want %v (dep - 2h short-haul lead)", res.Checkout, want)
	}
}

func TestSuggestHotelTimesLongHaulLead(t *testing.T) {
	// Outbound is an 8h block (long-haul), departing 14:00 → lead 3h → 11:00,
	// which equals standard checkout 11:00; earlier-of picks 11:00.
	dep := time.Date(2026, 6, 5, 14, 0, 0, 0, time.UTC)
	outbound := &store.PlanPart{StartsAt: dep, EndsAt: tp(dep.Add(8 * time.Hour))}
	stay := &store.PlanPart{
		StartsAt: time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC),
		EndsAt:   tp(time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)),
	}
	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Outbound: outbound})
	want := dep.Add(-3 * time.Hour) // 11:00
	if res.Checkout == nil || !res.Checkout.Equal(want) {
		t.Errorf("checkout = %v, want %v (long-haul 3h lead)", res.Checkout, want)
	}
}

func TestSuggestHotelTimesAirportTravelAddsTime(t *testing.T) {
	// Inbound arrives 20:00 at JFK (40.64,-73.78); hotel at a point ~25nm away.
	// The travel term should push check-in beyond arrival+1h.
	arrival := time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)
	jfkLat, jfkLon := 40.6413, -73.7781
	hotelLat, hotelLon := 40.7580, -73.9855 // Times Square-ish, ~12nm
	inbound := &store.PlanPart{
		StartsAt: arrival.Add(-5 * time.Hour), EndsAt: tp(arrival),
		EndLat: &jfkLat, EndLon: &jfkLon,
	}
	stay := &store.PlanPart{
		StartsAt: time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC),
		StartLat: &hotelLat, StartLon: &hotelLon,
	}
	res := SuggestHotelTimes(stay, &store.HotelDetail{}, HotelTimeFlights{Inbound: inbound})
	if res.Checkin == nil {
		t.Fatal("expected a check-in suggestion")
	}
	floor := arrival.Add(1 * time.Hour)
	if !res.Checkin.After(floor) {
		t.Errorf("checkin = %v, expected to exceed arrival+1h (%v) due to airport travel", res.Checkin, floor)
	}
}
