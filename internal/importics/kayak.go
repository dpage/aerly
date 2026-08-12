package importics

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

// Kayak's per-account "Trips calendar feed" (kayak.com/trips → iCal) differs
// from a TripIt export in two structural ways the mapper has to handle:
//
//   - It carries *many* trips in one VCALENDAR, not one. Each event names its
//     trip through the "View trip" URL (…/trips/!<token>?ref=calendar), and
//     every trip has a date-only envelope VEVENT holding the trip name + span.
//     mapKayak groups by that token and emits one MappedTrip per trip.
//   - It encodes a booking differently from TripIt: the flight ident sits in
//     the SUMMARY ("LH 6437 from Vienna (VIE) to Frankfurt am Main (FRA)"),
//     the provider/route for rail and bus live in the DESCRIPTION
//     ("… Train Number 1048 … Departing from X - <date> … Arriving at Y …"),
//     and lodging arrives as "Check in to …" / "Check out from …" pairs, with
//     vehicle hire arriving the same way as "Car Pickup" / "Car Dropoff".

// kayakTripRe extracts the Kayak trip token from a "View trip" URL, e.g.
// https://www.kayak.com/trips/!Beb7q6%24IgAwBD25W?ref=calendar → the token
// "Beb7q6%24IgAwBD25W". Every event of a trip (including its envelope) repeats
// the same token, so it is the grouping key and the re-import dedupe id.
var kayakTripRe = regexp.MustCompile(`kayak\.com/trips/!([^?\s]+)`)

// kayakFlightRe matches a Kayak flight SUMMARY:
// "LH 6437 from Vienna (VIE) to Frankfurt am Main (FRA)" → airline, number,
// origin IATA, destination IATA. The airline designator may carry a digit
// (easyJet "U2", Wizz "W6"), so the first group allows alphanumerics.
var kayakFlightRe = regexp.MustCompile(`^([A-Z0-9]{2,3})\s+([0-9]{1,4})\s+from\s+.+\(([A-Z]{3})\)\s+to\s+.+\(([A-Z]{3})\)$`)

// kayakDepartRe / kayakArriveRe pull the origin / destination place names out of
// a rail or bus DESCRIPTION line, e.g.
// "Departing from Bratislava - Central Bus Station Nivy, Slovakia - 11/04/2024 6:00AM CET".
// The place itself may contain " - ", so the capture is non-greedy up to the
// trailing "<date>".
var (
	kayakDepartRe = regexp.MustCompile(`Departing from (.+?) - \d{1,2}/\d{1,2}/\d{4}`)
	kayakArriveRe = regexp.MustCompile(`Arriving at (.+?) - \d{1,2}/\d{1,2}/\d{4}`)
)

// Free-text DESCRIPTION fields Kayak emits as "<Label>: <value>" on one line.
var (
	kayakConfirmRe     = regexp.MustCompile(`Confirmation Number:\s*([^\n]+)`)
	kayakBookingRe     = regexp.MustCompile(`Booking reference:\s*([^\n]+)`)
	kayakPhoneFieldRe  = regexp.MustCompile(`Phone Number:\s*([^\n]+)`)
	kayakAddressRe     = regexp.MustCompile(`Address:\s*([^\n]+)`)
	kayakAgencyRe      = regexp.MustCompile(`Agency:\s*([^)]+)\)`)
	kayakPickupAddrRe  = regexp.MustCompile(`Pickup Address:\s*([^\n]+)`)
	kayakDropoffAddrRe = regexp.MustCompile(`Dropoff Address:\s*([^\n]+)`)
	kayakCarTypeRe     = regexp.MustCompile(`Car Type:\s*([^\n]+)`)
)

// mapKayak splits one parsed Kayak calendar — which carries several trips — into
// one MappedTrip per trip. It backs the SourceKayak Mapper (see source.go);
// callers use MapAll for source detection + dispatch.
func mapKayak(cal *Calendar) []*MappedTrip {
	type group struct {
		envelope *Event
		events   []Event
	}
	groups := map[string]*group{}
	var order []string
	for i := range cal.Events {
		e := cal.Events[i]
		token := kayakTripToken(e)
		if token == "" {
			continue
		}
		g := groups[token]
		if g == nil {
			g = &group{}
			groups[token] = g
			order = append(order, token)
		}
		if isKayakEnvelope(e) {
			env := e
			g.envelope = &env
			continue
		}
		g.events = append(g.events, e)
	}

	trips := make([]*MappedTrip, 0, len(order))
	for _, token := range order {
		g := groups[token]
		if mt := mapKayakTrip(token, g.envelope, g.events); mt != nil {
			trips = append(trips, mt)
		}
	}
	return trips
}

