package planops

import "testing"

// The extractor learned to capture the per-type fields the source states; these
// verify proposePart actually carries them into the satellite it builds, which
// is where they were being dropped. The excursion case is the starkest: it used
// to build an empty ExcursionDetail, so nothing about an excursion was stored
// at all.

func TestProposePartTrainCarriesSeatAndPlatform(t *testing.T) {
	part := ExtractedPart{
		Type: "train", Confidence: "high", StartDate: "2026-06-01",
		Operator: "Test Rail", ServiceNo: "TR100", Class: "Standard",
		Coach: "C", Seat: "42A", Platform: "3",
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Train == nil {
		t.Fatal("train satellite not populated")
	}
	if got.Train.Coach != "C" || got.Train.Seat != "42A" || got.Train.Platform != "3" {
		t.Fatalf("train satellite = %+v", got.Train)
	}
}

func TestProposePartGroundCarriesContactAndPax(t *testing.T) {
	pax := 4
	part := ExtractedPart{
		Type: "ground", Confidence: "high", StartDate: "2026-06-01",
		Provider: "Test Transfers", Phone: "555-0100",
		Vehicle: "Minibus", Driver: "Test Driver", Pax: &pax,
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Ground == nil {
		t.Fatal("ground satellite not populated")
	}
	if got.Ground.Phone != "555-0100" || got.Ground.Driver != "Test Driver" {
		t.Fatalf("ground satellite = %+v", got.Ground)
	}
	if got.Ground.Pax == nil || *got.Ground.Pax != 4 {
		t.Fatalf("pax = %v, want 4", got.Ground.Pax)
	}
}

func TestProposePartDiningCarriesPartySizeAndPhone(t *testing.T) {
	size := 2
	part := ExtractedPart{
		Type: "dining", Confidence: "high", StartDate: "2026-06-01",
		ReservationName: "Test User", Phone: "555-0101", PartySize: &size,
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Dining == nil {
		t.Fatal("dining satellite not populated")
	}
	if got.Dining.Phone != "555-0101" {
		t.Fatalf("dining phone = %q", got.Dining.Phone)
	}
	if got.Dining.PartySize == nil || *got.Dining.PartySize != 2 {
		t.Fatalf("party size = %v, want 2", got.Dining.PartySize)
	}
}

func TestProposePartExcursionCarriesProviderAndTickets(t *testing.T) {
	tickets := 3
	part := ExtractedPart{
		Type: "excursion", Confidence: "high", StartDate: "2026-06-01",
		ExcursionTitle: "Test Walk", Provider: "Test Tours", TicketCount: &tickets,
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Excursion == nil {
		t.Fatal("excursion satellite not populated")
	}
	if got.Excursion.Provider != "Test Tours" {
		t.Fatalf("excursion provider = %q", got.Excursion.Provider)
	}
	if got.Excursion.TicketCount == nil || *got.Excursion.TicketCount != 3 {
		t.Fatalf("ticket count = %v, want 3", got.Excursion.TicketCount)
	}
}

func TestProposePartHotelCarriesGuests(t *testing.T) {
	guests := 2
	part := ExtractedPart{
		Type: "hotel", Confidence: "high", StartDate: "2026-06-01",
		HotelName: "Test Campsite", HotelKind: "Campsite", Guests: &guests,
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Hotel == nil {
		t.Fatal("hotel satellite not populated")
	}
	if got.Hotel.Guests == nil || *got.Hotel.Guests != 2 {
		t.Fatalf("guests = %v, want 2", got.Hotel.Guests)
	}
}

// An unstated count must stay nil all the way into the satellite, so the
// nullable column records "not stated" rather than a fabricated zero.
func TestProposePartLeavesUnstatedCountsNil(t *testing.T) {
	got, _ := proposePart(ctx, Deps{}, ExtractedPart{
		Type: "ground", Confidence: "high", StartDate: "2026-06-01",
		Provider: "Test Transfers",
	})
	if got.Ground == nil {
		t.Fatal("ground satellite not populated")
	}
	if got.Ground.Pax != nil {
		t.Errorf("pax = %v, want nil when the source did not say", *got.Ground.Pax)
	}
}
