package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/store"
)

func TestToPlanPartDTO_FlightTZFallbackAndEffectiveAt(t *testing.T) {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	est := out.Add(30 * time.Minute)
	part := &store.PlanPart{ID: 1, PlanID: 2, Type: "flight", StartsAt: out, EndsAt: &in}
	flight := &store.FlightDetail{
		Ident: "BA117", OriginIATA: "LHR", DestIATA: "JFK",
		ScheduledOut: out, ScheduledIn: in, EstimatedOut: &est,
	}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Empty part TZs fall back to the airport zones. (arg order: flight,hotel,train,ground,dining,excursion,iceCream,meeting,event,latest,track)
	if dto.StartTZ != "Europe/London" {
		t.Errorf("start_tz = %q, want Europe/London", dto.StartTZ)
	}
	if dto.EndTZ != "America/New_York" {
		t.Errorf("end_tz = %q, want America/New_York", dto.EndTZ)
	}
	if dto.StartTZSource != "confirmed" || dto.EndTZSource != "confirmed" {
		t.Errorf("fallback sources = %q/%q, want confirmed/confirmed", dto.StartTZSource, dto.EndTZSource)
	}
	// effective_at uses the flight's effective departure (estimated over scheduled).
	if !dto.EffectiveAt.Equal(est) {
		t.Errorf("effective_at = %v, want %v (estimated out)", dto.EffectiveAt, est)
	}
	if dto.Flight == nil || dto.Flight.Ident != "BA117" {
		t.Errorf("flight detail not projected: %+v", dto.Flight)
	}
}

func TestToPlanPartDTO_StoredTZNotOverwritten(t *testing.T) {
	part := &store.PlanPart{
		Type: "flight", StartsAt: time.Now(),
		StartTZ: "Asia/Tokyo", EndTZ: "Asia/Singapore",
		StartTZSource: "inferred", EndTZSource: "unknown",
	}
	flight := &store.FlightDetail{Ident: "X", OriginIATA: "LHR", DestIATA: "JFK"}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if dto.StartTZ != "Asia/Tokyo" || dto.EndTZ != "Asia/Singapore" {
		t.Errorf("stored TZs overwritten: start=%q end=%q", dto.StartTZ, dto.EndTZ)
	}
	if dto.StartTZSource != "inferred" || dto.EndTZSource != "unknown" {
		t.Errorf("stored sources overwritten: %q/%q", dto.StartTZSource, dto.EndTZSource)
	}
}

func TestToPlanPartDTO_NonFlightTypesSelectTheRightDetail(t *testing.T) {
	now := time.Now()
	base := func(typ string) *store.PlanPart {
		return &store.PlanPart{Type: typ, StartsAt: now}
	}

	if d := ToPlanPartDTO(base("hotel"), nil, &store.HotelDetail{PropertyName: "H"}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil); d.Hotel == nil || d.Hotel.PropertyName != "H" {
		t.Error("hotel detail not projected")
	}
	if d := ToPlanPartDTO(base("train"), nil, nil, &store.TrainDetail{Operator: "Eurostar"}, nil, nil, nil, nil, nil, nil, nil, nil, nil); d.Train == nil || d.Train.Operator != "Eurostar" {
		t.Error("train detail not projected")
	}
	if d := ToPlanPartDTO(base("ground"), nil, nil, nil, &store.GroundDetail{Provider: "Uber"}, nil, nil, nil, nil, nil, nil, nil, nil); d.Ground == nil || d.Ground.Provider != "Uber" {
		t.Error("ground detail not projected")
	}
	if d := ToPlanPartDTO(base("dining"), nil, nil, nil, nil, &store.DiningDetail{ReservationName: "R"}, nil, nil, nil, nil, nil, nil, nil); d.Dining == nil || d.Dining.ReservationName != "R" {
		t.Error("dining detail not projected")
	}
	if d := ToPlanPartDTO(base("excursion"), nil, nil, nil, nil, nil, &store.ExcursionDetail{Provider: "Tour"}, nil, nil, nil, nil, nil, nil); d.Excursion == nil || d.Excursion.Provider != "Tour" {
		t.Error("excursion detail not projected")
	}
	if d := ToPlanPartDTO(base("ice_cream"), nil, nil, nil, nil, nil, nil, &store.IceCreamDetail{Rating: 5, WhatOrdered: "Pistachio"}, nil, nil, nil, nil, nil); d.IceCream == nil || d.IceCream.Rating != 5 || d.IceCream.WhatOrdered != "Pistachio" {
		t.Error("ice_cream detail not projected")
	}
	// Non-flight effective_at is just StartsAt.
	d := ToPlanPartDTO(base("dining"), nil, nil, nil, nil, &store.DiningDetail{}, nil, nil, nil, nil, nil, nil, nil)
	if !d.EffectiveAt.Equal(now) {
		t.Errorf("non-flight effective_at = %v, want StartsAt %v", d.EffectiveAt, now)
	}
}