// mapKayakTrip maps one Kayak trip (its envelope plus its booking events) into a
// MappedTrip. Returns nil for a group with neither an envelope nor any mappable
// booking. The token is the trip's source id, used for re-import dedupe.
func mapKayakTrip(token string, envelope *Event, events []Event) *MappedTrip {
	mt := &MappedTrip{TripItID: token}
	if envelope != nil {
		mt.Name = strings.TrimSpace(envelope.Summary)
		mt.StartsOn, mt.EndsOn = envelopeDates(*envelope)
	}

	var hotelEvents, carEvents []Event
	for i := range events {
		e := events[i]
		switch classifyKayak(e) {
		case "flight":
			if p, ok := mapKayakFlight(e); ok {
				mt.Plans = append(mt.Plans, p)
			}
		case "hotel":
			hotelEvents = append(hotelEvents, e)
		case "train":
			if p, ok := mapKayakTransport(e, "train"); ok {
				mt.Plans = append(mt.Plans, p)
			}
		case "ground":
			if p, ok := mapKayakTransport(e, "ground"); ok {
				mt.Plans = append(mt.Plans, p)
			}
		case "car":
			carEvents = append(carEvents, e)
		case "dining":
			if p, ok := mapKayakSimple(e, "dining"); ok {
				mt.Plans = append(mt.Plans, p)
			}
		case "excursion":
			if p, ok := mapKayakSimple(e, "excursion"); ok {
				mt.Plans = append(mt.Plans, p)
			}
		}
	}
	// Lodging and vehicle hire are both ranged bookings Kayak writes as a pair
	// of events, so they are mapped after the loop, once both edges are in hand.
	mt.Plans = append(mt.Plans, mapKayakHotels(hotelEvents)...)
	mt.Plans = append(mt.Plans, mapKayakCars(carEvents)...)

	if envelope == nil && len(mt.Plans) == 0 {
		return nil
	}
	if mt.Name == "" {
		mt.Name = planops.TripNameForConfirmPlans(mt.Plans)
	}
	if mt.Name == "" {
		mt.Name = "Imported trip"
	}
	return mt
}

// kayakTripToken returns the trip token an event belongs to, or "" when its
// DESCRIPTION carries no "View trip" URL.
func kayakTripToken(e Event) string {
	if m := kayakTripRe.FindStringSubmatch(e.Description); m != nil {
		return m[1]
	}
	return ""
}

// isKayakEnvelope reports whether an event is a trip's whole-span summary entry
// rather than a booking: Kayak writes the envelope with a date-only span
// (VALUE=DATE) and the trip name in SUMMARY, while every booking carries a timed
// DTSTART.
func isKayakEnvelope(e Event) bool {
	return !e.Start.HasTime
}

// classifyKayak returns the Aerly plan type for a Kayak booking event, or "" to
// skip it. Lodging is recognised by its "Check in/out" SUMMARY and car rental by
// its "Car Pickup/Dropoff" SUMMARY; flights by the ident-bearing SUMMARY; rail,
// bus, dining and activities by the marker Kayak writes into the DESCRIPTION.
func classifyKayak(e Event) string {
	switch {
	case strings.HasPrefix(e.Summary, "Check in to "), strings.HasPrefix(e.Summary, "Check out from "):
		return "hotel"
	case strings.HasPrefix(e.Summary, "Car Pickup"), strings.HasPrefix(e.Summary, "Car Dropoff"):
		return "car"
	case kayakFlightRe.MatchString(e.Summary):
		return "flight"
	}
	d := e.Description
	switch {
	case strings.Contains(d, " Train Number "):
		return "train"
	case strings.Contains(d, " Bus Number "):
		return "ground"
	case strings.Contains(d, "Restaurant Name:"):
		return "dining"
	case strings.Contains(d, "Event Name:"):
		return "excursion"
	}
	return ""
}

