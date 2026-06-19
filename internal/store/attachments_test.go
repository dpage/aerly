package store

import (
	"errors"
	"testing"
	"time"
)

func TestPlanAttachmentsCRUD(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)
	tripID := seedTrip(t, s, uid)
	planID, _ := seedPlanWithPart(t, s, tripID, uid, "hotel", time.Now())

	// Empty to start.
	atts, err := s.AttachmentsByPlan(ctx, planID)
	if err != nil {
		t.Fatalf("AttachmentsByPlan: %v", err)
	}
	if len(atts) != 0 {
		t.Fatalf("new plan has %d attachments, want 0", len(atts))
	}

	a1, err := s.CreatePlanAttachment(ctx, CreatePlanAttachmentPayload{
		PlanID:      planID,
		UploadedBy:  &uid,
		Filename:    "ticket.pdf",
		ContentType: "application/pdf",
		SizeBytes:   1234,
		StorageKey:  "ab/cd/" + repeat32("a"),
	})
	if err != nil {
		t.Fatalf("CreatePlanAttachment: %v", err)
	}
	if a1.ID == 0 || a1.CreatedAt.IsZero() {
		t.Fatalf("create did not populate id/created_at: %+v", a1)
	}

	a2, err := s.CreatePlanAttachment(ctx, CreatePlanAttachmentPayload{
		PlanID:     planID,
		Filename:   "voucher.png",
		SizeBytes:  10,
		StorageKey: "ef/01/" + repeat32("b"),
	})
	if err != nil {
		t.Fatalf("CreatePlanAttachment 2: %v", err)
	}
	if a2.UploadedBy != nil {
		t.Errorf("UploadedBy should be nil when omitted, got %v", *a2.UploadedBy)
	}

	// Listing returns both, newest first (a2 then a1).
	atts, err = s.AttachmentsByPlan(ctx, planID)
	if err != nil {
		t.Fatalf("AttachmentsByPlan: %v", err)
	}
	if len(atts) != 2 || atts[0].ID != a2.ID || atts[1].ID != a1.ID {
		t.Fatalf("listing order wrong: %+v", atts)
	}

	// Fetch by id.
	got, err := s.PlanAttachmentByID(ctx, a1.ID)
	if err != nil {
		t.Fatalf("PlanAttachmentByID: %v", err)
	}
	if got.Filename != "ticket.pdf" || got.SizeBytes != 1234 || got.StorageKey != a1.StorageKey {
		t.Fatalf("fetched attachment mismatch: %+v", got)
	}

	// Storage keys helper (for blob sweeps).
	keys, err := s.StorageKeysByPlan(ctx, planID)
	if err != nil {
		t.Fatalf("StorageKeysByPlan: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("StorageKeysByPlan returned %d keys, want 2", len(keys))
	}

	// Delete one.
	if err := s.DeletePlanAttachment(ctx, a1.ID); err != nil {
		t.Fatalf("DeletePlanAttachment: %v", err)
	}
	if _, err := s.PlanAttachmentByID(ctx, a1.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted attachment fetch err = %v, want ErrNotFound", err)
	}
	if err := s.DeletePlanAttachment(ctx, a1.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-delete err = %v, want ErrNotFound", err)
	}

	// Deleting the plan cascades the remaining attachment row away.
	if err := s.DeletePlan(ctx, planID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	atts, err = s.AttachmentsByPlan(ctx, planID)
	if err != nil {
		t.Fatalf("AttachmentsByPlan after plan delete: %v", err)
	}
	if len(atts) != 0 {
		t.Fatalf("attachments survived plan delete: %+v", atts)
	}
}

func repeat32(c string) string {
	out := ""
	for i := 0; i < 32; i++ {
		out += c
	}
	return out
}
