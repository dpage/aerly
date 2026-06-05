package planops

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

var ctx = context.Background()

var userSeq atomic.Int64

// env bundles a store with the pool behind it so tests can insert users (which
// needs the raw pool) and call the store API.
type env struct {
	s    *store.Store
	pool *pgxpool.Pool
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := testsupport.NewPool(t)
	return env{s: store.New(pool), pool: pool}
}

func (e env) mkUser(t *testing.T) int64 {
	t.Helper()
	return testsupport.InsertUser(t, e.pool, fmt.Sprintf("user%d", userSeq.Add(1)), false, true)
}

// fakeExtractor returns canned plans, recording the body it was called with.
type fakeExtractor struct {
	plans    []ExtractedPlan
	err      error
	lastBody string
}

func (f *fakeExtractor) ExtractPlans(_ context.Context, body string, _ []Document) ([]ExtractedPlan, error) {
	f.lastBody = body
	return f.plans, f.err
}

// mkTrip creates a trip owned by userID via the store and returns its id.
func (e env) mkTrip(t *testing.T, userID int64) int64 {
	t.Helper()
	tr, err := e.s.CreateTrip(ctx, store.CreateTripPayload{Name: "Trip"}, userID)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	return tr.ID
}

// mkFlightPlan inserts a flight plan with one part into the trip, owned by
// userID and with userID as passenger. Returns the plan id and the part id.
func (e env) mkFlightPlan(t *testing.T, tripID, userID int64, ident, ref string, out, in time.Time) (int64, int64) {
	t.Helper()
	s := e.s
	plan, err := s.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: tripID, Type: "flight", Title: ident, ConfirmationRef: ref,
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "LHR", EndLabel: "JFK",
			Flight: &store.FlightDetail{
				Ident: ident, ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "JFK",
			},
		}},
	}, userID)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := s.AddPlanPassenger(ctx, plan.ID, userID); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	return plan.ID, parts[0].ID
}