// mapKayakFlight maps a Kayak flight event into a flight plan, taking the ident
// and IATA route from the SUMMARY and the schedule from the event's UTC instants.
func mapKayakFlight(e Event) (planops.ConfirmPlanInput, bool) {
	m := kayakFlightRe.FindStringSubmatch(e.Summary)
	if m == nil {
		return planops.ConfirmPlanInput{}, false
	}
	ident, origin, dest := m[1]+m[2], m[3], m[4]
	out := e.Start.Time
	in := e.End.Time

	fd := &store.FlightDetail{
		Ident:        ident,
		OriginIATA:   origin,
		DestIATA:     dest,
		ScheduledOut: out,
		ScheduledIn:  in,
		FlightStatus: kayakFlightStatus(in),
	}

	part := planops.ConfirmPartInput{
		Type:       "flight",
		StartsAt:   out,
		StartLabel: orDefault(e.Location, origin),
		EndLabel:   dest,
		Flight:     fd,
	}
	if !in.IsZero() {
		part.EndsAt = &in
	}
	if lat, lon, ok := airports.Lookup(origin); ok {
		part.StartLat, part.StartLon = &lat, &lon
	}
	if tz, ok := airports.LookupTZ(origin); ok {
		part.StartTZ = tz
	}
	if lat, lon, ok := airports.Lookup(dest); ok {
		part.EndLat, part.EndLon = &lat, &lon
	}
	if tz, ok := airports.LookupTZ(dest); ok {
		part.EndTZ = tz
	}

	return planops.ConfirmPlanInput{
		Type:            "flight",
		Title:           e.Summary,
		Source:          importSource,
		ConfirmationRef: kayakConfirmation(e.Description),
		TripItUID:       e.UID,
		Parts:           []planops.ConfirmPartInput{part},
	}, true
}

// kayakFlightStatus marks an already-arrived flight terminal (so the live poller
// skips it) and leaves a future one "Scheduled" so it is tracked. Kayak feeds
// mix past and future trips, unlike a single TripIt export.
func kayakFlightStatus(scheduledIn time.Time) string {
	if !scheduledIn.IsZero() && scheduledIn.Before(time.Now()) {
		return "Arrived"
	}
	return "Scheduled"
}

// mapKayakTransport maps a Kayak rail or bus event into a train / ground plan.
// The route comes from the DESCRIPTION's "Departing from … / Arriving at …"
// lines and the provider from the SUMMARY ("Bus Slovak Lines 102806").
func mapKayakTransport(e Event, planType string) (planops.ConfirmPlanInput, bool) {
	from := kayakField(kayakDepartRe, e.Description)
	to := kayakField(kayakArriveRe, e.Description)
	out := e.Start.Time
	in := e.End.Time

	part := planops.ConfirmPartInput{
		Type:       planType,
		StartsAt:   out,
		StartLabel: orDefault(from, e.Location),
		EndLabel:   to,
	}
	if !in.IsZero() {
		part.EndsAt = &in
	}
	provider := kayakProvider(e.Summary)
	if planType == "train" {
		part.Train = &store.TrainDetail{Operator: provider}
	} else {
		part.Ground = &store.GroundDetail{Provider: provider}
	}

	return planops.ConfirmPlanInput{
		Type:            planType,
		Title:           e.Summary,
		Source:          importSource,
		ConfirmationRef: kayakConfirmation(e.Description),
		TripItUID:       e.UID,
		Parts:           []planops.ConfirmPartInput{part},
	}, true
}

// mapKayakCars pairs "Car Pickup" / "Car Dropoff" events into vehicle-hire
// plans, one per rental. Pairing is what makes the plan a genuine range: the
// pickup event spans only the collection appointment (a half-hour at the desk),
// so a hire mapped from it alone would end on its first morning and never band
// into the Pickup / Return entries a hire is meant to read as.
//
// A dropoff with no pickup yields nothing, matching the old behaviour: the
// pickup is the anchor that carries both endpoints' addresses, the confirmation
// number and the UID the re-import dedupe keys on, so a stray return event on
// its own is not enough to build a plan from.
func mapKayakCars(events []Event) []planops.ConfirmPlanInput {
	var pickups, dropoffs []*Event
	for i := range events {
		if strings.HasPrefix(events[i].Summary, "Car Dropoff") {
			dropoffs = append(dropoffs, &events[i])
			continue
		}
		pickups = append(pickups, &events[i])
	}
	// Pair in chronological order rather than file order, so that two hires
	// claim their own returns even if the feed lists the events interleaved.
	sortEventsByStart(pickups)
	sortEventsByStart(dropoffs)

	claimed := make([]bool, len(dropoffs))
	out := make([]planops.ConfirmPlanInput, 0, len(pickups))
	for _, p := range pickups {
		out = append(out, mapKayakCar(*p, matchKayakDropoff(*p, dropoffs, claimed)))
	}
	return out
}

// sortEventsByStart orders events by their start instant, keeping the feed's
// own order for events that start together.
func sortEventsByStart(events []*Event) {
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Start.Time.Before(events[j].Start.Time)
	})
}

