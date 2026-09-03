package handlers

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
)

// TestCreateHotelPlanWithKindRoundTrips is an HTTP-level regression guard for
// a bug where hotelDetailReq had no Kind field: decode() calls
// DisallowUnknownFields(), so a create request whose hotel detail block
// carried "kind" (exactly as the client sends once a user picks a kind such
// as "Campsite") was rejected with a 400 before it ever reached the store.
// Every existing kind test called the store directly and so never exercised
// the HTTP decoder, which is how this got through.
func TestCreateHotelPlanWithKindRoundTrips(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2hotelkindcreate", false)
	tid := newTrip(t, e, owner, "Trip-hotel-kind")

	part := map[string]any{
		"type": "hotel", "starts_at": g2planOut, "ends_at": g2planOut.Add(2 * time.Hour),
		"start_label": "Test Campsite", "end_label": "Test Campsite",
		"hotel": map[string]any{"property_name": "Test Campsite", "kind": "Campsite"},
	}
	w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": "hotel", "title": "Test Campsite",
		"parts": []map[string]any{part},
	}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create hotel plan with kind = %d, want 201: %s", w.Code, w.Body.String())
	}
	pid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	getW := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, owner)
	if getW.Code != http.StatusOK {
		t.Fatalf("get trip = %d: %s", getW.Code, getW.Body.String())
	}
	trip := decodeBody[map[string]any](t, getW)
	plansRaw, _ := trip["plans"].([]any)
	var found bool
	for _, pr := range plansRaw {
		p, _ := pr.(map[string]any)
		if int64(p["id"].(float64)) != pid {
			continue
		}
		parts, _ := p["parts"].([]any)
		if len(parts) != 1 {
			t.Fatalf("expected 1 part, got %d", len(parts))
		}
		part, _ := parts[0].(map[string]any)
		hotel, ok := part["hotel"].(map[string]any)
		if !ok {
			t.Fatalf("expected hotel detail in part, got %+v", part)
		}
		if hotel["kind"] != "Campsite" {
			t.Errorf("stored kind = %v, want Campsite", hotel["kind"])
		}
		found = true
	}
	if !found {
		t.Fatalf("created plan %d not found in trip response: %+v", pid, trip)
	}
}

// TestIngestConfirmAccommodationWithKindSucceeds is an HTTP-level regression
// guard for the ingest-confirm path: HotelDetailDTO.Kind deliberately has no
// omitempty, so a propose response always carries "kind" (even "" when
// unset), and the real client's toHotelInput faithfully echoes that field
// back on confirm. Before the fix, hotelDetailReq's DisallowUnknownFields
// rejected the confirm body outright, so confirming ANY accommodation
// proposal was broken. This posts a confirm body with a populated kind,
// exactly as the client does, and asserts it succeeds and the kind persists.
func TestIngestConfirmAccommodationWithKindSucceeds(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2hotelkindconfirm", false)
	tid := newTrip(t, e, owner, "Trip")

	checkin := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 6, 5, 11, 0, 0, 0, time.UTC)
	confirm := map[string]any{
		"plans": []map[string]any{{
			"type": "hotel", "title": "Test Campsite", "confirmation_ref": "H1", "source": "paste",
			"parts": []map[string]any{{
				"type": "hotel", "starts_at": checkin, "ends_at": checkout,
				"start_label": "Test Campsite",
				"hotel":       map[string]any{"property_name": "Test Campsite", "kind": "Campsite"},
			}},
		}},
	}
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), confirm, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm accommodation with kind = %d, want 200: %s", w.Code, w.Body.String())
	}
	plans := decodeBody[[]api.PlanDTO](t, w)
	if len(plans) != 1 || plans[0].Type != "hotel" {
		t.Fatalf("created plans = %+v", plans)
	}
	if len(plans[0].Parts) != 1 || plans[0].Parts[0].Hotel == nil {
		t.Fatalf("created hotel part missing: %+v", plans[0].Parts)
	}
	if got := plans[0].Parts[0].Hotel.Kind; got != "Campsite" {
		t.Errorf("persisted kind = %q, want Campsite", got)
	}
}
