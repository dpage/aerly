package mailer

import (
	"strings"
	"testing"
	"time"
)

func TestPlanReminderLabel(t *testing.T) {
	cases := []struct{ typ, title, ident, want string }{
		{"flight", "", "BA123", "flight BA123"},
		{"flight", "Trip home", "BA123", "flight BA123"}, // ident wins for flights
		{"hotel", "Hilton Vienna", "", "Hilton Vienna"},
		{"hotel", "", "", "your hotel check-in"},
		{"dining", "", "", "your reservation"},
		{"excursion", "", "", "your excursion"},
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
		StartsAt:  time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC),
		StartTZ:   "UTC",
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
		StartsAt:  time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC),
		StartTZ:   "UTC",
	})
	if !strings.Contains(msg, "École dinner starts") {
		t.Errorf("expected rune-safe capitalisation 'École dinner', got:\n%s", msg)
	}
}

func TestBuildPlanReminderEmail_NonUTCZone(t *testing.T) {
	msg := BuildPlanReminderEmail(PlanReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    1,
		Label:     "your hotel check-in",
		StartsAt:  time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC),
		StartTZ:   "Europe/Vienna", // UTC+2 in June → 11:30
	})
	if !strings.Contains(msg, "11:30") {
		t.Errorf("expected localized 11:30 in Vienna, got:\n%s", msg)
	}
}