// matchKayakDropoff returns the return event belonging to a pickup, marking it
// claimed so a second hire cannot take the same one, or nil when the feed
// carries no return for this pickup.
//
// A rental is identified by its agency plus its confirmation number, both of
// which Kayak repeats on the return event, and the match is the earliest
// unclaimed return at or after the pickup: two hires from the same agency on one
// trip then pair in order instead of both seizing the first return. The second
// pass relaxes the match to the agency alone, so a return that omits the
// confirmation number still pairs with the hire it plainly belongs to.
func matchKayakDropoff(pickup Event, dropoffs []*Event, claimed []bool) *Event {
	for _, withRef := range []bool{true, false} {
		for i, d := range dropoffs {
			if claimed[i] || d.Start.Time.Before(pickup.Start.Time) {
				continue
			}
			if !kayakCarPairs(pickup, *d, withRef) {
				continue
			}
			claimed[i] = true
			return d
		}
	}
	return nil
}

// kayakCarPairs reports whether a return event belongs to a pickup: the same
// agency always, and the same confirmation number too when withRef is set.
func kayakCarPairs(pickup, dropoff Event, withRef bool) bool {
	if !strings.EqualFold(kayakField(kayakAgencyRe, pickup.Summary), kayakField(kayakAgencyRe, dropoff.Summary)) {
		return false
	}
	if !withRef {
		return true
	}
	return strings.EqualFold(kayakConfirmation(pickup.Description), kayakConfirmation(dropoff.Description))
}

// mapKayakCar maps one rental into a vehicle-hire plan spanning the hire: the
// pickup event's DTSTART is collection and the paired dropoff event's DTSTART is
// the return. The pickup carries both branches' addresses and the hire company
// goes on the plan's SupplierName, which is where the vehicle-hire design keeps
// it (VehicleHireDetail has no provider field of its own).
func mapKayakCar(e Event, dropoff *Event) planops.ConfirmPlanInput {
	agency := kayakField(kayakAgencyRe, e.Summary)
	label := orDefault(agency, "Car hire")
	pickupAddr := kayakField(kayakPickupAddrRe, e.Description)
	dropoffAddr := kayakField(kayakDropoffAddrRe, e.Description)
	if dropoffAddr == "" && dropoff != nil {
		dropoffAddr = orDefault(kayakField(kayakDropoffAddrRe, dropoff.Description), dropoff.Location)
	}

	part := planops.ConfirmPartInput{
		Type:         "vehicle_hire",
		StartsAt:     e.Start.Time,
		StartLabel:   label,
		StartAddress: pickupAddr,
		// Both edges are labelled with the hire company; the branches differ by
		// address, and a banded Return entry that showed the address as its
		// label would print the same line twice.
		EndLabel:    label,
		EndAddress:  orDefault(dropoffAddr, pickupAddr),
		VehicleHire: kayakVehicleHire(e.Description),
	}
	// Only fall back to the pickup's own DTEND when nothing paired with it: that
	// instant is the end of the collection appointment, not the return.
	switch {
	case dropoff != nil && !dropoff.Start.Time.IsZero():
		end := dropoff.Start.Time
		part.EndsAt = &end
	case !e.End.Time.IsZero():
		end := e.End.Time
		part.EndsAt = &end
	}

	return planops.ConfirmPlanInput{
		Type:            "vehicle_hire",
		Title:           label,
		Source:          importSource,
		ConfirmationRef: kayakConfirmation(e.Description),
		SupplierName:    agency,
		TripItUID:       e.UID,
		Parts:           []planops.ConfirmPartInput{part},
	}
}

// kayakVehicleHire builds the vehicle-hire satellite from what a Kayak
// description actually states, which is only the car type ("Sample Car or
// similar Automatic"); its trailing gearbox word is split off into Transmission.
// The remaining fields (category, fuel policy, mileage, excess, deposit) are
// absent from the feed and stay empty rather than being guessed at.
func kayakVehicleHire(desc string) *store.VehicleHireDetail {
	d := &store.VehicleHireDetail{Vehicle: kayakField(kayakCarTypeRe, desc)}
	for _, gearbox := range []string{"Automatic", "Manual"} {
		if strings.HasSuffix(d.Vehicle, " "+gearbox) {
			d.Transmission = gearbox
			d.Vehicle = strings.TrimSpace(strings.TrimSuffix(d.Vehicle, gearbox))
			break
		}
	}
	return d
}

