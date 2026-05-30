package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// seedVerifiedEmail attaches a verified address to userID so the friend-
// invite path can find them via UserByVerifiedEmail.
func seedVerifiedEmail(t *testing.T, e *testEnv, userID int64, addr string) {
	t.Helper()
	if err := e.store.UpsertVerifiedEmail(context.Background(), userID, addr); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
}

func TestListFriendsRequiresAuth(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, "GET", "/api/friends", nil, 0)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestInviteFriendByEmailKnownUser(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}

	// Confirm a pending friendship now exists between alice → bob with alice
	// as the requester.
	f, err := e.store.FriendshipBetween(context.Background(), inviter, target)
	if err != nil {
		t.Fatalf("FriendshipBetween: %v", err)
	}
	if f.Status != "pending" || f.RequestedBy != inviter {
		t.Errorf("unexpected friendship: %+v", f)
	}
}

func TestInviteFriendByEmailUnknownAddressQueues(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "stranger@example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", w.Code, w.Body.String())
	}

	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_friend_invites
		 WHERE inviter_id = $1 AND email_lower = 'stranger@example.com'`,
		inviter).Scan(&n); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 pending invite, got %d", n)
	}
}

func TestInviteFriendResponseIdenticalForKnownAndUnknown(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	known := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, known, "bob@example.com")

	known1 := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter)
	unknown := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter)

	if known1.Code != unknown.Code {
		t.Errorf("status codes differ: %d vs %d", known1.Code, unknown.Code)
	}
	if known1.Body.String() != unknown.Body.String() {
		t.Errorf("response bodies leak target existence:\n  known=%q\n  unknown=%q",
			known1.Body.String(), unknown.Body.String())
	}
}

func TestInviteFriendBadEmail(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "not-an-email"}, inviter)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", w.Code)
	}
}

func TestInviteFriendSelfMatchesQuietly(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "alice", false)
	seedVerifiedEmail(t, e, me, "alice@example.com")

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "alice@example.com"}, me)
	// Self-invite must produce the same accepted response (no leak), and
	// must NOT create a friendship row.
	if w.Code != http.StatusAccepted {
		t.Errorf("code = %d, want 202", w.Code)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM friendships WHERE user_low = $1 OR user_high = $1`,
		me).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("self-invite created %d rows, want 0", n)
	}
}

func TestAcceptAndRemoveFriendRoundTrip(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")

	// Alice invites Bob; pending row created with Alice as the requester.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: code=%d body=%s", w.Code, w.Body.String())
	}

	// Bob (the recipient) accepts.
	acceptPath := "/api/friends/" + strconv.FormatInt(alice, 10) + "/accept"
	w := e.req(t, "POST", acceptPath, nil, bob)
	if w.Code != http.StatusOK {
		t.Fatalf("accept: code=%d body=%s", w.Code, w.Body.String())
	}
	var dto api.FriendshipDTO
	dto = decodeBody[api.FriendshipDTO](t, w)
	if dto.Status != "accepted" || dto.FriendID != alice {
		t.Errorf("bad accept DTO: %+v", dto)
	}

	// Bob unfriends Alice.
	removePath := "/api/friends/" + strconv.FormatInt(alice, 10)
	w = e.req(t, "DELETE", removePath, nil, bob)
	if w.Code != http.StatusNoContent {
		t.Errorf("remove: code=%d body=%s", w.Code, w.Body.String())
	}

	if _, err := e.store.FriendshipBetween(context.Background(), alice, bob); err == nil {
		t.Error("friendship should be gone after unfriend")
	}
}

