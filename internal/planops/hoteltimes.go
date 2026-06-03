package planops

import (
	"time"

	"github.com/dpage/aerly/internal/geo"
	"github.com/dpage/aerly/internal/store"
)

// Default check-in / check-out times of day (local) used when a property does
// not publish its own (spec §10).
const (
	defaultCheckinHour  = 15 // 15:00
	defaultCheckoutHour = 11 // 11:00
)

// Long-haul heuristic: flights with a scheduled block time of more than this
// get the longer airport-departure lead (spec §10).
const longHaulThreshold = 6 * time.Hour

// Airport-departure lead times before an outbound flight's scheduled departure.
const (
	leadShortHaul = 2 * time.Hour
	leadLongHaul  = 3 * time.Hour
)

// groundSpeedKt is the conservative average speed used to estimate
// airport-to-hotel surface travel time from great-circle distance when both
// endpoints have coordinates. Real routing isn't available here, so this is a
// best-effort estimate; when coordinates are missing the travel term is omitted
// entirely (spec §10).
const groundSpeedKt = 25.0 // knots, ~46 km/h: city traffic + transfer overhead

// HotelTimeFlights bundles the two flights that flank a stay. Either may be nil
// when no such flight exists in the trip.
type HotelTimeFlights struct {
	// Inbound is the flight whose effective arrival is the latest one before
	// the stay begins. Its arrival drives the suggested check-in.
	Inbound *store.PlanPart
	// Outbound is the flight whose effective departure is the earliest one
	// after the stay begins. Its departure drives the suggested check-out.
	Outbound *store.PlanPart
}

// HotelSuggestedTimes is the result of the §10 calculation. A nil pointer means
// "no smarter time than the stored one" — the caller leaves the DTO field null.
type HotelSuggestedTimes struct {
	Checkin  *time.Time
	Checkout *time.Time
}

// SuggestHotelTimes computes the smart check-in / check-out instants for a
// hotel stay (spec §10), pure and at render time. `stay` is the hotel part;
// `detail` its satellite (for the property's standard times); `flanking` the
// surrounding inbound / outbound flights within the same trip. With no flanking
// flight the corresponding suggestion is nil and the caller falls back to the
// stored times.
//
// The result is intentionally *suggested*: it never mutates the stored
// starts_at / ends_at.
func SuggestHotelTimes(stay *store.PlanPart, detail *store.HotelDetail, flanking HotelTimeFlights) HotelSuggestedTimes {
	var out HotelSuggestedTimes
	if stay == nil {
		return out
	}

	// Check-in: later of the property's standard check-in (default 15:00 local)
	// and the inbound flight's arrival + 1h + airport travel.
	if flanking.Inbound != nil {
		arrival := partEffectiveEnd(flanking.Inbound)
		earliest := arrival.Add(1 * time.Hour).Add(airportTravel(flanking.Inbound, stay, true))
		standard := standardTimeOnDate(stay.StartsAt, stay.StartTZ, defaultCheckinHour, detailCheckin(detail))
		ci := laterOf(standard, earliest)
		out.Checkin = &ci
	}

	// Check-out: earlier of the property's standard check-out (default 11:00
	// local) and the outbound flight's departure − lead − airport travel.
	if flanking.Outbound != nil {
		departure := partEffectiveStart(flanking.Outbound)
		lead := leadShortHaul
		if isLongHaul(flanking.Outbound) {
			lead = leadLongHaul
		}
		latest := departure.Add(-lead).Add(-airportTravel(flanking.Outbound, stay, false))
		checkoutDate := stay.StartsAt
		if stay.EndsAt != nil {
			checkoutDate = *stay.EndsAt
		}
		// A hotel's check-out is at the same property as check-in, so EndTZ is
		// usually empty — fall back to StartTZ.
		checkoutTZ := stay.EndTZ
		if checkoutTZ == "" {
			checkoutTZ = stay.StartTZ
		}
		standard := standardTimeOnDate(checkoutDate, checkoutTZ, defaultCheckoutHour, detailCheckout(detail))
		co := earlierOf(standard, latest)
		out.Checkout = &co
	}
	return out
}

// partEffectiveStart / partEffectiveEnd collapse a flight part's times the way
// the tracker does (actual → estimated → scheduled). For a generic part with no
// flight satellite loaded onto it, they fall back to starts_at / ends_at.
func partEffectiveStart(p *store.PlanPart) time.Time { return p.StartsAt }

func partEffectiveEnd(p *store.PlanPart) time.Time {
	if p.EndsAt != nil {
		return *p.EndsAt
	}
	return p.StartsAt
}

// isLongHaul applies the §10 heuristic on the part's block time. Intercontinental
// detection would need IATA region data; the block-time test is the documented
// fallback and is what we apply here.
func isLongHaul(flight *store.PlanPart) bool {
	if flight.EndsAt == nil {
		return false
	}
	return flight.EndsAt.Sub(flight.StartsAt) > longHaulThreshold
}

// airportTravel estimates surface travel between the flight's airport endpoint
// and the hotel, best-effort via great-circle distance. toHotel=true uses the
// flight's arrival airport (its end coords) → hotel; toHotel=false uses the
// hotel → the flight's departure airport (its start coords). Returns 0 when any
// coordinate is missing (spec §10: omitted when unknown).
func airportTravel(flight, hotel *store.PlanPart, toHotel bool) time.Duration {
	var aLat, aLon *float64
	if toHotel {
		aLat, aLon = flight.EndLat, flight.EndLon
	} else {
		aLat, aLon = flight.StartLat, flight.StartLon
	}
	if aLat == nil || aLon == nil || hotel.StartLat == nil || hotel.StartLon == nil {
		return 0
	}
	nm := geo.HaversineNM(*aLat, *aLon, *hotel.StartLat, *hotel.StartLon)
	if nm <= 0 {
		return 0
	}
	hours := nm / groundSpeedKt
	return time.Duration(hours * float64(time.Hour))
}

// standardTimeOnDate returns the standard check-in/out instant: the property's
// published "HH:MM" (or the default hour) on `date`'s calendar day, in the
// property's timezone `tz`. Plan-part instants are stored/loaded as UTC, so
// without `tz` the standard time would land at e.g. 15:00 UTC rather than 15:00
// local — wrong by the property's offset. When `tz` is empty or unloadable we
// fall back to the instant's own location (UTC), preserving prior behaviour.
// The calendar day is taken in `tz` too, so a stay near local midnight anchors
// to the intended local date.
func standardTimeOnDate(date time.Time, tz string, defaultHour int, hhmm *string) time.Time {
	loc := date.Location()
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	y, m, d := date.In(loc).Date()
	hour, min := defaultHour, 0
	if hhmm != nil {
		if h, mi, ok := parseHHMM(*hhmm); ok {
			hour, min = h, mi
		}
	}
	return time.Date(y, m, d, hour, min, 0, 0, loc)
}

func detailCheckin(d *store.HotelDetail) *string {
	if d == nil {
		return nil
	}
	return d.StandardCheckin
}

func detailCheckout(d *store.HotelDetail) *string {
	if d == nil {
		return nil
	}
	return d.StandardCheckout
}

// parseHHMM parses "HH:MM" (24h). ok=false on any malformed input.
func parseHHMM(s string) (hour, min int, ok bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
