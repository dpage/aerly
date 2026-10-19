// Package importics parses iCalendar (RFC 5545) trip exports into Aerly plans.
//
// Travel services publish trips as .ics downloads or calendar feeds: TripIt's
// per-trip "Export trip to calendar" and Kayak's per-account "Trips calendar
// feed" are the two supported today. This package owns the format-independent
// half of that import — turning a .ics file into structured events — plus a
// per-source mapper that turns those events into plans (flight ident/route/
// times, hotel check-in/out, …). The mapping is source-specific and written
// against real exported samples, because how a producer encodes a booking
// inside SUMMARY/DESCRIPTION/LOCATION is not part of the iCalendar standard.
package importics

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// Calendar is the parsed contents of one .ics file.
type Calendar struct {
	// ProdID is the PRODID header value. Note TripIt's exports are produced by
	// "Bennu", so PRODID does NOT identify TripIt — the @tripit.com UIDs and the
	// X-WR-CAL* headers do.
	ProdID string
	// Name / Desc are the X-WR-CALNAME / X-WR-CALDESC headers. TripIt puts the
	// trip name here, e.g. Name="Test Traveller (TripIt - Test Conference 2016)" and
	// Desc="Test Conference 2016 (Trip Shared by Test Traveller)".
	Name string
	Desc string
	// Timezone is the X-WR-TIMEZONE header: the calendar's default IANA zone
	// (e.g. "America/Vancouver"). Conference exports commonly emit event times
	// in UTC and rely on this to render them in the event's local zone.
	Timezone string
	Events   []Event
	// Zones holds the calendar's VTIMEZONE definitions, keyed by their raw
	// TZID. Nil when the calendar has none.
	Zones map[string]*VTimezone
}

// Event is one VEVENT. The strongly-typed fields cover the properties the
// mapper cares about; Props keeps every raw property so the inspection CLI can
// surface anything TripIt emits that we haven't modelled yet.
type Event struct {
	UID         string
	Summary     string
	Description string
	Location    string
	Start       DateTime
	End         DateTime
	Geo         *LatLon // the GEO property, when present and parseable
	Props       []Property
}

// LatLon is a decoded iCalendar GEO value ("lat;lon").
type LatLon struct {
	Lat float64
	Lon float64
}

// DateTime is a parsed DTSTART/DTEND value. iCalendar permits three forms:
// a UTC instant ("…Z"), a floating/local time (optionally with a TZID param),
// and a date-only value (VALUE=DATE). Raw preserves the original token.
type DateTime struct {
	Raw string
	// TZID is the IANA zone name for the TZID param: the param itself when it
	// is an IANA name, or the IANA zone a Windows name maps to. Empty when
	// there was no TZID, it was "Local", or it named no zone we know.
	TZID string
	// RawTZID is the TZID param exactly as the file gave it (quotes trimmed).
	RawTZID string
	Time    time.Time // best-effort; zero if unparseable
	HasTime bool      // false for date-only values
	IsUTC   bool      // the value carried a trailing Z
	// Floating means Time holds wall-clock digits with no instant yet: no TZID
	// or Z, TZID=Local, or a TZID we could not resolve.
	Floating bool
	// UnknownTZID means a TZID was present but is neither an IANA name nor a
	// mappable Windows name. When the calendar's own VTIMEZONE for it can
	// give the offset, Time is settled to the true instant and Floating is
	// cleared, but UnknownTZID stays set: the zone still has no IANA name.
	UnknownTZID bool
}

// Property is a single iCalendar content line: NAME;PARAM=v;…:VALUE.
type Property struct {
	Name   string
	Params map[string]string
	Value  string // text-unescaped
}

