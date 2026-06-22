package store

import (
	"testing"
)

// These tests exercise the mid-transaction "if err != nil { return err }"
// branches that a healthy database never triggers. Each test runs against the
// isolated, throwaway database testsupport hands out (dropped on cleanup), so
// breaking schema objects here is safe and mirrors the established pattern in
// errors_test.go (TestLinkLoginFirstQueryErrors drops tables to force a real
// DB error). We break a specific table so that one inner statement fails while
// the statements before it still succeed.

func TestG2ApplyAutoSharesTxErrors(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	target := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// SELECT error: break user_auto_shares so the initial query fails.
	t.Run("query error", func(t *testing.T) {
		s2 := newStore(t)
		if s2 == nil {
			return
		}
		o := mkUser(t, s2)
		tr := mkTrip(t, s2, o)
		if _, err := s2.pool.Exec(ctx, `ALTER TABLE user_auto_shares RENAME TO user_auto_shares_x`); err != nil {
			t.Fatalf("rename: %v", err)
		}
		tx, err := s2.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := applyAutoSharesTx(ctx, tx, tr, o); err == nil {
			t.Error("applyAutoSharesTx should fail when user_auto_shares is gone")
		}
	})

	// passenger insert error: a passenger auto-share, with trip_passengers
	// dropped so the trip_passengers INSERT fails.
	t.Run("passenger insert error", func(t *testing.T) {
		s2 := newStore(t)
		if s2 == nil {
			return
		}
		o := mkUser(t, s2)
		tg := mkUser(t, s2)
		tr := mkTrip(t, s2, o)
		if err := s2.SetAutoShare(ctx, o, tg, "passenger"); err != nil {
			t.Fatalf("SetAutoShare: %v", err)
		}
		if _, err := s2.pool.Exec(ctx, `ALTER TABLE trip_passengers RENAME TO trip_passengers_x`); err != nil {
			t.Fatalf("rename: %v", err)
		}
		tx, err := s2.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := applyAutoSharesTx(ctx, tx, tr, o); err == nil {
			t.Error("applyAutoSharesTx should fail when trip_passengers is gone")
		}
	})

	// passenger membership insert error: a passenger auto-share with
	// trip_members dropped — the trip_passengers INSERT succeeds, then the
	// follow-up trip_members(viewer) INSERT fails (the passenger branch's
	// second statement).
	t.Run("passenger membership insert error", func(t *testing.T) {
		s2 := newStore(t)
		if s2 == nil {
			return
		}
		o := mkUser(t, s2)
		tg := mkUser(t, s2)
		tr := mkTrip(t, s2, o)
		if err := s2.SetAutoShare(ctx, o, tg, "passenger"); err != nil {
			t.Fatalf("SetAutoShare: %v", err)
		}
		if _, err := s2.pool.Exec(ctx, `ALTER TABLE trip_members RENAME TO trip_members_w`); err != nil {
			t.Fatalf("rename: %v", err)
		}
		tx, err := s2.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := applyAutoSharesTx(ctx, tx, tr, o); err == nil {
			t.Error("applyAutoSharesTx should fail when trip_members is gone (passenger branch)")
		}
	})

	// viewer/editor membership insert error: drop trip_members so the
	// non-passenger INSERT fails.
	if err := s.SetAutoShare(ctx, owner, target, "viewer"); err != nil {
		t.Fatalf("SetAutoShare: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `ALTER TABLE trip_members RENAME TO trip_members_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := applyAutoSharesTx(ctx, tx, trip, owner); err == nil {
		t.Error("applyAutoSharesTx should fail when trip_members is gone")
	}
}

func TestG2CreateTripInnerErrors(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	// Drop trip_members so the owner-membership INSERT (after the trips INSERT
	// succeeds) fails, exercising CreateTrip's second-statement error branch.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE trip_members RENAME TO trip_members_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := s.CreateTrip(ctx, CreateTripPayload{Name: "Boom"}, owner); err == nil {
		t.Error("CreateTrip should fail when trip_members is gone")
	}
}

func TestG2ConsumePendingSharesTxErrors(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	joiner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	if err := s.UpsertVerifiedEmail(ctx, joiner, "g2cps@example.com"); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}

	t.Run("trip grant insert error", func(t *testing.T) {
		if err := s.InsertPendingShare(ctx, PendingShare{
			EmailLower: "g2cps@example.com", Kind: "trip", TargetID: trip, Role: "viewer", InviterID: owner,
		}); err != nil {
			t.Fatalf("InsertPendingShare: %v", err)
		}
		// Drop trip_members so converting the trip pending-share fails. The
		// DELETE ... RETURNING on pending_shares runs first and succeeds.
		if _, err := s.pool.Exec(ctx, `ALTER TABLE trip_members RENAME TO trip_members_y`); err != nil {
			t.Fatalf("rename: %v", err)
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := consumePendingSharesTx(ctx, tx, joiner); err == nil {
			t.Error("consumePendingSharesTx should fail when trip_members is gone")
		}
	})
}

