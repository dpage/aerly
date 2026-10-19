package handlers

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

// Hand-rolled RFC 5545 (iCalendar) renderer. No external deps, as the format is
// small and well specified. We emit one VCALENDAR with one VEVENT per
// plan_part. Each time is written by its zone source: a zoned time (confirmed
// or inferred) as local time against a VTIMEZONE for its IANA zone, a floating
// time in the floating form (its digits, no Z and no TZID) so it imports back
// as floating, and an unknown one as UTC.
//
// VTIMEZONE strategy: for each referenced IANA zone we scan Go's bundled
// tzdata for offset changes in a window around the zone's instants (a year
// either side) and emit one STANDARD or DAYLIGHT observance per change, with
// no RRULE. A zone with no change in the window gets a single STANDARD
// observance starting at the window start.

const icsProdID = "-//Aerly//Trip Planner//EN"

// renderICS produces the full VCALENDAR text for the given events. calName is
// the X-WR-CALNAME shown by many clients. When tripBands is true the feed also
// emits one all-day VEVENT per trip spanning the trip's dates (issue #101) — the
// "name of the trip" banner. The single-plan feed passes false: a one-plan
// subscription shouldn't sprout a trip-wide banner derived from just that plan.
func renderICS(calName string, events []*store.CalendarEvent, tripBands bool) string {
	var b strings.Builder
	writeLine(&b, "BEGIN:VCALENDAR")
	writeLine(&b, "VERSION:2.0")
	writeLine(&b, "PRODID:"+icsProdID)
	writeLine(&b, "CALSCALE:GREGORIAN")
	writeLine(&b, "METHOD:PUBLISH")
	if calName != "" {
		writeLine(&b, "X-WR-CALNAME:"+escapeText(calName))
	}

	// Collect, per IANA zone, the set of instants that need a defined offset.
	type tzUse struct {
		loc      *time.Location
		instants []time.Time
	}
	zones := map[string]*tzUse{}
	noteZone := func(tzName string, t time.Time) {
		if tzName == "" {
			return
		}
		loc, err := time.LoadLocation(tzName)
		if err != nil {
			return
		}
		z := zones[tzName]
		if z == nil {
			z = &tzUse{loc: loc}
			zones[tzName] = z
		}
		z.instants = append(z.instants, t)
	}
	for _, e := range events {
		if e.StartTZ != "" {
			noteZone(e.StartTZ, e.StartsAt)
		}
		if e.EndsAt != nil && e.EndTZ != "" {
			noteZone(e.EndTZ, *e.EndsAt)
		}
	}

	// Emit a VTIMEZONE per referenced zone, ordered for stable output.
	tzNames := make([]string, 0, len(zones))
	for name := range zones {
		tzNames = append(tzNames, name)
	}
	sort.Strings(tzNames)
	for _, name := range tzNames {
		writeVTimezone(&b, name, zones[name].loc, zones[name].instants)
	}

	if tripBands {
		for _, te := range tripBandEvents(events) {
			writeTripBand(&b, te)
		}
	}

	for _, e := range events {
		// A banded booking (a multi-night stay, a multi-day hire) renders as two
		// point events, one at each end, exactly like the UI timeline, rather than
		// one banner spanning every night (issue #101). See banding.go.
		if isBandedEvent(e) {
			writeBandFirst(&b, e)
			writeBandLast(&b, e)
			continue
		}
		writeVEvent(&b, e)
	}
	writeLine(&b, "END:VCALENDAR")
	return b.String()
}

// tzTransition is one observed UTC-offset change at instant At: the offset and
// abbreviation in effect just before and just after, and whether the new offset
// is daylight (i.e. larger than the zone's standard offset).
type tzTransition struct {
	At         time.Time
	OffsetFrom int
	OffsetTo   int
	Abbr       string
	IsDST      bool
}

// tzWindow returns the [min,max] instant range a zone is referenced over,
// padded by a year on each side so the surrounding DST transitions are
// captured. When a zone has no instants (shouldn't happen) it falls back to a
// window around now.
func tzWindow(instants []time.Time) (time.Time, time.Time) {
	if len(instants) == 0 {
		now := time.Now().UTC()
		return now.AddDate(-1, 0, 0), now.AddDate(1, 0, 0)
	}
	min, max := instants[0], instants[0]
	for _, t := range instants[1:] {
		if t.Before(min) {
			min = t
		}
		if t.After(max) {
			max = t
		}
	}
	return min.AddDate(-1, 0, 0).UTC(), max.AddDate(1, 0, 0).UTC()
}