// Parse reads an iCalendar stream and returns its VEVENTs. It is lenient: a
// property it cannot fully understand is still retained on Event.Props, and an
// unparseable date is kept as Raw with a zero Time rather than failing the
// whole file.
func Parse(r io.Reader) (*Calendar, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}
	cal := &Calendar{}
	var cur *Event
	var zone *zoneBuilder
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		p, ok := parseLine(ln)
		if !ok {
			continue
		}
		name := strings.ToUpper(p.Name)
		if zone != nil {
			if z, done := zone.add(name, p); done {
				if z != nil {
					if cal.Zones == nil {
						cal.Zones = map[string]*VTimezone{}
					}
					cal.Zones[z.TZID] = z
				}
				zone = nil
			}
			continue
		}
		switch name {
		case "BEGIN":
			switch {
			case strings.EqualFold(p.Value, "VEVENT"):
				cur = &Event{}
			case strings.EqualFold(p.Value, "VTIMEZONE"):
				zone = &zoneBuilder{}
			}
			continue
		case "END":
			if strings.EqualFold(p.Value, "VEVENT") && cur != nil {
				cal.Events = append(cal.Events, *cur)
				cur = nil
			}
			continue
		case "PRODID":
			if cur == nil {
				cal.ProdID = p.Value
			}
			continue
		case "X-WR-CALNAME":
			if cur == nil {
				cal.Name = p.Value
			}
			continue
		case "X-WR-CALDESC":
			if cur == nil {
				cal.Desc = p.Value
			}
			continue
		case "X-WR-TIMEZONE":
			if cur == nil {
				cal.Timezone = strings.TrimSpace(p.Value)
			}
			continue
		}
		if cur == nil {
			continue
		}
		cur.Props = append(cur.Props, p)
		switch name {
		case "UID":
			cur.UID = p.Value
		case "SUMMARY":
			cur.Summary = p.Value
		case "DESCRIPTION":
			cur.Description = p.Value
		case "LOCATION":
			cur.Location = p.Value
		case "DTSTART":
			cur.Start = parseDateTime(p)
		case "DTEND":
			cur.End = parseDateTime(p)
		case "GEO":
			cur.Geo = parseGeo(p.Value)
		}
	}
	if cur != nil {
		// Unterminated VEVENT — keep what we have rather than dropping it.
		cal.Events = append(cal.Events, *cur)
	}
	// A VTIMEZONE may come after the events that use it, so unknown TZIDs
	// are settled against the calendar's own definitions only now.
	for i := range cal.Events {
		settleByVTimezone(&cal.Events[i].Start, cal.Zones)
		settleByVTimezone(&cal.Events[i].End, cal.Zones)
	}
	return cal, nil
}

// settleByVTimezone turns a floating time with an unknown TZID into a true
// instant when the calendar defines that TZID and its offsets answer for the
// wall clock. TZID stays empty and UnknownTZID set, as no IANA name is known.
func settleByVTimezone(dt *DateTime, zones map[string]*VTimezone) {
	if !dt.UnknownTZID || !dt.Floating || !dt.HasTime || dt.Time.IsZero() {
		return
	}
	z := zones[dt.RawTZID]
	if z == nil {
		return
	}
	off, ok := z.Offset(dt.Time)
	if !ok {
		return
	}
	dt.Time = dt.Time.Add(-off)
	dt.Floating = false
}

// WindowsToIANA maps a Windows time zone name, as Outlook and Exchange write
// in a TZID (e.g. "W. Europe Standard Time"), to CLDR's default IANA zone for
// it. The table is generated by cmd/gen-windows-zones.
func WindowsToIANA(name string) (string, bool) {
	tz, ok := windowsZones[name]
	return tz, ok
}

// resolveTZID turns a raw TZID param into an IANA zone name. known is false
// only for a TZID that is present but names no zone we can identify; an
// absent TZID or "Local" gives "", true.
func resolveTZID(raw string) (tz string, known bool) {
	switch {
	case raw == "" || strings.EqualFold(raw, "Local"):
		return "", true
	case timing.ValidZone(raw):
		return raw, true
	}
	if tz, ok := WindowsToIANA(raw); ok {
		return tz, true
	}
	return "", false
}

// unfold reads the stream and undoes RFC 5545 line folding: a CRLF (or LF)
// followed by a single space or tab continues the previous line. Returns the
// logical lines with folding removed.
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	// Calendar lines (esp. DESCRIPTION with a whole itinerary) can be long;
	// give the scanner room well beyond the default 64 KiB.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("importics: read ics: %w", err)
	}
	return out, nil
}

