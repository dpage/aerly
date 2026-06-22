package store

import (
	"errors"
	"testing"
)

func TestG2FriendshipIncomingToAndBetween(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	requester := mkUser(t, s)
	recipient := mkUser(t, s)

	if _, err := s.RequestFriendship(ctx, requester, recipient, ""); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}

	f, err := s.FriendshipBetween(ctx, recipient, requester)
	if err != nil {
		t.Fatalf("FriendshipBetween: %v", err)
	}
	// The recipient sees it as incoming; the requester does not.
	if !f.IncomingTo(recipient) {
		t.Error("IncomingTo(recipient) = false, want true")
	}
	if f.IncomingTo(requester) {
		t.Error("IncomingTo(requester) = true, want false")
	}
	// FriendID orients to the other user.
	if f.FriendID(recipient) != requester {
		t.Errorf("FriendID(recipient) = %d, want %d", f.FriendID(recipient), requester)
	}

	// Once accepted, it is no longer "incoming" to anyone.
	if _, err := s.AcceptFriendship(ctx, recipient, requester); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	f, _ = s.FriendshipBetween(ctx, requester, recipient)
	if f.IncomingTo(recipient) {
		t.Error("accepted friendship should not be incoming")
	}

	// Self / unknown → ErrNotFound.
	if _, err := s.FriendshipBetween(ctx, requester, requester); !errors.Is(err, ErrNotFound) {
		t.Errorf("FriendshipBetween self = %v, want ErrNotFound", err)
	}
	stranger := mkUser(t, s)
	if _, err := s.FriendshipBetween(ctx, requester, stranger); !errors.Is(err, ErrNotFound) {
		t.Errorf("FriendshipBetween unknown = %v, want ErrNotFound", err)
	}
}

func TestG2RequestFriendshipSelfAndCrossDirection(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	a := mkUser(t, s)
	b := mkUser(t, s)

	// Cannot friend yourself.
	if _, err := s.RequestFriendship(ctx, a, a, ""); err == nil {
		t.Error("RequestFriendship self should error")
	}

	// a → b pending.
	if _, err := s.RequestFriendship(ctx, a, b, ""); err != nil {
		t.Fatalf("RequestFriendship a→b: %v", err)
	}
	// Duplicate same-direction request is a no-op, still pending.
	dup, err := s.RequestFriendship(ctx, a, b, "")
	if err != nil {
		t.Fatalf("duplicate request: %v", err)
	}
	if dup.Status != "pending" {
		t.Errorf("duplicate status = %q, want pending", dup.Status)
	}
	// b → a (cross-direction) implicitly accepts.
	acc, err := s.RequestFriendship(ctx, b, a, "")
	if err != nil {
		t.Fatalf("cross-direction request: %v", err)
	}
	if acc.Status != "accepted" {
		t.Errorf("cross-direction status = %q, want accepted", acc.Status)
	}
	// A further request once accepted is a no-op returning the accepted row.
	again, err := s.RequestFriendship(ctx, a, b, "")
	if err != nil || again.Status != "accepted" {
		t.Errorf("request after accept = %+v, %v; want accepted", again, err)
	}
}

func TestG2ListFriendshipsAndOutgoingInvites(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	me := mkUser(t, s)
	friend := mkUser(t, s)
	befriendStore(t, s, me, friend)

	fs, err := s.ListFriendships(ctx, me)
	if err != nil {
		t.Fatalf("ListFriendships: %v", err)
	}
	if len(fs) != 1 || fs[0].Status != "accepted" {
		t.Errorf("ListFriendships = %+v, want one accepted", fs)
	}

	// An email-only outgoing invite shows under ListOutgoingPendingInvites.
	if _, err := s.UpsertPendingFriendInvite(ctx, me, "g2invite@example.com", "join"); err != nil {
		t.Fatalf("UpsertPendingFriendInvite: %v", err)
	}
	out, err := s.ListOutgoingPendingInvites(ctx, me)
	if err != nil {
		t.Fatalf("ListOutgoingPendingInvites: %v", err)
	}
	if len(out) != 1 || out[0].EmailLower != "g2invite@example.com" {
		t.Errorf("ListOutgoingPendingInvites = %+v", out)
	}
}

func TestG2CancelOutgoingInvite(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	inviter := mkUser(t, s)

	// Empty email → error.
	if err := s.CancelOutgoingInvite(ctx, inviter, "   "); err == nil {
		t.Error("CancelOutgoingInvite empty email should error")
	}

	// Cancel an email-only pending invite.
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "g2cancel@example.com", ""); err != nil {
		t.Fatalf("UpsertPendingFriendInvite: %v", err)
	}
	if err := s.CancelOutgoingInvite(ctx, inviter, "G2Cancel@Example.com"); err != nil {
		t.Fatalf("CancelOutgoingInvite: %v", err)
	}
	out, _ := s.ListOutgoingPendingInvites(ctx, inviter)
	if len(out) != 0 {
		t.Errorf("invite not cancelled: %+v", out)
	}

	// Cancel a known-target pending friendship (requested_by = inviter, with
	// invited_email). Drives the second DELETE branch.
	target := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, inviter, target, "g2known@example.com"); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if err := s.CancelOutgoingInvite(ctx, inviter, "g2known@example.com"); err != nil {
		t.Fatalf("CancelOutgoingInvite known: %v", err)
	}
	if _, err := s.FriendshipBetween(ctx, inviter, target); !errors.Is(err, ErrNotFound) {
		t.Errorf("friendship not cancelled = %v, want ErrNotFound", err)
	}
	// Cancelling nothing is still a no-op success (identical 204 semantics).
	if err := s.CancelOutgoingInvite(ctx, inviter, "g2nothing@example.com"); err != nil {
		t.Errorf("CancelOutgoingInvite no match should be nil, got %v", err)
	}
}