// TestPropose_InjectsHomeAddressContext checks the traveller's home address is
// prepended to the extractor input so references like "from home" resolve.
func TestPropose_InjectsHomeAddressContext(t *testing.T) {
	e := newEnv(t)
	s := e.s
	owner := e.mkUser(t)
	addr := "12 Acacia Avenue, Reading"
	if _, err := s.UpdateUser(ctx, owner, store.UpdateUserPayload{HomeAddress: &addr}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	trip := e.mkTrip(t, owner)
	fx := &fakeExtractor{plans: []ExtractedPlan{{
		Type: "ground", Title: "Taxi",
		Parts: []ExtractedPart{{Type: "ground", Confidence: "high", StartDate: "2026-04-07"}},
	}}}
	deps := Deps{Store: s, Extractor: fx}
	if _, err := Propose(ctx, deps, owner, trip, "Taxi from home to LHR T5", nil); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if !strings.Contains(fx.lastBody, addr) {
		t.Errorf("extractor body missing home address: %q", fx.lastBody)
	}
	if !strings.Contains(fx.lastBody, "Taxi from home to LHR T5") {
		t.Errorf("extractor body dropped original text: %q", fx.lastBody)
	}
}

// TestPropose_RebookingMatchByPNR proposes a flight that shares the trip's
// existing PNR; the proposal must carry a supersession pointing at the old
// part.
func TestPropose_RebookingMatchByPNR(t *testing.T) {
	e := newEnv(t)
	s := e.s
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, oldPart := e.mkFlightPlan(t, trip, owner, "BA286", "PNR123", out, in)

	// Incoming: same PNR, a day later (a rebooking).
	fx := &fakeExtractor{plans: []ExtractedPlan{{
		Type: "flight", Title: "BA286 (rebooked)", ConfirmationRef: "pnr123",
		Parts: []ExtractedPart{{
			Type: "flight", Confidence: "high",
			Flight: FlightFields{
				Ident: "BA286", Date: "2026-06-02",
				OriginIATA: "LHR", DestIATA: "JFK",
				DepartTimeLocal: "09:00", ArriveDate: "2026-06-02", ArriveTimeLocal: "17:00",
			},
		}},
	}}}
	deps := Deps{Store: s, Extractor: fx}
	props, err := Propose(ctx, deps, owner, trip, "body", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("len(props) = %d, want 1", len(props))
	}
	if props[0].SupersedesPartID == nil {
		t.Fatalf("expected a proposed supersession by PNR")
	}
	if *props[0].SupersedesPartID != oldPart {
		t.Errorf("supersedes = %d, want old part %d", *props[0].SupersedesPartID, oldPart)
	}
}

// TestPropose_RebookingMatchesRightTraveller checks that the match prefers the
// proposing user's own visible flight over a trip-mate's flight on the same
// route/day (PRD §6.9 "by traveller and route").
func TestPropose_RebookingMatchesRightTraveller(t *testing.T) {
	e := newEnv(t)
	s := e.s
	alice := e.mkUser(t)
	bob := e.mkUser(t)
	trip := e.mkTrip(t, alice)
	if err := s.AddTripMember(ctx, trip, bob, "editor"); err != nil {
		t.Fatalf("AddTripMember: %v", err)
	}
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	// Both Alice and Bob have BA286 on the same day, no shared PNR.
	_, alicePart := e.mkFlightPlan(t, trip, alice, "BA286", "ALICEPNR", out, in)
	e.mkFlightPlan(t, trip, bob, "BA286", "BOBPNR", out, in)

	// Bob's plan is hidden from Alice so it's not in her visible candidate set;
	// the match must land on Alice's part.
	bobPlans, _ := s.PlansByTrip(ctx, trip)
	for _, pl := range bobPlans {
		if pl.ConfirmationRef == "BOBPNR" {
			if err := s.SetPlanVisibility(ctx, pl.ID, "only_visible_to", []int64{bob}); err != nil {
				t.Fatalf("SetPlanVisibility: %v", err)
			}
		}
	}

	fx := &fakeExtractor{plans: []ExtractedPlan{{
		Type: "flight", Title: "BA286 rebooked",
		Parts: []ExtractedPart{{
			Type: "flight", Confidence: "high",
			Flight: FlightFields{
				Ident: "BA286", Date: "2026-06-01",
				OriginIATA: "LHR", DestIATA: "JFK",
				DepartTimeLocal: "11:00", ArriveDate: "2026-06-01", ArriveTimeLocal: "19:00",
			},
		}},
	}}}
	deps := Deps{Store: s, Extractor: fx}
	props, err := Propose(ctx, deps, alice, trip, "body", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(props) != 1 || props[0].SupersedesPartID == nil {
		t.Fatalf("expected one proposal with a supersession, got %+v", props)
	}
	if *props[0].SupersedesPartID != alicePart {
		t.Errorf("matched part %d, want Alice's %d (not Bob's)", *props[0].SupersedesPartID, alicePart)
	}
}

// TestCommit_SupersessionCancelsOldPart verifies that committing a proposal
// with a supersession inserts the new part with supersedes_id set and stamps
// the OLD part status='cancelled' (the signal the FE greys on).
func TestCommit_SupersessionCancelsOldPart(t *testing.T) {
	e := newEnv(t)
	s := e.s
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, oldPart := e.mkFlightPlan(t, trip, owner, "BA286", "PNR123", out, in)

	newOut := out.AddDate(0, 0, 1)
	newIn := in.AddDate(0, 0, 1)
	plans := []ConfirmPlanInput{{
		Type: "flight", Title: "BA286 (rebooked)", ConfirmationRef: "PNR123",
		SupersedesPartID: &oldPart,
		Parts: []ConfirmPartInput{{
			Type: "flight", StartsAt: newOut, EndsAt: &newIn,
			Flight: &store.FlightDetail{
				Ident: "BA286", ScheduledOut: newOut, ScheduledIn: newIn,
				OriginIATA: "LHR", DestIATA: "JFK",
			},
		}},
	}}
	created, err := Commit(ctx, Deps{Store: s}, trip, owner, plans)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}
	// Old part must be cancelled.
	op, err := s.PlanPartByID(ctx, oldPart)
	if err != nil {
		t.Fatalf("PlanPartByID(old): %v", err)
	}
	if op.Status != "cancelled" {
		t.Errorf("old part status = %q, want cancelled", op.Status)
	}
	// New part must link to the old via supersedes_id.
	newParts, err := s.PartsByPlan(ctx, created[0].ID)
	if err != nil || len(newParts) != 1 {
		t.Fatalf("PartsByPlan(new) = %d, %v", len(newParts), err)
	}
	if newParts[0].SupersedesID == nil || *newParts[0].SupersedesID != oldPart {
		t.Errorf("new part supersedes_id = %v, want %d", newParts[0].SupersedesID, oldPart)
	}
}

// TestCommit_SupersedeRejectsForeignPart verifies that a confirm body cannot
// supersede (and thereby cancel) a part belonging to a different trip. This
// guards against an editor of trip A cancelling another user's part in trip B
// by passing its id, since SupersedesPartID is client-controlled.
func TestCommit_SupersedeRejectsForeignPart(t *testing.T) {
	e := newEnv(t)
	s := e.s
	owner := e.mkUser(t)
	tripA := e.mkTrip(t, owner)
	tripB := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	// A part that lives in trip B.
	_, foreignPart := e.mkFlightPlan(t, tripB, owner, "BA286", "PNR123", out, in)

	// Try to supersede trip B's part while committing into trip A.
	plans := []ConfirmPlanInput{{
		Type: "flight", Title: "BA286 (rebooked)", ConfirmationRef: "PNR123",
		SupersedesPartID: &foreignPart,
		Parts: []ConfirmPartInput{{
			Type: "flight", StartsAt: out.AddDate(0, 0, 1), EndsAt: &in,
			Flight: &store.FlightDetail{
				Ident: "BA286", ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "JFK",
			},
		}},
	}}
	if _, err := Commit(ctx, Deps{Store: s}, tripA, owner, plans); err == nil {
		t.Fatal("Commit accepted a cross-trip supersession; want rejection")
	}
	// The foreign part must be untouched (NOT cancelled).
	fp, err := s.PlanPartByID(ctx, foreignPart)
	if err != nil {
		t.Fatalf("PlanPartByID(foreign): %v", err)
	}
	if fp.Status == "cancelled" {
		t.Errorf("foreign part was cancelled across trips; status = %q", fp.Status)
	}
}

// TestCommit_SupersedeRejectsMultiPart verifies a supersession on a multi-part
// plan is rejected rather than cancelling the old part without a forward link.
func TestCommit_SupersedeRejectsMultiPart(t *testing.T) {
	e := newEnv(t)
	s := e.s
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	_, oldPart := e.mkFlightPlan(t, trip, owner, "BA286", "PNR123", out, in)

	plans := []ConfirmPlanInput{{
		Type: "flight", Title: "BA286 (rebooked)", ConfirmationRef: "PNR123",
		SupersedesPartID: &oldPart,
		Parts: []ConfirmPartInput{
			{Type: "flight", StartsAt: out.AddDate(0, 0, 1), EndsAt: &in,
				Flight: &store.FlightDetail{Ident: "BA286", ScheduledOut: out, ScheduledIn: in, OriginIATA: "LHR", DestIATA: "JFK"}},
			{Type: "flight", StartsAt: out.AddDate(0, 0, 2), EndsAt: &in,
				Flight: &store.FlightDetail{Ident: "BA287", ScheduledOut: out, ScheduledIn: in, OriginIATA: "JFK", DestIATA: "LHR"}},
		},
	}}
	if _, err := Commit(ctx, Deps{Store: s}, trip, owner, plans); err == nil {
		t.Fatal("Commit accepted a multi-part supersession; want rejection")
	}
	op, err := s.PlanPartByID(ctx, oldPart)
	if err != nil {
		t.Fatalf("PlanPartByID(old): %v", err)
	}
	if op.Status == "cancelled" {
		t.Errorf("old part cancelled without a linked replacement; status = %q", op.Status)
	}
}

// TestPropose_GroupsByConfirmationRef checks that flight proposals sharing a
// confirmation reference are folded into one multi-part booking, ordered by
// start, while empty-ref proposals stay separate (issue #12).
func TestPropose_GroupsByConfirmationRef(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)

	leg := func(ident, ref, date, dep, arr string) ExtractedPlan {
		return ExtractedPlan{
			Type: "flight", Title: ident, ConfirmationRef: ref,
			Parts: []ExtractedPart{{
				Type: "flight", Confidence: "high",
				Flight: FlightFields{
					Ident: ident, Date: date, OriginIATA: "LHR", DestIATA: "HEL",
					DepartTimeLocal: dep, ArriveDate: date, ArriveTimeLocal: arr,
				},
			}},
		}
	}
	// Return leg listed first to prove start-time ordering is applied.
	fx := &fakeExtractor{plans: []ExtractedPlan{
		leg("AY1334", "ABC123", "2026-06-10", "18:00", "20:00"), // return
		leg("AY1333", "abc123", "2026-06-01", "09:00", "11:00"), // outbound (lowercase ref)
		leg("BA999", "", "2026-06-01", "12:00", "14:00"),        // no ref → separate
	}}
	props, err := Propose(ctx, Deps{Store: e.s, Extractor: fx}, owner, trip, "body", nil)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("want 2 proposals (one merged + one lone), got %d", len(props))
	}
	merged := props[0]
	if len(merged.Parts) != 2 {
		t.Fatalf("merged plan should have 2 parts, got %d", len(merged.Parts))
	}
	if !merged.Parts[0].StartsAt.Before(merged.Parts[1].StartsAt) {
		t.Errorf("merged parts not ordered by start: %v then %v",
			merged.Parts[0].StartsAt, merged.Parts[1].StartsAt)
	}
	if len(props[1].Parts) != 1 {
		t.Errorf("the ref-less flight should stay its own plan, got %d parts", len(props[1].Parts))
	}
}

