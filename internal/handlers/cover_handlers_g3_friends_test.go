package handlers

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/config"
)

// TestG3ListFriendsStoreErrs drives both store-error branches in listFriends.
func TestG3ListFriendsStoreErrs(t *testing.T) {
	// ListFriendships fails.
	e := setup(t, nil, nil)
	uid := e.user(t, "g3lffs", false)
	g1dropTable(t, e, "friendships")
	if w := e.req(t, "GET", "/api/friends", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("list friendships store err = %d, want 500", w.Code)
	}

	// ListOutgoingPendingInvites fails (friendships intact).
	e2 := setup(t, nil, nil)
	uid2 := e2.user(t, "g3lfpi", false)
	g1dropTable(t, e2, "pending_friend_invites")
	if w := e2.req(t, "GET", "/api/friends", nil, uid2); w.Code != http.StatusInternalServerError {
		t.Errorf("list pending invites store err = %d, want 500", w.Code)
	}
}

// TestG3InviteFriendBadInput covers the bad-body (77) and empty-email (82) 400s
// in inviteFriend.
func TestG3InviteFriendBadInput(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3ifbad", false)
	if w := e.req(t, "POST", "/api/friends/invite", "not-json", uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400", w.Code)
	}
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "   "}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("empty email = %d, want 400", w.Code)
	}
}

// TestG3InviteFriendLookupStoreErr drives the generic-error branch of the
// UserByVerifiedEmail lookup (103): dropping user_emails makes the email lookup
// error rather than return ErrNotFound.
func TestG3InviteFriendLookupStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3iflookup", false)
	g1dropTable(t, e, "user_emails")
	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "someone@example.com"}, uid)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("invite lookup store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG3InviteFriendByUserIDRequestErr drives inviteFriendByUserID's
// RequestFriendship-error branch (124): the target is found via a verified
// email, but the friendships table is gone so the request insert fails. The
// error is logged and swallowed, so the response is still 202.
func TestG3InviteFriendByUserIDRequestErr(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "g3ifurinv", false)
	target := e.user(t, "g3ifurtgt", false)
	seedVerifiedEmail(t, e, target, "g3target@example.com")
	g1dropTable(t, e, "friendships")
	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "g3target@example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Errorf("invite (request err swallowed) = %d, want 202", w.Code)
	}
}

// TestG3InviteFriendByEmailDuplicateSuppressed drives inviteFriendByEmail's
// !created branch (146): a second invite to the same unknown address is a no-op.
func TestG3InviteFriendByEmailDuplicateSuppressed(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "g3ifedup", false)
	body := map[string]any{"email": "g3ghost@example.com"}
	if w := e.req(t, "POST", "/api/friends/invite", body, inviter); w.Code != http.StatusAccepted {
		t.Fatalf("first invite = %d", w.Code)
	}
	if w := e.req(t, "POST", "/api/friends/invite", body, inviter); w.Code != http.StatusAccepted {
		t.Errorf("duplicate invite = %d, want 202", w.Code)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_friend_invites WHERE inviter_id = $1`, inviter).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("pending invites = %d, want 1 (duplicate suppressed)", n)
	}
}

// TestG3InviteFriendByEmailUpsertErr drives inviteFriendByEmail's upsert-error
// branch (142): dropping pending_friend_invites makes the upsert fail; the error
// is logged and swallowed, so the response stays 202.
func TestG3InviteFriendByEmailUpsertErr(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "g3ifeerr", false)
	g1dropTable(t, e, "pending_friend_invites")
	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "g3unknown@example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Errorf("invite (upsert err swallowed) = %d, want 202", w.Code)
	}
}

// TestG3SendFriendRequestNotificationSends drives the full notification-email
// path in sendFriendRequestNotification (158-192): MailFromAddress is set (so
// the early skip is bypassed), the matched target has a verified email, an
// accept token is minted, and a real sendmail (/bin/true) accepts the message.
func TestG3SendFriendRequestNotificationSends(t *testing.T) {
	e := setup(t, nil, &config.Config{
		MailFromAddress: "noreply@example.com",
		SendmailPath:    "/bin/true",
		PublicURL:       "https://aerly.example.com",
	})
	inviter := e.user(t, "g3srninv", false)
	target := e.user(t, "g3srntgt", false)
	seedVerifiedEmail(t, e, target, "g3srn@example.com")

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "g3srn@example.com", "message": "hi"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Errorf("invite = %d, want 202", w.Code)
	}
}

// TestG3SendFriendInviteEmailSends drives the by-email invite-email path in
// sendFriendInviteEmail with a configured sender + sendmail, for an address with
// no account yet.
func TestG3SendFriendInviteEmailSends(t *testing.T) {
	e := setup(t, nil, &config.Config{
		MailFromAddress: "noreply@example.com",
		SendmailPath:    "/bin/true",
		PublicURL:       "https://aerly.example.com",
	})
	inviter := e.user(t, "g3sieinv", false)
	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "g3newcomer@example.com", "message": "join us"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Errorf("invite = %d, want 202", w.Code)
	}
}

// TestG3AcceptFriendBadID covers the invalid-userId 400 in acceptFriend.
func TestG3AcceptFriendBadID(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3afbad", false)
	if w := e.req(t, "POST", "/api/friends/notanumber/accept", nil, uid); w.Code != http.StatusBadRequest {
		t.Errorf("accept bad id = %d, want 400", w.Code)
	}
}

// TestG3RemoveFriendBadIDAndStoreErr covers removeFriend's invalid-id 400 (238)
// and the RemoveFriendship store-error 500 (244).
func TestG3RemoveFriendBadIDAndStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3rfbad", false)
	if w := e.req(t, "DELETE", "/api/friends/notanumber", nil, uid); w.Code != http.StatusBadRequest {
		t.Errorf("remove bad id = %d, want 400", w.Code)
	}

	e2 := setup(t, nil, nil)
	me := e2.user(t, "g3rfme", false)
	other := e2.user(t, "g3rfother", false)
	g1dropTable(t, e2, "friendships")
	if w := e2.req(t, "DELETE", "/api/friends/"+strconv.FormatInt(other, 10), nil, me); w.Code != http.StatusInternalServerError {
		t.Errorf("remove store err = %d, want 500", w.Code)
	}
}

// TestG3CancelOutgoingInviteBadBody covers the decode-failure 400 (265) in
// cancelOutgoingInvite.
func TestG3CancelOutgoingInviteBadBody(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3coibad", false)
	if w := e.req(t, "DELETE", "/api/friends/outgoing", "not-json", uid); w.Code != http.StatusBadRequest {
		t.Errorf("cancel bad body = %d, want 400", w.Code)
	}
}

// TestG3AcceptFriendTokenBadBody covers acceptFriendToken's decode-failure 400
// (299).
func TestG3AcceptFriendTokenBadBody(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "g3aftbad", false)
	if w := e.req(t, "POST", "/api/friends/accept-token", "not-json", uid); w.Code != http.StatusBadRequest {
		t.Errorf("accept-token bad body = %d, want 400", w.Code)
	}
}

// TestG3AcceptFriendTokenStoreErr drives the generic AcceptFriendship error
// branch (330): with a valid token for the right recipient, but the friendships
// table dropped, AcceptFriendship errors generically (not ErrNotFound) → 500.
func TestG3AcceptFriendTokenStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "g3aftserr-a", false)
	bob := e.user(t, "g3aftserr-b", false)
	tok := mintTestToken(t, bob, alice, time.Hour)
	g1dropTable(t, e, "friendships")
	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, bob)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("accept-token store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