func TestAcceptFriendMissingPendingReturns404(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	path := "/api/friends/" + strconv.FormatInt(alice, 10) + "/accept"
	w := e.req(t, "POST", path, nil, bob)
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

func TestListFriendsReturnsViewerOrientedDTOs(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %s", w.Body.String())
	}

	// From Alice's view the pending request is outgoing — the row carries
	// the typed email, NOT Bob's user_id, so Alice can't enumerate.
	w := e.req(t, "GET", "/api/friends", nil, alice)
	if w.Code != http.StatusOK {
		t.Fatalf("alice list: %d %s", w.Code, w.Body.String())
	}
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 1 {
		t.Fatalf("alice rows = %d, want 1", len(rows))
	}
	if rows[0].Direction != "outgoing" || rows[0].FriendID != 0 || rows[0].Email != "bob@example.com" {
		t.Errorf("alice DTO = %+v", rows[0])
	}

	// From Bob's view it's incoming, and he legitimately knows who's
	// asking — so FriendID is set, Email is not.
	w = e.req(t, "GET", "/api/friends", nil, bob)
	rows = decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 1 {
		t.Fatalf("bob rows = %d", len(rows))
	}
	if rows[0].Direction != "incoming" || rows[0].FriendID != alice || rows[0].Email != "" {
		t.Errorf("bob DTO = %+v", rows[0])
	}
}

func TestFriendshipDTOOmitsDirectionForAccepted(t *testing.T) {
	// Lightweight DTO-shape check that doesn't need a DB round-trip.
	accepted := store.Friendship{
		UserLow: 1, UserHigh: 2, Status: "accepted", RequestedBy: 1,
	}
	dto := api.ToFriendshipDTO(&accepted, 2)
	if dto.Direction != "" {
		t.Errorf("accepted friendship should have empty direction, got %q", dto.Direction)
	}
	if dto.FriendID != 1 {
		t.Errorf("FriendID = %d, want 1", dto.FriendID)
	}
}

func TestInviteFriendStoresTypedEmailOnFriendshipRow(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "Bob@Example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202", w.Code)
	}
	f, err := e.store.FriendshipBetween(context.Background(), inviter, target)
	if err != nil {
		t.Fatalf("FriendshipBetween: %v", err)
	}
	// Stored lowercased so the wire response is byte-identical regardless
	// of whether the target is registered — see no-enumeration invariant.
	if f.InvitedEmail != "bob@example.com" {
		t.Errorf("InvitedEmail = %q, want %q", f.InvitedEmail, "bob@example.com")
	}
}

func TestListFriendsOutgoingPendingHidesIdentity(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	known := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, known, "bob@example.com")

	// Known target: friendship row with invited_email.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "Bob@Example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatalf("known invite code = %d", w.Code)
	}
	// Unknown target: pending_friend_invites row only.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatalf("unknown invite code = %d", w.Code)
	}

	w := e.req(t, "GET", "/api/friends", nil, inviter)
	if w.Code != http.StatusOK {
		t.Fatalf("list code = %d", w.Code)
	}
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	emails := map[string]bool{}
	for _, r := range rows {
		if r.Direction != "outgoing" || r.Status != "pending" {
			t.Errorf("row not outgoing-pending: %+v", r)
		}
		if r.FriendID != 0 {
			t.Errorf("row leaks FriendID=%d for outgoing pending: %+v", r.FriendID, r)
		}
		if r.Email == "" {
			t.Errorf("row missing Email: %+v", r)
		}
		emails[strings.ToLower(r.Email)] = true
	}
	if !emails["bob@example.com"] || !emails["ghost@example.com"] {
		t.Errorf("emails = %+v, want both bob and ghost", emails)
	}
}

func TestCancelOutgoingInviteKnownTarget(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("seed invite failed")
	}

	w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "bob@example.com"}, inviter)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if _, err := e.store.FriendshipBetween(context.Background(), inviter, target); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("friendship still present: %v", err)
	}
}

func TestCancelOutgoingInviteUnknownTarget(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("seed invite failed")
	}

	w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "ghost@example.com"}, inviter)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", w.Code)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_friend_invites WHERE inviter_id = $1`,
		inviter).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("invite not deleted: %d remain", n)
	}
}

func TestCancelOutgoingInviteNoMatch204(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "nobody@example.com"}, inviter)
	if w.Code != http.StatusNoContent {
		t.Errorf("no-match cancel = %d, want 204", w.Code)
	}
}

func TestCancelOutgoingInviteBadInput(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	if w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": ""}, inviter); w.Code != http.StatusBadRequest {
		t.Errorf("empty email = %d, want 400", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "not-an-email"}, inviter); w.Code != http.StatusBadRequest {
		t.Errorf("bad email = %d, want 400", w.Code)
	}
}

