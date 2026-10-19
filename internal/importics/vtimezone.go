package importics

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// VTimezone is one VTIMEZONE component: the calendar's own definition of the
// UTC offsets a TZID uses over time.
type VTimezone struct {
	TZID        string
	Observances []Observance
}

// Observance is one STANDARD or DAYLIGHT sub-component of a VTIMEZONE: from
// Start (and each RRULE or RDATE recurrence) the offset changes from
// OffsetFrom to OffsetTo.
type Observance struct {
	Start      time.Time // local wall clock as UTC digits, read in the OffsetFrom clock
	OffsetFrom time.Duration
	OffsetTo   time.Duration
	RRule      string      // the raw RRULE value, if any
	RDates     []time.Time // local wall clocks as UTC digits
	IsDST      bool        // a DAYLIGHT rather than a STANDARD block
}

// errUnsupportedRule marks an RRULE this package cannot expand, so Offset
// declines to answer rather than guess.
var errUnsupportedRule = errors.New("importics: unsupported VTIMEZONE RRULE")

// Offset returns the UTC offset in force at a wall-clock time (given as UTC
// digits). The observance whose latest onset is at or before local wins, with
// onsets compared as wall clocks. As RFC 5545 section 3.3.5 requires, a time
// in a spring-forward gap takes the offset from before the gap, and a time
// in an autumn overlap means its first occurrence (again the old offset),
// which matches how timing.Anchor settles the same digits. ok is false when
// local precedes every onset or an observance's RRULE is beyond the yearly
// forms handled here.
func (z *VTimezone) Offset(local time.Time) (time.Duration, bool) {
	var best time.Time
	var off time.Duration
	found := false
	for _, o := range z.Observances {
		onset, ok, err := o.lastOnset(local)
		if err != nil {
			return 0, false
		}
		if ok && (!found || onset.After(best)) {
			best, off, found = onset, o.OffsetTo, true
		}
	}
	return off, found
}

// lastOnset returns the observance's latest onset at or before local: its
// DTSTART, an RDATE, or a yearly RRULE recurrence. An onset that moves the
// clocks forward only takes effect once the skipped hour is past, so the
// times in the gap keep the old offset.
func (o Observance) lastOnset(local time.Time) (time.Time, bool, error) {
	var last time.Time
	found := false
	jump := max(0, o.OffsetTo-o.OffsetFrom)
	consider := func(t time.Time) {
		t = t.Add(jump)
		if !t.After(local) && (!found || t.After(last)) {
			last, found = t, true
		}
	}
	consider(o.Start)
	for _, r := range o.RDates {
		consider(r)
	}
	if o.RRule == "" {
		return last, found, nil
	}
	rule, err := parseYearlyRule(o.RRule)
	if err != nil {
		return time.Time{}, false, err
	}
	n := 0
	for y := o.Start.Year(); y <= local.Year(); y++ {
		occ, ok := rule.occurrence(y, o.Start)
		if !ok || occ.Before(o.Start) {
			continue
		}
		n++
		if rule.count > 0 && n > rule.count {
			break
		}
		if rule.hasUntil {
			// A UTC UNTIL is compared as the instant the onset happens,
			// which is its wall clock read in the OffsetFrom clock.
			at := occ
			if rule.untilUTC {
				at = occ.Add(-o.OffsetFrom)
			}
			if at.After(rule.until) {
				break
			}
		}
		consider(occ)
	}
	return last, found, nil
}

// yearlyRule is the subset of RRULE that VTIMEZONE producers use: once a
// year, in one month, on the nth (or nth-from-last) weekday or on DTSTART's
// day of the month, optionally ended by UNTIL or COUNT.
type yearlyRule struct {
	month    time.Month // 0 means DTSTART's month
	hasDay   bool
	ordinal  int // 1..5 from the start of the month, -1..-5 from the end
	weekday  time.Weekday
	count    int
	hasUntil bool
	untilUTC bool
	until    time.Time
}

var weekdays = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// parseYearlyRule reads an RRULE value, returning errUnsupportedRule for any
// part it does not handle (a non-yearly FREQ, an INTERVAL other than 1, lists
// in BYMONTH or BYDAY, a BYDAY with no ordinal, BYMONTHDAY, and so on).
func parseYearlyRule(v string) (yearlyRule, error) {
	var r yearlyRule
	yearly := false
	for _, part := range strings.Split(v, ";") {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return r, errUnsupportedRule
		}
		switch strings.ToUpper(key) {
		case "FREQ":
			yearly = strings.EqualFold(val, "YEARLY")
		case "INTERVAL":
			if val != "1" {
				return r, errUnsupportedRule
			}
		case "WKST":
			// Irrelevant to a single weekday within a month.
		case "BYMONTH":
			m, err := strconv.Atoi(val)
			if err != nil || m < 1 || m > 12 {
				return r, errUnsupportedRule
			}
			r.month = time.Month(m)
		case "BYDAY":
			ord, wd, ok := parseByDay(val)
			if !ok {
				return r, errUnsupportedRule
			}
			r.hasDay, r.ordinal, r.weekday = true, ord, wd
		case "COUNT":
			c, err := strconv.Atoi(val)
			if err != nil || c < 1 {
				return r, errUnsupportedRule
			}
			r.count = c
		case "UNTIL":
			t, isUTC, ok := parseUntil(val)
			if !ok {
				return r, errUnsupportedRule
			}
			r.hasUntil, r.untilUTC, r.until = true, isUTC, t
		default:
			return r, errUnsupportedRule
		}
	}
	if !yearly {
		return r, errUnsupportedRule
	}
	return r, nil
}

