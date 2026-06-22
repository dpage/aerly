package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

// recv does a non-blocking receive. Publish delivers synchronously into the
// subscriber's buffered channel, so by the time the notify call returns the
// event (if any) is already queued.
func recv(ch <-chan sse.Event) (sse.Event, bool) {
	select {
	case e := <-ch:
		return e, true
	default:
		return sse.Event{}, false
	}
}

func TestTripUpdated_ScopedToMembers(t *testing.T) {
	pool := testsupport.NewPool(t)
	st := store.New(pool)
	hub := sse.NewHub()
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "notify-owner", false, true)
	stranger := testsupport.InsertUser(t, pool, "notify-stranger", false, true)
	trip, err := st.CreateTrip(ctx, store.CreateTripPayload{Name: "Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}

	ownerCh, unsub1 := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub1()
	strangerCh, unsub2 := hub.Subscribe(sse.Subscription{ViewerID: stranger})
	defer unsub2()

	TripUpdated(ctx, st, hub, trip.ID)

	e, ok := recv(ownerCh)
	if !ok {
		t.Fatal("owner did not receive trip.updated")
	}
	if e.Type != "trip.updated" {
		t.Errorf("event type = %q, want trip.updated", e.Type)
	}
	if _, ok := recv(strangerCh); ok {
		t.Error("non-member received a trip.updated event they should not see")
	}
}

// TestTripUpdated_ReachesPlanScopedFriend confirms the event now fans out to the
// full friend-gated tile audience: an accepted friend who can see the trip only
// via a plan-scoped grant (a plan passenger, with no trip_members row of their
// own) still receives trip.updated.
func TestTripUpdated_ReachesPlanScopedFriend(t *testing.T) {
	pool := testsupport.NewPool(t)
	st := store.New(pool)
	hub := sse.NewHub()
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "notify-aown", false, true)
	friend := testsupport.InsertUser(t, pool, "notify-afriend", false, true)
	if _, err := st.RequestFriendship(ctx, owner, friend, ""); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if _, err := st.AcceptFriendship(ctx, friend, owner); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	trip, err := st.CreateTrip(ctx, store.CreateTripPayload{Name: "Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	plan, err := st.CreatePlan(ctx, store.CreatePlanPayload{TripID: trip.ID, Type: "dining", Title: "Dinner"}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.AddPlanPassenger(ctx, plan.ID, friend); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}

	friendCh, unsub := hub.Subscribe(sse.Subscription{ViewerID: friend})
	defer unsub()

	TripUpdated(ctx, st, hub, trip.ID)

	if e, ok := recv(friendCh); !ok || e.Type != "trip.updated" {
		t.Errorf("plan-scoped friend did not receive trip.updated (ok=%v, type=%q)", ok, e.Type)
	}
}

func TestPlanUpdated_ScopedToVisibility(t *testing.T) {
	pool := testsupport.NewPool(t)
	st := store.New(pool)
	hub := sse.NewHub()
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "notify-powner", false, true)
	stranger := testsupport.InsertUser(t, pool, "notify-pstranger", false, true)
	trip, err := st.CreateTrip(ctx, store.CreateTripPayload{Name: "Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	plan, err := st.CreatePlan(ctx, store.CreatePlanPayload{TripID: trip.ID, Type: "dining", Title: "Dinner"}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	ownerCh, unsub1 := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub1()
	strangerCh, unsub2 := hub.Subscribe(sse.Subscription{ViewerID: stranger})
	defer unsub2()

	PlanUpdated(ctx, st, hub, trip.ID, plan.ID)

	if e, ok := recv(ownerCh); !ok || e.Type != "plan.updated" {
		t.Fatalf("owner did not receive plan.updated (ok=%v, type=%q)", ok, e.Type)
	}
	if _, ok := recv(strangerCh); ok {
		t.Error("non-visible user received a plan.updated event")
	}
}

// A failing visibility query is logged and swallowed (never published): a
// cancelled context makes VisibleTripUserIDs / VisiblePlanUserIDs error.
func TestNotify_VisibilityErrorIsSwallowed(t *testing.T) {
	pool := testsupport.NewPool(t)
	st := store.New(pool)
	hub := sse.NewHub()

	owner := testsupport.InsertUser(t, pool, "notify-verr", false, true)
	trip, err := st.CreateTrip(context.Background(), store.CreateTripPayload{Name: "Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	plan, err := st.CreatePlan(context.Background(), store.CreatePlanPayload{TripID: trip.ID, Type: "dining", Title: "D"}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel() // the visibility query will fail on a cancelled context

	TripUpdated(cancelled, st, hub, trip.ID)
	PlanUpdated(cancelled, st, hub, trip.ID, plan.ID)

	if _, ok := recv(ch); ok {
		t.Error("nothing should be published when the visibility query fails")
	}
}

// A marshal failure is logged and swallowed. Marshalling an int-only payload
// can't fail in practice, so the encoder seam is forced to error here.
func TestNotify_MarshalErrorIsSwallowed(t *testing.T) {
	pool := testsupport.NewPool(t)
	st := store.New(pool)
	hub := sse.NewHub()
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "notify-merr", false, true)
	trip, err := st.CreateTrip(ctx, store.CreateTripPayload{Name: "Trip"}, owner)
	if err != nil {
		t.Fatalf("CreateTrip: %v", err)
	}
	plan, err := st.CreatePlan(ctx, store.CreatePlanPayload{TripID: trip.ID, Type: "dining", Title: "D"}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	orig := marshalJSON
	t.Cleanup(func() { marshalJSON = orig })
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }

	ch, unsub := hub.Subscribe(sse.Subscription{ViewerID: owner})
	defer unsub()

	TripUpdated(ctx, st, hub, trip.ID)
	PlanUpdated(ctx, st, hub, trip.ID, plan.ID)

	if _, ok := recv(ch); ok {
		t.Error("nothing should be published when marshalling fails")
	}
}

func TestNotify_NilStoreOrHubIsNoOp(t *testing.T) {
	ctx := context.Background()
	// Must not panic with nil collaborators.
	TripUpdated(ctx, nil, nil, 1)
	PlanUpdated(ctx, nil, nil, 1, 2)
	PlanDeleted(ctx, nil, nil, 1, 2)
}