func TestListFriendsOutgoingPendingDoesNotLeakViaCasing(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	known := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, known, "bob@example.com")

	// Both invites use mixed-case input. The wire response must NOT echo
	// the typed casing back, because that would let the inviter compare:
	// "did the case come back unchanged → target is registered".
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "Bob@Example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("known invite failed")
	}
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "Ghost@Example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("unknown invite failed")
	}

	w := e.req(t, "GET", "/api/friends", nil, inviter)
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Email != strings.ToLower(r.Email) {
			t.Errorf("email %q is not lowercase — leaks via casing", r.Email)
		}
	}
}

func TestListFriendsPendingPrecedesAcceptedAcrossSources(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	// Existing accepted friendship between alice and bob.
	if _, err := e.store.RequestFriendship(context.Background(), alice, bob, "bob@example.com"); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if _, err := e.store.AcceptFriendship(context.Background(), bob, alice); err != nil {
		t.Fatalf("seed accept: %v", err)
	}
	// Alice invites an unknown email — only lands in pending_friend_invites.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatal("ghost invite failed")
	}

	w := e.req(t, "GET", "/api/friends", nil, alice)
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	// Expect 2 rows: the ghost (pending) and bob (accepted), in that order.
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(rows), rows)
	}
	if rows[0].Status != "pending" {
		t.Errorf("row 0 status = %q, want pending; got %+v", rows[0].Status, rows[0])
	}
	if rows[1].Status != "accepted" {
		t.Errorf("row 1 status = %q, want accepted; got %+v", rows[1].Status, rows[1])
	}
}

func TestListFriendsOutgoingPendingShapeIdentical(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	known := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, known, "bob@example.com")

	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("known invite failed")
	}
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("unknown invite failed")
	}
	w := e.req(t, "GET", "/api/friends", nil, inviter)
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.FriendID != 0 || r.AcceptedAt != nil {
			t.Errorf("row carries leaky field: %+v", r)
		}
		if r.Status != "pending" || r.Direction != "outgoing" {
			t.Errorf("row shape differs: %+v", r)
		}
	}
}

func TestInviteKnownUserPublishesNotifications(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: target})
	defer unsub()

	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %d %s", w.Code, w.Body.String())
	}

	var sawNotif bool
	for _, ev := range drainEvents(ch) {
		if ev.Type == "notifications.updated" {
			sawNotif = true
		}
	}
	if !sawNotif {
		t.Error("recipient did not see a notifications.updated event after invite")
	}
}

func TestAcceptPublishesNotificationsToBothSides(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %s", w.Body.String())
	}

	bobCh, bobUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: bob})
	defer bobUnsub()
	aliceCh, aliceUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: alice})
	defer aliceUnsub()

	path := "/api/friends/" + strconv.FormatInt(alice, 10) + "/accept"
	if w := e.req(t, "POST", path, nil, bob); w.Code != http.StatusOK {
		t.Fatalf("accept: %s", w.Body.String())
	}
	var bobSaw, aliceSaw bool
	for _, ev := range drainEvents(bobCh) {
		if ev.Type == "notifications.updated" {
			bobSaw = true
		}
	}
	for _, ev := range drainEvents(aliceCh) {
		if ev.Type == "notifications.updated" {
			aliceSaw = true
		}
	}
	if !bobSaw {
		t.Error("accepter did not see a notifications.updated event")
	}
	if !aliceSaw {
		t.Error("inviter did not see a notifications.updated event (live-update regression)")
	}
}

