package planops

import (
	"context"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// adjacencyTolerance is the gap within which a plan's date span is treated as
// "adjacent" to a trip's span and attaches to it (spec §6.3 — catches the
// dinner the evening a trip ends). One day.
const adjacencyTolerance = 24 * time.Hour

// Catch-all guard: an existing trip whose effective span is disproportionately
// larger than the booking is treated as a dumping-ground (e.g. a bulk
// "Imported flights" trip that aggregates unrelated legs across weeks). Attaching
// a self-contained booking to it would bury the booking instead of giving it its
// own trip, so such candidates are skipped — the caller then creates a fresh
// trip. The guard only fires when ALL of:
//   - the booking itself spans at least catchAllMinBookingSpan (a point booking
//     like a single dinner always attaches — that's the mid-trip-dinner case);
//   - the trip's span is at least catchAllMinSpan long; and
//   - the trip's span is at least catchAllRatio× the booking's span.
//
// so a genuine long trip with a short side-booking, or a multi-day booking
// inside a comparably-sized trip, is unaffected. The result is best-effort and
// user-correctable in the UI.
const (
	catchAllMinBookingSpan = 2 * 24 * time.Hour
	catchAllMinSpan        = 14 * 24 * time.Hour
	catchAllRatio          = 3
)

// dateSpan is a closed [start, end] interval. A zero start means "no dates".
type dateSpan struct {
	start time.Time
	end   time.Time
}

func (s dateSpan) empty() bool { return s.start.IsZero() && s.end.IsZero() }

// PlanSpan computes the [start, end] date interval covered by a set of proposed
// parts — min(starts) … max(ends, starts). Used to place an email-ingested plan
// against the user's trips.
func PlanSpan(parts []ProposedPart) (start, end time.Time) {
	var ds dateSpan
	for _, p := range parts {
		if p.StartsAt.IsZero() {
			continue
		}
		s := p.StartsAt
		e := s
		if p.EndsAt != nil && !p.EndsAt.IsZero() {
			e = *p.EndsAt
		}
		if ds.start.IsZero() || s.Before(ds.start) {
			ds.start = s
		}
		if ds.end.IsZero() || e.After(ds.end) {
			ds.end = e
		}
	}
	return ds.start, ds.end
}

// TripCandidate is a trip considered for email auto-attach, with its effective
// date span (derived from plan_parts, falling back to starts_on/ends_on).
type TripCandidate struct {
	TripID int64
	Span   dateSpan
}

// SelectTrip chooses the trip an email-ingested plan attaches to by date
// proximity (spec §6.3): among the user's trips whose effective span overlaps,
// encompasses, or is adjacent to the plan's span (within adjacencyTolerance),
// pick the greatest overlap, then the smallest gap. Returns (tripID, true) on a
// match, or (0, false) when nothing matches (or the only candidates are
// date-less) — the caller then creates a new trip. A wrong auto-match is
// always correctable because the result is surfaced for confirmation, not
// silently committed.
func SelectTrip(ctx context.Context, deps Deps, userID int64, planStart, planEnd time.Time) (int64, bool, error) {
	if deps.Store == nil || planStart.IsZero() {
		return 0, false, nil
	}
	if planEnd.IsZero() || planEnd.Before(planStart) {
		planEnd = planStart
	}
	trips, err := deps.Store.ListTrips(ctx, userID)
	if err != nil {
		return 0, false, err
	}
	plan := dateSpan{start: planStart, end: planEnd}
	planSpan := planEnd.Sub(planStart)

	bestID := int64(0)
	bestOverlap := time.Duration(-1)
	bestGap := time.Duration(1<<62 - 1)
	for _, t := range trips {
		span, err := tripSpan(ctx, deps, t)
		if err != nil {
			return 0, false, err
		}
		if span.empty() {
			continue // date-less trips never auto-match
		}
		// Only attach to a trip the sender can edit. ListTrips also returns trips
		// they merely view (a friend's shared trip), and committing their booking
		// onto someone else's trip leaves it stranded — the sender can't move or
		// edit it. Such trips form a fresh, owned trip instead.
		canEdit, err := deps.Store.CanEditTrip(ctx, t.ID, userID)
		if err != nil {
			return 0, false, err
		}
		if !canEdit {
			continue
		}
		// Skip dumping-ground trips far larger than the booking (see the
		// catch-all guard consts) so a self-contained booking forms its own trip
		// rather than being absorbed.
		if tripDur := span.end.Sub(span.start); planSpan >= catchAllMinBookingSpan &&
			tripDur >= catchAllMinSpan && tripDur >= catchAllRatio*planSpan {
			continue
		}
		overlap, gap := overlapAndGap(plan, span)
		// Attach when intervals overlap (overlap>0) or are adjacent (gap small).
		if overlap <= 0 && gap > adjacencyTolerance {
			continue
		}
		switch {
		case overlap > bestOverlap:
			bestID, bestOverlap, bestGap = t.ID, overlap, gap
		case overlap == bestOverlap && gap < bestGap:
			bestID, bestGap = t.ID, gap
		}
	}
	if bestID == 0 {
		return 0, false, nil
	}
	return bestID, true, nil
}

// tripSpan computes a trip's effective date span: min(starts_at)…max(ends_at)
// over its plan_parts, falling back to trips.starts_on/ends_on.
func tripSpan(ctx context.Context, deps Deps, t *store.Trip) (dateSpan, error) {
	plans, err := deps.Store.PlansByTrip(ctx, t.ID)
	if err != nil {
		return dateSpan{}, err
	}
	var ds dateSpan
	for _, pl := range plans {
		parts, err := deps.Store.PartsByPlan(ctx, pl.ID)
		if err != nil {
			return dateSpan{}, err
		}
		for _, p := range parts {
			if p.DismissedAt != nil {
				continue
			}
			s := p.StartsAt
			e := s
			if p.EndsAt != nil {
				e = *p.EndsAt
			}
			if ds.start.IsZero() || s.Before(ds.start) {
				ds.start = s
			}
			if ds.end.IsZero() || e.After(ds.end) {
				ds.end = e
			}
		}
	}
	if ds.empty() {
		if t.StartsOn != nil {
			ds.start = *t.StartsOn
			ds.end = *t.StartsOn
			if t.EndsOn != nil {
				ds.end = *t.EndsOn
			}
		}
	}
	return ds, nil
}

// overlapAndGap returns the overlap duration (>0 when the intervals intersect)
// and the gap duration (>0 when they are disjoint; 0 when touching/overlapping)
// between two spans.
func overlapAndGap(a, b dateSpan) (overlap, gap time.Duration) {
	lo := maxTime(a.start, b.start)
	hi := minTime(a.end, b.end)
	if !lo.After(hi) {
		return hi.Sub(lo), 0
	}
	// Disjoint: gap is the distance between the nearer endpoints.
	if a.end.Before(b.start) {
		return 0, b.start.Sub(a.end)
	}
	return 0, a.start.Sub(b.end)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
