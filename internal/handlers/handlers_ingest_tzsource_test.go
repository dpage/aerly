package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
)

// TestToProposedPlanDTOZoneSources: an LLM proposal's sources reach the JSON the
// confirm dialog reads, under start_tz_source / end_tz_source.
func TestToProposedPlanDTOZoneSources(t *testing.T) {
	end := time.Date(2026, 7, 3, 11, 0, 0, 0, time.UTC)
	dto := toProposedPlanDTO(planops.ProposedPlan{Type: "hotel", Parts: []planops.ProposedPart{{
		Type: "hotel", StartsAt: time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC), EndsAt: &end,
		StartTZSource: "floating", EndTZSource: "floating",
	}}})
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, `"start_tz_source":"floating"`) || !strings.Contains(s, `"end_tz_source":"floating"`) {
		t.Errorf("JSON lacks the sources: %s", s)
	}
}

// TestICalProposalsZoneSources: an uploaded .ics previewed in the dialog keeps
// the importer's sources, so a zone-less UTC instant stays unknown on confirm.
func TestICalProposalsZoneSources(t *testing.T) {
	ics := "BEGIN:VCALENDAR\nPRODID:-//Test//Synthetic//EN\nBEGIN:VEVENT\nUID:bus-1@kayak.com\n" +
		"DTSTART:20260701T140000Z\nDTEND:20260701T160000Z\nSUMMARY:Bus Test Lines 100\n" +
		`DESCRIPTION:Test Lines Bus Number 100\n\nView trip: https://www.kayak.com/trips/!TESTBUS?ref=calendar` +
		"\nEND:VEVENT\nEND:VCALENDAR\n"
	out, ok := icalProposals([]byte(ics))
	if !ok || len(out.Proposals) != 1 {
		t.Fatalf("icalProposals ok=%v proposals=%d, want one", ok, len(out.Proposals))
	}
	p := out.Proposals[0].Parts[0]
	if p.StartTZSource != "unknown" || p.EndTZSource != "unknown" {
		t.Errorf("sources = %q/%q, want unknown/unknown", p.StartTZSource, p.EndTZSource)
	}
}