func TestGroupByConfirmationRef_PreservesTicketAndCostFromLaterFragment(t *testing.T) {
	cost := 480.0
	in := []ProposedPlan{
		// Primary fragment carries no ticket/cost...
		{Type: "flight", ConfirmationRef: "PNR1", Parts: []ProposedPart{{}}},
		// ...the later same-PNR fragment does — it must survive the fold.
		{Type: "flight", ConfirmationRef: "PNR1", TicketNumber: "T9", CostAmount: &cost, CostCurrency: "GBP", Parts: []ProposedPart{{}}},
	}
	out := groupByConfirmationRef(in)
	if len(out) != 1 {
		t.Fatalf("same-PNR flights should merge, got %d plans", len(out))
	}
	got := out[0]
	if got.TicketNumber != "T9" {
		t.Errorf("ticket number lost in fold: %q", got.TicketNumber)
	}
	if got.CostAmount == nil || *got.CostAmount != cost || got.CostCurrency != "GBP" {
		t.Errorf("cost lost in fold: %v %q", got.CostAmount, got.CostCurrency)
	}

	// The currency backfills independently of the amount: a primary that has an
	// amount but a blank currency picks up a later fragment's currency.
	primaryCost, laterCost := 100.0, 200.0
	merged := groupByConfirmationRef([]ProposedPlan{
		{Type: "train", ConfirmationRef: "R1", CostAmount: &primaryCost, CostCurrency: "", Parts: []ProposedPart{{}}},
		{Type: "train", ConfirmationRef: "R1", CostAmount: &laterCost, CostCurrency: "EUR", Parts: []ProposedPart{{}}},
	})
	if len(merged) != 1 {
		t.Fatalf("same-PNR trains should merge, got %d", len(merged))
	}
	if merged[0].CostAmount == nil || *merged[0].CostAmount != primaryCost {
		t.Errorf("primary amount should win: %v", merged[0].CostAmount)
	}
	if merged[0].CostCurrency != "EUR" {
		t.Errorf("currency not backfilled: %q", merged[0].CostCurrency)
	}
}

