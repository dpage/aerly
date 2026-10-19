package mailer

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

func TestCheckinReminderSubject(t *testing.T) {
	if got := CheckinReminderSubject("BA286"); got != "Check-in opens soon: BA286" {
		t.Errorf("subject = %q", got)
	}
}

func TestBuildCheckinReminderEmail(t *testing.T) {
	msg := BuildCheckinReminderEmail(CheckinReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test/",
		TripID:    7,
		Ident:     "BA286",
		Route:     "LHR → LIS",
		Start: timing.Block{At: time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC),
			TZ: "Europe/London", Source: timing.Confirmed, Place: "London"},
	})
	for _, want := range []string{
		"Subject: Check-in opens soon: BA286",
		"To: alice@example.com",
		"Online check-in for Flight BA286 (LHR",
		"opens in five minutes",
		// The departure is quoted on the airport's clock, not UTC.
		"It departs Mon 31 Aug, 10:00 London (UTC+1).",
		"https://aerly.test/trips/7",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("email missing %q\n---\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "09:00 UTC") {
		t.Errorf("departure quoted in UTC:\n%s", msg)
	}
}

// TestBuildCheckinReminderEmail_NoRoute: with no route known the flight is
// still named, without an empty pair of brackets trailing it.
func TestBuildCheckinReminderEmail_NoRoute(t *testing.T) {
	msg := BuildCheckinReminderEmail(CheckinReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    3,
		Ident:     "BA286",
		Start:     timing.Block{At: time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)},
	})
	if !strings.Contains(msg, "Online check-in for Flight BA286 opens in five minutes") {
		t.Errorf("routeless lead line wrong:\n%s", msg)
	}
	if strings.Contains(msg, "()") {
		t.Errorf("empty brackets left in:\n%s", msg)
	}
	// No zone anywhere to resolve, so the mailer's UTC fallback stands.
	if !strings.Contains(msg, "Mon 31 Aug, 09:00 UTC.") {
		t.Errorf("want the UTC fallback:\n%s", msg)
	}
}

// TestBuildCheckinReminderEmail_HomeZone: the recipient's own time follows the
// airport's when their home zone differs, here on the previous evening.
func TestBuildCheckinReminderEmail_HomeZone(t *testing.T) {
	msg := BuildCheckinReminderEmail(CheckinReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    3,
		Ident:     "XX100",
		Start: timing.Block{At: time.Date(2026, 8, 31, 2, 0, 0, 0, time.UTC),
			TZ: "Europe/London", Source: timing.Confirmed, Place: "London"},
		HomeTZ: "America/New_York",
	})
	if !strings.Contains(msg, "It departs Mon 31 Aug, 03:00 London (UTC+1) (Sun 30 Aug, 22:00 your time).") {
		t.Errorf("want the long your-time form:\n%s", msg)
	}
}
