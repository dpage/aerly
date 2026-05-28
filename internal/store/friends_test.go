package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

func TestRequestFriendshipFreshPending(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	f, err := s.RequestFriendship(ctx, a, b, "test@example.com")
	if err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if f.Status != "pending" || f.RequestedBy != a {
		t.Errorf("unexpected: %+v", f)
	}
	if f.FriendID(a) != b || f.FriendID(b) != a {
		t.Errorf("FriendID orientation broken: %+v", f)
	}
}

func TestRequestFriendshipCrossDirectionAccepts(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b, "test@example.com"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	// b initiating the reverse direction implicitly accepts a's pending.
	got, err := s.RequestFriendship(ctx, b, a, "test@example.com")
	if err != nil {
		t.Fatalf("reverse request: %v", err)
	}
	if got.Status != "accepted" {
		t.Errorf("status = %q, want accepted", got.Status)
	}
	if got.AcceptedAt == nil {
		t.Error("accepted_at should be set after implicit accept")
	}
}

func TestRequestFriendshipNoopOnDuplicate(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	first, _ := s.RequestFriendship(ctx, a, b, "test@example.com")
	second, _ := s.RequestFriendship(ctx, a, b, "test@example.com")
	if first.Status != "pending" || second.Status != "pending" {
		t.Errorf("status should stay pending: %+v / %+v", first, second)
	}
	if !first.RequestedAt.Equal(second.RequestedAt) {
		t.Error("duplicate request should not refresh requested_at")
	}
}

func TestRequestFriendshipRejectsSelf(t *testing.T) {
	s := newStore(t)
	a := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, a, "x@y.com"); err == nil {
		t.Error("self-friend should error")
	}
}

func TestAcceptFriendshipRequiresOtherParty(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b, "test@example.com"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The requester themselves can't accept their own pending row.
	if _, err := s.AcceptFriendship(ctx, a, b); !errors.Is(err, ErrNotFound) {
		t.Errorf("self-accept should be ErrNotFound, got %v", err)
	}
	got, err := s.AcceptFriendship(ctx, b, a)
	if err != nil || got.Status != "accepted" {
		t.Fatalf("recipient accept: %v %+v", err, got)
	}
}

func TestRemoveFriendshipDeletesPendingOrAccepted(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b, "test@example.com"); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if _, err := s.RemoveFriendship(ctx, b, a); err != nil {
		t.Fatalf("remove accepted: %v", err)
	}
	if _, err := s.FriendshipBetween(ctx, a, b); !errors.Is(err, ErrNotFound) {
		t.Errorf("after remove, FriendshipBetween should be ErrNotFound, got %v", err)
	}
	if _, err := s.RemoveFriendship(ctx, b, a); !errors.Is(err, ErrNotFound) {
		t.Errorf("double-remove → ErrNotFound, got %v", err)
	}
}