// parseByDay reads one BYDAY entry with an ordinal, such as "-1SU" or "2SU".
func parseByDay(v string) (int, time.Weekday, bool) {
	if len(v) < 3 {
		return 0, 0, false
	}
	wd, ok := weekdays[strings.ToUpper(v[len(v)-2:])]
	if !ok {
		return 0, 0, false
	}
	n, err := strconv.Atoi(v[:len(v)-2])
	if err != nil || n == 0 || n < -5 || n > 5 {
		return 0, 0, false
	}
	return n, wd, true
}

// parseUntil reads an UNTIL value: a UTC date-time, a local date-time or a
// date. isUTC reports a trailing Z.
func parseUntil(v string) (time.Time, bool, bool) {
	if t, err := time.Parse("20060102T150405Z", v); err == nil {
		return t, true, true
	}
	if t, err := time.Parse("20060102T150405", v); err == nil {
		return t, false, true
	}
	if t, err := time.Parse("20060102", v); err == nil {
		return t, false, true
	}
	return time.Time{}, false, false
}

// occurrence returns the rule's onset in year y, at DTSTART's time of day.
// ok is false when the year has no such day (a fifth Sunday that does not
// exist, or DTSTART's day of the month missing from the target month).
func (r yearlyRule) occurrence(y int, start time.Time) (time.Time, bool) {
	month := r.month
	if month == 0 {
		month = start.Month()
	}
	hh, mm, ss := start.Clock()
	if !r.hasDay {
		t := time.Date(y, month, start.Day(), hh, mm, ss, 0, time.UTC)
		return t, t.Month() == month
	}
	var day time.Time
	if r.ordinal > 0 {
		first := time.Date(y, month, 1, hh, mm, ss, 0, time.UTC)
		shift := (int(r.weekday) - int(first.Weekday()) + 7) % 7
		day = first.AddDate(0, 0, shift+7*(r.ordinal-1))
	} else {
		last := time.Date(y, month+1, 0, hh, mm, ss, 0, time.UTC)
		shift := (int(last.Weekday()) - int(r.weekday) + 7) % 7
		day = last.AddDate(0, 0, -shift-7*(-r.ordinal-1))
	}
	return day, day.Month() == month
}

// zoneBuilder accumulates one VTIMEZONE whilst Parse reads its lines.
type zoneBuilder struct {
	zone VTimezone
	obs  *obsBuilder
}

// obsBuilder accumulates one STANDARD or DAYLIGHT block; an observance is kept
// only once it has a readable DTSTART and both offsets.
type obsBuilder struct {
	Observance
	hasStart, hasFrom, hasTo bool
}

// add takes the next line inside a VTIMEZONE. done is true at END:VTIMEZONE,
// when z is the finished zone, or nil if it had no TZID.
func (b *zoneBuilder) add(name string, p Property) (z *VTimezone, done bool) {
	switch name {
	case "BEGIN":
		if v := strings.ToUpper(p.Value); v == "STANDARD" || v == "DAYLIGHT" {
			b.obs = &obsBuilder{Observance: Observance{IsDST: v == "DAYLIGHT"}}
		}
		return nil, false
	case "END":
		switch strings.ToUpper(p.Value) {
		case "STANDARD", "DAYLIGHT":
			if o := b.obs; o != nil && o.hasStart && o.hasFrom && o.hasTo {
				b.zone.Observances = append(b.zone.Observances, o.Observance)
			}
			b.obs = nil
		case "VTIMEZONE":
			if b.zone.TZID == "" {
				return nil, true
			}
			return &b.zone, true
		}
		return nil, false
	case "TZID":
		if b.obs == nil {
			b.zone.TZID = strings.TrimSpace(p.Value)
		}
		return nil, false
	}
	if b.obs != nil {
		b.obs.add(name, p)
	}
	return nil, false
}

// add records one property of a STANDARD or DAYLIGHT block.
func (o *obsBuilder) add(name string, p Property) {
	v := strings.TrimSpace(p.Value)
	switch name {
	case "DTSTART":
		if t, ok := parseLocalDigits(v); ok {
			o.Start, o.hasStart = t, true
		}
	case "TZOFFSETFROM":
		o.OffsetFrom, o.hasFrom = parseUTCOffset(v)
	case "TZOFFSETTO":
		o.OffsetTo, o.hasTo = parseUTCOffset(v)
	case "RRULE":
		o.RRule = v
	case "RDATE":
		if strings.EqualFold(p.Params["VALUE"], "PERIOD") {
			return
		}
		for _, item := range strings.Split(v, ",") {
			if t, ok := parseLocalDigits(strings.TrimSpace(item)); ok {
				o.RDates = append(o.RDates, t)
			}
		}
	}
}

// parseLocalDigits reads a VTIMEZONE date-time or date as wall-clock digits
// in UTC.
func parseLocalDigits(v string) (time.Time, bool) {
	if t, err := time.Parse("20060102T150405", v); err == nil {
		return t, true
	}
	if t, err := time.Parse("20060102", v); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// parseUTCOffset reads a UTC-OFFSET value: a sign, then HHMM with optional SS,
// with hours 00-23 and minutes and seconds 00-59 as RFC 5545 allows.
func parseUTCOffset(v string) (time.Duration, bool) {
	if (len(v) != 5 && len(v) != 7) || (v[0] != '+' && v[0] != '-') {
		return 0, false
	}
	var parts [3]int
	for i := 0; i*2+1 < len(v); i++ {
		n, err := strconv.Atoi(v[i*2+1 : i*2+3])
		if err != nil || (i == 0 && n > 23) || (i > 0 && n > 59) {
			return 0, false
		}
		parts[i] = n
	}
	d := time.Duration(parts[0])*time.Hour + time.Duration(parts[1])*time.Minute + time.Duration(parts[2])*time.Second
	if v[0] == '-' {
		d = -d
	}
	return d, true
}