func TestGroupByConfirmationRef_GroupsGround(t *testing.T) {
	// Ground transport is a linkable type, so transfers sharing one booking
	// reference fold into a single multi-part plan just like flights/trains.
	in := []ProposedPlan{
		{Type: "ground", ConfirmationRef: "REF1", Parts: []ProposedPart{{}}},
		{Type: "ground", ConfirmationRef: "ref1", Parts: []ProposedPart{{}}},
	}
	out := groupByConfirmationRef(in)
	if len(out) != 1 {
		t.Fatalf("same-ref ground transfers should merge, got %d plans", len(out))
	}
	if len(out[0].Parts) != 2 {
		t.Fatalf("merged ground plan should hold 2 parts, got %d", len(out[0].Parts))
	}
}

func TestGroupByConfirmationRef_LeavesDistinctRefsAndTypes(t *testing.T) {
	in := []ProposedPlan{
		{Type: "flight", ConfirmationRef: "A", Parts: []ProposedPart{{}}},
		{Type: "flight", ConfirmationRef: "B", Parts: []ProposedPart{{}}},
		{Type: "train", ConfirmationRef: "A", Parts: []ProposedPart{{}}}, // same ref, different type
		{Type: "hotel", ConfirmationRef: "A", Parts: []ProposedPart{{}}}, // non-linkable
	}
	out := groupByConfirmationRef(in)
	if len(out) != 4 {
		t.Fatalf("nothing should merge here, got %d plans", len(out))
	}
}
