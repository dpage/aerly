package feeds

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/importics"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/timing"
)

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://example.com/cal.ics", "https://example.com/cal.ics", false},
		{"http://example.com/cal.ics", "http://example.com/cal.ics", false},
		{"webcal://example.com/cal.ics", "https://example.com/cal.ics", false},
		{"  https://example.com/cal.ics  ", "https://example.com/cal.ics", false},
		{"ftp://example.com/cal.ics", "", true},
		{"file:///etc/passwd", "", true},
		{"https://127.0.0.1/cal.ics", "", true},
		{"http://10.0.0.5/cal.ics", "", true},
		{"http://[::1]/cal.ics", "", true},
		{"http://169.254.169.254/latest/meta-data/", "", true}, // cloud metadata
		{"not a url at all ::::", "", true},
		{"https:///nohost", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeURL(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeURL(%q) errored: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGuardAddrBlocksPrivate(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:443", "10.1.2.3:80", "192.168.0.1:443", "[::1]:443", "100.64.0.1:80", "0.1.2.3:80", "255.255.255.255:80"} {
		if err := guardAddr(addr); err == nil {
			t.Errorf("guardAddr(%q) = nil, want blocked", addr)
		}
	}
	if err := guardAddr("8.8.8.8:443"); err != nil {
		t.Errorf("guardAddr public = %v, want nil", err)
	}
}

const sampleICS = "BEGIN:VCALENDAR\r\n" +
	"X-WR-CALNAME:PGConf EU\r\n" +
	"X-WR-TIMEZONE:America/Vancouver\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:talk-1@example.com\r\n" +
	"SUMMARY:Opening Keynote\r\n" +
	"LOCATION:Main Hall\r\n" +
	"DTSTART:20261020T090000Z\r\n" +
	"DTEND:20261020T100000Z\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestFetchParsesAndConditionalGET(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()

	// httptest binds to loopback, which the dialer guard would normally reject.
	// Swap in a plain client so we exercise the fetch/parse/conditional-GET path
	// without the SSRF guard (which has its own test above).
	f := NewFetcher("test")
	f.HTTP = srv.Client()
	f.AllowPrivate = true // httptest binds to loopback

	res, err := f.Fetch(context.Background(), srv.URL, "", "", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.CalName != "PGConf EU" {
		t.Errorf("CalName = %q", res.CalName)
	}
	if len(res.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(res.Events))
	}
	ev := res.Events[0]
	if ev.UID != "talk-1@example.com" || ev.Summary != "Opening Keynote" || ev.Location != "Main Hall" {
		t.Errorf("event mismatch: %+v", ev)
	}
	// The feed stamps times in UTC but declares X-WR-TIMEZONE, so the display
	// zone falls back to it (the instant is unchanged).
	if ev.StartTZ != "America/Vancouver" {
		t.Errorf("StartTZ = %q, want America/Vancouver (from X-WR-TIMEZONE)", ev.StartTZ)
	}
	if !ev.StartsAt.Equal(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("StartsAt = %v, want 2026-10-20T09:00:00Z", ev.StartsAt)
	}
	if ev.EndsAt == nil {
		t.Errorf("expected an end time")
	}
	if res.ETag != `"v1"` {
		t.Errorf("ETag = %q", res.ETag)
	}

	// A conditional GET with the returned ETag yields ErrNotModified.
	if _, err := f.Fetch(context.Background(), srv.URL, res.ETag, "", ""); err != ErrNotModified {
		t.Errorf("conditional Fetch err = %v, want ErrNotModified", err)
	}
}

func TestMapEventsDropsUndated(t *testing.T) {
	in := []importics.Event{
		{UID: "a", Summary: "dated", Start: importics.DateTime{Time: time.Now(), HasTime: true}},
		{UID: "b", Summary: "undated"}, // zero Start.Time → dropped
	}
	out := mapEvents(in, "", "")
	if len(out) != 1 || out[0].UID != "a" {
		t.Errorf("mapEvents = %+v, want only the dated event", out)
	}
}

