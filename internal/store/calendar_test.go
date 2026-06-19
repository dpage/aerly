package store

import (
	"errors"
	"testing"
	"time"
)

// mkTypedPlan inserts a plan of the given type with a title and returns its id.
func mkTypedPlan(t *testing.T, s *Store, tripID, createdBy int64, typ, title, confirm, notes string) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, title, confirmation_ref, notes, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		tripID, typ, title, confirm, notes, createdBy,
	).Scan(&id); err != nil {
		t.Fatalf("insert typed plan: %v", err)
	}
	return id
}

// mkPart inserts a fully-specified plan_part and returns its id.
func mkPart(t *testing.T, s *Store, planID int64, startsAt time.Time, endsAt *time.Time, startTZ, endTZ, startLabel string) int64 {
	t.Helper()
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_tz, end_tz, start_label)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		planID, startsAt, endsAt, startTZ, endTZ, startLabel,
	).Scan(&id); err != nil {
		t.Fatalf("insert part: %v", err)
	}
	return id
}

func TestCalendarTokenIssueAndResolve(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)

	// CalendarToken issues on first call.
	ct, err := s.CalendarToken(ctx, u, "me", 0)
	if err != nil {
		t.Fatalf("CalendarToken: %v", err)
	}
	if ct.Token == "" || ct.Scope != "me" || ct.UserID != u || ct.ResourceID != 0 {
		t.Fatalf("unexpected token: %+v", ct)
	}

	// Second call returns the same token (idempotent fetch).
	ct2, err := s.CalendarToken(ctx, u, "me", 0)
	if err != nil {
		t.Fatalf("CalendarToken 2: %v", err)
	}
	if ct2.Token != ct.Token {
		t.Errorf("CalendarToken not stable: %q vs %q", ct.Token, ct2.Token)
	}

	// Resolve back to the owner with its scope+resource.
	info, err := s.CalendarTokenByValue(ctx, ct.Token)
	if err != nil {
		t.Fatalf("CalendarTokenByValue: %v", err)
	}
	if info.UserID != u || info.Scope != "me" || info.ResourceID != 0 {
		t.Errorf("CalendarTokenByValue = %+v, want user %d / me / 0", info, u)
	}

	// Unknown token → ErrNotFound.
	if _, err := s.CalendarTokenByValue(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown token err = %v, want ErrNotFound", err)
	}

	// Invalid scope is rejected.
	if _, err := s.CalendarToken(ctx, u, "bogus", 0); err == nil {
		t.Error("CalendarToken accepted invalid scope")
	}

	// trip/plan scope requires a resource id.
	if _, err := s.CalendarToken(ctx, u, "trip", 0); err == nil {
		t.Error("CalendarToken accepted trip scope without resource id")
	}
}

// TestCalendarTokenPerResourceGranularity: each (scope, resource) gets its own
// token, regenerating one does not disturb another, and a token resolves to its
// own resource only.
func TestCalendarTokenPerResourceGranularity(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)

	trip5, err := s.CalendarToken(ctx, u, "trip", 5)
	if err != nil {
		t.Fatalf("trip5 token: %v", err)
	}
	trip6, err := s.CalendarToken(ctx, u, "trip", 6)
	if err != nil {
		t.Fatalf("trip6 token: %v", err)
	}
	if trip5.Token == trip6.Token {
		t.Fatal("distinct trip resources shared a token")
	}
	if trip5.ResourceID != 5 || trip6.ResourceID != 6 {
		t.Fatalf("resource ids wrong: %d / %d", trip5.ResourceID, trip6.ResourceID)
	}

	// Each token resolves to its own resource.
	info5, _ := s.CalendarTokenByValue(ctx, trip5.Token)
	if info5.Scope != "trip" || info5.ResourceID != 5 {
		t.Errorf("trip5 resolves to %+v, want trip/5", info5)
	}

	// Regenerating trip 5 revokes ONLY trip 5; trip 6 is untouched.
	newTrip5, err := s.RegenerateCalendarToken(ctx, u, "trip", 5)
	if err != nil {
		t.Fatalf("regenerate trip5: %v", err)
	}
	if newTrip5.Token == trip5.Token {
		t.Fatal("regenerate did not change trip5 token")
	}
	if _, err := s.CalendarTokenByValue(ctx, trip5.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("old trip5 token still resolves: %v", err)
	}
	if _, err := s.CalendarTokenByValue(ctx, trip6.Token); err != nil {
		t.Errorf("trip6 token was disturbed by trip5 regenerate: %v", err)
	}
}