func TestRemoveFriendshipDropsCrossSharesOnAcceptedEdge(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	c := mkUser(t, s)

	// Accepted friendship so the cleanup branch runs.
	if _, err := s.RequestFriendship(ctx, a, b, ""); err != nil {
		t.Fatalf("seed friendship: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// Flight created by a, b is a passenger; flight created by b, a is
	// a sharee; flight created by c with both a and b as passengers
	// (not touched by the cleanup — it's a third party's flight).
	now := time.Now().UTC().Truncate(time.Second)
	soon := now.Add(time.Hour)
	later := now.Add(3 * time.Hour)

	makeFlight := func(creator int64, ident string) *Flight {
		f, err := s.CreateFlight(ctx, CreateFlightPayload{
			Ident: ident, ScheduledOut: soon, ScheduledIn: later,
			OriginIATA: "LHR", DestIATA: "JFK",
		}, creator)
		if err != nil {
			t.Fatalf("CreateFlight %s: %v", ident, err)
		}
		return f
	}
	fAB := makeFlight(a, "AB1") // a creates, b passenger
	fBA := makeFlight(b, "BA1") // b creates, a sharee
	fC := makeFlight(c, "CC1")  // c creates, a+b passengers

	for _, op := range []struct {
		add  func(ctx context.Context, fid, uid int64) error
		fid  int64
		uid  int64
		name string
	}{
		{s.AddPassenger, fAB.ID, b, "fAB passenger b"},
		{s.AddShare, fBA.ID, a, "fBA share a"},
		{s.AddPassenger, fC.ID, a, "fC passenger a"},
		{s.AddPassenger, fC.ID, b, "fC passenger b"},
	} {
		if err := op.add(ctx, op.fid, op.uid); err != nil {
			t.Fatalf("%s: %v", op.name, err)
		}
	}

	affected, err := s.RemoveFriendship(ctx, a, b)
	if err != nil {
		t.Fatalf("RemoveFriendship: %v", err)
	}

	// Expect fAB and fBA in affected (cross-pair rows removed); fC must
	// NOT appear (third-party creator — both passengers stay).
	gotAffected := map[int64]bool{}
	for _, id := range affected {
		gotAffected[id] = true
	}
	if !gotAffected[fAB.ID] || !gotAffected[fBA.ID] {
		t.Errorf("affected = %v, want fAB=%d and fBA=%d included", affected, fAB.ID, fBA.ID)
	}
	if gotAffected[fC.ID] {
		t.Errorf("affected = %v, must NOT include third-party flight fC=%d", affected, fC.ID)
	}

	// fAB now has no passengers; fBA has no shares; fC still has both.
	if pax, _ := s.PassengersByFlight(ctx, []int64{fAB.ID}); len(pax[fAB.ID]) != 0 {
		t.Errorf("fAB passengers after unfriend = %v, want empty", pax[fAB.ID])
	}
	if sh, _ := s.SharedUserIDsByFlight(ctx, []int64{fBA.ID}); len(sh[fBA.ID]) != 0 {
		t.Errorf("fBA shares after unfriend = %v, want empty", sh[fBA.ID])
	}
	pax, _ := s.PassengersByFlight(ctx, []int64{fC.ID})
	gotC := map[int64]bool{}
	for _, uid := range pax[fC.ID] {
		gotC[uid] = true
	}
	if !gotC[a] || !gotC[b] {
		t.Errorf("fC passengers after unfriend = %v, want a and b retained", pax[fC.ID])
	}
}

func TestRemoveFriendshipPendingLeavesGrantsAlone(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b, ""); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	f, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PA1", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, a)
	if err != nil {
		t.Fatalf("flight: %v", err)
	}
	if err := s.AddPassenger(ctx, f.ID, b); err != nil {
		t.Fatalf("AddPassenger: %v", err)
	}

	affected, err := s.RemoveFriendship(ctx, a, b)
	if err != nil {
		t.Fatalf("RemoveFriendship pending: %v", err)
	}
	if len(affected) != 0 {
		t.Errorf("pending cancel must not touch grants: affected = %v", affected)
	}
	pax, _ := s.PassengersByFlight(ctx, []int64{f.ID})
	if len(pax[f.ID]) != 1 || pax[f.ID][0] != b {
		t.Errorf("passenger row should survive pending cancel: %v", pax[f.ID])
	}
}

func TestListFriendshipsOrientedAroundViewer(t *testing.T) {
	s := newStore(t)
	a, b, c := mkUser(t, s), mkUser(t, s), mkUser(t, s)

	// a outgoing → b (pending)
	if _, err := s.RequestFriendship(ctx, a, b, "test@example.com"); err != nil {
		t.Fatalf("a→b: %v", err)
	}
	// c incoming ← a (pending, from a's view it's outgoing)
	if _, err := s.RequestFriendship(ctx, a, c, "test@example.com"); err != nil {
		t.Fatalf("a→c: %v", err)
	}
	// b later sends request back (accepts a↔b)
	if _, err := s.RequestFriendship(ctx, b, a, "test@example.com"); err != nil {
		t.Fatalf("b→a: %v", err)
	}

	rows, err := s.ListFriendships(ctx, a)
	if err != nil {
		t.Fatalf("ListFriendships(a): %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("a sees %d rows, want 2", len(rows))
	}
	var sawAcceptedB, sawPendingC bool
	for _, r := range rows {
		switch r.FriendID(a) {
		case b:
			if r.Status != "accepted" {
				t.Errorf("a↔b should be accepted, got %s", r.Status)
			}
			sawAcceptedB = true
		case c:
			if r.Status != "pending" || r.RequestedBy != a {
				t.Errorf("a↔c should be pending requested_by=a, got %+v", r)
			}
			sawPendingC = true
		}
	}
	if !sawAcceptedB || !sawPendingC {
		t.Errorf("missing rows: %+v", rows)
	}
}

func TestUpsertPendingFriendInviteAndConsume(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	created, err := s.UpsertPendingFriendInvite(ctx, inviter, "  NewFriend@Example.COM  ", "join us")
	if err != nil || !created {
		t.Fatalf("UpsertPendingFriendInvite: created=%v err=%v", created, err)
	}
	// Duplicate must return created=false (so the caller skips a second email).
	again, err := s.UpsertPendingFriendInvite(ctx, inviter, "newfriend@example.com", "")
	if err != nil || again {
		t.Fatalf("duplicate: created=%v err=%v", again, err)
	}
	// Different inviter, same email is its own queue entry.
	other := mkUser(t, s)
	if c, _ := s.UpsertPendingFriendInvite(ctx, other, "newfriend@example.com", ""); !c {
		t.Error("second inviter should get its own pending row")
	}
}

func TestLinkLoginConsumesPendingInvites(t *testing.T) {
	s := newStore(t)
	inviter1 := mkUser(t, s)
	inviter2 := mkUser(t, s)
	// Pre-seed two pending invites addressed at the same email from two
	// different inviters; LinkLogin should turn both into accepted
	// friendships once the new user signs in with that email.
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter1, "joiner@example.com", ""); err != nil {
		t.Fatalf("seed inv1: %v", err)
	}
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter2, "JOINER@example.com", ""); err != nil {
		t.Fatalf("seed inv2: %v", err)
	}

	joined, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "777",
			Username: "joiner", Email: "joiner@example.com"}, false)
	if err != nil {
		t.Fatalf("LinkLogin joiner: %v", err)
	}

	for _, inviter := range []int64{inviter1, inviter2} {
		got, err := s.FriendshipBetween(ctx, joined.ID, inviter)
		if err != nil {
			t.Errorf("missing friendship to inviter %d: %v", inviter, err)
			continue
		}
		if got.Status != "accepted" {
			t.Errorf("inviter %d → status %q, want accepted", inviter, got.Status)
		}
	}

	// Pending rows should be drained.
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_friend_invites WHERE email_lower = 'joiner@example.com'`,
	).Scan(&n); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if n != 0 {
		t.Errorf("pending invites not drained: %d remain", n)
	}
}

func TestListOutgoingPendingInvites(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "first@example.com", "hi"); err != nil {
		t.Fatalf("seed first: %v", err)
	}
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "Second@Example.COM", ""); err != nil {
		t.Fatalf("seed second: %v", err)
	}
	other := mkUser(t, s)
	if _, err := s.UpsertPendingFriendInvite(ctx, other, "other@example.com", ""); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	got, err := s.ListOutgoingPendingInvites(ctx, inviter)
	if err != nil {
		t.Fatalf("ListOutgoingPendingInvites: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	emails := map[string]bool{}
	for _, p := range got {
		emails[p.EmailLower] = true
	}
	if !emails["first@example.com"] || !emails["second@example.com"] {
		t.Errorf("unexpected emails: %+v", emails)
	}
}

func TestRequestFriendshipStoresInvitedEmail(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	f, err := s.RequestFriendship(ctx, a, b, "Typed@Example.com")
	if err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	// Store normalizes to lowercase so wire output can't leak target
	// existence via casing (see no-enumeration invariant in friends.go).
	if f.InvitedEmail != "typed@example.com" {
		t.Errorf("InvitedEmail = %q, want %q", f.InvitedEmail, "typed@example.com")
	}
	got, err := s.FriendshipBetween(ctx, a, b)
	if err != nil {
		t.Fatalf("FriendshipBetween: %v", err)
	}
	if got.InvitedEmail != "typed@example.com" {
		t.Errorf("re-read InvitedEmail = %q, want %q", got.InvitedEmail, "typed@example.com")
	}
}

// Sanity: open signups default to non-superuser and a unique username when
// the provider-supplied login collides via mixed case.
func TestLinkLoginCaseInsensitiveUsernameCollision(t *testing.T) {
	s := newStore(t)
	_, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "111", Username: "Alice"}, true)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	u, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "222", Username: "ALICE"}, false)
	if err != nil {
		t.Fatalf("conflicting username: %v", err)
	}
	if strings.EqualFold(u.Username, "alice") {
		t.Errorf("expected suffix, got %q", u.Username)
	}
}

func TestCancelOutgoingInviteKnownTarget(t *testing.T) {
	s := newStore(t)
	inviter, target := mkUser(t, s), mkUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, target, "target@example.com"); err != nil {
		t.Fatalf("seed verified email: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, inviter, target, "Target@Example.com"); err != nil {
		t.Fatalf("seed friendship: %v", err)
	}

	if err := s.CancelOutgoingInvite(ctx, inviter, "Target@Example.com"); err != nil {
		t.Fatalf("CancelOutgoingInvite: %v", err)
	}
	if _, err := s.FriendshipBetween(ctx, inviter, target); !errors.Is(err, ErrNotFound) {
		t.Errorf("friendship should be gone, got %v", err)
	}
}

func TestCancelOutgoingInviteUnknownTarget(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "stranger@example.com", "hi"); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	if err := s.CancelOutgoingInvite(ctx, inviter, "STRANGER@example.com"); err != nil {
		t.Fatalf("CancelOutgoingInvite: %v", err)
	}
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_friend_invites WHERE inviter_id = $1`,
		inviter).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("pending invite not deleted: %d remain", n)
	}
}

