package emailingest

import (
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
	item := planReplyItem(flightPlan(&store.FlightDetail{
		Ident:        "BA171",
		ScheduledOut: time.Date(2026, 7, 20, 15, 40, 0, 0, time.UTC),
		Resolved:     true, // resolver hit; ICAO24 stays nil for a future flight
	}))
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
	item := planReplyItem(flightPlan(&store.FlightDetail{
		Ident:        "BA171",
		ScheduledOut: time.Date(2026, 7, 20, 15, 40, 0, 0, time.UTC),
		Resolved:     false,
	}))
	if !item.ManualNote {
		t.Error("fallback flight should be flagged as from-email (ManualNote=true)")
	}
}
