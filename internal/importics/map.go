package importics

import (
	"regexp"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/geotz"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

// MappedTrip is the result of turning one TripIt .ics (one trip) into Aerly
// shapes: trip metadata plus a set of plans ready for planops.Commit. The
// importer creates a trip from Name/StartsOn/EndsOn (or matches an existing
// one) and commits Plans against it.
type MappedTrip struct {
	Name string
	// TripItID is the source TripIt trip id (from the envelope's show URL),
	// empty if it couldn't be found. The importer uses it to reuse an existing
	// trip on re-import rather than duplicating it.
	TripItID string
	StartsOn *time.Time // trip start (local date)
	EndsOn   *time.Time // trip end (local date; inclusive)
	// Plans carry their source event UID in ConfirmPlanInput.TripItUID for
	// per-plan re-import dedupe.
	Plans []planops.ConfirmPlanInput
}

// importSource is the plan source recorded for everything we bring in. The
// plans.source CHECK constraint allows only manual|paste|upload|email; an
// uploaded .ics is "upload".
const importSource = "upload"

// flightRe matches a TripIt flight SUMMARY, e.g. "AY832 LHR to HEL": an
// airline+number ident followed by an origin and destination IATA code. The
// airline designator may contain a digit (easyJet "U2", Wizz "W6", Sichuan
// "3U"), so the leading group allows alphanumerics.
var flightRe = regexp.MustCompile(`^([A-Z0-9]{2,3}[0-9]{1,4})\s+([A-Z]{3})\s+to\s+([A-Z]{3})$`)

// transportRe matches a ground/rail SUMMARY, e.g.
// "Bonny's Taxi - Home to Heathrow T3" → provider / from / to.
var transportRe = regexp.MustCompile(`^(.+?) - (.+) to (.+)$`)

// phoneRe pulls an international phone number out of a lodging description,
// e.g. "+372 631-5333" or "+1-604-689-8188".
var phoneRe = regexp.MustCompile(`\+[0-9][0-9 ()\-]{6,}[0-9]`)

// tripItIDRe extracts the numeric TripIt trip id from a show URL, which appears
// as "trip/show?id=175153005" (envelope) or "trip/show/id/175153005" (items).
var tripItIDRe = regexp.MustCompile(`trip/show(?:\?id=|/id/)(\d+)`)

// tripItID returns the TripIt trip id mentioned in a description, or "".
func tripItID(desc string) string {
	if m := tripItIDRe.FindStringSubmatch(desc); m != nil {
		return m[1]
	}
	return ""
}

// mapTripIt maps one parsed TripIt calendar into a MappedTrip. Events it can't
// classify are skipped; a multi-night hotel's two check-in/check-out events are
// paired back into a single hotel plan. It backs the SourceTripIt Mapper (see
// source.go); callers use Map for source detection + dispatch.
func mapTripIt(cal *Calendar) *MappedTrip {
	mt := &MappedTrip{Name: tripName(cal)}

	// TripIt stamps each transport event with a single GEO = its *arrival*
	// point, so a transfer's two ends can't both come from its own event. This
	// index maps each place to the coordinate TripIt recorded for it, letting a
	// leg recover its *origin* from the opposite leg that arrived there.
	placeGeo := transferPlaceIndex(cal)

	// First pass: split the envelope (whole-trip date span) from the bookings,
	// and collect hotel events for pairing.
	var hotelEvents []Event
	for _, e := range cal.Events {
		if isEnvelope(e) {
			s, end := envelopeDates(e)
			mt.StartsOn, mt.EndsOn = s, end
			mt.TripItID = tripItID(e.Description)
			continue
		}
		switch c := classify(e); c {
		case "flight":
			if p, ok := mapFlight(e); ok {
				mt.Plans = append(mt.Plans, p)
			}
		case "ground", "train":
			if p, ok := mapTransport(e, c, placeGeo); ok {
				mt.Plans = append(mt.Plans, p)
			}
		case "hotel":
			hotelEvents = append(hotelEvents, e)
		}
	}
	mt.Plans = append(mt.Plans, mapHotels(hotelEvents)...)
	// Name the trip for where it goes when the calendar carried no name of its
	// own, so a nameless import isn't a generic "Imported trip" (#21).
	if mt.Name == "" {
		mt.Name = planops.TripNameForConfirmPlans(mt.Plans)
	}
	if mt.Name == "" {
		mt.Name = "Imported trip"
	}
	return mt
}

// isEnvelope reports whether an event is the whole-trip summary entry rather
// than a booking: TripIt writes it with a date-only span and a UID without the
// "item-" prefix the booking events carry.
func isEnvelope(e Event) bool {
	return !e.Start.HasTime && !strings.HasPrefix(e.UID, "item-")
}

// classify returns the Aerly plan type for a booking event, or "" to skip it.
// The [Type] tag TripIt embeds in the description is the primary signal, with
// the SUMMARY shape as a fallback. Rail arrives two ways: tagged [Rail]
// directly (#65), or mis-filed under [Ground Transportation] for
// some operators (e.g. Eurotunnel); both are lifted to "train" here so this is
// the single place that settles the transport type.
func classify(e Event) string {
	if strings.HasPrefix(e.Summary, "Check-in:") || strings.HasPrefix(e.Summary, "Check-out:") {
		return "hotel"
	}
	d := e.Description
	switch {
	case strings.Contains(d, "[Flight]"):
		return "flight"
	case strings.Contains(d, "[Lodging]"):
		return "hotel"
	case strings.Contains(d, "[Rail]"):
		return "train"
	case strings.Contains(d, "[Ground Transportation]"), strings.Contains(d, "[Transportation]"):
		if isRail(e.Summary) {
			return "train"
		}
		return "ground"
	}
	if flightRe.MatchString(e.Summary) {
		return "flight"
	}
	return ""
}

// isRail recognises rail providers TripIt mis-files as ground transport.
func isRail(summary string) bool {
	s := strings.ToLower(summary)
	for _, kw := range []string{"eurotunnel", "eurostar", "rail", "railway", "train"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// mapFlight maps a TripIt flight event into a flight plan. ok is false when the
// SUMMARY isn't a recognisable "<ident> <ORIG> to <DEST>" flight line.
func mapFlight(e Event) (planops.ConfirmPlanInput, bool) {
	m := flightRe.FindStringSubmatch(e.Summary)
	if m == nil {
		return planops.ConfirmPlanInput{}, false
	}
	ident, origin, dest := m[1], m[2], m[3]
	out := e.Start.Time
	in := e.End.Time

	fd := &store.FlightDetail{
		Ident:        ident,
		OriginIATA:   origin,
		DestIATA:     dest,
		ScheduledOut: out,
		ScheduledIn:  in,
		// Past flights are imported as historical records: a terminal status
		// keeps the live poller (which polls non-terminal flights whose
		// departure is past) from trying to track them.
		FlightStatus: "Arrived",
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
	// Origin coords + tz from the IATA table.
	if lat, lon, ok := airports.Lookup(origin); ok {
		part.StartLat, part.StartLon = &lat, &lon
	}
	if tz, ok := airports.LookupTZ(origin); ok {
		part.StartTZ = tz
	}
	// Destination coords/tz from the IATA table, else the event's GEO (TripIt
	// puts the destination coordinate on the flight event).
	if lat, lon, ok := airports.Lookup(dest); ok {
		part.EndLat, part.EndLon = &lat, &lon
	} else if e.Geo != nil {
		part.EndLat, part.EndLon = &e.Geo.Lat, &e.Geo.Lon
	}
	if tz, ok := airports.LookupTZ(dest); ok {
		part.EndTZ = tz
	} else if e.Geo != nil {
		if tz, ok := geotz.Lookup(e.Geo.Lat, e.Geo.Lon); ok {
			part.EndTZ = tz
		}
	}

	return planops.ConfirmPlanInput{
		Type:      "flight",
		Title:     e.Summary,
		Source:    importSource,
		TripItUID: e.UID,
		Parts:     []planops.ConfirmPartInput{part},
	}, true
}

// mapTransport maps a TripIt ground/rail event into a ground or train plan,
// parsing "<provider> - <from> to <to>" from the SUMMARY. planType ("ground" or
// "train") comes from classify, which is the single arbiter of the type; this
// function trusts it rather than re-sniffing the summary. placeGeo (built by
// transferPlaceIndex) supplies the origin coordinate TripIt omits.
func mapTransport(e Event, planType string, placeGeo map[string]LatLon) (planops.ConfirmPlanInput, bool) {
	provider, from, to := e.Summary, "", ""
	if m := transportRe.FindStringSubmatch(e.Summary); m != nil {
		provider, from, to = m[1], m[2], m[3]
	}
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
	if planType == "train" {
		part.Train = &store.TrainDetail{Operator: provider}
	} else {
		part.Ground = &store.GroundDetail{Provider: provider}
	}

	if to == "" {
		// SUMMARY didn't parse as A→B: treat it as a single point, as before.
		applyGeoTZ(&part, e.Geo)
	} else {
		// A genuine transfer: TripIt's GEO is the arrival → the end. Recover the
		// start from the place index (the opposite leg arrived there). Distinct
		// endpoints are what let the map draw the crow-flight leg.
		applyTransferGeo(&part, lookupPlace(placeGeo, from), e.Geo)
	}

	return planops.ConfirmPlanInput{
		Type:      planType,
		Title:     e.Summary,
		Source:    importSource,
		TripItUID: e.UID,
		Parts:     []planops.ConfirmPartInput{part},
	}, true
}

// transferPlaceIndex maps a normalised place name to the coordinate TripIt
// recorded for it. TripIt stamps each transport event with a single GEO = the
// *arrival* point, so the event's parsed destination is the key. In a round
// trip this lets a later leg recover its origin from the earlier leg that
// arrived there (and vice versa).
func transferPlaceIndex(cal *Calendar) map[string]LatLon {
	idx := map[string]LatLon{}
	for _, e := range cal.Events {
		if e.Geo == nil {
			continue
		}
		switch classify(e) {
		case "ground", "train":
			if m := transportRe.FindStringSubmatch(e.Summary); m != nil {
				if to := strings.ToLower(strings.TrimSpace(m[3])); to != "" {
					idx[to] = *e.Geo
				}
			}
		}
	}
	return idx
}

// lookupPlace returns a copy of the indexed coordinate for a place label, or nil
// when the label isn't known (e.g. a one-way transfer whose origin never appears
// as another leg's destination — the start is then left for later geocoding).
func lookupPlace(idx map[string]LatLon, label string) *LatLon {
	if g, ok := idx[strings.ToLower(strings.TrimSpace(label))]; ok {
		return &g
	}
	return nil
}

// applyTransferGeo fills a transfer part's start/end coordinates and timezones
// from two distinct points, each via geotz for its own zone. Either may be nil
// (an end always known from the event's GEO; a start only when the index has it).
func applyTransferGeo(part *planops.ConfirmPartInput, startGeo, endGeo *LatLon) {
	if startGeo != nil {
		part.StartLat, part.StartLon = &startGeo.Lat, &startGeo.Lon
		if tz, ok := geotz.Lookup(startGeo.Lat, startGeo.Lon); ok {
			part.StartTZ = tz
		}
	}
	if endGeo != nil {
		part.EndLat, part.EndLon = &endGeo.Lat, &endGeo.Lon
		if tz, ok := geotz.Lookup(endGeo.Lat, endGeo.Lon); ok {
			part.EndTZ = tz
		}
	}
}

// mapHotels pairs check-in and check-out events that share a property name into
// a single hotel plan spanning the stay. An unpaired event still yields a plan
// using whatever instant it carries.
func mapHotels(events []Event) []planops.ConfirmPlanInput {
	type stay struct {
		in, out *Event
	}
	stays := map[string]*stay{}
	var order []string
	for i := range events {
		e := events[i]
		name, edge := hotelNameEdge(e.Summary)
		st := stays[name]
		if st == nil {
			st = &stay{}
			stays[name] = st
			order = append(order, name)
		}
		switch edge {
		case "check-out":
			st.out = &events[i]
		default:
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
				Phone:        phoneRe.FindString(anchor.Description),
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
			Type:      "hotel",
			Title:     name,
			Source:    importSource,
			TripItUID: anchor.UID,
			Parts:     []planops.ConfirmPartInput{part},
		})
	}
	return out
}

// applyGeoTZ fills a part's start/end coordinates and timezone from a GEO point
// (a single venue, so both ends share it) using geotz for the zone.
func applyGeoTZ(part *planops.ConfirmPartInput, geo *LatLon) {
	if geo == nil {
		return
	}
	part.StartLat, part.StartLon = &geo.Lat, &geo.Lon
	part.EndLat, part.EndLon = &geo.Lat, &geo.Lon
	if tz, ok := geotz.Lookup(geo.Lat, geo.Lon); ok {
		part.StartTZ, part.EndTZ = tz, tz
	}
}

// hotelNameEdge splits a hotel SUMMARY ("Check-in: Hotel X" / "Check-out:
// Hotel X") into the property name and which edge of the stay it marks.
func hotelNameEdge(summary string) (name, edge string) {
	switch {
	case strings.HasPrefix(summary, "Check-in:"):
		return strings.TrimSpace(strings.TrimPrefix(summary, "Check-in:")), "check-in"
	case strings.HasPrefix(summary, "Check-out:"):
		return strings.TrimSpace(strings.TrimPrefix(summary, "Check-out:")), "check-out"
	default:
		return strings.TrimSpace(summary), "check-in"
	}
}

// tripName derives the Aerly trip name from the calendar's own name. TripIt's
// X-WR-CALDESC reads "<name> (Trip Shared by <user>)"; strip that suffix.
// Returns "" when the calendar carries no name of its own, leaving the caller
// to fall back to a destination-based name (see mapTripIt).
func tripName(cal *Calendar) string {
	if d := strings.TrimSpace(cal.Desc); d != "" {
		if i := strings.Index(d, " (Trip Shared by"); i > 0 {
			return strings.TrimSpace(d[:i])
		}
		return d
	}
	return strings.TrimSpace(cal.Name)
}

// envelopeDates returns the trip's inclusive start and end dates from the
// whole-trip envelope event. iCalendar DTEND for a date-only value is
// exclusive (the morning after), so the inclusive end is one day earlier.
func envelopeDates(e Event) (*time.Time, *time.Time) {
	var start, end *time.Time
	if !e.Start.Time.IsZero() {
		s := e.Start.Time
		start = &s
	}
	if !e.End.Time.IsZero() {
		en := e.End.Time.AddDate(0, 0, -1)
		end = &en
	}
	return start, end
}

// orDefault returns s, or fallback when s is blank (after trimming).
func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
