package emailingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestIngest_PlanCapture_VehicleHireSatelliteSurvives is the end-to-end
// regression guard for the automated email pipeline's confirm-hop data-loss
// bug: toConfirmInput (ingest.go) built its planops.ConfirmPartInput via an
// explicit field-by-field copy that omitted VehicleHire, so an auto-captured
// hire's satellite never reached planops.Commit even though propose.go and
// commit.go carried it correctly. This runs a real hire email through the
// full maildir -> extract -> propose -> auto-confirm pipeline and reads the
// persisted vehicle_hire_details row back via the store, proving the
// distinctive category actually arrived rather than merely compiling.
func TestIngest_PlanCapture_VehicleHireSatelliteSurvives(t *testing.T) {
	llmResp := `{"plans":[{"type":"vehicle_hire","title":"Hertz hire","confirmation_ref":"VH1","parts":[
		{"type":"vehicle_hire","confidence":"high",
		 "start_date":"2026-09-09","start_time":"15:30","end_date":"2026-09-11","end_time":"14:00",
		 "vehicle_hire":{"category":"Standard SUV, distinctive-marker-9c2e"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	writeMessage(t, h.maildir, "40", goodMessage)
	if state := h.runUntilProcessed(t, "40", 5*time.Second); state != "removed" {
		t.Fatalf("expected removed, got %s", state)
	}

	trips, err := h.store.ListTrips(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("expected 1 auto-created trip, got %d", len(trips))
	}
	plans, err := h.store.PlansByTrip(ctx, trips[0].ID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("PlansByTrip = %d, %v", len(plans), err)
	}
	if plans[0].Type != "vehicle_hire" || plans[0].Source != "email" {
		t.Fatalf("plan = %+v, want vehicle_hire/email", plans[0])
	}
	parts, err := h.store.PartsByPlan(ctx, plans[0].ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	detail, err := h.store.VehicleHireDetailFor(ctx, parts[0].ID)
	if err != nil {
		t.Fatalf("VehicleHireDetailFor: %v", err)
	}
	if detail == nil {
		t.Fatal("vehicle_hire_details row missing: the confirm-hop drop this test guards against")
	}
	if detail.Category != "Standard SUV, distinctive-marker-9c2e" {
		t.Errorf("persisted category = %q, want the distinctive marker to have survived the auto-confirm hop", detail.Category)
	}
}