// findTransitions scans loc's UTC offset across [start,end] and returns each
// offset change, refined to the (UTC) second it occurs. It steps day-by-day to
// spot a change, then binary-searches the day to pin the exact transition. The
// zone's standard (smallest) offset over the window is used to classify each
// target offset as daylight or standard.
func findTransitions(loc *time.Location, start, end time.Time) []tzTransition {
	offsetAt := func(t time.Time) (int, string) {
		abbr, off := t.In(loc).Zone()
		return off, abbr
	}

	// First pass: determine the standard (minimum) offset over the window so we
	// can label DST observances.
	stdOffset := 1 << 30
	for t := start; !t.After(end); t = t.AddDate(0, 0, 1) {
		off, _ := offsetAt(t)
		if off < stdOffset {
			stdOffset = off
		}
	}

	var out []tzTransition
	prevOff, _ := offsetAt(start)
	const day = 24 * time.Hour
	for t := start.Add(day); !t.After(end); t = t.Add(day) {
		off, _ := offsetAt(t)
		if off == prevOff {
			continue
		}
		// A transition lies in (t-day, t]; binary-search to the second.
		lo, hi := t.Add(-day), t
		for hi.Sub(lo) > time.Second {
			mid := lo.Add(hi.Sub(lo) / 2)
			if o, _ := offsetAt(mid); o == prevOff {
				lo = mid
			} else {
				hi = mid
			}
		}
		newOff, newAbbr := offsetAt(hi)
		out = append(out, tzTransition{
			At:         hi,
			OffsetFrom: prevOff,
			OffsetTo:   newOff,
			Abbr:       newAbbr,
			IsDST:      newOff > stdOffset,
		})
		prevOff = newOff
	}
	return out
}

// tzName is the TZNAME for an observance of tz beginning at instant at: Go's
// abbreviation, unless tzdata has none and gives a bare offset ("+05"), in
// which case the offset label the app shows ("UTC+5").
func tzName(abbr string, at time.Time, tz string) string {
	if abbr == "" || abbr[0] == '+' || abbr[0] == '-' {
		return timing.OffsetLabel(at, tz)
	}
	return abbr
}

func writeVTimezone(b *strings.Builder, tz string, loc *time.Location, instants []time.Time) {
	start, end := tzWindow(instants)
	transitions := findTransitions(loc, start, end)

	writeLine(b, "BEGIN:VTIMEZONE")
	writeLine(b, "TZID:"+tz)

	if len(transitions) == 0 {
		// No change in the window: one STANDARD observance with the fixed
		// offset from the window start (RFC 5545 requires at least one).
		abbr, off := start.In(loc).Zone()
		writeObservance(b, "STANDARD", start.In(loc), off, off, tzName(abbr, start, tz))
		writeLine(b, "END:VTIMEZONE")
		return
	}

	// One observance per change. Its DTSTART is the local time just before
	// the change, read in the offset being left (RFC 5545 section 3.6.5).
	for _, tr := range transitions {
		comp := "STANDARD"
		if tr.IsDST {
			comp = "DAYLIGHT"
		}
		before := tr.At.In(time.FixedZone("", tr.OffsetFrom))
		writeObservance(b, comp, before, tr.OffsetFrom, tr.OffsetTo, tzName(tr.Abbr, tr.At, tz))
	}
	writeLine(b, "END:VTIMEZONE")
}

// writeObservance writes one STANDARD or DAYLIGHT sub-block.
func writeObservance(b *strings.Builder, comp string, local time.Time, from, to int, name string) {
	writeLine(b, "BEGIN:"+comp)
	writeLine(b, "DTSTART:"+local.Format("20060102T150405"))
	writeLine(b, "TZOFFSETFROM:"+formatTZOffset(from))
	writeLine(b, "TZOFFSETTO:"+formatTZOffset(to))
	writeLine(b, "TZNAME:"+name)
	writeLine(b, "END:"+comp)
}

func writeVEvent(b *strings.Builder, e *store.CalendarEvent) {
	writeLine(b, "BEGIN:VEVENT")
	writeLine(b, fmt.Sprintf("UID:plan-part-%d@aerly", e.PartID))
	writeStamp(b, e)

	writeLine(b, dtLine("DTSTART", e.StartsAt, e.StartTZ, e.StartTZSource))
	if e.EndsAt != nil {
		writeLine(b, dtLine("DTEND", *e.EndsAt, e.EndTZ, e.EndTZSource))
	}

	writeLine(b, "SUMMARY:"+escapeText(summaryFor(e)))
	if e.StartLabel != "" {
		writeLine(b, "LOCATION:"+escapeText(e.StartLabel))
	}
	if desc := descriptionFor(e); desc != "" {
		writeLine(b, "DESCRIPTION:"+escapeText(desc))
	}
	writeStatus(b, e)
	writeLine(b, "END:VEVENT")
}

