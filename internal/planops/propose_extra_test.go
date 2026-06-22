package planops

import (
	"errors"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

// TestConfidenceScore covers every branch of the high/medium/low/default map.
func TestConfidenceScore(t *testing.T) {
	cases := map[string]float64{
		"high":    0.95,
		"HIGH":    0.95,
		"medium":  0.6,
		"low":     0.3,
		"":        0.6, // default
		"unknown": 0.6, // default
	}
	for in, want := range cases {
		if got := confidenceScore(in); got != want {
			t.Errorf("confidenceScore(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestProposePartHotel: a hotel part fills the satellite, defaults the start
// label to the property name, and uses the check-in/out date+time.
func TestProposePartHotel(t *testing.T) {
	part := ExtractedPart{
		Type: "hotel", Confidence: "high",
		StartDate: "2026-06-01", StartTime: "14:00",
		EndDate: "2026-06-05", EndTime: "10:00",
		HotelName: "The Synthetic Arms", Address: "1 Example Street",
		Phone: "555-0100", RoomType: "Double",
	}
	got, conf := proposePart(ctx, Deps{}, part)
	if conf != 0.95 {
		t.Errorf("conf = %v, want 0.95", conf)
	}
	if got.Hotel == nil || got.Hotel.PropertyName != "The Synthetic Arms" {
		t.Fatalf("hotel satellite not populated: %+v", got.Hotel)
	}
	if got.StartLabel != "The Synthetic Arms" {
		t.Errorf("StartLabel = %q, want the property name", got.StartLabel)
	}
	if got.EndsAt == nil {
		t.Error("a hotel with a check-out date should have EndsAt set")
	}
}

// TestProposePartHotelNoEndDate: a hotel without a check-out date has no EndsAt
// and, with no name, no defaulted label.
func TestProposePartHotelNoEndDate(t *testing.T) {
	part := ExtractedPart{Type: "hotel", Confidence: "medium", StartDate: "2026-06-01"}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.EndsAt != nil {
		t.Errorf("EndsAt = %v, want nil (no check-out date)", got.EndsAt)
	}
	if got.StartLabel != "" {
		t.Errorf("StartLabel = %q, want empty (no property name)", got.StartLabel)
	}
}

// TestProposePartTrain: a train part fills the satellite and end time. The
// end-date falls back to the start date when only an end time is given.
func TestProposePartTrain(t *testing.T) {
	part := ExtractedPart{
		Type: "train", Confidence: "high",
		StartDate: "2026-06-01", StartTime: "08:00", EndTime: "11:30",
		Operator: "Synthetic Rail", ServiceNo: "SR100", Class: "Standard",
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Train == nil || got.Train.Operator != "Synthetic Rail" {
		t.Fatalf("train satellite not populated: %+v", got.Train)
	}
	if got.EndsAt == nil {
		t.Fatal("a train with an end time should have EndsAt")
	}
	// End date should fall back to the start date (same day).
	if got.EndsAt.UTC().Format("2006-01-02") != "2026-06-01" {
		t.Errorf("end date = %s, want it to fall back to the start date", got.EndsAt.UTC().Format("2006-01-02"))
	}
}

// TestProposePartTrainExplicitEndDate exercises the branch where the train
// carries its own end date.
func TestProposePartTrainExplicitEndDate(t *testing.T) {
	part := ExtractedPart{
		Type: "train", Confidence: "high",
		StartDate: "2026-06-01", StartTime: "23:00",
		EndDate: "2026-06-02", EndTime: "06:00",
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.EndsAt == nil || got.EndsAt.UTC().Format("2006-01-02") != "2026-06-02" {
		t.Errorf("end date should honour the explicit EndDate, got %v", got.EndsAt)
	}
}

// TestProposePartDining defaults the dining start time to 19:00.
func TestProposePartDining(t *testing.T) {
	part := ExtractedPart{Type: "dining", Confidence: "high", StartDate: "2026-06-01", ReservationName: "Doe"}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Dining == nil || got.Dining.ReservationName != "Doe" {
		t.Fatalf("dining satellite not populated: %+v", got.Dining)
	}
	if got.StartsAt.UTC().Hour() != 19 {
		t.Errorf("dining default hour = %d, want 19", got.StartsAt.UTC().Hour())
	}
}

// TestProposePartExcursion defaults the start label to the excursion title.
func TestProposePartExcursion(t *testing.T) {
	part := ExtractedPart{Type: "excursion", Confidence: "high", StartDate: "2026-06-01", ExcursionTitle: "City Walk"}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Excursion == nil {
		t.Fatal("excursion satellite not populated")
	}
	if got.StartLabel != "City Walk" {
		t.Errorf("StartLabel = %q, want the excursion title", got.StartLabel)
	}
}

// TestProposePartMeeting defaults the start label to the meeting location.
func TestProposePartMeeting(t *testing.T) {
	part := ExtractedPart{
		Type: "meeting", Confidence: "high", StartDate: "2026-06-01",
		MeetingLocation: "Room 1", MeetingOrganiser: "Test User", MeetingPlatform: "Zoom",
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Meeting == nil || got.Meeting.Organiser != "Test User" {
		t.Fatalf("meeting satellite not populated: %+v", got.Meeting)
	}
	if got.StartLabel != "Room 1" {
		t.Errorf("StartLabel = %q, want the meeting location", got.StartLabel)
	}
}

// TestProposePartEvent defaults the start label to the performer.
func TestProposePartEvent(t *testing.T) {
	part := ExtractedPart{
		Type: "event", Confidence: "high", StartDate: "2026-06-01",
		EventPerformer: "The Synthetics", EventCategory: "Music",
		EventVenueArea: "Stalls", EventURL: "https://example.com/e",
	}
	got, _ := proposePart(ctx, Deps{}, part)
	if got.Event == nil || got.Event.Performer != "The Synthetics" {
		t.Fatalf("event satellite not populated: %+v", got.Event)
	}
	if got.StartLabel != "The Synthetics" {
		t.Errorf("StartLabel = %q, want the performer", got.StartLabel)
	}
}

// TestCombineLocal covers the blank-date (zero), happy, and date-only fallback
// paths.
func TestCombineLocal(t *testing.T) {
	if !combineLocal("", "", 9).IsZero() {
		t.Error("blank date should yield the zero time")
	}
	got := combineLocal("2026-06-01", "09:30", 0)
	if got.UTC().Format("2006-01-02T15:04") != "2026-06-01T09:30" {
		t.Errorf("combineLocal happy path = %v", got)
	}
	// A bare default hour fills the time.
	defaulted := combineLocal("2026-06-01", "", 7)
	if defaulted.UTC().Hour() != 7 {
		t.Errorf("default hour not applied: %v", defaulted)
	}
}

// TestFlightFromLegOvernightRollover: a stated arrival time at/before departure
// with no arrival date is rolled forward a day (overnight flight).
func TestFlightFromLegOvernightRollover(t *testing.T) {
	leg := FlightFields{
		Ident: "ZZ1", Date: "2026-06-01", OriginIATA: "LHR", DestIATA: "SIN",
		DepartTimeLocal: "23:10", ArriveTimeLocal: "06:30", // no ArriveDate → overnight
	}
	fd := flightFromLeg(leg, "ZZ1")
	if !fd.ScheduledIn.After(fd.ScheduledOut) {
		t.Errorf("overnight arrival not rolled forward: out=%v in=%v", fd.ScheduledOut, fd.ScheduledIn)
	}
}

// TestFlightFromLegNoTimes: with no departure/arrival times, ScheduledIn equals
// ScheduledOut (the date-only fallback) and never lands before it.
func TestFlightFromLegNoTimes(t *testing.T) {
	leg := FlightFields{Ident: "ZZ1", Date: "2026-06-01", OriginIATA: "LHR", DestIATA: "JFK"}
	fd := flightFromLeg(leg, "ZZ1")
	if fd.ScheduledIn.Before(fd.ScheduledOut) {
		t.Errorf("arrival before departure: out=%v in=%v", fd.ScheduledOut, fd.ScheduledIn)
	}
}

// TestEnrichFlightResolverICAO: a resolved flight carrying an ICAO24 records it
// on the FlightDetail.
func TestEnrichFlightResolverICAO(t *testing.T) {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)
	resolver := &fakeFlightResolver{rf: &providers.ResolvedFlight{
		Ident: "TP123", ScheduledOut: out, ScheduledIn: in,
		OriginIATA: "LHR", DestIATA: "JFK", ICAO24: "ABC123",
	}}
	deps := Deps{Resolver: resolver}
	part := ExtractedPart{Type: "flight", Confidence: "high",
		Flight: FlightFields{Ident: "TP123", Date: "2026-06-01"}}
	got, _ := proposePart(ctx, deps, part)
	if got.Flight == nil || got.Flight.ICAO24 == nil || *got.Flight.ICAO24 != "ABC123" {
		t.Errorf("ICAO24 not propagated from resolver: %+v", got.Flight)
	}
}

// TestProposeErrors covers the nil-Store, nil-Extractor, and extractor-error
// guards.
func TestProposeErrors(t *testing.T) {
	if _, err := Propose(ctx, Deps{}, 0, 0, "", nil); err == nil {
		t.Error("nil Store should error")
	}
	if _, err := Propose(ctx, Deps{Store: &store.Store{}}, 0, 0, "", nil); err == nil {
		t.Error("nil Extractor should error")
	}
	fx := &fakeExtractor{err: errors.New("boom")}
	if _, err := Propose(ctx, Deps{Store: &store.Store{}, Extractor: fx}, 0, 0, "", nil); err == nil {
		t.Error("extractor error should propagate")
	}
}

// TestProposeDropsEmptyPlan: a plan whose only part has an unknown type yields
// no usable part and is dropped from the output. tripID==0 also exercises the
// no-candidate-loading path.
func TestProposeDropsEmptyPlan(t *testing.T) {
	fx := &fakeExtractor{plans: []ExtractedPlan{{
		Type:  "mystery",
		Parts: []ExtractedPart{}, // no parts at all → dropped
	}}}
	props, err := Propose(ctx, Deps{Store: &store.Store{}, Extractor: fx}, 0, 0, "body", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(props) != 0 {
		t.Errorf("a part-less plan should be dropped, got %d proposals", len(props))
	}
}