func TestCalendarTokenRegenerateRevokes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)
	first, err := s.CalendarToken(ctx, u, "trip", 1)
	if err != nil {
		t.Fatalf("CalendarToken: %v", err)
	}
	second, err := s.RegenerateCalendarToken(ctx, u, "trip", 1)
	if err != nil {
		t.Fatalf("RegenerateCalendarToken: %v", err)
	}
	if second.Token == first.Token {
		t.Fatal("regenerate did not change the token")
	}
	// Old token no longer resolves.
	if _, err := s.CalendarTokenByValue(ctx, first.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token still resolves: err=%v", err)
	}
	// New token resolves.
	if _, err := s.CalendarTokenByValue(ctx, second.Token); err != nil {
		t.Errorf("new token does not resolve: %v", err)
	}
}

func TestCalendarTokenListAndRevoke(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)
	other := mkUser(t, s)
	me, _ := s.CalendarToken(ctx, u, "me", 0)
	_, _ = s.CalendarToken(ctx, u, "trip", 7)

	toks, err := s.ListCalendarTokens(ctx, u)
	if err != nil {
		t.Fatalf("ListCalendarTokens: %v", err)
	}
	if len(toks) != 2 {
		t.Fatalf("ListCalendarTokens len = %d, want 2", len(toks))
	}

	// Another user cannot revoke u's token.
	if err := s.RevokeCalendarToken(ctx, other, me.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user revoke err = %v, want ErrNotFound", err)
	}
	// Owner can revoke.
	if err := s.RevokeCalendarToken(ctx, u, me.Token); err != nil {
		t.Errorf("owner revoke: %v", err)
	}
	if _, err := s.CalendarTokenByValue(ctx, me.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token still resolves: %v", err)
	}
}

// TestCalendarEventsVisibility is the central security test: a plan hidden from
// the token owner must be absent from their feed, and another user's token
// (resolving to a different viewer) must never see the owner's private plans.
func TestCalendarEventsVisibility(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")
	befriendStore(t, s, owner, member) // friend gate: a trip member must be an accepted friend (spec §4)

	start := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)

	// A public (default-visibility) plan everyone on the trip sees.
	pubPlan := mkTypedPlan(t, s, trip, owner, "flight", "BA286", "ABC123", "window seat")
	mkPart(t, s, pubPlan, start, &end, "Europe/London", "America/New_York", "LHR")

	// A plan hidden from `member`.
	hidPlan := mkTypedPlan(t, s, trip, owner, "hotel", "Secret Hotel", "", "")
	mkPart(t, s, hidPlan, start.Add(24*time.Hour), nil, "Europe/Paris", "", "Hotel")
	setVisibility(t, s, hidPlan, "hidden_from", member)

	// Owner sees both.
	ownerEv, err := s.CalendarEventsForTrip(ctx, owner, trip)
	if err != nil {
		t.Fatalf("CalendarEventsForTrip(owner): %v", err)
	}
	if len(ownerEv) != 2 {
		t.Fatalf("owner trip feed len = %d, want 2", len(ownerEv))
	}

	// Member sees only the public plan — the hidden one is absent.
	memberEv, err := s.CalendarEventsForTrip(ctx, member, trip)
	if err != nil {
		t.Fatalf("CalendarEventsForTrip(member): %v", err)
	}
	if len(memberEv) != 1 {
		t.Fatalf("member trip feed len = %d, want 1 (hidden plan must not leak)", len(memberEv))
	}
	if memberEv[0].PlanID != pubPlan {
		t.Errorf("member sees plan %d, want public plan %d", memberEv[0].PlanID, pubPlan)
	}

	// The owner's "me" feed contains both; the stranger's "me" feed (a totally
	// different token owner) sees none of the owner's plans.
	strangerEv, err := s.CalendarEventsForUser(ctx, stranger)
	if err != nil {
		t.Fatalf("CalendarEventsForUser(stranger): %v", err)
	}
	if len(strangerEv) != 0 {
		t.Errorf("stranger me feed len = %d, want 0 (no membership)", len(strangerEv))
	}

	// Single-plan feed: member cannot see the hidden plan even by id.
	hidForMember, err := s.CalendarEventsForPlan(ctx, member, hidPlan)
	if err != nil {
		t.Fatalf("CalendarEventsForPlan(member,hidden): %v", err)
	}
	if len(hidForMember) != 0 {
		t.Errorf("member single-plan feed for hidden plan len = %d, want 0", len(hidForMember))
	}
	// Owner can.
	hidForOwner, err := s.CalendarEventsForPlan(ctx, owner, hidPlan)
	if err != nil {
		t.Fatalf("CalendarEventsForPlan(owner,hidden): %v", err)
	}
	if len(hidForOwner) != 1 {
		t.Errorf("owner single-plan feed for hidden plan len = %d, want 1", len(hidForOwner))
	}

	// Field assembly check on the public flight event.
	ev := ownerEv[0]
	if ev.PlanID == pubPlan {
		if ev.Title != "BA286" || ev.Type != "flight" || ev.ConfirmationRef != "ABC123" {
			t.Errorf("event field assembly wrong: %+v", ev)
		}
		if ev.StartLabel != "LHR" || ev.StartTZ != "Europe/London" {
			t.Errorf("event place/tz wrong: %+v", ev)
		}
	}
}

