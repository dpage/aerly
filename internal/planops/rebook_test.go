package planops

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestMatchRebookingByRouteProximity: with no shared PNR and no exact
// ident+day match, a candidate on the same origin/dest within the ±2-day date
// tolerance matches at medium confidence (exercises sameRoute +
// withinDateProximity, the route branch of matchRebooking).
func TestMatchRebookingByRouteProximity(t *testing.T) {
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	candidate := rebookCandidate{
		partID: 42,
		flight: &store.FlightDetail{
			Ident: "BA111", ScheduledOut: base,
			OriginIATA: "LHR", DestIATA: "JFK",
		},
	}
	// Incoming has a different flight number but the same route, a day later.
	incoming := &store.FlightDetail{
		Ident: "BA999", ScheduledOut: base.AddDate(0, 0, 1),
		OriginIATA: "LHR", DestIATA: "JFK",
	}
	m := matchRebooking("", incoming, []rebookCandidate{candidate})
	if m == nil {
		t.Fatal("expected a route-proximity match")
	}
	if m.partID != 42 {
		t.Errorf("matched part %d, want 42", m.partID)
	}
	if m.confidence != 0.6 {
		t.Errorf("confidence = %v, want 0.6 (medium route match)", m.confidence)
	}
}

// TestMatchRebookingIdentSameDayBeatsRoute: when one candidate matches by
// ident+day (score 2) and another only by route (score 1), the ident match
// wins.
func TestMatchRebookingIdentSameDayBeatsRoute(t *testing.T) {
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	routeOnly := rebookCandidate{partID: 1, flight: &store.FlightDetail{
		Ident: "XX1", ScheduledOut: base.AddDate(0, 0, 1), OriginIATA: "LHR", DestIATA: "JFK"}}
	identMatch := rebookCandidate{partID: 2, flight: &store.FlightDetail{
		Ident: "BA111", ScheduledOut: base, OriginIATA: "LHR", DestIATA: "JFK"}}

	incoming := &store.FlightDetail{
		Ident: "BA111", ScheduledOut: base.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}
	m := matchRebooking("", incoming, []rebookCandidate{routeOnly, identMatch})
	if m == nil || m.partID != 2 {
		t.Fatalf("ident+day match should win, got %+v", m)
	}
}

// TestMatchRebookingNoMatch: nothing shares a PNR, ident-day, or nearby route,
// so there is no match.
func TestMatchRebookingNoMatch(t *testing.T) {
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	candidate := rebookCandidate{partID: 1, flight: &store.FlightDetail{
		Ident: "AA1", ScheduledOut: base, OriginIATA: "LHR", DestIATA: "CDG"}}
	// Different route, far-off date, different ident.
	incoming := &store.FlightDetail{
		Ident: "BB2", ScheduledOut: base.AddDate(0, 0, 30),
		OriginIATA: "MAN", DestIATA: "JFK",
	}
	if m := matchRebooking("ZZ", incoming, []rebookCandidate{candidate}); m != nil {
		t.Errorf("expected no match, got %+v", m)
	}
}

// TestSameRoute covers the route-equality helper, including the empty-IATA
// guards (an empty origin or dest never counts as the same route).
func TestSameRoute(t *testing.T) {
	a := &store.FlightDetail{OriginIATA: "LHR", DestIATA: "JFK"}
	b := &store.FlightDetail{OriginIATA: "LHR", DestIATA: "JFK"}
	if !sameRoute(a, b) {
		t.Error("identical routes should match")
	}
	if sameRoute(&store.FlightDetail{DestIATA: "JFK"}, b) {
		t.Error("empty origin must not match")
	}
	if sameRoute(&store.FlightDetail{OriginIATA: "LHR"}, b) {
		t.Error("empty dest must not match")
	}
	if sameRoute(a, &store.FlightDetail{OriginIATA: "MAN", DestIATA: "JFK"}) {
		t.Error("different origin must not match")
	}
}

// TestWithinDateProximity covers both directions of the ±48h tolerance.
func TestWithinDateProximity(t *testing.T) {
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	if !withinDateProximity(base, base.Add(36*time.Hour)) {
		t.Error("36h apart should be within tolerance")
	}
	// Negative delta (b after a) also folds to a positive distance.
	if !withinDateProximity(base.Add(36*time.Hour), base) {
		t.Error("36h apart (reversed) should be within tolerance")
	}
	if withinDateProximity(base, base.Add(72*time.Hour)) {
		t.Error("72h apart should exceed tolerance")
	}
}

// TestVisibleFlightCandidatesSkipsCancelled: a cancelled flight part is not a
// rebooking candidate.
func TestVisibleFlightCandidatesSkipsCancelled(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, partID := e.mkFlightPlan(t, trip, owner, "BA286", "PNR1", out, in)

	// Cancel the part: it must drop out of the candidate set.
	cancelled := "cancelled"
	if _, err := e.s.UpdatePlanPart(ctx, partID, store.UpdatePlanPartPayload{Status: &cancelled}); err != nil {
		t.Fatalf("UpdatePlanPart: %v", err)
	}
	cands, err := visibleFlightCandidates(ctx, Deps{Store: e.s}, owner, trip)
	if err != nil {
		t.Fatalf("visibleFlightCandidates: %v", err)
	}
	for _, c := range cands {
		if c.partID == partID {
			t.Errorf("cancelled part %d should not be a candidate", partID)
		}
	}
}

// TestVisibleFlightCandidatesSkipsMissingDetail: a flight part whose
// flight_details row is missing (orphaned out-of-band) is skipped rather than
// becoming a candidate (the fd==nil guard).
func TestVisibleFlightCandidatesSkipsMissingDetail(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, partID := e.mkFlightPlan(t, trip, owner, "BA286", "PNRORPHAN", out, in)

	// Delete the flight_details row directly, leaving the part orphaned.
	if _, err := e.pool.Exec(ctx, `DELETE FROM flight_details WHERE plan_part_id = $1`, partID); err != nil {
		t.Fatalf("delete flight_details: %v", err)
	}
	cands, err := visibleFlightCandidates(ctx, Deps{Store: e.s}, owner, trip)
	if err != nil {
		t.Fatalf("visibleFlightCandidates: %v", err)
	}
	for _, c := range cands {
		if c.partID == partID {
			t.Errorf("a detail-less flight part %d should be skipped", partID)
		}
	}
}

// TestVisibleFlightCandidatesReturnsLiveFlight: a live flight part is returned
// with its plan's confirmation_ref and passenger set populated.
func TestVisibleFlightCandidatesReturnsLiveFlight(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, partID := e.mkFlightPlan(t, trip, owner, "BA286", "PNRLIVE", out, in)

	cands, err := visibleFlightCandidates(ctx, Deps{Store: e.s}, owner, trip)
	if err != nil {
		t.Fatalf("visibleFlightCandidates: %v", err)
	}
	var found *rebookCandidate
	for i := range cands {
		if cands[i].partID == partID {
			found = &cands[i]
		}
	}
	if found == nil {
		t.Fatalf("live part %d should be a candidate", partID)
	}
	if found.confirmRef != "PNRLIVE" {
		t.Errorf("confirmRef = %q, want PNRLIVE", found.confirmRef)
	}
	if len(found.passengerIDs) != 1 || found.passengerIDs[0] != owner {
		t.Errorf("passengerIDs = %v, want [%d]", found.passengerIDs, owner)
	}
}
