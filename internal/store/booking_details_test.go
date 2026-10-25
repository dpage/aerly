package store

import (
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestCanSeeBookingDetails covers who sees a plan's booking details (issue
// #163): the trip owner, the plan's creator, its passengers, owner/editor trip
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
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	admin := testsupport.InsertUser(t, s.pool, "bookingadmin", true, true)
	trip := mkTrip(t, s, owner)
	for _, u := range []int64{creator, editor, viewer, pax, friend} {
		befriendStore(t, s, owner, u)
	}
	addMember(t, s, trip, creator, "viewer")
	addMember(t, s, trip, editor, "editor")
	addMember(t, s, trip, viewer, "viewer")
	plan := mkPlan(t, s, trip, creator)
	addPlanPassenger(t, s, plan, pax)
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