// TestCalendarMeFeedScopedToOwnTrips: the "me" feed contains only the viewer's
// own trips — ones they created plus plans they're a passenger on — and excludes
// a friend's trip merely shared with them (issue #76).
func TestCalendarMeFeedScopedToOwnTrips(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	viewer := mkUser(t, s)
	friend := mkUser(t, s)
	befriendStore(t, s, viewer, friend)

	now := time.Now().UTC()

	// 1) A trip the viewer owns → its plan should be on the me feed.
	ownTrip := mkTrip(t, s, viewer)
	ownPlan := mkTypedPlan(t, s, ownTrip, viewer, "flight", "Own Flight", "", "")
	mkPart(t, s, ownPlan, now, nil, "Europe/London", "", "LHR")

	// 2) A friend's trip the viewer can see (default-visible, shared via
	//    membership + friendship) but is NOT travelling on → must be excluded.
	sharedTrip := mkTrip(t, s, friend)
	addMember(t, s, sharedTrip, viewer, "viewer")
	sharedPlan := mkTypedPlan(t, s, sharedTrip, friend, "hotel", "Friend Hotel", "", "")
	mkPart(t, s, sharedPlan, now.Add(24*time.Hour), nil, "Europe/Paris", "", "Hotel")

	// 3) A friend's trip where the viewer IS a passenger on a plan → that plan
	//    should be on the me feed (they're travelling on it).
	paxTrip := mkTrip(t, s, friend)
	paxPlan := mkTypedPlan(t, s, paxTrip, friend, "flight", "Pax Flight", "", "")
	mkPart(t, s, paxPlan, now.Add(48*time.Hour), nil, "Europe/London", "", "CDG")
	addPlanPassenger(t, s, paxPlan, viewer)

	ev, err := s.CalendarEventsForUser(ctx, viewer)
	if err != nil {
		t.Fatalf("CalendarEventsForUser: %v", err)
	}
	got := map[int64]bool{}
	for _, e := range ev {
		got[e.PlanID] = true
	}
	if !got[ownPlan] {
		t.Errorf("me feed missing the viewer's own plan %d", ownPlan)
	}
	if !got[paxPlan] {
		t.Errorf("me feed missing the passenger plan %d", paxPlan)
	}
	if got[sharedPlan] {
		t.Errorf("me feed LEAKED a friend's shared-only trip plan %d (issue #76)", sharedPlan)
	}
}

// TestCalendarEventsExcludeDismissed: a superseded/dismissed part is omitted.
func TestCalendarEventsExcludeDismissed(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkTypedPlan(t, s, trip, owner, "flight", "BA1", "", "")
	now := time.Now().UTC()
	pid := mkPart(t, s, plan, now, nil, "Europe/London", "", "LHR")
	if _, err := s.pool.Exec(ctx, `UPDATE plan_parts SET dismissed_at = NOW() WHERE id = $1`, pid); err != nil {
		t.Fatalf("dismiss part: %v", err)
	}
	ev, err := s.CalendarEventsForUser(ctx, owner)
	if err != nil {
		t.Fatalf("CalendarEventsForUser: %v", err)
	}
	if len(ev) != 0 {
		t.Errorf("dismissed part should be excluded; got %d events", len(ev))
	}
}
