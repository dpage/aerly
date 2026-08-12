package planops_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

// fakeExtractor returns a single canned plan wrapping whatever part it is
// primed with. Copied (trimmed to what's needed here) from the stub in
// internal/planops/propose_test.go: proposeOne below only needs the
// ExtractPlans method, not the body-recording extras that stub also offers.
type fakeExtractor struct {
	plans []planops.ExtractedPlan
}

func (f *fakeExtractor) ExtractPlans(_ context.Context, _ string, _ []planops.Document) ([]planops.ExtractedPlan, error) {
	return f.plans, nil
}

// proposeOne runs planops.Propose over a single extracted part via a stub
// Deps and returns the single resulting proposed part. userID and tripID are
// both 0 (no trip context), so Propose never dereferences the store's pool:
// a *store.Store wrapping a nil pool satisfies the "Store must be non-nil"
// guard without needing a live database.
func proposeOne(t *testing.T, part planops.ExtractedPart) planops.ProposedPart {
	t.Helper()
	deps := planops.Deps{
		Store: store.New(nil),
		Extractor: &fakeExtractor{plans: []planops.ExtractedPlan{{
			Type:  part.Type,
			Parts: []planops.ExtractedPart{part},
		}}},
	}
	plans, err := planops.Propose(context.Background(), deps, 0, 0, "irrelevant body", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Parts) != 1 {
		t.Fatalf("Propose returned %d plans, want 1 with 1 part", len(plans))
	}
	return plans[0].Parts[0]
}

// TestProposeVehicleHireSetsEndsAt checks a vehicle_hire part with a stated
// return instant carries it through into the proposed part's EndsAt, and that
// the hire satellite is populated.
func TestProposeVehicleHireSetsEndsAt(t *testing.T) {
	part := proposeOne(t, planops.ExtractedPart{
		Type: "vehicle_hire", Confidence: "high",
		StartDate: "2026-09-09", StartTime: "15:30",
		EndDate: "2026-09-11", EndTime: "14:00",
		HireCategory: "Standard SUV",
	})
	if part.EndsAt == nil {
		t.Fatal("a hire's return instant must survive into the proposed part")
	}
	if got := part.EndsAt.Format("2006-01-02 15:04"); got != "2026-09-11 14:00" {
		t.Fatalf("EndsAt = %s", got)
	}
	if part.VehicleHire == nil || part.VehicleHire.Category != "Standard SUV" {
		t.Fatalf("satellite = %+v", part.VehicleHire)
	}
}

// TestProposeVehicleHireDefaultsToNineAM checks a hire with no stated pickup
// or return time defaults both to 09:00 (a hire desk opening time), not
// hotel's 15:00/11:00 check-in/out defaults.
func TestProposeVehicleHireDefaultsToNineAM(t *testing.T) {
	part := proposeOne(t, planops.ExtractedPart{
		Type: "vehicle_hire", Confidence: "high",
		StartDate: "2026-09-09", EndDate: "2026-09-11",
	})
	if got := part.StartsAt.Format("15:04"); got != "09:00" {
		t.Fatalf("default pickup = %s, want 09:00", got)
	}
	if got := part.EndsAt.Format("15:04"); got != "09:00" {
		t.Fatalf("default return = %s, want 09:00", got)
	}
}

// TestProposeGroundNowKeepsEndsAt is a regression guard for the live
// data-loss bug: proposePart's ground case set only StartsAt and silently
// discarded the extractor's end instant, so a car hire confirmation ingested
// with its pickup time lost its drop-off entirely.
func TestProposeGroundNowKeepsEndsAt(t *testing.T) {
	part := proposeOne(t, planops.ExtractedPart{
		Type: "ground", Confidence: "high",
		StartDate: "2026-09-09", StartTime: "08:00",
		EndDate: "2026-09-09", EndTime: "09:15",
	})
	if part.EndsAt == nil {
		t.Fatal("a ground transfer's end instant must survive into the proposed part")
	}
}

// TestProposeGroundWithoutEndLeavesEndsAtNil checks the fix does not
// overreach: a transfer with no stated end must not gain a fabricated one.
func TestProposeGroundWithoutEndLeavesEndsAtNil(t *testing.T) {
	part := proposeOne(t, planops.ExtractedPart{
		Type: "ground", Confidence: "high", StartDate: "2026-09-09", StartTime: "08:00",
	})
	if part.EndsAt != nil {
		t.Fatal("a transfer with no stated end must not gain a fabricated one")
	}
}
