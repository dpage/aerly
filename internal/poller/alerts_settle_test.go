package poller

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// pollStep is one poll's post-refresh view of a flight in a table-driven
// alert sequence, and what that poll should announce.
type pollStep struct {
	originGate, destGate         string
	originTerminal, destTerminal string
	status                       string
	stale                        bool // a tracking tick with no fresh provider report
	wantKind                     string
	wantDetail                   string
}

// TestEvaluateAlertSequences drives evaluateAlert through successive polls the
// way refresh does: each poll's prev is the previous poll's cur, since the
// store already holds what the last poll persisted. That is the flow the
// terminal confirmation has to survive, because by the second poll the new
// terminal is in both snapshots.
func TestEvaluateAlertSequences(t *testing.T) {
	far := 6 * time.Hour     // well outside the quiet period
	near := 20 * time.Minute // inside it
	cases := []struct {
		name  string
		depIn time.Duration // scheduled departure relative to now
		start pollStep      // the state on file before the first poll
		polls []pollStep
	}{
		{
			name:  "origin gate first assigned",
			depIn: far,
			polls: []pollStep{{originGate: "A12", wantKind: "gate_assigned", wantDetail: "departs gate A12"}},
		},
		{
			name:  "destination gate first assigned",
			depIn: far,
			polls: []pollStep{{destGate: "B12", wantKind: "gate_assigned", wantDetail: "arrives at gate B12"}},
		},
		{
			name:  "origin gate moves",
			depIn: far,
			start: pollStep{originGate: "A12"},
			polls: []pollStep{{originGate: "D70", wantKind: "gate", wantDetail: "now departs gate D70"}},
		},
		{
			name:  "destination gate moves",
			depIn: far,
			start: pollStep{destGate: "B12"},
			polls: []pollStep{{destGate: "B14", wantKind: "gate", wantDetail: "now arrives at gate B14"}},
		},
		{
			name:  "a move at one end leads over an assignment at the other",
			depIn: far,
			start: pollStep{destGate: "B12"},
			polls: []pollStep{{originGate: "A12", destGate: "B14", wantKind: "gate", wantDetail: "now arrives at gate B14"}},
		},
		{
			name:  "gate assigned inside the quiet period still alerts",
			depIn: near,
			polls: []pollStep{{originGate: "A12", wantKind: "gate_assigned", wantDetail: "departs gate A12"}},
		},
		{
			name:  "origin terminal change confirmed on the second poll",
			depIn: far,
			start: pollStep{originTerminal: "5"},
			polls: []pollStep{
				{originTerminal: "3"},
				{originTerminal: "3", wantKind: "terminal", wantDetail: "now departs terminal 3"},
				{originTerminal: "3"},
			},
		},
		{
			name:  "destination terminal change confirmed on the second poll",
			depIn: far,
			start: pollStep{destTerminal: "2"},
			polls: []pollStep{
				{destTerminal: "1"},
				{destTerminal: "1", wantKind: "terminal", wantDetail: "now arrives at terminal 1"},
			},
		},
		{
			name:  "terminal flicker and back sends nothing",
			depIn: far,
			start: pollStep{originTerminal: "5"},
			polls: []pollStep{
				{originTerminal: "3"},
				{originTerminal: "5"},
				{originTerminal: "5"},
			},
		},
		{
			name:  "terminal moving on again restarts confirmation against the stable value",
			depIn: far,
			start: pollStep{originTerminal: "5"},
			polls: []pollStep{
				{originTerminal: "3"},
				{originTerminal: "4"},
				{originTerminal: "4", wantKind: "terminal", wantDetail: "now departs terminal 4"},
			},
		},
		{
			name:  "a tick with no fresh report neither confirms nor drops a pending terminal",
			depIn: far,
			start: pollStep{originTerminal: "5"},
			polls: []pollStep{
				{originTerminal: "3"},
				{originTerminal: "3", stale: true},
				{originTerminal: "3", wantKind: "terminal", wantDetail: "now departs terminal 3"},
			},
		},
		{
			name:  "a terminal edited between reports is not announced",
			depIn: far,
			start: pollStep{originTerminal: "5"},
			polls: []pollStep{{originTerminal: "3", stale: true}, {originTerminal: "3"}},
		},
		{
			name:  "a terminal briefly missing keeps the pending change",
			depIn: far,
			start: pollStep{destTerminal: "2"},
			polls: []pollStep{
				{destTerminal: "1"},
				{destTerminal: ""},
				{destTerminal: "1", wantKind: "terminal", wantDetail: "now arrives at terminal 1"},
			},
		},
		{
			name:  "terminal first filled in is not a change",
			depIn: far,
			polls: []pollStep{{originTerminal: "5"}, {originTerminal: "5"}},
		},
		{
			name:  "origin terminal change inside the quiet period with the gate unchanged",
			depIn: near,
			start: pollStep{originGate: "A12", originTerminal: "5"},
			polls: []pollStep{
				{originGate: "A12", originTerminal: "1"},
				{originGate: "A12", originTerminal: "1"},
			},
		},
		{
			name:  "destination terminal change inside the quiet period with the gate unchanged",
			depIn: near,
			start: pollStep{destTerminal: "2"},
			polls: []pollStep{
				{destTerminal: "1"},
				{destTerminal: "1"},
			},
		},
		{
			name:  "terminal and gate change together inside the quiet period: the gate alerts",
			depIn: near,
			start: pollStep{originGate: "A12", originTerminal: "5"},
			polls: []pollStep{
				{originGate: "A10", originTerminal: "1", wantKind: "gate", wantDetail: "now departs gate A10"},
			},
		},
		{
			name:  "terminal pending when the quiet period starts is dropped",
			depIn: 31 * time.Minute, // first poll just outside; the clock below moves into it
			start: pollStep{originTerminal: "5"},
			polls: []pollStep{
				{originTerminal: "1"},
				{originTerminal: "1"},
			},
		},
		{
			name:  "departed flight sends no terminal alert",
			depIn: far, // timetable says later, but the flight has gone
			start: pollStep{status: "Enroute", destTerminal: "2"},
			polls: []pollStep{
				{status: "Enroute", destTerminal: "1"},
				{status: "Enroute", destTerminal: "1"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Poller{}
			now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
			dep := now.Add(tc.depIn)
			mk := func(s pollStep) *store.Flight {
				status := s.status
				if status == "" {
					status = "Scheduled"
				}
				return &store.Flight{
					Ident: "XX100", Status: status, ScheduledOut: dep,
					OriginGate: s.originGate, DestGate: s.destGate,
					OriginTerminal: s.originTerminal, DestTerminal: s.destTerminal,
				}
			}
			prev := mk(tc.start)
			for i, step := range tc.polls {
				cur := mk(step)
				kind, detail, _ := p.evaluateAlert(1, prev, cur, !step.stale, now)
				if kind != step.wantKind || detail != step.wantDetail {
					t.Fatalf("poll %d: got (%q, %q), want (%q, %q)", i+1, kind, detail, step.wantKind, step.wantDetail)
				}
				prev = cur
				now = now.Add(2 * time.Minute)
			}
		})
	}
}

// TestEvaluateAlert_PendingTerminalKeepsStableValueInSignature: a poll that
// alerts on something else whilst a terminal change is still unconfirmed must
// stamp the old terminal into the dedupe signature, or the confirmed terminal
// alert on the next poll would carry the same signature and be swallowed.
func TestEvaluateAlert_PendingTerminalKeepsStableValueInSignature(t *testing.T) {
	p := &Poller{}
	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	dep := now.Add(6 * time.Hour)
	prev := &store.Flight{Ident: "XX100", Status: "Scheduled", ScheduledOut: dep, OriginTerminal: "5", DestTerminal: "2"}
	cur := &store.Flight{Ident: "XX100", Status: "Scheduled", ScheduledOut: dep, OriginTerminal: "3", DestTerminal: "4", OriginGate: "A12"}

	kind, _, sigSt := p.evaluateAlert(1, prev, cur, true, now)
	if kind != "gate_assigned" {
		t.Fatalf("first poll kind = %q, want gate_assigned", kind)
	}
	first := alertSignature(sigSt)
	if sigSt.originTerminal != "5" || sigSt.destTerminal != "2" {
		t.Fatalf("unconfirmed terminals leaked into the signature: %q", first)
	}

	kind, _, sigSt = p.evaluateAlert(1, cur, cur, true, now.Add(2*time.Minute))
	if kind != "terminal" {
		t.Fatalf("second poll kind = %q, want terminal", kind)
	}
	if second := alertSignature(sigSt); second == first {
		t.Fatalf("confirmed terminal signature %q matches the earlier one", second)
	}
}

// TestInQuietPeriod pins the window edges: departure measured from the revised
// off-block time when there is one, else the timetable.
func TestInQuietPeriod(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	sched := now.Add(20 * time.Minute)
	late := now.Add(2 * time.Hour)
	cases := []struct {
		name string
		f    *store.Flight
		want bool
	}{
		{"timetable inside the window", &store.Flight{ScheduledOut: sched}, true},
		{"revised departure pushes it out of the window", &store.Flight{ScheduledOut: sched, EstimatedOut: &late}, false},
		{"exactly at the window edge", &store.Flight{ScheduledOut: now.Add(terminalQuietPeriod)}, true},
		{"just before the window", &store.Flight{ScheduledOut: now.Add(terminalQuietPeriod + time.Minute)}, false},
		{"departure time already passed", &store.Flight{ScheduledOut: now.Add(-time.Hour)}, true},
	}
	for _, tc := range cases {
		if got := inQuietPeriod(tc.f, now); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
