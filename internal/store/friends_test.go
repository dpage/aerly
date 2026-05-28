package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"

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
	if err := s.RemoveFriendship(ctx, b, a); err != nil {
		t.Fatalf("remove accepted: %v", err)
	}
	if _, err := s.FriendshipBetween(ctx, a, b); !errors.Is(err, ErrNotFound) {
		t.Errorf("after remove, FriendshipBetween should be ErrNotFound, got %v", err)
	}
	if err := s.RemoveFriendship(ctx, b, a); !errors.Is(err, ErrNotFound) {
		t.Errorf("double-remove → ErrNotFound, got %v", err)
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
