package planops

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestPlanDateSpanSkipsZeroStart: a part with a zero StartsAt contributes
// nothing, and the end timezone falls back to the start timezone when EndTZ is
// blank.
func TestPlanDateSpanSkipsZeroStart(t *testing.T) {
	dep := time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)
	arr := time.Date(2026, 6, 1, 22, 30, 0, 0, time.UTC) // 01:30 next day in Istanbul (+3)
	parts := []ProposedPart{
		{Type: "dining"}, // zero StartsAt → skipped
		{Type: "flight", StartsAt: dep, EndsAt: &arr, StartTZ: "Europe/Istanbul"}, // no EndTZ → falls back
	}
	start, end := PlanDateSpan(parts)
	if got := start.Format("2006-01-02"); got != "2026-06-01" {
		t.Errorf("start = %s, want 2026-06-01", got)
	}
	// With EndTZ falling back to Istanbul (+3), the arrival lands on the 2nd.
	if got := end.Format("2006-01-02"); got != "2026-06-02" {
		t.Errorf("end = %s, want 2026-06-02 (EndTZ falls back to StartTZ)", got)
	}
}

// TestSelectTripNilStoreOrZeroStart: a nil Store or a zero plan start never
// matches.
func TestSelectTripNilStoreOrZeroStart(t *testing.T) {
	if _, ok, _ := SelectTrip(ctx, Deps{}, 1, time.Time{}, time.Time{}); ok {
		t.Error("nil Store should not match")
	}
	e := newEnv(t)
	owner := e.mkUser(t)
	if _, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, time.Time{}, time.Time{}); err != nil || ok {
		t.Errorf("zero plan start should not match: ok=%v err=%v", ok, err)
	}
}

// TestSelectTripNormalisesPlanEnd: when planEnd is zero or before planStart it
// is coerced to planStart, and the plan still attaches to an overlapping trip.
func TestSelectTripNormalisesPlanEnd(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	e.mkFlightPlan(t, trip, owner, "BA286", "PNR1",
		time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 8, 17, 0, 0, 0, time.UTC))

	planStart := time.Date(2026, 6, 4, 19, 0, 0, 0, time.UTC)
	// planEnd before planStart → coerced.
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart.Add(-time.Hour))
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != trip {
		t.Errorf("SelectTrip = (%d, %v), want (%d, true)", id, ok, trip)
	}
}

// TestSelectTripFallsBackToTripDates: a trip with no plan_parts but explicit
// starts_on/ends_on uses those dates (tripSpan fallback) and still attaches.
func TestSelectTripFallsBackToTripDates(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	startsOn := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	endsOn := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	tr, err := e.s.CreateTrip(ctx, store.CreateTripPayload{
		Name: "Dated trip", StartsOn: &startsOn, EndsOn: &endsOn,
	}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	planStart := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != tr.ID {
		t.Errorf("SelectTrip = (%d, %v), want the dated trip (%d)", id, ok, tr.ID)
	}
}

// TestSelectTripFallsBackToStartOnOnly: a trip with only starts_on (no ends_on)
// covers the StartsOn-without-EndsOn branch of tripSpan; the plan on that single
// day attaches.
func TestSelectTripFallsBackToStartOnOnly(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	startsOn := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	tr, err := e.s.CreateTrip(ctx, store.CreateTripPayload{
		Name: "Single-day trip", StartsOn: &startsOn,
	}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	planStart := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok || id != tr.ID {
		t.Errorf("SelectTrip = (%d, %v), want the single-day trip (%d)", id, ok, tr.ID)
	}
}

// TestSelectTripTieBreaksOnGap: two trips with equal overlap (zero, both purely
// adjacent) are decided by the smaller gap to the plan.
func TestSelectTripTieBreaksOnGap(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)

	// Plan is a point booking on 06-10 12:00.
	planStart := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Trip A ends 06-09 12:00 → gap ~1 day (closer).
	tripA := e.mkTrip(t, owner)
	e.mkFlightPlan(t, tripA, owner, "AA1", "PA",
		time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC))

	// Trip B starts 06-11 11:00 → gap ~23h; both within adjacency tolerance.
	tripB := e.mkTrip(t, owner)
	e.mkFlightPlan(t, tripB, owner, "BB1", "PB",
		time.Date(2026, 6, 11, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC))

	id, ok, err := SelectTrip(ctx, Deps{Store: e.s}, owner, planStart, planStart)
	if err != nil {
		t.Fatalf("SelectTrip: %v", err)
	}
	if !ok {
		t.Fatalf("expected a match")
	}
	// Trip B's start (06-11 11:00) is ~23h after the plan; Trip A's end (06-09
	// 12:00) is ~24h before. B is marginally closer, so it wins the gap tie-break.
	if id != tripB {
		t.Errorf("SelectTrip = %d, want trip B (%d) on the smaller gap", id, tripB)
	}
}

// TestOverlapAndGapDisjointBothDirections covers both disjoint branches: a
// before b, and b before a.
func TestOverlapAndGapDisjointBothDirections(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	a := dateSpan{start: base, end: base.Add(24 * time.Hour)}
	b := dateSpan{start: base.Add(72 * time.Hour), end: base.Add(96 * time.Hour)}

	// a entirely before b.
	overlap, gap := overlapAndGap(a, b)
	if overlap != 0 || gap != 48*time.Hour {
		t.Errorf("a before b: overlap=%v gap=%v, want 0 and 48h", overlap, gap)
	}
	// b entirely before a (swap the args).
	overlap, gap = overlapAndGap(b, a)
	if overlap != 0 || gap != 48*time.Hour {
		t.Errorf("b before a: overlap=%v gap=%v, want 0 and 48h", overlap, gap)
	}

	// Overlapping spans return a positive overlap and zero gap.
	c := dateSpan{start: base.Add(12 * time.Hour), end: base.Add(36 * time.Hour)}
	overlap, gap = overlapAndGap(a, c)
	if overlap <= 0 || gap != 0 {
		t.Errorf("overlapping: overlap=%v gap=%v, want positive overlap and 0 gap", overlap, gap)
	}
}