// parseLine splits one content line into name, params, and an unescaped value.
// The split honours double-quoted param values so a ':' or ';' inside quotes
// doesn't terminate the name/params section early. ok is false for a line with
// no ':' (not a property).
func parseLine(line string) (Property, bool) {
	colon := indexUnquoted(line, ':')
	if colon < 0 {
		return Property{}, false
	}
	head := line[:colon]
	value := line[colon+1:]

	parts := splitUnquoted(head, ';')
	if len(parts) == 0 || parts[0] == "" {
		return Property{}, false
	}
	p := Property{Name: parts[0], Value: unescapeText(value)}
	if len(parts) > 1 {
		p.Params = make(map[string]string, len(parts)-1)
		for _, kv := range parts[1:] {
			eq := strings.IndexByte(kv, '=')
			if eq < 0 {
				continue
			}
			key := strings.ToUpper(strings.TrimSpace(kv[:eq]))
			val := strings.Trim(strings.TrimSpace(kv[eq+1:]), `"`)
			p.Params[key] = val
		}
	}
	return p, true
}

// parseDateTime interprets a DTSTART/DTEND property into a DateTime. Supported
// value forms: 20060102T150405Z (UTC), 20060102T150405 (floating or, with a
// TZID param, zone-local), and 20060102 (date only, VALUE=DATE). A TZID is
// resolved as an IANA name first, then as a Windows name; "Local" counts as
// no TZID, and anything else leaves the time floating and flagged unknown.
func parseDateTime(p Property) DateTime {
	v := strings.TrimSpace(p.Value)
	raw := p.Params["TZID"]
	tz, known := resolveTZID(raw)
	dt := DateTime{Raw: v, TZID: tz, RawTZID: raw, UnknownTZID: !known}
	if strings.EqualFold(p.Params["VALUE"], "DATE") || len(v) == 8 {
		if t, err := time.Parse("20060102", v); err == nil {
			dt.Time = t
		}
		return dt
	}
	dt.HasTime = true
	if strings.HasSuffix(v, "Z") {
		dt.IsUTC = true
		if t, err := time.Parse("20060102T150405Z", v); err == nil {
			dt.Time = t
		}
		return dt
	}
	// Zoned local time: resolveTZID only returns names that load.
	if tz != "" {
		loc, _ := time.LoadLocation(tz)
		if t, err := time.ParseInLocation("20060102T150405", v, loc); err == nil {
			dt.Time = t
			return dt
		}
	}
	// Floating local time: no TZID, TZID=Local, an unknown TZID, or digits
	// that would not parse. The wall clock is parsed naively and flagged
	// Floating so callers know no zone was applied and must not treat
	// dt.Time as an absolute instant.
	dt.Floating = true
	if t, err := time.Parse("20060102T150405", v); err == nil {
		dt.Time = t
	}
	return dt
}

// parseGeo decodes an iCalendar GEO value, "lat;lon" (RFC 5545 §3.8.1.6), into
// a LatLon. Returns nil when the value isn't two parseable floats.
func parseGeo(v string) *LatLon {
	lat, lon, ok := strings.Cut(strings.TrimSpace(v), ";")
	if !ok {
		return nil
	}
	la, err1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
	lo, err2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
	if err1 != nil || err2 != nil {
		return nil
	}
	return &LatLon{Lat: la, Lon: lo}
}

// indexUnquoted returns the index of the first occurrence of b that is not
// inside a double-quoted run, or -1.
func indexUnquoted(s string, b byte) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case b:
			if !inQuote {
				return i
			}
		}
	}
	return -1
}

// splitUnquoted splits s on sep, ignoring separators inside double quotes.
func splitUnquoted(s string, sep byte) []string {
	var out []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case sep:
			if !inQuote {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// unescapeText reverses RFC 5545 TEXT escaping: \n / \N → newline, and \\, \;,
// \, → the literal character.
func unescapeText(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n', 'N':
				b.WriteByte('\n')
			case '\\', ';', ',':
				b.WriteByte(s[i+1])
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