// writeBandFirst / writeBandLast split a banded booking into its two point
// events. Each is a single-instant VEVENT (DTSTART only) at the booked opening
// or closing time, matching the UI's two timeline tiles instead of one
// all-night banner (issue #101). The label and the UID suffix both come from
// banding.go, so a hotel keeps its historic checkin/checkout UIDs whilst a
// vehicle hire gets pickup/return.
func writeBandFirst(b *strings.Builder, e *store.CalendarEvent) {
	writeLine(b, "BEGIN:VEVENT")
	writeLine(b, fmt.Sprintf("UID:plan-part-%d-%s@aerly", e.PartID, bandUIDSuffix(e.Type, false)))
	writeStamp(b, e)
	writeLine(b, dtLine("DTSTART", e.StartsAt, e.StartTZ, e.StartTZSource))
	writeLine(b, "SUMMARY:"+escapeText(bandEdgeSummary(e, bandEdgeLabel(e.Type, false))))
	if e.StartLabel != "" {
		writeLine(b, "LOCATION:"+escapeText(e.StartLabel))
	}
	if desc := describe(e, startBlock(e), nil); desc != "" {
		writeLine(b, "DESCRIPTION:"+escapeText(desc))
	}
	writeStatus(b, e)
	writeLine(b, "END:VEVENT")
}

func writeBandLast(b *strings.Builder, e *store.CalendarEvent) {
	loc := e.EndLabel
	if loc == "" {
		loc = e.StartLabel
	}
	writeLine(b, "BEGIN:VEVENT")
	writeLine(b, fmt.Sprintf("UID:plan-part-%d-%s@aerly", e.PartID, bandUIDSuffix(e.Type, true)))
	writeStamp(b, e)
	writeLine(b, dtLine("DTSTART", *e.EndsAt, e.EndTZ, e.EndTZSource))
	writeLine(b, "SUMMARY:"+escapeText(bandEdgeSummary(e, bandEdgeLabel(e.Type, true))))
	if loc != "" {
		writeLine(b, "LOCATION:"+escapeText(loc))
	}
	if desc := describe(e, nil, endBlock(e)); desc != "" {
		writeLine(b, "DESCRIPTION:"+escapeText(desc))
	}
	writeStatus(b, e)
	writeLine(b, "END:VEVENT")
}

// writeTripBand emits one all-day, multi-day VEVENT spanning the whole trip —
// the "name of the trip" banner (issue #101).
func writeTripBand(b *strings.Builder, tb tripBand) {
	name := strings.TrimSpace(tb.name)
	if name == "" {
		name = "Trip"
	}
	stamp := tb.updatedAt
	if stamp.IsZero() {
		stamp = time.Now()
	}
	writeLine(b, "BEGIN:VEVENT")
	writeLine(b, fmt.Sprintf("UID:trip-%d@aerly", tb.id))
	writeLine(b, "DTSTAMP:"+stamp.UTC().Format("20060102T150405Z"))
	writeLine(b, "LAST-MODIFIED:"+stamp.UTC().Format("20060102T150405Z"))
	// All-day DATE values: DTEND is exclusive (RFC 5545 §3.8.2.2), so it's the
	// day after the trip's last day. TRANSP:TRANSPARENT keeps the banner from
	// marking every day busy.
	writeLine(b, "DTSTART;VALUE=DATE:"+tb.start.Format("20060102"))
	writeLine(b, "DTEND;VALUE=DATE:"+tb.end.AddDate(0, 0, 1).Format("20060102"))
	writeLine(b, "SUMMARY:"+escapeText(name))
	writeLine(b, "TRANSP:TRANSPARENT")
	writeLine(b, "END:VEVENT")
}

// writeStamp emits DTSTAMP/LAST-MODIFIED so clients detect updates (a delayed
// flight whose part times moved re-renders on next refresh).
func writeStamp(b *strings.Builder, e *store.CalendarEvent) {
	writeLine(b, "DTSTAMP:"+e.UpdatedAt.UTC().Format("20060102T150405Z"))
	writeLine(b, "LAST-MODIFIED:"+e.UpdatedAt.UTC().Format("20060102T150405Z"))
}

// writeStatus maps a part's status to the RFC 5545 STATUS property.
func writeStatus(b *strings.Builder, e *store.CalendarEvent) {
	switch e.Status {
	case "cancelled":
		writeLine(b, "STATUS:CANCELLED")
	case "confirmed":
		writeLine(b, "STATUS:CONFIRMED")
	default:
		writeLine(b, "STATUS:TENTATIVE")
	}
}