func TestG2ConsumePendingSharesPlanInsertError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	joiner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	planID := mkPlan(t, s, trip, owner)
	if err := s.UpsertVerifiedEmail(ctx, joiner, "g2cpsp@example.com"); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	if err := s.InsertPendingShare(ctx, PendingShare{
		EmailLower: "g2cpsp@example.com", Kind: "plan", TargetID: planID, Role: "", InviterID: owner,
	}); err != nil {
		t.Fatalf("InsertPendingShare: %v", err)
	}
	// Drop plan_passengers so converting the plan pending-share fails.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE plan_passengers RENAME TO plan_passengers_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := consumePendingSharesTx(ctx, tx, joiner); err == nil {
		t.Error("consumePendingSharesTx should fail when plan_passengers is gone")
	}
}

func TestG2ConsumePendingInvitesTxErrors(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	joiner := mkUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, joiner, "g2cpi@example.com"); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	if _, err := s.UpsertPendingFriendInvite(ctx, owner, "g2cpi@example.com", ""); err != nil {
		t.Fatalf("UpsertPendingFriendInvite: %v", err)
	}
	// Drop friendships so the accepted-friendship upsert fails. The
	// DELETE ... RETURNING on pending_friend_invites runs first and succeeds.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE friendships RENAME TO friendships_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := consumePendingInvitesTx(ctx, tx, joiner); err == nil {
		t.Error("consumePendingInvitesTx should fail when friendships is gone")
	}
}

func TestG2RemoveTripMemberInnerErrors(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	other := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, other, "owner")

	// Drop trip_members after the lock so the owner-existence query (the first
	// statement after lockTripTx) fails. lockTripTx itself touches only trips.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE trip_members RENAME TO trip_members_z`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.RemoveTripMember(ctx, trip, other); err == nil {
		t.Error("RemoveTripMember should fail when trip_members is gone")
	}
}

func TestG2AddTripPassengerInnerErrors(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Drop trip_passengers so the first insert after the lock fails.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE trip_passengers RENAME TO trip_passengers_z`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.AddTripPassenger(ctx, trip, pax); err == nil {
		t.Error("AddTripPassenger should fail when trip_passengers is gone")
	}
}

func TestG2RemoveTripPassengerInnerErrors(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pax := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	if err := s.AddTripPassenger(ctx, trip, pax); err != nil {
		t.Fatalf("AddTripPassenger: %v", err)
	}
	planID := mkPlan(t, s, trip, owner)
	_ = planID
	// Drop plan_passengers so the second statement (the materialised-row delete)
	// fails after the trip_passengers delete reports a row.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE plan_passengers RENAME TO plan_passengers_z`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.RemoveTripPassenger(ctx, trip, pax); err == nil {
		t.Error("RemoveTripPassenger should fail when plan_passengers is gone")
	}
}

func TestG2RequestFriendshipConflictScanError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	a := mkUser(t, s)
	b := mkUser(t, s)
	// Seed a pending row from b→a so a's later request hits the conflict path.
	if _, err := s.RequestFriendship(ctx, b, a, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Rename the accepted_at column so the cross-direction accept UPDATE's
	// RETURNING fails after the locked SELECT succeeds.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE friendships RENAME COLUMN accepted_at TO accepted_at_x`); err != nil {
		t.Fatalf("rename column: %v", err)
	}
	// a→b: the INSERT conflicts (DO NOTHING, empty RETURNING -> ErrNotFound),
	// then the locked SELECT runs, then the accept UPDATE fails on the renamed
	// column.
	if _, err := s.RequestFriendship(ctx, a, b, ""); err == nil {
		t.Error("RequestFriendship should fail when accepted_at column is gone")
	}
}

func TestG2CancelOutgoingInviteInnerError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	inviter := mkUser(t, s)
	// Drop pending_friend_invites so the first DELETE in the tx fails.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE pending_friend_invites RENAME TO pfi_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.CancelOutgoingInvite(ctx, inviter, "g2gone@example.com"); err == nil {
		t.Error("CancelOutgoingInvite should fail when pending_friend_invites is gone")
	}
}

func TestG2SetTripTagsInnerError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	// Drop trip_tags so the DELETE (first tx statement) fails.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE trip_tags RENAME TO trip_tags_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.SetTripTags(ctx, trip, []string{"a"}); err == nil {
		t.Error("SetTripTags should fail when trip_tags is gone")
	}
}

func TestG2ReplaceFeedEventsInnerError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	feed, err := s.AddTripFeed(ctx, trip, "https://example.com/x.ics", "F", "")
	if err != nil {
		t.Fatalf("AddTripFeed: %v", err)
	}
	// Drop trip_feed_events so the initial DELETE fails.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE trip_feed_events RENAME TO tfe_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := s.ReplaceFeedEvents(ctx, feed.ID, nil); err == nil {
		t.Error("ReplaceFeedEvents should fail when trip_feed_events is gone")
	}
}