func TestG2UpsertPendingFriendInviteEmptyEmail(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	inviter := mkUser(t, s)
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "   ", ""); err == nil {
		t.Error("UpsertPendingFriendInvite empty email should error")
	}
}

// TestG2ConsumePendingShareUnverified covers consumePendingSharesTx leaving a
// share queued when the joiner's matching email isn't verified yet, then
// consuming it once it is. Driven through the real LinkLogin entrypoint.
func TestG2ConsumeMultiplePendingShares(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	planID := mkPlan(t, s, trip, owner)

	// Two shares (a trip and a plan) plus a friend invite for the same email.
	if err := s.InsertPendingShare(ctx, PendingShare{
		EmailLower: "g2multi@example.com", Kind: "trip", TargetID: trip, Role: "editor", InviterID: owner,
	}); err != nil {
		t.Fatalf("InsertPendingShare trip: %v", err)
	}
	if err := s.InsertPendingShare(ctx, PendingShare{
		EmailLower: "g2multi@example.com", Kind: "plan", TargetID: planID, Role: "", InviterID: owner,
	}); err != nil {
		t.Fatalf("InsertPendingShare plan: %v", err)
	}
	if _, err := s.UpsertPendingFriendInvite(ctx, owner, "g2multi@example.com", ""); err != nil {
		t.Fatalf("UpsertPendingFriendInvite: %v", err)
	}

	joiner := linkLoginWithEmail(t, s, "g2multilogin", "g2multi@example.com")

	// The trip share converted to an editor membership.
	role, err := s.TripRole(ctx, trip, joiner)
	if err != nil || role != "editor" {
		t.Errorf("joiner trip role = %q, %v; want editor", role, err)
	}
	// The plan share converted to a plan passenger.
	var onPlan bool
	s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM plan_passengers WHERE plan_id=$1 AND user_id=$2)`,
		planID, joiner).Scan(&onPlan)
	if !onPlan {
		t.Error("plan pending share should convert to a passenger")
	}
	// The friendship landed accepted.
	if ok, _ := s.AreAcceptedFriends(ctx, owner, joiner); !ok {
		t.Error("invite should accept the friendship on login")
	}
}

func TestG2CountIncomingFriendRequests(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	me := mkUser(t, s)
	r1 := mkUser(t, s)
	r2 := mkUser(t, s)

	if n, _ := s.CountIncomingFriendRequests(ctx, me); n != 0 {
		t.Errorf("initial incoming = %d, want 0", n)
	}
	if _, err := s.RequestFriendship(ctx, r1, me, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestFriendship(ctx, r2, me, ""); err != nil {
		t.Fatal(err)
	}
	// An outgoing request from me does not count.
	out := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, me, out, ""); err != nil {
		t.Fatal(err)
	}
	n, err := s.CountIncomingFriendRequests(ctx, me)
	if err != nil {
		t.Fatalf("CountIncomingFriendRequests: %v", err)
	}
	if n != 2 {
		t.Errorf("incoming = %d, want 2", n)
	}
}

func TestG2FriendsErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	if _, err := s.FriendshipBetween(cc, 1, 2); err == nil {
		t.Error("FriendshipBetween cancelled should error")
	}
	if _, err := s.AreAcceptedFriends(cc, 1, 2); err == nil {
		t.Error("AreAcceptedFriends cancelled should error")
	}
	if _, err := s.AnyFriendshipEdge(cc, 1, 2); err == nil {
		t.Error("AnyFriendshipEdge cancelled should error")
	}
	if _, err := s.ListFriendships(cc, 1); err == nil {
		t.Error("ListFriendships cancelled should error")
	}
	if _, err := s.ListOutgoingPendingInvites(cc, 1); err == nil {
		t.Error("ListOutgoingPendingInvites cancelled should error")
	}
	if _, err := s.RequestFriendship(cc, 1, 2, ""); err == nil {
		t.Error("RequestFriendship cancelled should error")
	}
	if _, err := s.UpsertPendingFriendInvite(cc, 1, "x@example.com", ""); err == nil {
		t.Error("UpsertPendingFriendInvite cancelled should error")
	}
	if err := s.CancelOutgoingInvite(cc, 1, "x@example.com"); err == nil {
		t.Error("CancelOutgoingInvite cancelled should error")
	}
	if _, err := s.CountIncomingFriendRequests(cc, 1); err == nil {
		t.Error("CountIncomingFriendRequests cancelled should error")
	}
}