// bandEdgeSummary labels one edge of a banded booking, e.g. "Hilton Zürich
// (Check-in)" or "Hertz (Pickup)", falling back to the bare edge when untitled.
func bandEdgeSummary(e *store.CalendarEvent, edge string) string {
	title := strings.TrimSpace(e.Title)
	if title == "" {
		return edge
	}
	return fmt.Sprintf("%s (%s)", title, edge)
}

// isBandedEvent reports whether an event should split into two point events
// rather than render as one spanning banner: a booking of a banding type (see
// banding.go) whose end falls on a later local calendar day than its start.
// Mirrors the frontend rule in web/src/lib/trip-format.ts so the feed and the
// UI timeline agree. Each end is read in its own zone; one with none is read
// in UTC, which is its instant when unknown and its digits when floating, as
// the printed itinerary does.
func isBandedEvent(e *store.CalendarEvent) bool {
	if _, ok := bandLabelsFor(e.Type); !ok || e.EndsAt == nil {
		return false
	}
	return bandsToLaterDay(e.StartsAt, e.StartTZ, *e.EndsAt, e.EndTZ)
}

// tripBand is one all-day trip banner: its id, name, inclusive local date span,
// and the newest part timestamp (for DTSTAMP).
type tripBand struct {
	id        int64
	name      string
	start     time.Time // inclusive first day, midnight UTC
	end       time.Time // inclusive last day, midnight UTC
	updatedAt time.Time
}

// tripBandEvents derives one banner per trip from the feed's events. It uses the
// trip's stored DATE span when set, otherwise the min/max local date across the
// trip's parts. Trips come back in ascending id order for stable output.
func tripBandEvents(events []*store.CalendarEvent) []tripBand {
	type agg struct {
		name                   string
		updatedAt              time.Time
		storedStart, storedEnd time.Time
		haveStored             bool
		partStart, partEnd     time.Time
		havePart               bool
	}
	byTrip := map[int64]*agg{}
	var order []int64
	for _, e := range events {
		if e.TripID == 0 {
			continue
		}
		a := byTrip[e.TripID]
		if a == nil {
			a = &agg{name: e.TripName}
			byTrip[e.TripID] = a
			order = append(order, e.TripID)
		}
		if e.UpdatedAt.After(a.updatedAt) {
			a.updatedAt = e.UpdatedAt
		}
		if !a.haveStored && e.TripStartsOn != nil && e.TripEndsOn != nil {
			a.storedStart, a.storedEnd = dateOnly(*e.TripStartsOn), dateOnly(*e.TripEndsOn)
			a.haveStored = true
		}
		start := localDate(e.StartsAt, e.StartTZ)
		end := start
		if e.EndsAt != nil {
			end = localDate(*e.EndsAt, e.EndTZ)
		}
		if !a.havePart {
			a.partStart, a.partEnd, a.havePart = start, end, true
		} else {
			if start.Before(a.partStart) {
				a.partStart = start
			}
			if end.After(a.partEnd) {
				a.partEnd = end
			}
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]tripBand, 0, len(order))
	for _, id := range order {
		a := byTrip[id]
		tb := tripBand{id: id, name: a.name, updatedAt: a.updatedAt}
		switch {
		case a.haveStored:
			tb.start, tb.end = a.storedStart, a.storedEnd
		case a.havePart:
			tb.start, tb.end = a.partStart, a.partEnd
		default:
			continue // no dates to anchor a banner
		}
		// A stored span can end before it starts only through bad data; clamp so
		// DTEND never precedes DTSTART.
		if tb.end.Before(tb.start) {
			tb.end = tb.start
		}
		out = append(out, tb)
	}
	return out
}

// localDate returns the calendar date (midnight UTC of the y/m/d) that instant t
// falls on in the named IANA zone, or in UTC when the zone is empty/unknown.
func localDate(t time.Time, tzName string) time.Time {
	loc := time.UTC
	if tzName != "" {
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.UTC)
}

// dateOnly strips a stored DATE value to midnight UTC of its y/m/d.
func dateOnly(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// dtLine formats a DTSTART/DTEND property by the time's zone source. A zoned
// time (confirmed or inferred, the only sources a zone is stored with) is
// local time with a TZID referencing the matching VTIMEZONE. A floating time
// is its UTC digits with no Z and no TZID, the RFC 5545 floating form, so it
// imports back as floating. Anything else, an unknown source or a zone that
// does not load, is UTC with a Z, which keeps the instant unambiguous.
func dtLine(prop string, t time.Time, tz, source string) string {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return fmt.Sprintf("%s;TZID=%s:%s", prop, tz, t.In(loc).Format("20060102T150405"))
		}
	}
	if timing.Source(source) == timing.Floating {
		return prop + ":" + t.UTC().Format("20060102T150405")
	}
	return prop + ":" + t.UTC().Format("20060102T150405Z")
}

