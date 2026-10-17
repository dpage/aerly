package handlers

import (
	"io"
	"mime/quotedprintable"
	"strings"
	"testing"
)

// decodeQP decodes any quoted-printable soft-wrap artefacts so that
// tests can search for human-readable strings even though the HTML MIME
// part is transferred as quoted-printable.
func decodeQP(s string) string {
	r := quotedprintable.NewReader(strings.NewReader(s))
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestBuildFriendRequestEmailEmbedsAcceptToken(t *testing.T) {
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr:     "noreply@aerly.test",
		ToAddr:       "bob@example.com",
		PublicURL:    "https://aerly.test",
		InviterName:  "Alice",
		InviterLogin: "alice",
		Message:      "",
		Token:        "the-token-bytes",
	})
	decoded := decodeQP(msg)

	// Plain-text body should include a clearly-labelled Accept URL.
	if !strings.Contains(decoded, "https://aerly.test/?friend_accept=the-token-bytes") {
		t.Errorf("missing Accept URL in body:\n%s", msg)
	}
	// HTML body should include an Accept button anchor.
	if !strings.Contains(decoded, `href="https://aerly.test/?friend_accept=the-token-bytes"`) {
		t.Error("missing Accept button anchor")
	}
	if !strings.Contains(strings.ToLower(decoded), ">accept<") {
		t.Error("missing visible Accept label on the button")
	}
}

func TestBuildFriendRequestEmailKeepsReviewLinkAlongsideAccept(t *testing.T) {
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr: "n@a", ToAddr: "b@b", PublicURL: "https://aerly.test",
		InviterLogin: "alice", Token: "tok",
	})
	decoded := decodeQP(msg)
	if !strings.Contains(decoded, "/friends") {
		t.Error("Review URL (/friends) should still be present")
	}
	if !strings.Contains(decoded, "/?friend_accept=tok") {
		t.Error("Accept URL should be present alongside Review")
	}
}

// The invite only lands for an account that verifies the invited address, so
// the email must name that address rather than promise "just sign in".
func TestBuildFriendInviteEmailNamesInvitedAddress(t *testing.T) {
	in := friendInviteInput{
		FromAddr:     "noreply@aerly.test",
		ToAddr:       "bob@example.com",
		PublicURL:    "https://aerly.test",
		InviterName:  "Alice",
		InviterLogin: "alice",
	}
	decoded := decodeQP(buildFriendInviteEmail(in))
	if !strings.Contains(decoded, "with an account that uses bob@example.com") {
		t.Errorf("plain body does not name the invited address:\n%s", decoded)
	}
	if !strings.Contains(decoded, "uses <strong>bob@example.com</strong>") {
		t.Errorf("HTML body does not name the invited address:\n%s", decoded)
	}
	if strings.Contains(decoded, "Preferences → Emails") {
		t.Error("add-address hint shown although addresses can't be added")
	}

	in.CanAddEmails = true
	decoded = decodeQP(buildFriendInviteEmail(in))
	if n := strings.Count(decoded, "add bob@example.com under Preferences → Emails"); n != 2 {
		t.Errorf("add-address hint appears %d times, want once per MIME part:\n%s", n, decoded)
	}
}
