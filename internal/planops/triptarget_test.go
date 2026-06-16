package planops

import (
	"testing"
	"time"
)

// TestPlanDateSpan_EastboundOvernightUsesLocalLandingDay: a flight that departs
// one evening and lands after local midnight in a timezone to the east must set
// the trip's end date to the landing day in the destination tz, not the UTC day
// of the arrival instant (issue #57). Here the arrival instant is still
// 2026-01-15 in UTC but 2026-01-16 in Istanbul (+3) — the trip ends on the 16th.
func TestPlanDateSpan_EastboundOvernightUsesLocalLandingDay(t *testing.T) {
	dep := time.Date(2026, 1, 15, 20, 0, 0, 0, time.UTC)  // 20:00 in London (GMT)
	arr := time.Date(2026, 1, 15, 22, 30, 0, 0, time.UTC) // 01:30 next day in Istanbul (+3)
	parts := []ProposedPart{{
		Type:     "flight",
		StartsAt: dep,
		EndsAt:   &arr,
		StartTZ:  "Europe/London",
		EndTZ:    "Europe/Istanbul",
	}}

	// The UTC-instant span (used for date proximity) lands the end on the 15th —
	// the very off-by-one PlanDateSpan exists to avoid.
	if _, e := PlanSpan(parts); e.UTC().Format("2006-01-02") != "2026-01-15" {
		t.Fatalf("precondition: PlanSpan end = %s, want 2026-01-15", e.UTC().Format("2006-01-02"))
	}

	start, end := PlanDateSpan(parts)
	if got := start.Format("2006-01-02"); got != "2026-01-15" {
		t.Errorf("start = %s, want 2026-01-15 (departure day in London)", got)
	}
	if got := end.Format("2006-01-02"); got != "2026-01-16" {
		t.Errorf("end = %s, want 2026-01-16 (landing day in Istanbul)", got)
	}
}

// TestPlanDateSpan_SpansMinStartMaxEnd: across several parts the span is the
// earliest local start day and the latest local end day; a part with no EndsAt
// contributes its start day as its end.
func TestPlanDateSpan_SpansMinStartMaxEnd(t *testing.T) {
	outDep := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	outArr := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	dinner := time.Date(2026, 6, 3, 19, 0, 0, 0, time.UTC) // point booking, no end
	parts := []ProposedPart{
		{Type: "dining", StartsAt: dinner},
		{Type: "flight", StartsAt: outDep, EndsAt: &outArr, StartTZ: "UTC", EndTZ: "UTC"},
	}
	start, end := PlanDateSpan(parts)
	if got := start.Format("2006-01-02"); got != "2026-06-01" {
		t.Errorf("start = %s, want 2026-06-01", got)
	}
	if got := end.Format("2006-01-02"); got != "2026-06-03" {
		t.Errorf("end = %s, want 2026-06-03", got)
	}
}

// TestPlanDateSpan_Empty: parts with no usable start contribute nothing.
func TestPlanDateSpan_Empty(t *testing.T) {
	start, end := PlanDateSpan([]ProposedPart{{Type: "dining"}})
	if !start.IsZero() || !end.IsZero() {
		t.Errorf("PlanDateSpan = (%v, %v), want zero span", start, end)
	}
}

// TestSelectTrip_AttachesToOverlappingTrip: a plan whose dates fall inside an
// existing trip's effective span attaches to that trip.
func TestSelectTrip_AttachesToOverlappingTrip(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	// Trip's effective span comes from a flight 2026-06-01..06-08.
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	// A dinner mid-trip.
	planStart := time.Date(2026, 6, 4, 19, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != trip {
		t.Errorf("SelectTrip = (%d, %v), want (%d, true)", id, ok, trip)
	}
}

// TestSelectTrip_AttachesAdjacent: a plan one day after a trip ends is within
// the adjacency tolerance and still attaches.
func TestSelectTrip_AttachesAdjacent(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	// Dinner the evening the trip ends + a few hours later (< 1 day gap).
	planStart := time.Date(2026, 6, 8, 20, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != trip {
		t.Errorf("SelectTrip = (%d, %v), want (%d, true)", id, ok, trip)
	}
}

// TestSelectTrip_NoMatchCreatesNew: a plan far from any trip's span does not
// attach (the caller then creates a new trip).
func TestSelectTrip_NoMatchCreatesNew(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	// A plan two months away.
	planStart := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if ok {
		t.Errorf("SelectTrip = (%d, true), want no match", id)
	}
}

// TestSelectTrip_PrefersGreatestOverlap: with two candidate trips, the one with
// the larger overlap wins.
func TestSelectTrip_PrefersGreatestOverlap(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)

	// Trip A: 06-01..06-03 (barely overlaps the plan's start).
	tripA := e.mkTrip(t, owner)
	e.mkFlightPlan(t, tripA, owner, "AA1", "PA",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC))

	// Trip B: 06-02..06-10 (overlaps the plan span fully).
	tripB := e.mkTrip(t, owner)
	e.mkFlightPlan(t, tripB, owner, "BB1", "PB",
		time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC))

	planStart := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	planEnd := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planEnd)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != tripB {
		t.Errorf("SelectTrip = (%d, %v), want trip B (%d)", id, ok, tripB)
	}
}

