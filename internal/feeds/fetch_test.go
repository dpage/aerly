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
	for _, addr := range []string{"127.0.0.1:443", "10.1.2.3:80", "192.168.0.1:443", "[::1]:443", "100.64.0.1:80"} {
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

	res, err := f.Fetch(context.Background(), srv.URL, "", "")
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
	if ev.EndsAt == nil {
		t.Errorf("expected an end time")
	}
	if res.ETag != `"v1"` {
		t.Errorf("ETag = %q", res.ETag)
	}

	// A conditional GET with the returned ETag yields ErrNotModified.
	if _, err := f.Fetch(context.Background(), srv.URL, res.ETag, ""); err != ErrNotModified {
		t.Errorf("conditional Fetch err = %v, want ErrNotModified", err)
	}
}

func TestMapEventsDropsUndated(t *testing.T) {
	in := []importics.Event{
		{UID: "a", Summary: "dated", Start: importics.DateTime{Time: time.Now(), HasTime: true}},
		{UID: "b", Summary: "undated"}, // zero Start.Time → dropped
	}
	out := mapEvents(in)
	if len(out) != 1 || out[0].UID != "a" {
		t.Errorf("mapEvents = %+v, want only the dated event", out)
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

	if _, err := f.Fetch(context.Background(), srv.URL, "", ""); err != ErrNotICalendar {
		t.Errorf("Fetch err = %v, want ErrNotICalendar", err)
	}
}