func TestMapEventsTimezone(t *testing.T) {
	// A UTC-stamped event (DTSTART:...Z) with a calendar default zone: the
	// instant is unchanged, but the display zone becomes the calendar's so it
	// renders in local time rather than UTC.
	utc := time.Date(2026, 5, 12, 16, 0, 0, 0, time.UTC)
	in := []importics.Event{
		{UID: "z", Summary: "keynote", Start: importics.DateTime{Time: utc, HasTime: true, IsUTC: true}},
	}
	out := mapEvents(in, "America/Vancouver", "")
	if len(out) != 1 {
		t.Fatalf("want 1 event, got %d", len(out))
	}
	if !out[0].StartsAt.Equal(utc) {
		t.Errorf("UTC instant changed: %v", out[0].StartsAt)
	}
	if out[0].StartTZ != "America/Vancouver" {
		t.Errorf("StartTZ = %q, want America/Vancouver", out[0].StartTZ)
	}

	// A floating wall-clock (no Z, no TZID) is anchored in the calendar zone:
	// 09:00 in Vancouver (UTC-7 in May) is 16:00 UTC.
	wall := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	in = []importics.Event{
		{UID: "f", Summary: "session", Start: importics.DateTime{Time: wall, HasTime: true, Floating: true}},
	}
	out = mapEvents(in, "America/Vancouver", "")
	if got := out[0].StartsAt; !got.Equal(time.Date(2026, 5, 12, 16, 0, 0, 0, time.UTC)) {
		t.Errorf("floating not anchored to Vancouver: got %v, want 16:00Z", got)
	}
	if out[0].StartTZ != "America/Vancouver" {
		t.Errorf("StartTZ = %q, want America/Vancouver", out[0].StartTZ)
	}

	// An explicit per-event TZID wins over the calendar default.
	in = []importics.Event{
		{UID: "t", Summary: "x", Start: importics.DateTime{Time: utc, HasTime: true, TZID: "Europe/London"}},
	}
	out = mapEvents(in, "America/Vancouver", "")
	if out[0].StartTZ != "Europe/London" {
		t.Errorf("StartTZ = %q, want Europe/London (event TZID wins)", out[0].StartTZ)
	}

	// With no event TZID and no calendar zone, the user-set fallback applies —
	// the case for a UTC feed that declares no zone at all (e.g. PGDay UK).
	in = []importics.Event{
		{UID: "u", Summary: "x", Start: importics.DateTime{Time: utc, HasTime: true, IsUTC: true}},
	}
	out = mapEvents(in, "", "Europe/London")
	if out[0].StartTZ != "Europe/London" {
		t.Errorf("StartTZ = %q, want Europe/London (user fallback)", out[0].StartTZ)
	}
	if !out[0].StartsAt.Equal(utc) {
		t.Errorf("UTC instant changed under fallback: %v", out[0].StartsAt)
	}
}

func TestMapEventsZoneSource(t *testing.T) {
	utc := time.Date(2026, 5, 12, 16, 0, 0, 0, time.UTC)
	timed := func(tzid string) []importics.Event {
		return []importics.Event{{UID: "e", Summary: "Test Event",
			Start: importics.DateTime{Time: utc, HasTime: true, TZID: tzid}}}
	}
	cases := []struct {
		name          string
		in            []importics.Event
		calTZ, userTZ string
		wantTZ        string
		want          timing.Source
	}{
		{"event TZID is confirmed", timed("Europe/London"), "America/Vancouver", "Europe/Paris", "Europe/London", timing.Confirmed},
		{"calendar zone is inferred", timed(""), "America/Vancouver", "", "America/Vancouver", timing.Inferred},
		{"user fallback is inferred", timed(""), "", "Europe/London", "Europe/London", timing.Inferred},
		{"no zone is unknown", timed(""), "", "", "", timing.Unknown},
		{"all-day is unknown", []importics.Event{{UID: "d", Summary: "Test Event",
			Start: importics.DateTime{Time: utc}}}, "", "Europe/London", "", timing.Unknown},
	}
	for _, c := range cases {
		out := mapEvents(c.in, c.calTZ, c.userTZ)
		if len(out) != 1 {
			t.Fatalf("%s: want 1 event, got %d", c.name, len(out))
		}
		if out[0].StartTZ != c.wantTZ || out[0].StartTZSource != string(c.want) {
			t.Errorf("%s: tz/source = %q/%q, want %q/%q", c.name, out[0].StartTZ, out[0].StartTZSource, c.wantTZ, c.want)
		}
	}
}

