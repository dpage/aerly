package emailingest

import (
	"context"
	"strings"
	"testing"
)

// TestPlansPromptCoversAllAccommodation verifies the prompt tells the model that
// 'hotel' is every sort of place to sleep, and asks it to record which sort.
func TestPlansPromptCoversAllAccommodation(t *testing.T) {
	x, l := newExtractor(`{"plans":[]}`)
	if _, err := x.ExtractPlans(context.Background(), "body", nil); err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	p := l.lastPrompt

	for _, want := range []string{"campsite", "caravan park", `"kind"`} {
		if !strings.Contains(strings.ToLower(p), strings.ToLower(want)) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// TestExtractPlansHotelKind verifies the accommodation kind survives extraction.
func TestExtractPlansHotelKind(t *testing.T) {
	raw := `{"plans":[{"type":"hotel","title":"Test Campsite",
	  "parts":[{"type":"hotel","confidence":"high",
	    "start_date":"2026-09-11","end_date":"2026-09-14",
	    "hotel":{"property_name":"Test Campsite","address":"","phone":"",
	      "room_type":"Grass pitch","kind":"Campsite"}}]}]}`
	x, _ := newExtractor(raw)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Parts) != 1 {
		t.Fatalf("got %d plans", len(plans))
	}
	if got := plans[0].Parts[0].HotelKind; got != "Campsite" {
		t.Fatalf("HotelKind = %q, want %q", got, "Campsite")
	}
}