// mapKayakSimple maps a single-venue booking (a dining reservation or an
// activity) into a dining / excursion plan anchored at one place.
func mapKayakSimple(e Event, planType string) (planops.ConfirmPlanInput, bool) {
	name := strings.TrimSpace(e.Summary)
	out := e.Start.Time
	in := e.End.Time

	part := planops.ConfirmPartInput{
		Type:         planType,
		StartsAt:     out,
		StartLabel:   name,
		StartAddress: orDefault(kayakField(kayakAddressRe, e.Description), e.Location),
	}
	if !in.IsZero() {
		part.EndsAt = &in
	}
	if planType == "dining" {
		part.Dining = &store.DiningDetail{ReservationName: name}
	} else {
		part.Excursion = &store.ExcursionDetail{}
	}
	applyGeoTZ(&part, e.Geo)

	return planops.ConfirmPlanInput{
		Type:            planType,
		Title:           name,
		Source:          importSource,
		ConfirmationRef: kayakConfirmation(e.Description),
		TripItUID:       e.UID,
		Parts:           []planops.ConfirmPartInput{part},
	}, true
}

// mapKayakHotels pairs "Check in to X" / "Check out from X" events that share a
// property name into a single hotel plan spanning the stay (the check-in event's
// DTSTART is arrival; the check-out event's DTSTART is departure). An unpaired
// event still yields a plan from whatever instant it carries.
func mapKayakHotels(events []Event) []planops.ConfirmPlanInput {
	type stay struct{ in, out *Event }
	stays := map[string]*stay{}
	var order []string
	for i := range events {
		name, edge := kayakHotelNameEdge(events[i].Summary)
		st := stays[name]
		if st == nil {
			st = &stay{}
			stays[name] = st
			order = append(order, name)
		}
		if edge == "check-out" {
			st.out = &events[i]
		} else {
			st.in = &events[i]
		}
	}

	out := make([]planops.ConfirmPlanInput, 0, len(order))
	for _, name := range order {
		st := stays[name]
		anchor := st.in
		if anchor == nil {
			anchor = st.out
		}
		part := planops.ConfirmPartInput{
			Type:       "hotel",
			StartsAt:   anchor.Start.Time,
			StartLabel: name,
			Hotel: &store.HotelDetail{
				PropertyName: name,
				Address:      anchor.Location,
				Phone:        kayakField(kayakPhoneFieldRe, anchor.Description),
			},
		}
		if st.out != nil && !st.out.Start.Time.IsZero() {
			end := st.out.Start.Time
			part.EndsAt = &end
		}
		geo := anchor.Geo
		if geo == nil && st.out != nil {
			geo = st.out.Geo
		}
		applyGeoTZ(&part, geo)
		out = append(out, planops.ConfirmPlanInput{
			Type:            "hotel",
			Title:           name,
			Source:          importSource,
			ConfirmationRef: kayakConfirmation(anchor.Description),
			TripItUID:       anchor.UID,
			Parts:           []planops.ConfirmPartInput{part},
		})
	}
	return out
}

// kayakHotelNameEdge splits a Kayak lodging SUMMARY ("Check in to Hotel X" /
// "Check out from Hotel X") into the property name and which edge it marks.
func kayakHotelNameEdge(summary string) (name, edge string) {
	switch {
	case strings.HasPrefix(summary, "Check in to "):
		return strings.TrimSpace(strings.TrimPrefix(summary, "Check in to ")), "check-in"
	case strings.HasPrefix(summary, "Check out from "):
		return strings.TrimSpace(strings.TrimPrefix(summary, "Check out from ")), "check-out"
	default:
		return strings.TrimSpace(summary), "check-in"
	}
}

// kayakProvider strips the leading transport word from a SUMMARY, turning
// "Bus Slovak Lines 102806" → "Slovak Lines 102806" and "Train RJ 1048" →
// "RJ 1048".
func kayakProvider(summary string) string {
	for _, p := range []string{"Bus ", "Train "} {
		if strings.HasPrefix(summary, p) {
			return strings.TrimSpace(strings.TrimPrefix(summary, p))
		}
	}
	return strings.TrimSpace(summary)
}

// kayakConfirmation returns the booking's confirmation number, falling back to
// its booking reference, from a DESCRIPTION.
func kayakConfirmation(desc string) string {
	if v := kayakField(kayakConfirmRe, desc); v != "" {
		return v
	}
	return kayakField(kayakBookingRe, desc)
}

// kayakField returns the trimmed first capture of re against s, or "".
func kayakField(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}
