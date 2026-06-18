// Command kayak-ics-convert reads Kayak backup JSON files from a folder and
// emits a single .ics file in Kayak's "Trips calendar feed" format, suitable
// for import into Aerly via the importics/kayak mapper.
//
// Each file in the source folder is a single-trip JSON export as produced by
// Kayak's backup/download feature. The tool groups all trips that overlap the requested year (and
// optional month), then writes one VCALENDAR containing every event from those
// trips — one date-only envelope VEVENT per trip plus one timed VEVENT per
// booking — exactly as Kayak's live feed would.
//
// Usage:
//
//	go run ./cmd/kayak-ics-convert -folder _trips -period 2025        > trips_2025.ics
//	go run ./cmd/kayak-ics-convert -folder _trips -period 2025-11     > trips_2025_11.ics
//	go run ./cmd/kayak-ics-convert -folder _trips -period 2025 -out trips_2025.ics
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("convert failed", "err", err)
		os.Exit(1)
	}
}

// ── CLI ──────────────────────────────────────────────────────────────────────

func run() error {
	folder := flag.String("folder", "", "path to folder containing Kayak backup JSON files (required)")
	period := flag.String("period", "", "year or year-month to include, e.g. 2025 or 2025-11 (required)")
	outFile := flag.String("out", "", "output .ics file (default: stdout)")
	flag.Parse()

	if *folder == "" || *period == "" {
		flag.Usage()
		return fmt.Errorf("both -folder and -period are required")
	}

	start, end, err := parsePeriod(*period)
	if err != nil {
		return fmt.Errorf("invalid -period %q: %w", *period, err)
	}

	trips, err := loadTrips(*folder)
	if err != nil {
		return fmt.Errorf("loading trips: %w", err)
	}

	// Filter to trips that overlap [start, end).
	var selected []tripFile
	for _, tf := range trips {
		if overlaps(tf.tripStart, tf.tripEnd, start, end) {
			selected = append(selected, tf)
		}
	}
	slog.Info("period filter", "total", len(trips), "selected", len(selected), "start", start.Format("2006-01-02"), "end", end.Format("2006-01-02"))

	ics := buildICS(selected)

	if *outFile == "" {
		fmt.Print(ics)
	} else {
		if err := os.WriteFile(*outFile, []byte(ics), 0o644); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
		slog.Info("wrote", "file", *outFile)
	}
	return nil
}

// parsePeriod accepts "YYYY" or "YYYY-MM" and returns the half-open [start,end)
// interval for that year or month.
func parsePeriod(s string) (start, end time.Time, err error) {
	switch len(s) {
	case 4:
		y, e := time.Parse("2006", s)
		if e != nil {
			return time.Time{}, time.Time{}, e
		}
		return y, y.AddDate(1, 0, 0), nil
	case 7:
		m, e := time.Parse("2006-01", s)
		if e != nil {
			return time.Time{}, time.Time{}, e
		}
		return m, m.AddDate(0, 1, 0), nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("must be YYYY or YYYY-MM")
	}
}

// overlaps reports whether the trip [tripStart, tripEnd] overlaps the filter
// window [wStart, wEnd). A trip is included if any day of it falls inside the
// window. We use day-level granularity: truncate to midnight UTC.
func overlaps(tripStart, tripEnd, wStart, wEnd time.Time) bool {
	ts := tripStart.Truncate(24 * time.Hour)
	te := tripEnd.Truncate(24 * time.Hour).Add(24 * time.Hour) // make inclusive end exclusive
	return ts.Before(wEnd) && te.After(wStart)
}

// ── JSON model ───────────────────────────────────────────────────────────────

// kayakDate is a "2006-01-02 15:04:05.0" timestamp as stored in the backup.
type kayakDate struct{ time.Time }

func (d *kayakDate) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	// Strip sub-second suffix ".0"
	s = strings.TrimSuffix(s, ".0")
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return fmt.Errorf("kayakDate %q: %w", s, err)
	}
	d.Time = t
	return nil
}

