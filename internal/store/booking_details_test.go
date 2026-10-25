package store

import (
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestCanSeeBookingDetails covers who sees a plan's booking details (issue
// #163): the trip owner, the plan's creator, its and the trip's passengers, owner/editor trip
// members and superusers do; a viewer member, a friend the plan is shared
// with and a stranger do not, and a missing plan reports false.
func TestCanSeeBookingDetails(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	creator := mkUser(t, s)
	editor := mkUser(t, s)
	viewer := mkUser(t, s)
	pax := mkUser(t, s)
	tripPax := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	admin := testsupport.InsertUser(t, s.pool, "bookingadmin", true, true)
	trip := mkTrip(t, s, owner)
	for _, u := range []int64{creator, editor, viewer, pax, tripPax, friend} {
		befriendStore(t, s, owner, u)
	}
	addMember(t, s, trip, creator, "viewer")
	addMember(t, s, trip, editor, "editor")
	addMember(t, s, trip, viewer, "viewer")
	plan := mkPlan(t, s, trip, creator)
	addPlanPassenger(t, s, plan, pax)
	// A trip passenger with no plan_passengers row, as after a plan is moved
	// into the trip.
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_passengers (trip_id, user_id) VALUES ($1, $2)`, trip, tripPax); err != nil {
		t.Fatalf("add trip passenger: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE plans SET share_all_friends = true WHERE id = $1`, plan); err != nil {
		t.Fatalf("share plan with all friends: %v", err)
	}

	for name, c := range map[string]struct {
		plan, user int64
		want       bool
	}{
		"owner":     {plan, owner, true},
		"creator":   {plan, creator, true},
		"editor":    {plan, editor, true},
		"passenger": {plan, pax, true},
		"trip pax":  {plan, tripPax, true},
		"superuser": {plan, admin, true},
		"viewer":    {plan, viewer, false},
		"friend":    {plan, friend, false},
		"stranger":  {plan, stranger, false},
		"missing":   {plan + 1000000, owner, false},
	} {
		got, err := s.CanSeeBookingDetails(ctx, c.plan, c.user)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != c.want {
			t.Errorf("%s: CanSeeBookingDetails = %v, want %v", name, got, c.want)
		}
	}
}
