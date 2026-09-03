package emailingest

import (
	"context"
	"strings"
	"testing"
)

// TestPlansPromptDrawsHireLineAtOwnership guards the fix for a drive in one's
// own vehicle being classified as 'vehicle_hire'. The prompt used to define
// vehicle_hire as "a self-drive rental the traveller collects and returns
// themselves" and to push only driver-provided journeys to 'ground', so a
// self-driven journey in a vehicle the traveller already owns matched neither
// clause and fell to the "self-drive" half. The dividing line is ownership, not
// who holds the wheel.
func TestPlansPromptDrawsHireLineAtOwnership(t *testing.T) {
	x, l := newExtractor(`{"plans":[]}`)
	if _, err := x.ExtractPlans(context.Background(), "body", nil); err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	p := l.lastPrompt

	for _, want := range []string{
		"already own",
		`is "ground", not "vehicle_hire"`,
		"hire company",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// TestExtractPlansSelfDrivenJourneyStaysGround verifies that a two-leg,
// out-and-back drive parses into two ground parts, each keeping its own start
// and end place, rather than collapsing into a hire's pickup and drop-off.
func TestExtractPlansSelfDrivenJourneyStaysGround(t *testing.T) {
	raw := `{"plans":[{"type":"ground","title":"Driving to the bash",
	  "parts":[
	    {"type":"ground","confidence":"high","start_date":"2026-09-11",
	     "start_label":"Home","end_label":"Test Campsite",
	     "ground":{"provider":"","phone":"","vehicle":"Test Van","driver":"","pax":2}},
	    {"type":"ground","confidence":"high","start_date":"2026-09-14",
	     "start_label":"Test Campsite","end_label":"Home",
	     "ground":{"provider":"","phone":"","vehicle":"Test Van","driver":"","pax":2}}]}]}`
	x, _ := newExtractor(raw)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Parts) != 2 {
		t.Fatalf("got %d plans / %d parts, want 1 plan with 2 parts",
			len(plans), len(plans[0].Parts))
	}
	for i, p := range plans[0].Parts {
		if p.Type != "ground" {
			t.Fatalf("part %d type = %q, want ground", i, p.Type)
		}
	}
	if plans[0].Parts[0].StartLabel != "Home" || plans[0].Parts[0].EndLabel != "Test Campsite" {
		t.Fatalf("outbound = %q -> %q", plans[0].Parts[0].StartLabel, plans[0].Parts[0].EndLabel)
	}
	if plans[0].Parts[1].StartLabel != "Test Campsite" || plans[0].Parts[1].EndLabel != "Home" {
		t.Fatalf("return = %q -> %q", plans[0].Parts[1].StartLabel, plans[0].Parts[1].EndLabel)
	}
}