func TestCancelOutgoingInviteNoMatchIsNoop(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	// No row exists. Must return nil (the handler relies on the no-op being
	// indistinguishable from a real cancellation).
	if err := s.CancelOutgoingInvite(ctx, inviter, "nobody@example.com"); err != nil {
		t.Errorf("no-op cancel returned %v", err)
	}
}

func TestCountIncomingFriendRequestsEmpty(t *testing.T) {
	s := newStore(t)
	me := mkUser(t, s)
	n, err := s.CountIncomingFriendRequests(ctx, me)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
}

func TestCountIncomingFriendRequestsIgnoresOutgoingAndAccepted(t *testing.T) {
	s := newStore(t)
	me := mkUser(t, s)
	otherA := mkUser(t, s)
	otherB := mkUser(t, s)

	// Outgoing pending: me requested otherA.
	if _, err := s.RequestFriendship(ctx, me, otherA, ""); err != nil {
		t.Fatalf("outgoing: %v", err)
	}
	// Accepted: cross-direction with otherB.
	if _, err := s.RequestFriendship(ctx, me, otherB, ""); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, otherB, me); err != nil {
		t.Fatalf("accept: %v", err)
	}

	n, err := s.CountIncomingFriendRequests(ctx, me)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0 (outgoing+accepted only)", n)
	}
}