func TestToFlightDetailDTO_PositionAndTrack(t *testing.T) {
	d := &store.FlightDetail{Ident: "BA1", OriginIATA: "LHR", DestIATA: "JFK"}
	latest := &store.Position{Ts: time.Now(), Lat: 51.5, Lon: -0.1}
	track := []*store.Position{
		{Ts: time.Now(), Lat: 51.5, Lon: -0.1},
		{Ts: time.Now(), Lat: 52.0, Lon: -1.0},
	}
	dto := ToFlightDetailDTO(d, latest, track, "", "", "", "")
	if dto.LatestPosition == nil {
		t.Error("latest position not projected")
	}
	if len(dto.Track) != 2 {
		t.Errorf("track len = %d, want 2", len(dto.Track))
	}
	// No position/track → both nil/empty.
	bare := ToFlightDetailDTO(d, nil, nil, "", "", "", "")
	if bare.LatestPosition != nil || bare.Track != nil {
		t.Error("bare flight should carry no position or track")
	}
}

func TestToTripDTO_NilSlicesAndDateFormatting(t *testing.T) {
	starts := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	tr := &store.Trip{ID: 1, Name: "Trip", StartsOn: &starts, EndsOn: &ends}
	dto := ToTripDTO(tr, "owner", nil, nil)

	if dto.Members == nil || len(dto.Members) != 0 {
		t.Errorf("nil members should project as empty slice, got %v", dto.Members)
	}
	if dto.Tags == nil || len(dto.Tags) != 0 {
		t.Errorf("nil tags should project as empty slice, got %v", dto.Tags)
	}
	if dto.MyRole != "owner" {
		t.Errorf("my_role = %q", dto.MyRole)
	}
	if dto.StartsOn == nil || *dto.StartsOn != "2026-07-01" {
		t.Errorf("starts_on = %v, want 2026-07-01", dto.StartsOn)
	}
	if dto.EndsOn == nil || *dto.EndsOn != "2026-07-08" {
		t.Errorf("ends_on = %v, want 2026-07-08", dto.EndsOn)
	}
}

func TestToTripMemberDTO(t *testing.T) {
	m := &store.TripMember{UserID: 7, Role: "editor"}
	dto := ToTripMemberDTO(m)
	if dto.UserID != 7 || dto.Role != "editor" {
		t.Errorf("unexpected member dto: %+v", dto)
	}
}

func TestToCalendarTokenDTO_EmbedsURLAndScope(t *testing.T) {
	tok := &store.CalendarToken{
		Scope: "trip", ResourceID: 42, Token: "secret-abc",
		CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	}
	url := "https://aerly.example/api/calendar/trip/42.ics?token=secret-abc"
	dto := ToCalendarTokenDTO(tok, url)
	if dto.Scope != "trip" || dto.ResourceID != 42 {
		t.Errorf("scope/resource mismapped: %+v", dto)
	}
	if dto.Token != "secret-abc" || dto.URL != url {
		t.Errorf("token/url mismapped: %+v", dto)
	}
	if dto.CreatedAt != "2026-06-01T10:00:00Z" {
		t.Errorf("created_at = %q", dto.CreatedAt)
	}
}

func TestToUserEmailDTO(t *testing.T) {
	at := time.Now()
	e := &store.UserEmail{ID: 3, Address: "a@b.com", Verified: true, VerifiedAt: &at, CreatedAt: at, IsPrimary: true}
	dto := ToUserEmailDTO(e)
	if dto.ID != 3 || dto.Address != "a@b.com" || !dto.Verified || dto.VerifiedAt == nil || !dto.IsPrimary {
		t.Errorf("unexpected user-email dto: %+v", dto)
	}
}

func TestToFlightAlertDTO(t *testing.T) {
	a := store.FlightAlert{
		ID: 5, PlanPartID: 1, PlanID: 2, TripID: 3,
		Ident: "BA286", Kind: "gate", Status: "Scheduled", Message: "gate B32",
		CreatedAt: time.Now(),
	}
	dto := ToFlightAlertDTO(a)
	if dto.ID != 5 || dto.Ident != "BA286" || dto.Kind != "gate" || dto.Message != "gate B32" {
		t.Errorf("unexpected alert dto: %+v", dto)
	}
	if dto.ReadAt != nil {
		t.Error("unread alert should have nil read_at")
	}
}