// TestUnfriendPublishesNotifications covers the surviving unfriend fan-out.
// The legacy variant of this test (TestUnfriendDropsCrossSharesAndFiresDelete)
// asserted flight_passengers/flight_shares cleanup and a flight.deleted SSE
// event; that behaviour was retired in Wave 3 along with the flights table.
// Unfriending now only refreshes both parties' notification counts.
func TestUnfriendPublishesNotifications(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")

	// Get to an accepted friendship.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %s", w.Body.String())
	}
	if w := e.req(t, "POST",
		"/api/friends/"+strconv.FormatInt(alice, 10)+"/accept", nil, bob); w.Code != http.StatusOK {
		t.Fatalf("accept: %s", w.Body.String())
	}

	bobCh, bobUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: bob})
	defer bobUnsub()

	// Alice unfriends bob.
	if w := e.req(t, "DELETE",
		"/api/friends/"+strconv.FormatInt(bob, 10), nil, alice); w.Code != http.StatusNoContent {
		t.Fatalf("unfriend: %d %s", w.Code, w.Body.String())
	}

	// The friendship row should be gone.
	if _, err := e.store.FriendshipBetween(context.Background(), alice, bob); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("friendship should be removed, got %v", err)
	}

	// Bob should have received a notifications.updated for the unfriend.
	var sawNotif bool
	for _, ev := range drainEvents(bobCh) {
		if ev.Type == "notifications.updated" {
			sawNotif = true
		}
	}
	if !sawNotif {
		t.Error("bob did not see a notifications.updated event after unfriend")
	}
}

func TestRemovePendingPublishesNotifications(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %s", w.Body.String())
	}

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: bob})
	defer unsub()

	// Alice cancels the outgoing pending request.
	path := "/api/friends/" + strconv.FormatInt(bob, 10)
	if w := e.req(t, "DELETE", path, nil, alice); w.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	var got bool
	for _, ev := range drainEvents(ch) {
		if ev.Type == "notifications.updated" {
			got = true
		}
	}
	if !got {
		t.Error("recipient did not see a notifications.updated event after cancel")
	}
}

func mintTestToken(t *testing.T, recipient, inviter int64, ttl time.Duration) string {
	t.Helper()
	return auth.MintFriendAcceptToken(sessKey, recipient, inviter, time.Now().Add(ttl))
}

func TestAcceptFriendTokenHappyPath(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	if _, err := e.store.RequestFriendship(context.Background(), alice, bob, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok := mintTestToken(t, bob, alice, time.Hour)

	bobCh, bobUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: bob})
	defer bobUnsub()
	aliceCh, aliceUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: alice})
	defer aliceUnsub()

	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, bob)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Friendship *api.FriendshipDTO `json:"friendship"`
		Already    bool               `json:"already"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Already {
		t.Error("already should be false on first accept")
	}
	if resp.Friendship == nil || resp.Friendship.Status != "accepted" {
		t.Errorf("friendship dto = %+v", resp.Friendship)
	}
	var bobSaw, aliceSaw bool
	for _, ev := range drainEvents(bobCh) {
		if ev.Type == "notifications.updated" {
			bobSaw = true
		}
	}
	for _, ev := range drainEvents(aliceCh) {
		if ev.Type == "notifications.updated" {
			aliceSaw = true
		}
	}
	if !bobSaw {
		t.Error("expected notifications.updated event after token accept (accepter)")
	}
	if !aliceSaw {
		t.Error("expected notifications.updated event after token accept (inviter)")
	}
}

func TestAcceptFriendTokenAlreadyAccepted(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	// No pending row exists — token-accept should report already=true.
	tok := mintTestToken(t, bob, alice, time.Hour)
	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, bob)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"already":true`) {
		t.Errorf("body should report already=true: %s", w.Body.String())
	}
}

func TestAcceptFriendTokenWrongRecipient(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	mallory := e.user(t, "mallory", false)
	if _, err := e.store.RequestFriendship(context.Background(), alice, bob, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok := mintTestToken(t, bob, alice, time.Hour)
	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, mallory)
	if w.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestAcceptFriendTokenExpired(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	tok := auth.MintFriendAcceptToken(sessKey, bob, alice, time.Now().Add(-time.Second))
	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, bob)
	if w.Code != http.StatusGone {
		t.Errorf("code = %d, want 410; body=%s", w.Code, w.Body.String())
	}
}

func TestAcceptFriendTokenMalformed(t *testing.T) {
	e := setup(t, nil, nil)
	bob := e.user(t, "bob", false)
	for _, body := range []map[string]any{
		{},
		{"token": ""},
		{"token": "garbage"},
		{"token": "still.garbage"},
	} {
		w := e.req(t, "POST", "/api/friends/accept-token", body, bob)
		if w.Code != http.StatusBadRequest {
			t.Errorf("token=%v -> code=%d want 400 (body=%s)", body, w.Code, w.Body.String())
		}
	}
}