func TestCountIncomingFriendRequestsMultiple(t *testing.T) {
	s := newStore(t)
	me := mkUser(t, s)
	inviter1 := mkUser(t, s)
	inviter2 := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, inviter1, me, ""); err != nil {
		t.Fatalf("seed1: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, inviter2, me, ""); err != nil {
		t.Fatalf("seed2: %v", err)
	}

	n, err := s.CountIncomingFriendRequests(ctx, me)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
}

func TestAreAcceptedFriends(t *testing.T) {
	s := newStore(t)
	a := testsupport.InsertUser(t, s.pool, fmt.Sprintf("aaf-a%d", loginSeq.Add(1)), false, true)
	b := testsupport.InsertUser(t, s.pool, fmt.Sprintf("aaf-b%d", loginSeq.Add(1)), false, true)
	c := testsupport.InsertUser(t, s.pool, fmt.Sprintf("aaf-c%d", loginSeq.Add(1)), false, true)

	// No row at all → false.
	ok, err := s.AreAcceptedFriends(ctx, a, b)
	if err != nil || ok {
		t.Fatalf("no row: ok=%v err=%v want false,nil", ok, err)
	}

	// Pending request → false.
	if _, err := s.RequestFriendship(ctx, a, b, ""); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	ok, err = s.AreAcceptedFriends(ctx, a, b)
	if err != nil || ok {
		t.Fatalf("pending: ok=%v err=%v want false,nil", ok, err)
	}

	// Accept → true, and order of arguments doesn't matter.
	if _, err := s.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	ok, err = s.AreAcceptedFriends(ctx, a, b)
	if err != nil || !ok {
		t.Fatalf("accepted (a,b): ok=%v err=%v want true,nil", ok, err)
	}
	ok, err = s.AreAcceptedFriends(ctx, b, a)
	if err != nil || !ok {
		t.Fatalf("accepted (b,a): ok=%v err=%v want true,nil", ok, err)
	}

	// Unrelated user → false.
	ok, err = s.AreAcceptedFriends(ctx, a, c)
	if err != nil || ok {
		t.Fatalf("unrelated: ok=%v err=%v want false,nil", ok, err)
	}

	// Self → false (cheap guard; mirrors FriendshipBetween).
	ok, err = s.AreAcceptedFriends(ctx, a, a)
	if err != nil || ok {
		t.Fatalf("self: ok=%v err=%v want false,nil", ok, err)
	}
}
