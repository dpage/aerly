package store

import (
	"errors"
	"testing"
	"time"
)

func TestResolveSource(t *testing.T) {
	cases := []struct {
		tz, src, want string
		err           bool
	}{
		{"Europe/Paris", "", "confirmed", false},
		{"", "", "floating", false}, // today's meaning, for callers not yet passing a source
		{"Europe/Paris", "inferred", "inferred", false},
		{"", "unknown", "unknown", false},
		{"", "confirmed", "", true},
		{"Europe/Paris", "floating", "", true},
		{"Mars/Olympus", "confirmed", "", true},
		{"Europe/Paris", "guessed", "", true},
	}
	for _, c := range cases {
		got, err := ResolveSource(c.tz, c.src)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("ResolveSource(%q,%q) = %q,%v", c.tz, c.src, got, err)
		}
		if c.err && !errors.Is(err, ErrInvalidZone) {
			t.Errorf("want ErrInvalidZone, got %v", err)
		}
	}
}

func strp(s string) *string { return &s }

// mkTZPart creates a hotel plan with one part and returns the part and its
// owner.
func mkTZPart(t *testing.T, s *Store, in CreatePlanPartPayload) (*PlanPart, int64) {
	t.Helper()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	in.Hotel = &HotelDetail{}
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "hotel", Title: "Test Hotel",
		Parts: []CreatePlanPartPayload{in},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan: %v (%d parts)", err, len(parts))
	}
	return parts[0], owner
}

func TestCreatePlanPartTZSource(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	start := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)

	p, _ := mkTZPart(t, s, CreatePlanPartPayload{StartsAt: start, EndsAt: &end, StartTZ: "Europe/Paris"})
	if p.StartTZSource != "confirmed" || p.EndTZSource != "floating" {
		t.Errorf("sources = %q/%q, want confirmed/floating", p.StartTZSource, p.EndTZSource)
	}

	// No end at all: the end source is unknown rather than floating.
	p, _ = mkTZPart(t, s, CreatePlanPartPayload{StartsAt: start})
	if p.StartTZSource != "floating" || p.EndTZSource != "unknown" {
		t.Errorf("no-end sources = %q/%q, want floating/unknown", p.StartTZSource, p.EndTZSource)
	}

	// Explicit sources are kept.
	p, viewer := mkTZPart(t, s, CreatePlanPartPayload{StartsAt: start, EndsAt: &end,
		StartTZ: "Asia/Tokyo", StartTZSource: "inferred", EndTZSource: "unknown"})
	if p.StartTZSource != "inferred" || p.EndTZSource != "unknown" {
		t.Errorf("explicit sources = %q/%q, want inferred/unknown", p.StartTZSource, p.EndTZSource)
	}

	// The scan in ListVisiblePlanParts carries the sources too.
	visible, err := s.ListVisiblePlanParts(ctx, viewer, ListVisiblePlanPartsOpts{})
	if err != nil {
		t.Fatalf("ListVisiblePlanParts: %v", err)
	}
	found := false
	for _, v := range visible {
		if v.ID == p.ID {
			found = true
			if v.StartTZSource != "inferred" || v.EndTZSource != "unknown" {
				t.Errorf("listed sources = %q/%q", v.StartTZSource, v.EndTZSource)
			}
		}
	}
	if !found {
		t.Error("part missing from ListVisiblePlanParts")
	}

	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	for _, bad := range []CreatePlanPartPayload{
		{StartsAt: start, StartTZ: "Nope/Nope"},
		{StartsAt: start, EndsAt: &end, EndTZSource: "confirmed"},
	} {
		bad.Hotel = &HotelDetail{}
		if _, err := s.CreatePlan(ctx, CreatePlanPayload{TripID: trip, Type: "hotel",
			Parts: []CreatePlanPartPayload{bad}}, owner); !errors.Is(err, ErrInvalidZone) {
			t.Errorf("CreatePlan(%+v) = %v, want ErrInvalidZone", bad, err)
		}
	}
}