// A floating flight time shown with an airport zone keeps its floating
// source, since its digits have not been anchored in that zone.
func TestToPlanPartDTO_FlightTZFallbackKeepsFloating(t *testing.T) {
	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	part := &store.PlanPart{
		Type: "flight", StartsAt: at, EndsAt: &at,
		StartTZSource: "floating", EndTZSource: "unknown",
	}
	flight := &store.FlightDetail{Ident: "XX100", OriginIATA: "LHR", DestIATA: "JFK"}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if dto.StartTZ != "Europe/London" || dto.StartTZSource != "floating" {
		t.Errorf("start = %q/%q, want Europe/London/floating", dto.StartTZ, dto.StartTZSource)
	}
	if dto.EndTZ != "America/New_York" || dto.EndTZSource != "confirmed" {
		t.Errorf("end = %q/%q, want America/New_York/confirmed", dto.EndTZ, dto.EndTZSource)
	}
}

func TestPlanPartTimeBlocks_FlightWithLateDeparture(t *testing.T) {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	est := out.Add(40 * time.Minute)
	part := &store.PlanPart{ID: 1, PlanID: 2, Type: "flight", StartsAt: out, EndsAt: &in}
	flight := &store.FlightDetail{
		Ident: "XX100", OriginIATA: "LHR", DestIATA: "JFK",
		ScheduledOut: out, ScheduledIn: in, EstimatedOut: &est,
	}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	lhr, _ := airports.LookupCity("LHR")
	jfk, _ := airports.LookupCity("JFK")
	dep := dto.Flight.Departure
	if !dep.At.Equal(est) || dep.TZ != "Europe/London" || dep.TZSource != "confirmed" ||
		dep.Place != lhr || dep.Kind != "estimated" || dep.DelayMin != 40 {
		t.Errorf("departure block = %+v", dep)
	}
	arr := dto.Flight.Arrival
	if arr == nil {
		t.Fatal("arrival block missing")
	}
	if !arr.At.Equal(in.Add(40*time.Minute)) || arr.TZ != "America/New_York" || arr.TZSource != "confirmed" ||
		arr.Place != jfk || arr.Kind != "derived" || arr.DelayMin != 40 {
		t.Errorf("arrival block = %+v", arr)
	}
	if !sameBlock(dto.Start, dep) || dto.End == nil || !sameBlock(*dto.End, *arr) {
		t.Errorf("part start/end %+v / %+v differ from flight blocks", dto.Start, dto.End)
	}
	if !dto.EffectiveAt.Equal(est) {
		t.Errorf("effective_at = %v, want %v", dto.EffectiveAt, est)
	}
}