type address struct {
	LocationName string  `json:"locationName"`
	Address1     string  `json:"address1"`
	City         string  `json:"city"`
	Region       string  `json:"region"`
	Zip          string  `json:"zip"`
	Country      string  `json:"country"`
	PhoneNumber  string  `json:"phoneNumber"`
	Website      string  `json:"website"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	RawAddress   string  `json:"rawAddress"`
}

func (a address) label() string {
	if a.LocationName != "" {
		return a.LocationName
	}
	// Prefer rawAddress when it contains more than just a city — e.g. station
	// names like "Bratislava, hl.n, Slovakia" or airport codes like "VIE (VIE)".
	if a.RawAddress != "" {
		return a.RawAddress
	}
	if a.City != "" && a.Country != "" {
		return a.City + ", " + a.Country
	}
	if a.City != "" {
		return a.City
	}
	return ""
}

// iataFromRaw extracts "VIE" from "VIE (VIE)" raw address values.
var iataRe = regexp.MustCompile(`^([A-Z]{3})\s+\(([A-Z]{3})\)$`)

func iataFromRaw(raw string) string {
	if m := iataRe.FindStringSubmatch(strings.TrimSpace(raw)); m != nil {
		return m[1]
	}
	return ""
}

type segment struct {
	Number           string    `json:"number"`
	DepartureDate    kayakDate `json:"departureDate"`
	DepartureTZ      string    `json:"departureTimeZoneID"`
	ArrivalDate      kayakDate `json:"arrivalDate"`
	ArrivalTZ        string    `json:"arrivalTimeZoneID"`
	Carrier          string    `json:"carrier"`
	DepartureAddress address   `json:"departureAddress"`
	ArrivalAddress   address   `json:"arrivalAddress"`
}

type leg struct {
	Segments []segment `json:"segments"`
}

type bookingDetail struct {
	BookingReferenceNumber string `json:"bookingReferenceNumber"`
}

type tripEvent struct {
	ID              int           `json:"id"`
	UIDescription   string        `json:"UIDescription"`
	ConfirmationNum string        `json:"confirmationNumber"`
	BookingDetail   bookingDetail `json:"bookingDetail"`
	Legs            []leg         `json:"legs"`
	// Hotel / Restaurant / Other / Directions
	PlaceDescription string     `json:"placeDescription"`
	Address          address    `json:"address"`
	VenueStartDate   *kayakDate `json:"venueStartDate"`
	VenueStartTZ     string     `json:"venueStartTimeZoneID"`
	VenueEndDate     *kayakDate `json:"venueEndDate"`
	VenueEndTZ       string     `json:"venueEndTimeZoneID"`
	VenueSubType     string     `json:"venueSubType"`
	// Car rental
	AgencyName     string     `json:"agencyName"`
	CarType        string     `json:"carType"`
	Pickup         *kayakDate `json:"pickup"`
	PickupTZ       string     `json:"pickupTimeZoneID"`
	Dropoff        *kayakDate `json:"dropoff"`
	DropoffTZ      string     `json:"dropoffTimeZoneID"`
	PickupAddress  address    `json:"pickupAddress"`
	DropoffAddress address    `json:"dropoffAddress"`
}

type tripJSON struct {
	TripID     string      `json:"tripID"`
	Name       string      `json:"name"`
	StartDate  *kayakDate  `json:"startDate"`
	EndDate    *kayakDate  `json:"endDate"`
	TripEvents []tripEvent `json:"tripEvents"`
}

type tripFile struct {
	trip      tripJSON
	tripStart time.Time
	tripEnd   time.Time
}

// ── File loading ─────────────────────────────────────────────────────────────

func loadTrips(folder string) ([]tripFile, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	var out []tripFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		path := filepath.Join(folder, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("read error, skipping", "file", path, "err", err)
			continue
		}
		var tj tripJSON
		if err := json.Unmarshal(data, &tj); err != nil {
			slog.Warn("parse error, skipping", "file", path, "err", err)
			continue
		}
		if tj.TripID == "" {
			slog.Warn("no tripID, skipping", "file", path)
			continue
		}

		ts, te := tripSpan(tj)
		if ts.IsZero() {
			slog.Warn("could not determine trip dates, skipping", "file", path, "name", tj.Name)
			continue
		}
		out = append(out, tripFile{trip: tj, tripStart: ts, tripEnd: te})
	}
	// Sort by start date for deterministic output order.
	sort.Slice(out, func(i, j int) bool {
		return out[i].tripStart.Before(out[j].tripStart)
	})
	return out, nil
}

// tripSpan returns the trip's effective date span. It prefers the explicit
// startDate/endDate fields; when those are missing it falls back to the
// earliest/latest event times.
func tripSpan(tj tripJSON) (start, end time.Time) {
	if tj.StartDate != nil && !tj.StartDate.IsZero() {
		start = tj.StartDate.Time
	}
	if tj.EndDate != nil && !tj.EndDate.IsZero() {
		end = tj.EndDate.Time
	}
	// Walk events for a tighter bound when top-level dates are missing.
	for _, ev := range tj.TripEvents {
		for _, l := range ev.Legs {
			for _, seg := range l.Segments {
				if !seg.DepartureDate.IsZero() {
					if start.IsZero() || seg.DepartureDate.Before(start) {
						start = seg.DepartureDate.Time
					}
					if end.IsZero() || seg.ArrivalDate.After(end) {
						end = seg.ArrivalDate.Time
					}
				}
			}
		}
		if ev.VenueStartDate != nil && !ev.VenueStartDate.IsZero() {
			if start.IsZero() || ev.VenueStartDate.Before(start) {
				start = ev.VenueStartDate.Time
			}
		}
		if ev.VenueEndDate != nil && !ev.VenueEndDate.IsZero() {
			if end.IsZero() || ev.VenueEndDate.After(end) {
				end = ev.VenueEndDate.Time
			}
		}
		if ev.Pickup != nil && !ev.Pickup.IsZero() {
			if start.IsZero() || ev.Pickup.Before(start) {
				start = ev.Pickup.Time
			}
		}
		if ev.Dropoff != nil && !ev.Dropoff.IsZero() {
			if end.IsZero() || ev.Dropoff.After(end) {
				end = ev.Dropoff.Time
			}
		}
	}
	if end.IsZero() {
		end = start
	}
	return
}

// ── ICS building ─────────────────────────────────────────────────────────────

// buildICS assembles the VCALENDAR text for the selected trips.
func buildICS(trips []tripFile) string {
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//Kayak Software Corporation//NONSGML Kayak Trips//EN\r\n")
	sb.WriteString("METHOD:PUBLISH\r\n")
	sb.WriteString("X-WR-CALNAME:traveler@example.com's Trips on KAYAK\r\n")
	sb.WriteString("X-WR-CALDESC:KAYAK - Trips calendar feed\r\n")

	for _, tf := range trips {
		writeTrip(&sb, tf)
	}

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

// writeTrip emits all VEVENTs for one trip: booking events first, envelope last
// (mirrors Kayak's live feed ordering).
func writeTrip(sb *strings.Builder, tf tripFile) {
	tj := tf.trip
	token := tf.trip.TripID
	viewURL := "https://www.kayak.com/trips/!" + token + "?ref=calendar"

	for _, ev := range tj.TripEvents {
		switch ev.UIDescription {
		case "Flight":
			writeFlightEvents(sb, ev, token, viewURL)
		case "Train":
			writeTransportEvent(sb, ev, token, viewURL, "Train")
		case "Bus":
			writeTransportEvent(sb, ev, token, viewURL, "Bus")
		case "Ferry":
			writeTransportEvent(sb, ev, token, viewURL, "Bus") // mapped as ground
		case "Hotel":
			writeHotelEvents(sb, ev, token, viewURL)
		case "Car":
			writeCarEvents(sb, ev, token, viewURL)
		case "Restaurant":
			writeDiningEvent(sb, ev, token, viewURL)
		case "Other", "Directions":
			writeExcursionEvent(sb, ev, token, viewURL)
		}
	}

	// Date-only envelope — the trip's name + span, TRANSP:TRANSPARENT.
	writeEnvelope(sb, tj, token, viewURL)
}

// ── Event writers ─────────────────────────────────────────────────────────────

func writeFlightEvents(sb *strings.Builder, ev tripEvent, token, viewURL string) {
	for li, l := range ev.Legs {
		for si, seg := range l.Segments {
			uid := fmt.Sprintf("%s-flt-%d-%d-%d@kayak.com", token, ev.ID, li, si)
			conf := firstNonEmpty(ev.ConfirmationNum, ev.BookingDetail.BookingReferenceNumber)

			depIATA := iataFromRaw(seg.DepartureAddress.RawAddress)
			arrIATA := iataFromRaw(seg.ArrivalAddress.RawAddress)

			depCity := cityFromAddress(seg.DepartureAddress, depIATA)
			arrCity := cityFromAddress(seg.ArrivalAddress, arrIATA)

			// SUMMARY: "LH 6437 from Vienna (VIE) to Frankfurt am Main (FRA)"
			// carrier is always 2-3 chars, number is the segment number.
			carrier := strings.ToUpper(seg.Carrier)
			summary := fmt.Sprintf("%s %s from %s (%s) to %s (%s)",
				carrier, seg.Number, depCity, depIATA, arrCity, arrIATA)

			depUTC := toUTC(seg.DepartureDate.Time, seg.DepartureTZ)
			arrUTC := toUTC(seg.ArrivalDate.Time, seg.ArrivalTZ)

			depLocal := localDisplay(seg.DepartureDate.Time, seg.DepartureTZ)
			arrLocal := localDisplay(seg.ArrivalDate.Time, seg.ArrivalTZ)

			desc := fmt.Sprintf("%s Flight %s    \\nBooking reference: %s\n "+
				"\\nDeparting from %s (%s) - %s\\nArriving at %s (%s) - %s\\n\\nView trip: %s",
				carrier, seg.Number, conf,
				depCity, depIATA, depLocal,
				arrCity, arrIATA, arrLocal,
				viewURL)

			writeEvent(sb, uid, depUTC, arrUTC, false, summary, desc, "", "")
		}
	}
}

func writeTransportEvent(sb *strings.Builder, ev tripEvent, token, viewURL, kind string) {
	for li, l := range ev.Legs {
		for si, seg := range l.Segments {
			uid := fmt.Sprintf("%s-%s-%d-%d-%d@kayak.com", token, strings.ToLower(kind), ev.ID, li, si)
			conf := firstNonEmpty(ev.ConfirmationNum, ev.BookingDetail.BookingReferenceNumber)

			provider := seg.Carrier
			number := seg.Number

			// SUMMARY: "Bus Slovak Lines 102806" or "Train RJ 1048" or "Bus RegioJet"
			var summary string
			if number != "" {
				summary = strings.TrimSpace(fmt.Sprintf("%s %s %s", kind, provider, number))
			} else {
				summary = strings.TrimSpace(fmt.Sprintf("%s %s", kind, provider))
			}

			depLabel := seg.DepartureAddress.label()
			arrLabel := seg.ArrivalAddress.label()

			depUTC := toUTC(seg.DepartureDate.Time, seg.DepartureTZ)
			arrUTC := toUTC(seg.ArrivalDate.Time, seg.ArrivalTZ)

			depLocal := localDisplay(seg.DepartureDate.Time, seg.DepartureTZ)
			arrLocal := localDisplay(seg.ArrivalDate.Time, seg.ArrivalTZ)

			// Produce description with the marker the kayak mapper detects:
			//   " Train Number 1048" or " Bus Number 102806"
			// The number may be empty; still include "Number" so the marker matches.
			nounMap := map[string]string{"Train": "Train", "Bus": "Bus"}
			noun := nounMap[kind]
			if noun == "" {
				noun = kind
			}
			var confLine string
			if conf != "" {
				confLine = fmt.Sprintf("\\nConfirmation Number: %s\n    \\nBooking reference: %s\n ", conf, conf)
			}

			desc := fmt.Sprintf("%s %s Number %s %s"+
				"\\nDeparting from %s - %s\\nArriving at %s - %s\\n\\nView trip: %s",
				provider, noun, strings.TrimSpace(number), confLine,
				depLabel, depLocal,
				arrLabel, arrLocal,
				viewURL)

			writeEvent(sb, uid, depUTC, arrUTC, false, summary, desc, "", "")
		}
	}
}

func writeHotelEvents(sb *strings.Builder, ev tripEvent, token, viewURL string) {
	name := ev.Address.LocationName
	if name == "" {
		name = ev.PlaceDescription
	}
	if name == "" {
		name = "Hotel"
	}

	conf := firstNonEmpty(ev.ConfirmationNum, ev.BookingDetail.BookingReferenceNumber)
	addrStr := formatAddress(ev.Address)
	phone := ev.Address.PhoneNumber

	var checkIn, checkOut time.Time
	if ev.VenueStartDate != nil {
		checkIn = toUTC(ev.VenueStartDate.Time, ev.VenueStartTZ)
	}
	if ev.VenueEndDate != nil {
		checkOut = toUTC(ev.VenueEndDate.Time, ev.VenueEndTZ)
	}

	checkInLocal := localDisplay(checkIn, ev.VenueStartTZ)
	checkOutLocal := localDisplay(checkOut, ev.VenueEndTZ)

	// Check-in event (DTSTART = 30 min before real check-in, DTEND = check-in)
	inUID := fmt.Sprintf("%s-hotel-in-%d@kayak.com", token, ev.ID)
	inStart := checkIn.Add(-30 * time.Minute)
	inDesc := fmt.Sprintf("\\nCheck In Time: %s\n \\nCheck Out Time: %s\n    \\nBooking reference: %s\n "+
		"\\nAddress: %s\n \\nPhone Number: %s\n \\n\\nView trip: %s",
		checkInLocal, checkOutLocal, conf, addrStr, phone, viewURL)
	inSummary := "Check in to " + name
	geo := ""
	if ev.Address.Latitude != 0 || ev.Address.Longitude != 0 {
		geo = fmt.Sprintf("%f;%f", ev.Address.Latitude, ev.Address.Longitude)
	}
	writeEvent(sb, inUID, inStart, checkIn, false, inSummary, inDesc, addrStr, geo)

	// Check-out event (DTSTART = check-out, DTEND = 30 min later)
	outUID := fmt.Sprintf("%s-hotel-out-%d@kayak.com", token, ev.ID)
	outEnd := checkOut.Add(30 * time.Minute)
	outDesc := fmt.Sprintf("\\nCheck Out Time: %s\n    \\nBooking reference: %s\n "+
		"\\nAddress: %s\n \\nPhone Number: %s\n \\n\\nView trip: %s",
		checkOutLocal, conf, addrStr, phone, viewURL)
	outSummary := "Check out from " + name
	writeEvent(sb, outUID, checkOut, outEnd, false, outSummary, outDesc, addrStr, geo)
}

func writeCarEvents(sb *strings.Builder, ev tripEvent, token, viewURL string) {
	agency := ev.AgencyName
	if agency == "" {
		agency = "Car Rental"
	}
	conf := firstNonEmpty(ev.ConfirmationNum, ev.BookingDetail.BookingReferenceNumber)
	carType := ev.CarType

	var pickupTime, dropoffTime time.Time
	if ev.Pickup != nil {
		pickupTime = toUTC(ev.Pickup.Time, ev.PickupTZ)
	}
	if ev.Dropoff != nil {
		dropoffTime = toUTC(ev.Dropoff.Time, ev.DropoffTZ)
	}

	pickupAddr := formatAddress(ev.PickupAddress)
	dropoffAddr := formatAddress(ev.DropoffAddress)

	// Pickup event
	pickupUID := fmt.Sprintf("%s-car-pickup-%d@kayak.com", token, ev.ID)
	pickupEnd := pickupTime.Add(30 * time.Minute)
	pickupSummary := fmt.Sprintf("Car Pickup (Agency: %s)", agency)
	pickupDesc := fmt.Sprintf("Car Pickup at %s         \\nPickup Time: %s\n "+
		"\\nPickup Address: %s\n \\nDropoff Time: %s\n "+
		"\\nDropoff Address: %s\n \\nConfirmation Number: %s\n "+
		"\\nCar Type: %s\n \\n\\nView trip: %s",
		agency,
		localDisplay(pickupTime, ev.PickupTZ),
		pickupAddr,
		localDisplay(dropoffTime, ev.DropoffTZ),
		dropoffAddr,
		conf,
		carType,
		viewURL)
	writeEvent(sb, pickupUID, pickupTime, pickupEnd, false, pickupSummary, pickupDesc, "", "")

	// Dropoff event
	dropoffUID := fmt.Sprintf("%s-car-dropoff-%d@kayak.com", token, ev.ID)
	dropoffEnd := dropoffTime.Add(30 * time.Minute)
	dropoffSummary := fmt.Sprintf("Car Dropoff (Agency: %s)", agency)
	dropoffDesc := fmt.Sprintf("Car Dropoff at %s         \\nDropoff Time: %s\n "+
		"\\nDropoff Address: %s\n \\nConfirmation Number: %s\n "+
		"\\nCar Type: %s\n \\n\\nView trip: %s",
		agency,
		localDisplay(dropoffTime, ev.DropoffTZ),
		dropoffAddr,
		conf,
		carType,
		viewURL)
	writeEvent(sb, dropoffUID, dropoffTime, dropoffEnd, false, dropoffSummary, dropoffDesc, "", "")
}

func writeDiningEvent(sb *strings.Builder, ev tripEvent, token, viewURL string) {
	name := ev.PlaceDescription
	if name == "" {
		name = ev.Address.LocationName
	}
	if name == "" {
		name = "Restaurant"
	}
	conf := firstNonEmpty(ev.ConfirmationNum, ev.BookingDetail.BookingReferenceNumber)
	addrStr := formatAddress(ev.Address)

	var start, end time.Time
	if ev.VenueStartDate != nil {
		start = toUTC(ev.VenueStartDate.Time, ev.VenueStartTZ)
	}
	if ev.VenueEndDate != nil {
		end = toUTC(ev.VenueEndDate.Time, ev.VenueEndTZ)
	}
	if end.IsZero() {
		end = start.Add(time.Hour)
	}

	startLocal := localDisplay(start, ev.VenueStartTZ)
	endLocal := localDisplay(end, ev.VenueEndTZ)

	uid := fmt.Sprintf("%s-dining-%d@kayak.com", token, ev.ID)
	var confLine string
	if conf != "" {
		confLine = fmt.Sprintf("\\nBooking reference: %s\n ", conf)
	}
	desc := fmt.Sprintf("Restaurant Name: %s\n \\nGuests: \n \\nStart Time: %s\n "+
		"\\nEnd Time: %s\n \\nAddress: %s\n %s\\n\\nView trip: %s",
		name, startLocal, endLocal, addrStr, confLine, viewURL)

	writeEvent(sb, uid, start, end, false, name, desc, addrStr, "")
}

func writeExcursionEvent(sb *strings.Builder, ev tripEvent, token, viewURL string) {
	name := ev.PlaceDescription
	if name == "" {
		name = ev.Address.LocationName
	}
	if name == "" {
		name = "Event"
	}
	conf := firstNonEmpty(ev.ConfirmationNum, ev.BookingDetail.BookingReferenceNumber)
	addrStr := formatAddress(ev.Address)

	var start, end time.Time
	if ev.VenueStartDate != nil {
		start = toUTC(ev.VenueStartDate.Time, ev.VenueStartTZ)
	}
	if ev.VenueEndDate != nil {
		end = toUTC(ev.VenueEndDate.Time, ev.VenueEndTZ)
	}
	if end.IsZero() {
		end = start.Add(time.Hour)
	}

	startLocal := localDisplay(start, ev.VenueStartTZ)
	endLocal := localDisplay(end, ev.VenueEndTZ)

	uid := fmt.Sprintf("%s-excursion-%d@kayak.com", token, ev.ID)
	var confLine string
	if conf != "" {
		confLine = fmt.Sprintf("\\nBooking reference: %s\n ", conf)
	}
	desc := fmt.Sprintf("Event Name: %s\n \\nStart Time: %s\n "+
		"\\nEnd Time: %s\n \\nAddress: %s\n %s\\n\\nView trip: %s",
		name, startLocal, endLocal, addrStr, confLine, viewURL)

	writeEvent(sb, uid, start, end, false, name, desc, addrStr, "")
}

func writeEnvelope(sb *strings.Builder, tj tripJSON, token, viewURL string) {
	uid := fmt.Sprintf("%s-envelope@kayak.com", token)

	// Envelope spans the full trip in DATE-only form.
	// DTEND is exclusive in iCalendar: add one day past the last day.
	start := tj.StartDate
	end := tj.EndDate

	var startDay, endDay time.Time
	if start != nil && !start.IsZero() {
		startDay = start.Time.Truncate(24 * time.Hour)
	}
	if end != nil && !end.IsZero() {
		// endDate in the JSON is often the time of last event, not midnight.
		// Round up to the start of the next calendar day.
		endDay = end.Time.Truncate(24 * time.Hour).Add(24 * time.Hour)
	}
	if endDay.IsZero() {
		endDay = startDay.Add(24 * time.Hour)
	}

	startStr := startDay.UTC().Format("20060102")
	endStr := endDay.UTC().Format("20060102")

	desc := tj.Name + " - " + viewURL

	sb.WriteString("BEGIN:VEVENT\r\n")
	writeProperty(sb, "DTSTAMP", time.Now().UTC().Format("20060102T150405Z"))
	writeProperty(sb, "UID", uid)
	writeProperty(sb, "SEQUENCE", "1")
	sb.WriteString("DTSTART;VALUE=DATE:" + startStr + "\r\n")
	sb.WriteString("DTEND;VALUE=DATE:" + endStr + "\r\n")
	writeProperty(sb, "SUMMARY", tj.Name)
	writeProperty(sb, "DESCRIPTION", desc)
	writeProperty(sb, "TRANSP", "TRANSPARENT")
	sb.WriteString("END:VEVENT\r\n")
}

// writeEvent emits a timed VEVENT. start/end are UTC instants.
// geo is optional "lat;lon", loc is optional LOCATION text.
func writeEvent(sb *strings.Builder, uid string, start, end time.Time, dateOnly bool, summary, desc, loc, geo string) {
	sb.WriteString("BEGIN:VEVENT\r\n")
	writeProperty(sb, "DTSTAMP", time.Now().UTC().Format("20060102T150405Z"))
	writeProperty(sb, "UID", uid)
	writeProperty(sb, "SEQUENCE", "1")
	if dateOnly {
		writeProperty(sb, "DTSTART;VALUE=DATE", start.UTC().Format("20060102"))
		writeProperty(sb, "DTEND;VALUE=DATE", end.UTC().Format("20060102"))
	} else {
		writeProperty(sb, "DTSTART", start.UTC().Format("20060102T150405Z"))
		if !end.IsZero() {
			writeProperty(sb, "DTEND", end.UTC().Format("20060102T150405Z"))
		}
	}
	writeProperty(sb, "SUMMARY", summary)
	writeProperty(sb, "DESCRIPTION", foldDescription(desc))
	if loc != "" {
		writeProperty(sb, "LOCATION", loc)
	}
	if geo != "" {
		writeProperty(sb, "GEO", geo)
	}
	sb.WriteString("END:VEVENT\r\n")
}

// writeProperty writes "NAME:value\r\n", folding lines longer than 75 octets.
func writeProperty(sb *strings.Builder, name, value string) {
	line := name + ":" + value
	sb.WriteString(foldLine(line))
	sb.WriteString("\r\n")
}

// foldLine implements RFC 5545 line folding at 75 octets.
func foldLine(line string) string {
	const maxLen = 75
	if len(line) <= maxLen {
		return line
	}
	var sb strings.Builder
	runes := []rune(line)
	pos := 0
	first := true
	for pos < len(runes) {
		prefix := ""
		if !first {
			prefix = " "
		}
		// Compute how many runes fit in maxLen bytes including prefix.
		taken := 0
		byteCount := len(prefix)
		for pos+taken < len(runes) {
			r := runes[pos+taken]
			rc := len(string(r))
			if byteCount+rc > maxLen {
				break
			}
			byteCount += rc
			taken++
		}
		if taken == 0 {
			taken = 1 // always advance
		}
		sb.WriteString(prefix)
		sb.WriteString(string(runes[pos : pos+taken]))
		sb.WriteString("\r\n")
		pos += taken
		first = false
	}
	// Remove trailing \r\n — caller adds it.
	result := sb.String()
	return strings.TrimSuffix(result, "\r\n")
}

// foldDescription is a no-op: our description builder already uses the iCalendar
// escaped newline literal "\\n" (two characters: backslash + n). Real Go newlines
// in a description value would be a bug, but we replace them just in case.
func foldDescription(s string) string {
	// Replace any accidental real newlines with the iCalendar escape sequence.
	return strings.ReplaceAll(s, "\n", "\\n")
}

// ── Timezone / time helpers ──────────────────────────────────────────────────

// toUTC converts a "wall clock" time (as stored in the Kayak JSON, which has no
// timezone offset embedded) into a UTC instant using the given IANA timezone ID.
// If the timezone is empty or unresolvable, the time is assumed to already be UTC.
func toUTC(t time.Time, tzID string) time.Time {
	if t.IsZero() {
		return t
	}
	if tzID == "" {
		return t.UTC()
	}
	loc, err := time.LoadLocation(tzID)
	if err != nil {
		slog.Warn("unknown timezone, treating as UTC", "tz", tzID)
		return t.UTC()
	}
	// t is currently in UTC clock representation but semantically local — attach
	// the correct location then convert.
	local := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
	return local.UTC()
}

// localDisplay formats a UTC-converted time back into a human-readable local
// form (matching Kayak's DESCRIPTION style), e.g. "04/01/2026 10:30PM CEST".
func localDisplay(t time.Time, tzID string) string {
	if t.IsZero() {
		return ""
	}
	loc := time.UTC
	abbr := "UTC"
	if tzID != "" {
		if l, err := time.LoadLocation(tzID); err == nil {
			loc = l
			lt := t.In(loc)
			_, offset := lt.Zone()
			abbr = lt.Format("MST")
			_ = offset
		}
	}
	lt := t.In(loc)
	return lt.Format("01/02/2006 3:04PM") + " " + abbr
}

// ── Formatting helpers ───────────────────────────────────────────────────────

func cityFromAddress(a address, iata string) string {
	// For airport addresses that only carry "VIE (VIE)" raw, synthesise a name.
	if iata != "" && a.City == "" {
		return iata
	}
	if a.City != "" {
		return a.City
	}
	return a.label()
}

func formatAddress(a address) string {
	parts := []string{}
	if a.Address1 != "" {
		parts = append(parts, a.Address1)
	}
	if a.City != "" {
		parts = append(parts, a.City)
	}
	if a.Country != "" {
		parts = append(parts, a.Country)
	}
	if len(parts) == 0 {
		return a.RawAddress
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