func TestUpdatePlanPartTZSource(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	start := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	p, _ := mkTZPart(t, s, CreatePlanPartPayload{StartsAt: start, EndsAt: &end,
		StartTZ: "Europe/Paris", EndTZ: "Europe/Paris"})

	upd, err := s.UpdatePlanPart(ctx, p.ID, UpdatePlanPartPayload{StartTZ: strp(""), StartTZSource: strp("unknown")})
	if err != nil {
		t.Fatalf("clear zone: %v", err)
	}
	if upd.StartTZ != "" || upd.StartTZSource != "unknown" {
		t.Errorf("cleared start = %q/%q, want ''/unknown", upd.StartTZ, upd.StartTZSource)
	}
	if upd.EndTZ != "Europe/Paris" || upd.EndTZSource != "confirmed" {
		t.Errorf("end changed: %q/%q", upd.EndTZ, upd.EndTZSource)
	}

	if _, err := s.UpdatePlanPart(ctx, p.ID, UpdatePlanPartPayload{StartTZ: strp("Nope/Nope")}); !errors.Is(err, ErrInvalidZone) {
		t.Errorf("invalid zone = %v, want ErrInvalidZone", err)
	}
	// A source that contradicts the current zone is refused too.
	if _, err := s.UpdatePlanPart(ctx, p.ID, UpdatePlanPartPayload{EndTZSource: strp("floating")}); !errors.Is(err, ErrInvalidZone) {
		t.Errorf("contradicting source = %v, want ErrInvalidZone", err)
	}

	upd, err = s.UpdatePlanPart(ctx, p.ID, UpdatePlanPartPayload{StartTZ: strp("Asia/Tokyo")})
	if err != nil {
		t.Fatalf("set zone: %v", err)
	}
	if upd.StartTZ != "Asia/Tokyo" || upd.StartTZSource != "confirmed" {
		t.Errorf("set start = %q/%q, want Asia/Tokyo/confirmed", upd.StartTZ, upd.StartTZSource)
	}

	upd, err = s.UpdatePlanPart(ctx, p.ID, UpdatePlanPartPayload{StartTZSource: strp("inferred")})
	if err != nil {
		t.Fatalf("set source: %v", err)
	}
	if upd.StartTZ != "Asia/Tokyo" || upd.StartTZSource != "inferred" {
		t.Errorf("source-only = %q/%q, want Asia/Tokyo/inferred", upd.StartTZ, upd.StartTZSource)
	}

	// Clearing the end zone with no source keeps the legacy floating meaning.
	upd, err = s.UpdatePlanPart(ctx, p.ID, UpdatePlanPartPayload{EndTZ: strp("")})
	if err != nil {
		t.Fatalf("clear end: %v", err)
	}
	if upd.EndTZ != "" || upd.EndTZSource != "floating" {
		t.Errorf("cleared end = %q/%q, want ''/floating", upd.EndTZ, upd.EndTZSource)
	}

	// A zone edit on a missing part reports not found.
	if _, err := s.UpdatePlanPart(ctx, 999999, UpdatePlanPartPayload{StartTZ: strp("Asia/Tokyo")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing part = %v, want ErrNotFound", err)
	}
	if !(UpdatePlanPartPayload{}).IsEmpty() || (UpdatePlanPartPayload{EndTZSource: strp("unknown")}).IsEmpty() {
		t.Error("IsEmpty should account for the source fields")
	}
}

func TestUpdateFlightPartRouteTZSource(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(11 * time.Hour)
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "ZZ100",
		Parts: []CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "AAA", EndLabel: "BBB",
			Flight: &FlightDetail{Ident: "ZZ100", ScheduledOut: out, ScheduledIn: in},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, _ := s.PartsByPlan(ctx, plan.ID)
	pid := parts[0].ID

	// The update carries both instants, as a provider resolve does, so the
	// sources are taken as sent (TestUpdateFlightPartRouteAnchorsFloating
	// covers an update with no time for a floating end).
	if err := s.UpdateFlightPartRoute(ctx, pid, FlightRouteUpdate{
		StartsAt: &out, EndsAt: &in,
		StartTZ: strp("Europe/London"), EndTZ: strp(""), EndTZSource: strp("unknown"),
	}); err != nil {
		t.Fatalf("UpdateFlightPartRoute: %v", err)
	}
	got, _ := s.PlanPartByID(ctx, pid)
	if got.StartTZ != "Europe/London" || got.StartTZSource != "confirmed" || got.EndTZSource != "unknown" {
		t.Errorf("after route edit = %q/%q end %q/%q", got.StartTZ, got.StartTZSource, got.EndTZ, got.EndTZSource)
	}

	// Source only: keeps the zone, changes the source.
	if err := s.UpdateFlightPartRoute(ctx, pid, FlightRouteUpdate{StartTZSource: strp("inferred")}); err != nil {
		t.Fatalf("source-only route edit: %v", err)
	}
	got, _ = s.PlanPartByID(ctx, pid)
	if got.StartTZ != "Europe/London" || got.StartTZSource != "inferred" {
		t.Errorf("source-only = %q/%q", got.StartTZ, got.StartTZSource)
	}

	for _, bad := range []FlightRouteUpdate{
		{StartTZ: strp("Nope/Nope")},
		{EndTZSource: strp("confirmed")},
	} {
		if err := s.UpdateFlightPartRoute(ctx, pid, bad); !errors.Is(err, ErrInvalidZone) {
			t.Errorf("UpdateFlightPartRoute(%+v) = %v, want ErrInvalidZone", bad, err)
		}
	}
	if err := s.UpdateFlightPartRoute(ctx, 999999, FlightRouteUpdate{StartTZ: strp("Europe/London")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing part = %v, want ErrNotFound", err)
	}
}

// TestUpdateFlightPartRouteAnchorsFloating: a route update that names a zone
// but carries no time for a floating end (a provider resolve with no schedule)
// anchors that end's wall-clock digits in the zone and confirms it. With no zone
// the end stays floating whatever source was sent, and an end that is not
// floating keeps its instant.
func TestUpdateFlightPartRouteAnchorsFloating(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(2 * time.Hour)
	mk := func(ident string) int64 {
		t.Helper()
		plan, err := s.CreatePlan(ctx, CreatePlanPayload{
			TripID: trip, Type: "flight", Title: ident,
			Parts: []CreatePlanPartPayload{{
				StartsAt: out, EndsAt: &in, StartLabel: "AAA", EndLabel: "BBB",
				Flight: &FlightDetail{Ident: ident, ScheduledOut: out, ScheduledIn: in},
			}},
		}, owner)
		if err != nil {
			t.Fatalf("CreatePlan: %v", err)
		}
		parts, _ := s.PartsByPlan(ctx, plan.ID)
		if parts[0].StartTZSource != "floating" || parts[0].EndTZSource != "floating" {
			t.Fatalf("setup sources = %s/%s, want floating", parts[0].StartTZSource, parts[0].EndTZSource)
		}
		return parts[0].ID
	}

	// Zone found, no time: 09:00 in London in July is 08:00Z; 11:00 in Paris
	// is 09:00Z.
	pid := mk("XX110")
	if err := s.UpdateFlightPartRoute(ctx, pid, FlightRouteUpdate{
		StartTZ: strp("Europe/London"), StartTZSource: strp("confirmed"),
		EndTZ: strp("Europe/Paris"), EndTZSource: strp("confirmed"),
	}); err != nil {
		t.Fatalf("UpdateFlightPartRoute: %v", err)
	}
	got, _ := s.PlanPartByID(ctx, pid)
	if !got.StartsAt.Equal(out.Add(-time.Hour)) || got.StartTZSource != "confirmed" {
		t.Errorf("start = %v (%s), want %v confirmed", got.StartsAt, got.StartTZSource, out.Add(-time.Hour))
	}
	if got.EndsAt == nil || !got.EndsAt.Equal(out) || got.EndTZSource != "confirmed" {
		t.Errorf("end = %v (%s), want %v confirmed", got.EndsAt, got.EndTZSource, out)
	}

	// Now neither end is floating: a further zone change keeps the instants.
	if err := s.UpdateFlightPartRoute(ctx, pid, FlightRouteUpdate{
		StartTZ: strp("America/New_York"), EndTZ: strp("Asia/Tokyo"),
	}); err != nil {
		t.Fatalf("UpdateFlightPartRoute (not floating): %v", err)
	}
	got, _ = s.PlanPartByID(ctx, pid)
	if !got.StartsAt.Equal(out.Add(-time.Hour)) || got.EndsAt == nil || !got.EndsAt.Equal(out) {
		t.Errorf("non-floating ends moved: %v → %v", got.StartsAt, got.EndsAt)
	}

	// No zone, no time: the mapper's unknown is overridden, the end stays
	// floating and its digits are kept.
	pid = mk("XX111")
	if err := s.UpdateFlightPartRoute(ctx, pid, FlightRouteUpdate{
		StartTZ: strp(""), StartTZSource: strp("unknown"),
		EndTZ: strp(""), EndTZSource: strp("unknown"),
	}); err != nil {
		t.Fatalf("UpdateFlightPartRoute (no zone): %v", err)
	}
	got, _ = s.PlanPartByID(ctx, pid)
	if got.StartTZSource != "floating" || got.EndTZSource != "floating" ||
		!got.StartsAt.Equal(out) || got.EndsAt == nil || !got.EndsAt.Equal(in) {
		t.Errorf("no-zone = %v (%s) → %v (%s), want floating and unchanged",
			got.StartsAt, got.StartTZSource, got.EndsAt, got.EndTZSource)
	}

	// An invalid zone for a floating end is still refused.
	if err := s.UpdateFlightPartRoute(ctx, pid, FlightRouteUpdate{StartTZ: strp("Nope/Nope")}); !errors.Is(err, ErrInvalidZone) {
		t.Errorf("invalid zone on a floating end = %v, want ErrInvalidZone", err)
	}
}