func TestPlanPartTimeBlocks_DepartureOnlyFlightOmitsArrival(t *testing.T) {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	part := &store.PlanPart{ID: 1, PlanID: 2, Type: "flight", StartsAt: out}
	flight := &store.FlightDetail{Ident: "XX100", OriginIATA: "LHR", DestIATA: "JFK", ScheduledOut: out}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if dto.Flight.Arrival != nil || dto.End != nil {
		t.Errorf("arrival/end = %+v / %+v, want nil", dto.Flight.Arrival, dto.End)
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	if !strings.Contains(js, `"start":{`) || !strings.Contains(js, `"departure":{`) {
		t.Errorf("JSON lacks start/departure blocks: %s", js)
	}
	if strings.Contains(js, `"arrival"`) || strings.Contains(js, `"end":`) {
		t.Errorf("JSON should omit arrival and end: %s", js)
	}
}

// sameBlock compares blocks, using Equal for the instant.
func sameBlock(a, b TimeBlockDTO) bool {
	return a.At.Equal(b.At) && a.TZ == b.TZ && a.TZSource == b.TZSource &&
		a.Place == b.Place && a.Kind == b.Kind && a.DelayMin == b.DelayMin
}

func TestPlanPartTimeBlocks_FloatingFlightEndHasNoZone(t *testing.T) {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	part := &store.PlanPart{Type: "flight", StartsAt: out, StartTZSource: "floating"}
	flight := &store.FlightDetail{Ident: "XX100", OriginIATA: "LHR", DestIATA: "JFK", ScheduledOut: out}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if dto.Start.TZ != "" || dto.Start.Place != "" || dto.Start.TZSource != "floating" {
		t.Errorf("start = %+v, want no zone or place and floating source", dto.Start)
	}
	// The part-level raw fields keep their airport fallback.
	if dto.StartTZ != "Europe/London" || dto.StartTZSource != "floating" {
		t.Errorf("raw start zone = %q/%q", dto.StartTZ, dto.StartTZSource)
	}
}

// Only a floating end's timetable-based times (scheduled, and derived from
// the timetable) are wall-clock digits; a provider estimate or actual on the
// same part is a true instant, so it keeps the airport's zone and place.
func TestPlanPartTimeBlocks_FloatingFlightProviderTimesKeepZone(t *testing.T) {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(7 * time.Hour)
	estOut := out.Add(time.Hour)
	actIn := in.Add(30 * time.Minute)
	part := &store.PlanPart{Type: "flight", StartsAt: out, EndsAt: &in, StartTZSource: "floating", EndTZSource: "floating"}
	flight := &store.FlightDetail{Ident: "XX100", OriginIATA: "LHR", DestIATA: "JFK",
		ScheduledOut: out, ScheduledIn: in, EstimatedOut: &estOut, ActualIn: &actIn}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	lhr, _ := airports.LookupCity("LHR")
	jfk, _ := airports.LookupCity("JFK")
	if dto.Start.Kind != "estimated" || dto.Start.TZ != "Europe/London" || dto.Start.Place != lhr {
		t.Errorf("start = %+v, want the estimate quoted at %s", dto.Start, lhr)
	}
	if dto.End == nil || dto.End.Kind != "actual" || dto.End.TZ != "America/New_York" || dto.End.Place != jfk {
		t.Errorf("end = %+v, want the actual quoted at %s", dto.End, jfk)
	}

	// The same part with only a departure estimate derives its arrival from
	// the timetable, so that end is digits again and gets no zone.
	flight.ActualIn = nil
	dto = ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if dto.End == nil || dto.End.Kind != "derived" || dto.End.TZ != "" || dto.End.Place != "" {
		t.Errorf("derived end = %+v, want no zone or place", dto.End)
	}
}

func TestPlanPartTimeBlocks_FlightEndWithBlankZoneUsesAirport(t *testing.T) {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	part := &store.PlanPart{Type: "flight", StartsAt: out}
	flight := &store.FlightDetail{Ident: "XX100", OriginIATA: "LHR", DestIATA: "JFK", ScheduledOut: out}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	lhr, _ := airports.LookupCity("LHR")
	if dto.Start.TZ != "Europe/London" || dto.Start.TZSource != "confirmed" || dto.Start.Place != lhr {
		t.Errorf("start = %+v, want airport zone, confirmed, %q", dto.Start, lhr)
	}
}

func TestPlanPartTimeBlocks_Hotel(t *testing.T) {
	in := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	outT := time.Date(2026, 6, 3, 11, 0, 0, 0, time.UTC)
	part := &store.PlanPart{
		Type: "hotel", StartsAt: in, EndsAt: &outT,
		StartTZ: "Europe/Paris", StartTZSource: "confirmed",
		EndTZ: "Europe/Paris", EndTZSource: "inferred",
	}
	dto := ToPlanPartDTO(part, nil, &store.HotelDetail{PropertyName: "Test Hotel"}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	want := TimeBlockDTO{At: in, TZ: "Europe/Paris", TZSource: "confirmed", Place: "Paris", Kind: "scheduled"}
	if !sameBlock(dto.Start, want) {
		t.Errorf("start = %+v, want %+v", dto.Start, want)
	}
	if dto.End == nil || !dto.End.At.Equal(outT) || dto.End.TZSource != "inferred" ||
		dto.End.Place != "Paris" || dto.End.Kind != "scheduled" {
		t.Errorf("end = %+v", dto.End)
	}
	if dto.Flight != nil {
		t.Error("hotel should carry no flight block")
	}
}

func TestPlanPartTimeBlocks_FloatingPartHasNoZoneOrPlace(t *testing.T) {
	at := time.Date(2026, 6, 1, 19, 0, 0, 0, time.UTC)
	part := &store.PlanPart{Type: "dining", StartsAt: at, StartTZSource: "floating"}
	dto := ToPlanPartDTO(part, nil, nil, nil, nil, &store.DiningDetail{}, nil, nil, nil, nil, nil, nil, nil)
	if dto.Start.TZ != "" || dto.Start.Place != "" || dto.Start.TZSource != "floating" {
		t.Errorf("floating start = %+v", dto.Start)
	}
	if dto.End != nil {
		t.Errorf("end = %+v, want nil", dto.End)
	}
}
