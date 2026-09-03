package store

import (
	"testing"
	"time"
)

// TestHotelDetailKindRoundTrip verifies the accommodation kind survives a
// create, a read back, and an update, so a campsite stays a campsite.
func TestHotelDetailKindRoundTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	start := time.Date(2026, 9, 9, 15, 0, 0, 0, time.UTC)

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Test Campsite",
		Parts: []CreatePlanPartPayload{{
			StartsAt: start,
			Hotel:    &HotelDetail{PropertyName: "Test Campsite", Kind: "Campsite"},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	partID := parts[0].ID

	// 1 & 2: create carries Kind, and a read back returns it.
	hd, err := s.HotelDetailFor(ctx, partID)
	if err != nil || hd == nil {
		t.Fatalf("HotelDetailFor: %v, %v", hd, err)
	}
	if hd.Kind != "Campsite" {
		t.Fatalf("Kind after create = %q, want %q", hd.Kind, "Campsite")
	}

	// 3: an update changes it.
	newKind := "Caravan park"
	if err := s.UpdateHotelDetail(ctx, partID, HotelDetailUpdate{Kind: &newKind}); err != nil {
		t.Fatalf("UpdateHotelDetail: %v", err)
	}
	hd, err = s.HotelDetailFor(ctx, partID)
	if err != nil || hd == nil {
		t.Fatalf("HotelDetailFor after update: %v, %v", hd, err)
	}
	if hd.Kind != "Caravan park" {
		t.Fatalf("Kind after update = %q, want %q", hd.Kind, "Caravan park")
	}

	// 4: a nil Kind on update leaves the existing value alone (COALESCE path).
	if err := s.UpdateHotelDetail(ctx, partID, HotelDetailUpdate{}); err != nil {
		t.Fatalf("UpdateHotelDetail with nil Kind: %v", err)
	}
	hd, err = s.HotelDetailFor(ctx, partID)
	if err != nil || hd == nil {
		t.Fatalf("HotelDetailFor after nil update: %v, %v", hd, err)
	}
	if hd.Kind != "Caravan park" {
		t.Fatalf("Kind after nil update = %q, want unchanged %q", hd.Kind, "Caravan park")
	}
}
