package handlers

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/planops"
)

// TestIngestProposeAndConfirm_VehicleHire is the end-to-end regression guard
// for two data-loss bugs on the LLM/text ingest path:
//  1. toProposedPlanDTO (the propose-preview projection) called
//     api.ToPlanPartDTO with the vehicleHire positional argument hardcoded to
//     nil instead of part.VehicleHire, so the review screen the user sees
//     before confirming showed a hire with no details.
//  2. ingestTripConfirm's toConfirmPlanInput built its planops.ConfirmPartInput
//     via an explicit field-by-field copy that omitted VehicleHire, so a
//     confirmed hire's satellite never reached planops.Commit even though
//     propose.go and commit.go carried it correctly.
//
// This exercises the real HTTP propose -> confirm round trip, asserting the
// satellite on the propose response (bug 1) and then reading the created
// plan back from the store via a.planDTO (bug 2), not just an echo of the
// request body, so a passing test proves the value actually arrives at both
// hops rather than merely that the code compiles.
func TestIngestProposeAndConfirm_VehicleHire(t *testing.T) {
	e := setup(t, nil, nil)
	e.api.Extractor = &fakeIngestExtractor{plans: []planops.ExtractedPlan{{
		Type: "vehicle_hire", Title: "Hertz hire", ConfirmationRef: "VH1",
		Parts: []planops.ExtractedPart{{
			Type: "vehicle_hire", Confidence: "high",
			StartDate: "2026-09-09", StartTime: "15:30",
			EndDate: "2026-09-11", EndTime: "14:00",
			HireCategory: "Standard SUV, distinctive-marker-7f3a",
		}},
	}}}
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")

	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), map[string]any{"text": "hire", "source": "paste"}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("propose = %d: %s", w.Code, w.Body.String())
	}
	res := decodeBody[api.IngestResultDTO](t, w)
	if len(res.Proposals) != 1 || res.Proposals[0].Type != "vehicle_hire" {
		t.Fatalf("proposals = %+v", res.Proposals)
	}
	if len(res.Proposals[0].Parts) != 1 || res.Proposals[0].Parts[0].VehicleHire == nil {
		t.Fatalf("proposed vehicle_hire part missing satellite (the propose-preview drop this test guards against): %+v", res.Proposals[0].Parts)
	}
	if got := res.Proposals[0].Parts[0].VehicleHire.Category; got != "Standard SUV, distinctive-marker-7f3a" {
		t.Fatalf("proposed category = %q, want the distinctive marker to appear in the review screen", got)
	}

	// Confirm it, exactly as the FE would after the user reviews the proposal.
	pickup := time.Date(2026, 9, 9, 15, 30, 0, 0, time.UTC)
	dropoff := time.Date(2026, 9, 11, 14, 0, 0, 0, time.UTC)
	confirm := map[string]any{
		"plans": []map[string]any{{
			"type": "vehicle_hire", "title": "Hertz hire", "confirmation_ref": "VH1", "source": "paste",
			"parts": []map[string]any{{
				"type": "vehicle_hire", "starts_at": pickup, "ends_at": dropoff,
				"vehicle_hire": map[string]any{"category": "Standard SUV, distinctive-marker-7f3a"},
			}},
		}},
	}
	w = e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), confirm, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm = %d: %s", w.Code, w.Body.String())
	}
	plans := decodeBody[[]api.PlanDTO](t, w)
	if len(plans) != 1 || plans[0].Type != "vehicle_hire" {
		t.Fatalf("created plans = %+v", plans)
	}
	if len(plans[0].Parts) != 1 || plans[0].Parts[0].VehicleHire == nil {
		t.Fatalf("created vehicle_hire part missing satellite (the confirm-hop drop this test guards against): %+v", plans[0].Parts)
	}
	if got := plans[0].Parts[0].VehicleHire.Category; got != "Standard SUV, distinctive-marker-7f3a" {
		t.Errorf("persisted category = %q, want the distinctive marker to have survived the confirm hop", got)
	}
}
