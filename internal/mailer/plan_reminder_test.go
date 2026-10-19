package mailer

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

func TestPlanReminderLabel(t *testing.T) {
	cases := []struct{ typ, title, ident, want string }{
		{"flight", "", "BA123", "flight BA123"},
		{"flight", "Trip home", "BA123", "flight BA123"}, // ident wins for flights
		{"hotel", "Hilton Vienna", "", "Hilton Vienna"},
		{"hotel", "", "", "your check-in"},
		{"dining", "", "", "your reservation"},
		{"excursion", "", "", "your excursion"},
		{"flight", "", "", "your flight"}, // flight with no ident falls through to the type word
		{"train", "", "", "your train"},
		{"ground", "", "", "your transfer"},
		{"meeting", "", "", "your meeting"},
		{"event", "", "", "your event"},
		{"vehicle_hire", "", "", "your car hire"},
		{"mystery", "", "", "your plan"},      // unknown type → default
		{"hotel", "   ", "", "your check-in"}, // whitespace-only title is not used
	}
	for _, c := range cases {
		if got := PlanReminderLabel(c.typ, c.title, c.ident); got != c.want {
			t.Errorf("PlanReminderLabel(%q,%q,%q) = %q, want %q", c.typ, c.title, c.ident, got, c.want)
		}
	}
}

func TestBuildPlanReminderEmail(t *testing.T) {
	msg := BuildPlanReminderEmail(PlanReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    7,
		Label:     "flight BA123",
		Start:     timing.Block{At: time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC), TZ: "UTC", Source: timing.Confirmed},
	})
	for _, want := range []string{
		"Subject: Upcoming: flight BA123",
		"Flight BA123 starts",
		"https://aerly.test/trips/7",
		"To: alice@example.com",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("email missing %q\n---\n%s", want, msg)
		}
	}
}

func TestBuildPlanReminderEmail_NonASCIITitle(t *testing.T) {
	// A user-entered title with a multi-byte leading rune must not be corrupted
	// by the lead-line capitalisation.
	msg := BuildPlanReminderEmail(PlanReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    1,
		Label:     "école dinner",
		Start:     timing.Block{At: time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC), TZ: "UTC", Source: timing.Confirmed},
	})
	if !strings.Contains(msg, "École dinner starts") {
		t.Errorf("expected rune-safe capitalisation 'École dinner', got:\n%s", msg)
	}
}

func TestCapitalise_Empty(t *testing.T) {
	// The empty-string fast path must return the empty string untouched
	// rather than indexing into a zero-length rune slice.
	if got := capitalise(""); got != "" {
		t.Errorf("capitalise(\"\") = %q, want empty", got)
	}
}

func TestBuildPlanReminderEmail_NonUTCZone(t *testing.T) {
	msg := BuildPlanReminderEmail(PlanReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    1,
		Label:     "your check-in",
		Start: timing.Block{At: time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC),
			TZ: "Europe/Vienna", Source: timing.Confirmed, Place: "Vienna"}, // UTC+2 in June
	})
	if !strings.Contains(msg, "Your check-in starts Fri 5 Jun, 11:30 Vienna (UTC+2).") {
		t.Errorf("expected the Vienna wall clock with its offset, got:\n%s", msg)
	}
	if strings.Contains(msg, "your time") {
		t.Errorf("no home zone, so no second time:\n%s", msg)
	}
}

// TestBuildPlanReminderEmail_Floating: a floating start prints its stored
// digits as they are, with no offset and no second time, whatever the
// recipient's home zone (review focus 3).
func TestBuildPlanReminderEmail_Floating(t *testing.T) {
	msg := BuildPlanReminderEmail(PlanReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    1,
		Label:     "Test Hotel",
		Start:     timing.Block{At: time.Date(2026, 6, 5, 15, 0, 0, 0, time.UTC), Source: timing.Floating},
		HomeTZ:    "America/New_York",
	})
	if !strings.Contains(msg, "Test Hotel starts Fri 5 Jun, 15:00.\r\n") {
		t.Errorf("floating start should read bare digits, got:\n%s", msg)
	}
	if strings.Contains(msg, "UTC") || strings.Contains(msg, "your time") {
		t.Errorf("floating start must carry no label:\n%s", msg)
	}
}

// TestBuildPlanReminderEmail_HomeZone: a recipient whose home zone differs
// from the event's gets their own time after the event's.
func TestBuildPlanReminderEmail_HomeZone(t *testing.T) {
	msg := BuildPlanReminderEmail(PlanReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    1,
		Label:     "flight XX100",
		Start: timing.Block{At: time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC),
			TZ: "Europe/London", Source: timing.Confirmed, Place: "London"},
		HomeTZ: "America/New_York",
	})
	if !strings.Contains(msg, "Flight XX100 starts Fri 5 Jun, 10:30 London (UTC+1) (05:30 your time).") {
		t.Errorf("want the your-time suffix, got:\n%s", msg)
	}
}

func TestFullTime(t *testing.T) {
	b := timing.Block{At: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC),
		TZ: "Asia/Tashkent", Source: timing.Confirmed, Place: "Tashkent"}
	if got, want := FullTime(b, ""), "Sun 18 Oct, 14:00 Tashkent (UTC+5)"; got != want {
		t.Errorf("no home: got %q, want %q", got, want)
	}
	if got, want := FullTime(b, "Europe/London"), "Sun 18 Oct, 14:00 Tashkent (UTC+5) (10:00 your time)"; got != want {
		t.Errorf("London home: got %q, want %q", got, want)
	}
	if got, want := WithYourTime("x", b, "Asia/Tashkent"), "x"; got != want {
		t.Errorf("same zone: got %q, want %q", got, want)
	}
}