func TestMapEventsUnloadableZones(t *testing.T) {
	wall := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	floating := func(tzid string) []importics.Event {
		return []importics.Event{{UID: "e", Summary: "Test Event",
			Start: importics.DateTime{Time: wall, HasTime: true, Floating: true, TZID: tzid}}}
	}
	// Unloadable TZID, nothing else: no zone, and the wall clock stays as UTC.
	out := mapEvents(floating("Pacific Time"), "", "")
	if len(out) != 1 || out[0].StartTZ != "" || out[0].StartTZSource != string(timing.Unknown) {
		t.Fatalf("invalid TZID alone: %+v", out)
	}
	if !out[0].StartsAt.Equal(wall) {
		t.Errorf("StartsAt = %v, want %v", out[0].StartsAt, wall)
	}
	// Unloadable TZID with a valid calendar zone: the calendar zone is inferred
	// and anchors the wall clock (09:00 Vancouver = 16:00Z in May).
	out = mapEvents(floating("Pacific Time"), "America/Vancouver", "")
	if out[0].StartTZ != "America/Vancouver" || out[0].StartTZSource != string(timing.Inferred) {
		t.Errorf("invalid TZID with calTZ: %q/%q", out[0].StartTZ, out[0].StartTZSource)
	}
	if want := time.Date(2026, 5, 12, 16, 0, 0, 0, time.UTC); !out[0].StartsAt.Equal(want) {
		t.Errorf("StartsAt = %v, want %v", out[0].StartsAt, want)
	}
	// Invalid calendar zone falls through to a valid user fallback; "Local" is refused.
	out = mapEvents(floating(""), "Local", "Europe/London")
	if out[0].StartTZ != "Europe/London" || out[0].StartTZSource != string(timing.Inferred) {
		t.Errorf("invalid calTZ: %q/%q", out[0].StartTZ, out[0].StartTZSource)
	}
	if want := time.Date(2026, 5, 12, 8, 0, 0, 0, time.UTC); !out[0].StartsAt.Equal(want) {
		t.Errorf("StartsAt = %v, want %v", out[0].StartsAt, want)
	}
	// A valid TZID on a floating value anchors in that zone (confirmed).
	out = mapEvents(floating("Europe/Paris"), "America/Vancouver", "")
	if out[0].StartTZSource != string(timing.Confirmed) ||
		!out[0].StartsAt.Equal(time.Date(2026, 5, 12, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("valid TZID floating: %+v", out[0])
	}
}

func TestLooksLikeICalendar(t *testing.T) {
	cases := map[string]bool{
		"BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n":  true,
		"\ufeffbegin:vcalendar\n":               true,  // BOM + lowercase
		"<schedule><day/></schedule>":           false, // frab/Pentabarf XML
		"<!DOCTYPE html><html>Not found</html>": false,
		"":                                      false,
	}
	for body, want := range cases {
		if got := looksLikeICalendar([]byte(body)); got != want {
			t.Errorf("looksLikeICalendar(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestFetchRejectsNonICalendar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A frab/Pentabarf XML schedule — valid XML, not iCalendar.
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><schedule><conference/></schedule>`))
	}))
	defer srv.Close()

	f := NewFetcher("test")
	f.HTTP = srv.Client()
	f.AllowPrivate = true

	if _, err := f.Fetch(context.Background(), srv.URL, "", "", ""); err != ErrNotICalendar {
		t.Errorf("Fetch err = %v, want ErrNotICalendar", err)
	}
}

// TestMapEventsOutlookFixture: an Outlook calendar feed's Windows TZID gives
// Europe/Berlin, confirmed, and its TZID=Local event, with no zone to anchor
// it, keeps its digits as UTC and is unknown, as feeds have no floating.
func TestMapEventsOutlookFixture(t *testing.T) {
	b, err := os.ReadFile("../importics/testdata/outlook_windows_tz.ics")
	if err != nil {
		t.Fatal(err)
	}
	cal, err := importics.Parse(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out := mapEvents(cal.Events, cal.Timezone, "")
	if len(out) != 2 {
		t.Fatalf("got %d events, want 2", len(out))
	}
	cases := []struct {
		got            store.TripFeedEvent
		start, end, tz string
		src            timing.Source
	}{
		{out[0], "2026-07-01T07:00:00Z", "2026-07-01T08:00:00Z", "Europe/Berlin", timing.Confirmed},
		{out[1], "2026-07-02T09:00:00Z", "2026-07-02T09:30:00Z", "", timing.Unknown},
	}
	for _, c := range cases {
		if s := c.got.StartsAt.UTC().Format(time.RFC3339); s != c.start || c.got.EndsAt == nil ||
			c.got.EndsAt.UTC().Format(time.RFC3339) != c.end || c.got.StartTZ != c.tz || c.got.StartTZSource != string(c.src) {
			t.Errorf("%s: %s-%v %q/%q, want %s-%s %q/%q", c.got.UID, s, c.got.EndsAt,
				c.got.StartTZ, c.got.StartTZSource, c.start, c.end, c.tz, c.src)
		}
	}
}

// TestMapEventsAnchorsLikeTheMappers: a London DST-gap wall clock and a
// floating end settle with timing.Anchor, the end in its start's TZID.
func TestMapEventsAnchorsLikeTheMappers(t *testing.T) {
	gap := time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC)
	want := time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC) // the pre-gap offset (GMT)
	out := mapEvents([]importics.Event{{UID: "g", Summary: "Test Event",
		Start: importics.DateTime{Time: gap, HasTime: true, Floating: true}}}, "Europe/London", "")
	if !out[0].StartsAt.Equal(want) || out[0].StartTZSource != string(timing.Inferred) {
		t.Errorf("gap = %v %q, want %v inferred", out[0].StartsAt, out[0].StartTZSource, want)
	}

	paris, _ := time.LoadLocation("Europe/Paris")
	wall := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	out = mapEvents([]importics.Event{{UID: "e", Summary: "Test Event",
		Start: importics.DateTime{Time: time.Date(2026, 7, 1, 9, 0, 0, 0, paris), HasTime: true, TZID: "Europe/Paris"},
		End:   importics.DateTime{Time: wall, HasTime: true, Floating: true}}}, "America/Vancouver", "")
	if out[0].EndsAt == nil || !out[0].EndsAt.Equal(time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("end = %v, want 08:00Z (10:00 in Paris)", out[0].EndsAt)
	}
}
