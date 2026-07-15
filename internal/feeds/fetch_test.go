package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/importics"
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
