package emailingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestIngest_PlanCapture_MeetingSatelliteSurvives and
// TestIngest_PlanCapture_EventSatelliteSurvives are end-to-end regression
// guards for a pre-existing (migration-0046-era, unrelated to vehicle hire)
// confirm-hop data-loss bug: toConfirmInput (ingest.go) built its
// planops.ConfirmPartInput via an explicit field-by-field copy that omitted
// Meeting and Event, so an auto-captured meeting's or event's satellite never
// reached planops.Commit even though propose.go and commit.go carried them
// correctly. These run a real meeting/event email through the full
// maildir -> extract -> propose -> auto-confirm pipeline and read the
// persisted meeting_details/event_details row back via the store, proving
// the distinctive values actually arrived rather than merely compiling.

func TestIngest_PlanCapture_MeetingSatelliteSurvives(t *testing.T) {
	llmResp := `{"plans":[{"type":"meeting","title":"Board sync","confirmation_ref":"M1","parts":[
		{"type":"meeting","confidence":"high","start_date":"2026-09-09","start_time":"10:00",
		 "meeting":{"location":"HQ distinctive-marker-4b1d","organiser":"Priya","platform":"Zoom"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	writeMessage(t, h.maildir, "50", goodMessage)
	if state := h.runUntilProcessed(t, "50", 5*time.Second); state != "removed" {
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
	if plans[0].Type != "meeting" || plans[0].Source != "email" {
		t.Fatalf("plan = %+v, want meeting/email", plans[0])
	}
	parts, err := h.store.PartsByPlan(ctx, plans[0].ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	detail, err := h.store.MeetingDetailFor(ctx, parts[0].ID)
	if err != nil {
		t.Fatalf("MeetingDetailFor: %v", err)
	}
	if detail == nil {
		t.Fatal("meeting_details row missing: the confirm-hop drop this test guards against")
	}
	if detail.Location != "HQ distinctive-marker-4b1d" {
		t.Errorf("persisted location = %q, want the distinctive marker to have survived the auto-confirm hop", detail.Location)
	}
}

func TestIngest_PlanCapture_EventSatelliteSurvives(t *testing.T) {
	llmResp := `{"plans":[{"type":"event","title":"Gig night","confirmation_ref":"E1","parts":[
		{"type":"event","confidence":"high","start_date":"2026-09-09","start_time":"19:30",
		 "event":{"performer":"distinctive-marker-6e7f","category":"Concert","venue_area":"Main Stage","url":"https://tickets.example/e1"}}
	]}]}`
	h := newHarness(t, llmResp, nil, false)
	ctx := context.Background()
	u, _ := h.store.InviteUser(ctx, store.InvitePayload{Username: "alice"})
	if err := h.store.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	writeMessage(t, h.maildir, "51", goodMessage)
	if state := h.runUntilProcessed(t, "51", 5*time.Second); state != "removed" {
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
	if plans[0].Type != "event" || plans[0].Source != "email" {
		t.Fatalf("plan = %+v, want event/email", plans[0])
	}
	parts, err := h.store.PartsByPlan(ctx, plans[0].ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	detail, err := h.store.EventDetailFor(ctx, parts[0].ID)
	if err != nil {
		t.Fatalf("EventDetailFor: %v", err)
	}
	if detail == nil {
		t.Fatal("event_details row missing: the confirm-hop drop this test guards against")
	}
	if detail.Performer != "distinctive-marker-6e7f" {
		t.Errorf("persisted performer = %q, want the distinctive marker to have survived the auto-confirm hop", detail.Performer)
	}
}
