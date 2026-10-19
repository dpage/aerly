package mailer

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

func TestFlightAlertSubject(t *testing.T) {
	cases := map[string]string{
		"delayed":       "Your flight BA123 is now delayed",
		"cancelled":     "Your flight BA123 has been cancelled",
		"diverted":      "Your flight BA123 has been diverted",
		"gate":          "Gate change: BA123",
		"gate_assigned": "Gate assigned: BA123",
		"belt":          "Baggage belt: BA123",
		"terminal":      "Terminal change: BA123",
		"":              "Your flight BA123 is now delayed", // unknown kind → delayed
	}
	for kind, want := range cases {
		if got := FlightAlertSubject("BA123", kind); got != want {
			t.Errorf("kind %q: got %q, want %q", kind, got, want)
		}
	}
}

func TestBuildFlightAlertEmail_DelayHeadlineAndStructure(t *testing.T) {
	msg := BuildFlightAlertEmail(FlightAlertInput{
		FromAddr:  "alerts@aerly.test",
		ToAddr:    "owner@aerly.test",
		PublicURL: "http://localhost:8080",
		Ident:     "BA123",
		Kind:      "delayed",
		Detail:    "now departing 14:35 UTC",
	})
	for _, want := range []string{
		`From: "Aerly" <alerts@aerly.test>`,
		"To: owner@aerly.test",
		"MIME-Version: 1.0",
		"multipart/alternative",
		"Your flight BA123 is now delayed",
		"now departing 14:35 UTC",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n---\n%s", want, msg)
		}
	}
	// Subject is RFC-encoded but the plain-text part carries the headline.
	if !strings.Contains(msg, "Subject:") {
		t.Errorf("no Subject header")
	}
}

func TestBuildFlightAlertEmail_CancellationNoDetail(t *testing.T) {
	msg := BuildFlightAlertEmail(FlightAlertInput{
		FromAddr:  "a@x",
		ToAddr:    "b@x",
		PublicURL: "http://localhost:8080",
		Ident:     "LH9",
		Kind:      "cancelled",
	})
	if !strings.Contains(msg, "Your flight LH9 has been cancelled") {
		t.Errorf("cancellation headline missing:\n%s", msg)
	}
}

func TestBuildFlightAlertEmail_GateChange(t *testing.T) {
	msg := BuildFlightAlertEmail(FlightAlertInput{
		FromAddr:  "a@x",
		ToAddr:    "b@x",
		PublicURL: "http://localhost:8080",
		Ident:     "BA286",
		Kind:      "gate",
		Detail:    "now departs gate B32",
	})
	for _, want := range []string{"Gate change: BA286", "now departs gate B32"} {
		if !strings.Contains(msg, want) {
			t.Errorf("gate-change email missing %q:\n%s", want, msg)
		}
	}
}

// A first gate is announced as an assignment, never as a change.
func TestBuildFlightAlertEmail_GateAssigned(t *testing.T) {
	msg := BuildFlightAlertEmail(FlightAlertInput{
		FromAddr:  "alerts@example.com",
		ToAddr:    "test.user@example.com",
		PublicURL: "http://localhost:8080",
		Ident:     "XX100",
		Kind:      "gate_assigned",
		Detail:    "departs gate A12",
	})
	for _, want := range []string{"Gate assigned: XX100", "Gate assigned: XX100 — departs gate A12."} {
		if !strings.Contains(msg, want) {
			t.Errorf("gate-assigned email missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Gate change") {
		t.Errorf("gate-assigned email reads as a change:\n%s", msg)
	}
}

func TestFlightAlertDetails(t *testing.T) {
	out := time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC)
	in := out.Add(22 * time.Hour)
	cases := []struct {
		name string
		in   FlightAlertInput
		want string
	}{
		{
			name: "route and both times, each on its own clock",
			in: FlightAlertInput{
				Origin: "London Heathrow (LHR)", Dest: "Sydney (SYD)",
				Departs: timing.Block{At: out, TZ: "Europe/London", Source: timing.Confirmed, Place: "London"},
				Arrives: &timing.Block{At: in, TZ: "Australia/Sydney", Source: timing.Confirmed, Place: "Sydney"},
			},
			want: "Flight from London Heathrow (LHR) to Sydney (SYD), departing Mon 1 Mar, 10:00 London (UTC), arriving Tue 2 Mar, 19:00 Sydney (UTC+11).",
		},
		{
			name: "a derived arrival and a recipient at home elsewhere",
			in: FlightAlertInput{
				Departs: timing.Block{At: out, TZ: "Europe/London", Source: timing.Confirmed, Place: "London"},
				Arrives: &timing.Block{At: in, TZ: "Australia/Sydney", Source: timing.Confirmed, Place: "Sydney", Kind: timing.Derived},
				HomeTZ:  "America/New_York",
			},
			want: "Flight departing Mon 1 Mar, 10:00 London (UTC) (05:00 your time), arriving Tue 2 Mar, ≈19:00 Sydney (UTC+11) (based on departure delay) (03:00 your time).",
		},
		{
			name: "origin only, no arrival",
			in:   FlightAlertInput{Origin: "LHR", Departs: timing.Block{At: out}},
			want: "Flight from LHR, departing Mon 1 Mar, 10:00 UTC.",
		},
		{
			name: "destination only",
			in:   FlightAlertInput{Dest: "SYD"},
			want: "Flight to SYD.",
		},
		{
			name: "times without a route",
			in:   FlightAlertInput{Departs: timing.Block{At: out}},
			want: "Flight departing Mon 1 Mar, 10:00 UTC.",
		},
		{
			name: "nothing known",
			in:   FlightAlertInput{},
			want: "",
		},
	}
	for _, c := range cases {
		if got := FlightAlertDetails(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuildFlightAlertEmail_TerminalChangeNamesTheFlight(t *testing.T) {
	out := time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC)
	msg := BuildFlightAlertEmail(FlightAlertInput{
		FromAddr:  "a@x",
		ToAddr:    "b@x",
		PublicURL: "http://localhost:8080",
		Ident:     "BA15",
		Kind:      "terminal",
		Detail:    "now arrives at terminal 1",
		Origin:    "London Heathrow (LHR)",
		Dest:      "Sydney (SYD)",
		Departs:   timing.Block{At: out, TZ: "Europe/London", Source: timing.Confirmed, Place: "London"},
	})
	for _, want := range []string{
		"Terminal change: BA15",
		"Flight from London Heathrow (LHR) to Sydney (SYD), departing Mon 1 Mar, 10:00 London (UTC).",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("terminal-change email missing %q:\n%s", want, msg)
		}
	}
}