func summaryFor(e *store.CalendarEvent) string {
	title := strings.TrimSpace(e.Title)
	typ := titleCaseType(e.Type)
	if title == "" {
		return typ
	}
	return fmt.Sprintf("%s (%s)", title, typ)
}

// descriptionFor is the DESCRIPTION of an event covering the whole part: its
// start and, when it has one, its end.
func descriptionFor(e *store.CalendarEvent) string {
	return describe(e, startBlock(e), endBlock(e))
}

// startBlock and endBlock are a part's two ends as the time labels need them.
// endBlock is nil when the part has no end. The end never borrows the start's
// zone: a blank end zone is labelled by its own source.
func startBlock(e *store.CalendarEvent) *timing.Block {
	return icsBlock(e.StartsAt, e.StartTZ, e.StartTZSource)
}

func endBlock(e *store.CalendarEvent) *timing.Block {
	if e.EndsAt == nil {
		return nil
	}
	return icsBlock(*e.EndsAt, e.EndTZ, e.EndTZSource)
}

func icsBlock(at time.Time, tz, source string) *timing.Block {
	return &timing.Block{
		At: at, TZ: tz, Source: timing.Source(source),
		Place: timing.PlaceName(tz, ""), Kind: timing.Scheduled,
	}
}

// describe builds a DESCRIPTION from the ends given (either may be nil, as a
// band edge event carries only its own): "Starts:" and "Ends:" lines in the
// app's full form, "Time zone unknown" when a given end has no zone, then the
// confirmation and the notes. A feed has no viewer, so an unknown time is
// shown in UTC.
func describe(e *store.CalendarEvent, start, end *timing.Block) string {
	var parts []string
	zoneless := false
	for _, x := range []struct {
		label string
		b     *timing.Block
	}{{"Starts: ", start}, {"Ends: ", end}} {
		if x.b == nil {
			continue
		}
		parts = append(parts, x.label+timing.Full(*x.b, ""))
		if x.b.TZ == "" {
			zoneless = true
		}
	}
	if zoneless {
		parts = append(parts, "Time zone unknown")
	}
	if ref := strings.TrimSpace(e.ConfirmationRef); ref != "" {
		parts = append(parts, "Confirmation: "+ref)
	}
	if notes := strings.TrimSpace(e.Notes); notes != "" {
		parts = append(parts, notes)
	}
	return strings.Join(parts, "\n")
}

func titleCaseType(t string) string {
	switch t {
	case "":
		return "Plan"
	case "vehicle_hire":
		return "Car hire"
	case "hotel":
		return "Accommodation"
	default:
		return strings.ToUpper(t[:1]) + t[1:]
	}
}

// formatTZOffset renders seconds-east-of-UTC as the RFC 5545 ±HHMM(SS) form.
func formatTZOffset(secs int) string {
	sign := "+"
	if secs < 0 {
		sign = "-"
		secs = -secs
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60
	if s != 0 {
		return fmt.Sprintf("%s%02d%02d%02d", sign, h, m, s)
	}
	return fmt.Sprintf("%s%02d%02d", sign, h, m)
}

// escapeText escapes a value per RFC 5545 §3.3.11 (TEXT): backslash, semicolon,
// comma, and newline.
func escapeText(s string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		";", "\\;",
		",", "\\,",
		"\r\n", "\\n",
		"\n", "\\n",
		"\r", "\\n",
	)
	return r.Replace(s)
}

// writeLine writes one content line, folding it at 75 octets per RFC 5545
// §3.1, and terminating with CRLF. Folding is byte-based with a leading space
// on continuation lines; we avoid splitting a multi-byte UTF-8 rune.
func writeLine(b *strings.Builder, line string) {
	const max = 75
	if len(line) <= max {
		b.WriteString(line)
		b.WriteString("\r\n")
		return
	}
	// First chunk up to 75 octets, subsequent chunks up to 74 (the leading
	// space counts toward the octet budget).
	i := 0
	limit := max
	for i < len(line) {
		end := i + limit
		if end > len(line) {
			end = len(line)
		} else {
			// Back off so we don't split a UTF-8 continuation byte.
			for end > i && (line[end]&0xC0) == 0x80 {
				end--
			}
		}
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(line[i:end])
		b.WriteString("\r\n")
		i = end
		limit = max - 1
	}
}
