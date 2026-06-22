package planops

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestCommitNilStore: Commit with no Store errors before touching anything.
func TestCommitNilStore(t *testing.T) {
	if _, err := Commit(ctx, Deps{}, 1, 1, nil); err == nil {
		t.Error("nil Store should error")
	}
}

// TestCommitSupersedeMissingPartErrors: a supersession pointing at a
// non-existent part fails resolution and aborts the commit.
func TestCommitSupersedeMissingPartErrors(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	missing := int64(999999999)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	plans := []ConfirmPlanInput{{
		Type: "flight", Title: "X", SupersedesPartID: &missing,
		Parts: []ConfirmPartInput{{
			Type: "flight", StartsAt: out, EndsAt: &in,
			Flight: &store.FlightDetail{Ident: "ZZ1", ScheduledOut: out, ScheduledIn: in, OriginIATA: "LHR", DestIATA: "JFK"},
		}},
	}}
	if _, err := Commit(ctx, Deps{Store: e.s}, trip, owner, plans); err == nil {
		t.Error("superseding a non-existent part should error")
	}
}

// TestCommitRejectsNonFriendPassenger: naming a passenger who is not a friend
// of the editor is rejected before anything is written.
func TestCommitRejectsNonFriendPassenger(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	stranger := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	plans := []ConfirmPlanInput{{
		Type: "dining", Title: "Dinner", PassengerIDs: []int64{stranger},
		Parts: []ConfirmPartInput{{Type: "dining", StartsAt: out}},
	}}
	if _, err := Commit(ctx, Deps{Store: e.s}, trip, owner, plans); err == nil {
		t.Error("a non-friend passenger should be rejected")
	}
	// No plan should have been created.
	plansInTrip, err := e.s.PlansByTrip(ctx, trip)
	if err != nil {
		t.Fatalf("PlansByTrip: %v", err)
	}
	if len(plansInTrip) != 0 {
		t.Errorf("no plan should have been written, found %d", len(plansInTrip))
	}
}

// TestCommitWithSelfPassengerAndVisibility: the editor as their own passenger is
// always allowed (the uid==createdBy skip), and a friend passenger plus a
// visibility override are persisted (commitPlanExtras success path). The
// "everyone" mode is normalised to the default ("").
func TestCommitWithSelfPassengerAndVisibility(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	friend := e.mkUser(t)
	// Establish an accepted friendship so the friend may be a passenger.
	if _, err := e.s.RequestFriendship(ctx, owner, friend, ""); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if _, err := e.s.AcceptFriendship(ctx, friend, owner); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 19, 0, 0, 0, time.UTC)
	plans := []ConfirmPlanInput{{
		Type:         "dining",
		Title:        "Dinner",
		Source:       "", // exercises the default "paste" source
		PassengerIDs: []int64{owner, friend},
		Visibility:   &ConfirmVisibility{Mode: "everyone"}, // normalised to ""
		Parts:        []ConfirmPartInput{{Type: "dining", Seq: 0, StartsAt: out}},
	}}
	created, err := Commit(ctx, Deps{Store: e.s}, trip, owner, plans)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}
	if created[0].Source != "paste" {
		t.Errorf("source = %q, want defaulted 'paste'", created[0].Source)
	}
	pax, err := e.s.PassengersByPlan(ctx, []int64{created[0].ID})
	if err != nil {
		t.Fatalf("PassengersByPlan: %v", err)
	}
	if len(pax[created[0].ID]) != 2 {
		t.Errorf("passenger count = %d, want 2", len(pax[created[0].ID]))
	}
}

// TestCommitRollsBackOnExtrasFailure: when a post-CreatePlan write fails (here a
// visibility override naming a non-existent user id, which violates the members
// FK), Commit compensates by deleting the just-created plan so each plan commits
// all-or-nothing. This exercises commitPlanExtras' error path and the rollback
// DeletePlan.
func TestCommitRollsBackOnExtrasFailure(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 19, 0, 0, 0, time.UTC)
	plans := []ConfirmPlanInput{{
		Type:  "dining",
		Title: "Doomed Dinner",
		// A user id that does not exist → the visibility members insert FK-fails.
		Visibility: &ConfirmVisibility{Mode: "hidden_from", UserIDs: []int64{999999999}},
		Parts:      []ConfirmPartInput{{Type: "dining", StartsAt: out}},
	}}
	if _, err := Commit(ctx, Deps{Store: e.s}, trip, owner, plans); err == nil {
		t.Fatal("a visibility override on a non-existent user should fail the commit")
	}
	// The plan must have been rolled back (deleted), leaving the trip empty.
	plansInTrip, err := e.s.PlansByTrip(ctx, trip)
	if err != nil {
		t.Fatalf("PlansByTrip: %v", err)
	}
	if len(plansInTrip) != 0 {
		t.Errorf("the failed plan should have been rolled back, found %d", len(plansInTrip))
	}
}

// TestCommitHiddenFromVisibility persists a non-default visibility override.
func TestCommitHiddenFromVisibility(t *testing.T) {
	e := newEnv(t)
	owner := e.mkUser(t)
	other := e.mkUser(t)
	trip := e.mkTrip(t, owner)
	out := time.Date(2026, 6, 1, 19, 0, 0, 0, time.UTC)
	plans := []ConfirmPlanInput{{
		Type:       "dining",
		Title:      "Secret Dinner",
		Visibility: &ConfirmVisibility{Mode: "hidden_from", UserIDs: []int64{other}},
		Parts:      []ConfirmPartInput{{Type: "dining", StartsAt: out}},
	}}
	created, err := Commit(ctx, Deps{Store: e.s}, trip, owner, plans)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}
}
