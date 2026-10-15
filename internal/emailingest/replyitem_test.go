package emailingest

import (
	"errors"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

func flightPlan(fd *store.FlightDetail) planops.ProposedPlan {
	return planops.ProposedPlan{
		Type:  "flight",
		Parts: []planops.ProposedPart{{Type: "flight", Flight: fd}},
	}
}

// A flight whose schedule came from the resolver must not be flagged "from the
// email" in the confirmation reply, even though the resolver never populates an
// ICAO24/Mode-S airframe for a future flight: that hex only appears once the
// aircraft is live-tracked close to departure. The manual-note signal is
// FlightDetail.Resolved, not the presence of an airframe.
func TestPlanReplyItem_ResolvedFlightNotManual(t *testing.T) {
	item := planReplyItems(flightPlan(&store.FlightDetail{
		Ident:        "BA171",
		ScheduledOut: time.Date(2026, 7, 20, 15, 40, 0, 0, time.UTC),
		Resolved:     true, // resolver hit; ICAO24 stays nil for a future flight
	}))[0]
	if item.ManualNote {
		t.Error("resolved flight wrongly flagged as from-email (ManualNote=true)")
	}
	if item.Label != "BA171" {
		t.Errorf("label = %q, want BA171", item.Label)
	}
	if item.Detail != "2026-07-20" {
		t.Errorf("detail = %q, want 2026-07-20", item.Detail)
	}
}

// The fallback path (resolver had no record) leaves Resolved=false; that — and
// only that — should set the manual note.
func TestPlanReplyItem_FallbackFlightIsManual(t *testing.T) {
	item := planReplyItems(flightPlan(&store.FlightDetail{
		Ident:        "BA171",
		ScheduledOut: time.Date(2026, 7, 20, 15, 40, 0, 0, time.UTC),
		Resolved:     false,
	}))[0]
	if !item.ManualNote {
		t.Error("fallback flight should be flagged as from-email (ManualNote=true)")
	}
}

// A booking with several flight legs lists every leg in the reply, not just the
// first one (issue #128), each with its own manual-note flag.
func TestPlanReplyItems_MultiLegFlightListsEveryLeg(t *testing.T) {
	p := planops.ProposedPlan{
		Type: "flight",
		Parts: []planops.ProposedPart{
			{Type: "flight", Flight: &store.FlightDetail{
				Ident: "BA171", ScheduledOut: time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC), Resolved: true}},
			{Type: "flight", Flight: &store.FlightDetail{
				Ident: "AA100", ScheduledOut: time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC), Resolved: false}},
		},
	}
	items := planReplyItems(p)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	if items[0].Label != "BA171" || items[0].ManualNote {
		t.Errorf("first leg = %+v", items[0])
	}
	if items[1].Label != "AA100" || items[1].Detail != "2026-07-20" || !items[1].ManualNote {
		t.Errorf("second leg = %+v", items[1])
	}

	fails := planReplyFailures(p, errors.New("commit failed"))
	if len(fails) != 2 || fails[0].Label != "BA171" || fails[1].Label != "AA100" {
		t.Errorf("failures = %+v, want one per leg", fails)
	}
}
