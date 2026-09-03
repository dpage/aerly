package emailingest

import (
	"context"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/planops"
)

// TestPlansPromptAsksForEveryStatedField guards the instruction that tells the
// model to fill the per-type fields the source states rather than only the ones
// that identify the booking. Several satellite columns (a train's coach and
// seat, a transfer's driver and head count, a restaurant's party size, an
// excursion's operator and ticket count) existed in the schema for a long time
// whilst the extractor never populated any of them.
func TestPlansPromptAsksForEveryStatedField(t *testing.T) {
	x, l := newExtractor(`{"plans":[]}`)
	if _, err := x.ExtractPlans(context.Background(), "body", nil); err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}

	// The guidance itself, and each newly advertised schema key. Asserting the
	// keys as well as the prose catches a schema line that drifts away from the
	// struct we unmarshal into.
	for _, want := range []string{
		"not merely the ones needed to identify the booking",
		"never guess a default of 1",
		`"coach"`, `"seat"`, `"platform"`,
		`"driver"`, `"pax"`,
		`"party_size"`, `"ticket_count"`, `"guests"`,
	} {
		if !strings.Contains(l.lastPrompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// TestExtractPlansFillsTrainDetailFields verifies coach, seat and platform
// survive extraction; they are printed on most tickets and were previously
// discarded.
func TestExtractPlansFillsTrainDetailFields(t *testing.T) {
	raw := `{"plans":[{"type":"train","title":"Test Rail",
	  "parts":[{"type":"train","confidence":"high","start_date":"2026-06-01",
	    "train":{"operator":"Test Rail","service_no":"TR100","class":"Standard",
	      "coach":"C","seat":"42A","platform":"3"}}]}]}`
	p := onlyPart(t, raw)
	if p.Coach != "C" || p.Seat != "42A" || p.Platform != "3" {
		t.Fatalf("train fields = coach %q, seat %q, platform %q", p.Coach, p.Seat, p.Platform)
	}
}

// TestExtractPlansFillsGroundDetailFields verifies a transfer's contact number,
// named driver and head count survive extraction.
func TestExtractPlansFillsGroundDetailFields(t *testing.T) {
	raw := `{"plans":[{"type":"ground","title":"Test Transfers",
	  "parts":[{"type":"ground","confidence":"high","start_date":"2026-06-01",
	    "ground":{"provider":"Test Transfers","phone":"555-0100",
	      "vehicle":"Minibus","driver":"Test Driver","pax":4}}]}]}`
	p := onlyPart(t, raw)
	if p.Provider != "Test Transfers" || p.Phone != "555-0100" {
		t.Fatalf("provider/phone = %q / %q", p.Provider, p.Phone)
	}
	if p.Vehicle != "Minibus" || p.Driver != "Test Driver" {
		t.Fatalf("vehicle/driver = %q / %q", p.Vehicle, p.Driver)
	}
	if p.Pax == nil || *p.Pax != 4 {
		t.Fatalf("pax = %v, want 4", p.Pax)
	}
}

// TestExtractPlansFillsDiningAndExcursionFields covers the two remaining
// satellites. The excursion one matters most: propose built an entirely empty
// ExcursionDetail before this, so nothing about an excursion was ever stored.
func TestExtractPlansFillsDiningAndExcursionFields(t *testing.T) {
	raw := `{"plans":[{"type":"dining","title":"Test Bistro",
	  "parts":[{"type":"dining","confidence":"high","start_date":"2026-06-01",
	    "dining":{"reservation_name":"Test User","phone":"555-0101","party_size":2}}]}]}`
	p := onlyPart(t, raw)
	if p.ReservationName != "Test User" || p.Phone != "555-0101" {
		t.Fatalf("dining name/phone = %q / %q", p.ReservationName, p.Phone)
	}
	if p.PartySize == nil || *p.PartySize != 2 {
		t.Fatalf("party_size = %v, want 2", p.PartySize)
	}

	raw = `{"plans":[{"type":"excursion","title":"Test Walk",
	  "parts":[{"type":"excursion","confidence":"high","start_date":"2026-06-01",
	    "excursion":{"title":"Test Walk","provider":"Test Tours","ticket_count":3}}]}]}`
	p = onlyPart(t, raw)
	if p.ExcursionTitle != "Test Walk" || p.Provider != "Test Tours" {
		t.Fatalf("excursion title/provider = %q / %q", p.ExcursionTitle, p.Provider)
	}
	if p.TicketCount == nil || *p.TicketCount != 3 {
		t.Fatalf("ticket_count = %v, want 3", p.TicketCount)
	}
}

// TestExtractPlansFillsHotelGuests verifies the guest count survives, and is
// the one hotel satellite column the extractor previously ignored.
func TestExtractPlansFillsHotelGuests(t *testing.T) {
	raw := `{"plans":[{"type":"hotel","title":"Test Campsite",
	  "parts":[{"type":"hotel","confidence":"high","start_date":"2026-06-01",
	    "hotel":{"property_name":"Test Campsite","kind":"Campsite","guests":2}}]}]}`
	p := onlyPart(t, raw)
	if p.Guests == nil || *p.Guests != 2 {
		t.Fatalf("guests = %v, want 2", p.Guests)
	}
}

// TestExtractPlansCountsDistinguishUnstatedFromZero is the point of the
// pointers: the columns behind these counts are nullable so that "the source
// didn't say" and a stated zero stay different facts. A negative count is
// nonsense from the model and is dropped rather than stored.
func TestExtractPlansCountsDistinguishUnstatedFromZero(t *testing.T) {
	// Omitted entirely, and explicitly null, both mean "not stated".
	for _, raw := range []string{
		`{"plans":[{"type":"ground","parts":[{"type":"ground","confidence":"high",
		   "start_date":"2026-06-01","ground":{"provider":"Test Transfers"}}]}]}`,
		`{"plans":[{"type":"ground","parts":[{"type":"ground","confidence":"high",
		   "start_date":"2026-06-01","ground":{"provider":"Test Transfers","pax":null}}]}]}`,
	} {
		if p := onlyPart(t, raw); p.Pax != nil {
			t.Errorf("pax = %v, want nil for an unstated count", *p.Pax)
		}
	}

	// A stated zero is a real answer and must survive as one, not become nil.
	raw := `{"plans":[{"type":"dining","parts":[{"type":"dining","confidence":"high",
	  "start_date":"2026-06-01","dining":{"party_size":0}}]}]}`
	if p := onlyPart(t, raw); p.PartySize == nil || *p.PartySize != 0 {
		t.Errorf("party_size = %v, want a stored zero", p.PartySize)
	}

	// A negative count is not a fact about the world; drop it.
	raw = `{"plans":[{"type":"excursion","parts":[{"type":"excursion","confidence":"high",
	  "start_date":"2026-06-01","excursion":{"ticket_count":-2}}]}]}`
	if p := onlyPart(t, raw); p.TicketCount != nil {
		t.Errorf("ticket_count = %v, want nil for a negative count", *p.TicketCount)
	}
}

// onlyPart runs the extractor over a canned response that describes exactly one
// plan with one part, and returns that part.
func onlyPart(t *testing.T, raw string) planops.ExtractedPart {
	t.Helper()
	x, _ := newExtractor(raw)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Parts) != 1 {
		t.Fatalf("got %d plans, want exactly one with one part", len(plans))
	}
	return plans[0].Parts[0]
}
