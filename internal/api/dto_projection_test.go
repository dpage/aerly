package api

import (
	"testing"
	"time"

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
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil)

	// Empty part TZs fall back to the airport zones.
	if dto.StartTZ != "Europe/London" {
		t.Errorf("start_tz = %q, want Europe/London", dto.StartTZ)
	}
	if dto.EndTZ != "America/New_York" {
		t.Errorf("end_tz = %q, want America/New_York", dto.EndTZ)
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
	}
	flight := &store.FlightDetail{Ident: "X", OriginIATA: "LHR", DestIATA: "JFK"}
	dto := ToPlanPartDTO(part, flight, nil, nil, nil, nil, nil, nil, nil, nil)
	if dto.StartTZ != "Asia/Tokyo" || dto.EndTZ != "Asia/Singapore" {
		t.Errorf("stored TZs overwritten: start=%q end=%q", dto.StartTZ, dto.EndTZ)
	}
}

func TestToPlanPartDTO_NonFlightTypesSelectTheRightDetail(t *testing.T) {
	now := time.Now()
	base := func(typ string) *store.PlanPart {
		return &store.PlanPart{Type: typ, StartsAt: now}
	}

	if d := ToPlanPartDTO(base("hotel"), nil, &store.HotelDetail{PropertyName: "H"}, nil, nil, nil, nil, nil, nil, nil); d.Hotel == nil || d.Hotel.PropertyName != "H" {
		t.Error("hotel detail not projected")
	}
	if d := ToPlanPartDTO(base("train"), nil, nil, &store.TrainDetail{Operator: "Eurostar"}, nil, nil, nil, nil, nil, nil); d.Train == nil || d.Train.Operator != "Eurostar" {
		t.Error("train detail not projected")
	}
	if d := ToPlanPartDTO(base("ground"), nil, nil, nil, &store.GroundDetail{Provider: "Uber"}, nil, nil, nil, nil, nil); d.Ground == nil || d.Ground.Provider != "Uber" {
		t.Error("ground detail not projected")
	}
	if d := ToPlanPartDTO(base("dining"), nil, nil, nil, nil, &store.DiningDetail{ReservationName: "R"}, nil, nil, nil, nil); d.Dining == nil || d.Dining.ReservationName != "R" {
		t.Error("dining detail not projected")
	}
	if d := ToPlanPartDTO(base("excursion"), nil, nil, nil, nil, nil, &store.ExcursionDetail{Provider: "Tour"}, nil, nil, nil); d.Excursion == nil || d.Excursion.Provider != "Tour" {
		t.Error("excursion detail not projected")
	}
	if d := ToPlanPartDTO(base("ice_cream"), nil, nil, nil, nil, nil, nil, &store.IceCreamDetail{Rating: 5, WhatOrdered: "Pistachio"}, nil, nil); d.IceCream == nil || d.IceCream.Rating != 5 || d.IceCream.WhatOrdered != "Pistachio" {
		t.Error("ice_cream detail not projected")
	}
	// Non-flight effective_at is just StartsAt.
	d := ToPlanPartDTO(base("dining"), nil, nil, nil, nil, &store.DiningDetail{}, nil, nil, nil, nil)
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
	dto := ToFlightDetailDTO(d, latest, track)
	if dto.LatestPosition == nil {
		t.Error("latest position not projected")
	}
	if len(dto.Track) != 2 {
		t.Errorf("track len = %d, want 2", len(dto.Track))
	}
	// No position/track → both nil/empty.
	bare := ToFlightDetailDTO(d, nil, nil)
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
	e := &store.UserEmail{ID: 3, Address: "a@b.com", Verified: true, VerifiedAt: &at, CreatedAt: at}
	dto := ToUserEmailDTO(e)
	if dto.ID != 3 || dto.Address != "a@b.com" || !dto.Verified || dto.VerifiedAt == nil {
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
