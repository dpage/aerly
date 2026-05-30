package handlers

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// Hand-rolled RFC 5545 (iCalendar) renderer. No external deps — the format is
// small and well specified. We emit one VCALENDAR with one VEVENT per
// plan_part, each anchored to a VTIMEZONE for the part's IANA zone so clients
// show the correct local wall-clock time.
//
// VTIMEZONE strategy: for each referenced IANA zone we emit proper
// STANDARD/DAYLIGHT observances carrying RRULE transitions, derived directly
// from Go's bundled tzdata. We scan the offset transitions in a window around
// the feed's events (a couple of years either side is plenty for a calendar
// feed) and group them by the (offsetFrom, offsetTo, abbreviation) they switch
// to. Each group becomes one observance whose DTSTART is the first transition's
// local wall-clock time and whose RRULE recurs yearly on the matching
// month/weekday so clients render correct local times across DST boundaries.
// Zones with no DST in the window collapse to a single STANDARD observance with
// no RRULE. This stays dependency-free — Go's time package already carries the
// tz rules we need.

const icsProdID = "-//Aerly//Trip Planner//EN"

// renderICS produces the full VCALENDAR text for the given events. calName is
// the X-WR-CALNAME shown by many clients.
func renderICS(calName string, events []*store.CalendarEvent) string {
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

	for _, e := range events {
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

// rruleFor renders a yearly RRULE describing a transition that recurs on the
// same weekday-of-month as the given local transition time (e.g. the last
// Sunday of October). This matches how civil DST rules are defined, so a single
// observance covers years of transitions.
func rruleFor(local time.Time) string {
	weekday := int(local.Weekday()) // 0=Sun..6=Sat
	days := []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}
	// Which occurrence of this weekday within the month: 1..5, or -1 for "last"
	// when the date falls in the final week.
	nth := (local.Day()-1)/7 + 1
	lastOfMonth := time.Date(local.Year(), local.Month()+1, 0, 0, 0, 0, 0, local.Location()).Day()
	setpos := strconv.Itoa(nth)
	if local.Day()+7 > lastOfMonth {
		setpos = "-1"
	}
	return fmt.Sprintf("RRULE:FREQ=YEARLY;BYMONTH=%d;BYDAY=%s%s",
		int(local.Month()), setpos, days[weekday])
}

func writeVTimezone(b *strings.Builder, tzName string, loc *time.Location, instants []time.Time) {
	start, end := tzWindow(instants)
	transitions := findTransitions(loc, start, end)

	writeLine(b, "BEGIN:VTIMEZONE")
	writeLine(b, "TZID:"+tzName)

	if len(transitions) == 0 {
		// No DST in the window: a single STANDARD observance with the fixed
		// offset at the window start (RFC 5545 requires at least one observance).
		at := start
		if len(instants) > 0 {
			at = instants[0].UTC()
		}
		abbr, off := at.In(loc).Zone()
		if abbr == "" {
			abbr = tzName
		}
		writeLine(b, "BEGIN:STANDARD")
		writeLine(b, "DTSTART:"+at.In(loc).Format("20060102T150405"))
		writeLine(b, "TZOFFSETFROM:"+formatTZOffset(off))
		writeLine(b, "TZOFFSETTO:"+formatTZOffset(off))
		writeLine(b, "TZNAME:"+abbr)
		writeLine(b, "END:STANDARD")
		writeLine(b, "END:VTIMEZONE")
		return
	}

	// Group transitions by their target (offsetTo, abbr, isDST): each group
	// becomes one observance, anchored at its earliest transition with a yearly
	// RRULE. Recurring civil rules produce two groups (into-DST, out-of-DST).
	type group struct {
		first      tzTransition
		offsetFrom int
	}
	groups := map[string]*group{}
	var order []string
	for _, tr := range transitions {
		key := fmt.Sprintf("%d|%s|%t", tr.OffsetTo, tr.Abbr, tr.IsDST)
		g := groups[key]
		if g == nil {
			groups[key] = &group{first: tr, offsetFrom: tr.OffsetFrom}
			order = append(order, key)
		} else if tr.At.Before(g.first.At) {
			g.first = tr
			g.offsetFrom = tr.OffsetFrom
		}
	}
	sort.Strings(order)

	for _, key := range order {
		g := groups[key]
		local := g.first.At.In(loc)
		comp := "STANDARD"
		if g.first.IsDST {
			comp = "DAYLIGHT"
		}
		abbr := g.first.Abbr
		if abbr == "" {
			abbr = tzName
		}
		writeLine(b, "BEGIN:"+comp)
		writeLine(b, "DTSTART:"+local.Format("20060102T150405"))
		writeLine(b, rruleFor(local))
		writeLine(b, "TZOFFSETFROM:"+formatTZOffset(g.offsetFrom))
		writeLine(b, "TZOFFSETTO:"+formatTZOffset(g.first.OffsetTo))
		writeLine(b, "TZNAME:"+abbr)
		writeLine(b, "END:"+comp)
	}
	writeLine(b, "END:VTIMEZONE")
}

func writeVEvent(b *strings.Builder, e *store.CalendarEvent) {
	writeLine(b, "BEGIN:VEVENT")
	writeLine(b, fmt.Sprintf("UID:plan-part-%d@aerly", e.PartID))
	// DTSTAMP/LAST-MODIFIED let clients detect updates (a delayed flight whose
	// part times moved re-renders on next refresh).
	writeLine(b, "DTSTAMP:"+e.UpdatedAt.UTC().Format("20060102T150405Z"))
	writeLine(b, "LAST-MODIFIED:"+e.UpdatedAt.UTC().Format("20060102T150405Z"))

	writeLine(b, dtLine("DTSTART", e.StartsAt, e.StartTZ))
	if e.EndsAt != nil {
		endTZ := e.EndTZ
		if endTZ == "" {
			endTZ = e.StartTZ
		}
		writeLine(b, dtLine("DTEND", *e.EndsAt, endTZ))
	}

	writeLine(b, "SUMMARY:"+escapeText(summaryFor(e)))
	if e.StartLabel != "" {
		writeLine(b, "LOCATION:"+escapeText(e.StartLabel))
	}
	if desc := descriptionFor(e); desc != "" {
		writeLine(b, "DESCRIPTION:"+escapeText(desc))
	}
	if e.Status == "cancelled" {
		writeLine(b, "STATUS:CANCELLED")
	} else if e.Status == "confirmed" {
		writeLine(b, "STATUS:CONFIRMED")
	} else {
		writeLine(b, "STATUS:TENTATIVE")
	}
	writeLine(b, "END:VEVENT")
}

// dtLine formats a DTSTART/DTEND property. When the zone is known we emit a
// floating local time with a TZID parameter referencing the matching
// VTIMEZONE; otherwise we fall back to UTC ("Z") so the instant is still
// unambiguous.
func dtLine(prop string, t time.Time, tzName string) string {
	if tzName != "" {
		if loc, err := time.LoadLocation(tzName); err == nil {
			return fmt.Sprintf("%s;TZID=%s:%s", prop, tzName, t.In(loc).Format("20060102T150405"))
		}
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

func descriptionFor(e *store.CalendarEvent) string {
	var parts []string
	if ref := strings.TrimSpace(e.ConfirmationRef); ref != "" {
		parts = append(parts, "Confirmation: "+ref)
	}
	if notes := strings.TrimSpace(e.Notes); notes != "" {
		parts = append(parts, notes)
	}
	return strings.Join(parts, "\n")
}

func titleCaseType(t string) string {
	if t == "" {
		return "Plan"
	}
	return strings.ToUpper(t[:1]) + t[1:]
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