// TestSelectTrip_SkipsCatchAllTrip: a substantial self-contained booking is NOT
// absorbed by a dumping-ground trip whose span is far larger (the Vancouver
// regression — a 6-day booking falling inside a bulk "Imported flights" trip
// that spans many weeks). The caller then creates a fresh trip.
func TestSelectTrip_SkipsCatchAllTrip(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)

	// A catch-all trip aggregating unrelated legs across ~6 weeks.
	catchAll := e.mkTrip(t, owner)
	e.mkFlightPlan(t, catchAll, owner, "AA1", "PA",
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))

	// A self-contained 6-day booking inside that span.
	planStart := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	planEnd := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planEnd)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if ok {
		t.Errorf("SelectTrip = (%d, true), want no match (catch-all should be skipped)", id)
	}
}

// TestSelectTrip_PointBookingStillAttachesToLongTrip: the catch-all guard must
// NOT fire for a point booking (e.g. a single dinner) inside a long trip — that
// dinner belongs to the trip.
func TestSelectTrip_PointBookingStillAttachesToLongTrip(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)

	trip := e.mkTrip(t, owner)
	e.mkFlightPlan(t, trip, owner, "AA1", "PA",
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)) // 3-week trip

	// A single dinner mid-trip (zero-span booking).
	planStart := time.Date(2026, 5, 10, 19, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != trip {
		t.Errorf("SelectTrip = (%d, %v), want (%d, true)", id, ok, trip)
	}
}

// TestSelectTrip_SkipsTripUserOnlyViews: an ingested booking must NOT attach to
// a trip the sender only *views* (a friend's shared trip) — committing it there
// would strand the booking on a trip the sender can't edit (the Devrim
// regression). The caller then creates a fresh, owned trip.
func TestSelectTrip_SkipsTripUserOnlyViews(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	viewer := e.mkUser(t)

	trip := e.mkTrip(t, owner)
	if err := e.s.AddTripMember(ctx, trip, viewer, "viewer"); err != nil {
		t.Fatalf("AddTripMember: %v", err)
	}
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	planStart := time.Date(2026, 6, 4, 19, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, viewer, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if ok {
		t.Errorf("SelectTrip = (%d, true), want no match (sender only views the trip)", id)
	}
}

// TestSelectTrip_AttachesToEditableSharedTrip: when the sender is an editor (not
// just a viewer) of an overlapping trip, the booking DOES attach — they can edit
// it there.
func TestSelectTrip_AttachesToEditableSharedTrip(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	editor := e.mkUser(t)

	trip := e.mkTrip(t, owner)
	if err := e.s.AddTripMember(ctx, trip, editor, "editor"); err != nil {
		t.Fatalf("AddTripMember: %v", err)
	}
	// The trip-list visibility gate now requires an accepted friendship between
	// owner and member before the member can see the trip via ListTrips.
	if _, err := e.s.RequestFriendship(ctx, owner, editor, ""); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if _, err := e.s.AcceptFriendship(ctx, editor, owner); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	tOut := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	tIn := time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", tOut, tIn)

	planStart := time.Date(2026, 6, 4, 19, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, editor, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != trip {
		t.Errorf("SelectTrip = (%d, %v), want (%d, true)", id, ok, trip)
	}
}

// TestSelectTrip_DatelessTripNeverMatches: a trip with no plan_parts and no
// starts_on/ends_on is not a candidate.
func TestSelectTrip_DatelessTripNeverMatches(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	e.mkTrip(t, owner) // date-less

	planStart := time.Date(2026, 6, 4, 19, 0, 0, 0, time.UTC)
	_, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if ok {
		t.Error("date-less trip should not match")
	}
}
