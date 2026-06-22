package handlers

import (
	"strings"
	"testing"
)

// TestBuildShareEmailG1 covers buildShareEmail: the rendered RFC822 message
// should carry the actor, item name and the site+path deep link.
func TestBuildShareEmailG1(t *testing.T) {
	msg := buildShareEmail(shareEmailInput{
		FromAddr:  "noreply@aerly.test",
		ToAddr:    "sharee@example.com",
		PublicURL: "https://aerly.test/", // trailing slash should be trimmed
		ActorName: "Test User",
		ItemName:  "Lisbon 2026",
		Path:      "/trips/42",
	})
	decoded := decodeQP(msg)

	if !strings.Contains(decoded, "https://aerly.test/trips/42") {
		t.Errorf("missing deep link in body:\n%s", decoded)
	}
	if !strings.Contains(decoded, "Test User") {
		t.Error("missing actor name")
	}
	if !strings.Contains(decoded, "Lisbon 2026") {
		t.Error("missing item name")
	}
	// The subject line embeds both actor and item.
	if !strings.Contains(msg, "shared") {
		t.Error("missing subject")
	}
}

// TestBuildFriendInviteEmailG1 covers buildFriendInviteEmail, including the
// optional personal-message branch and the username fallback when the display
// name is blank.
func TestBuildFriendInviteEmailG1(t *testing.T) {
	// With a name and a message.
	msg := buildFriendInviteEmail(friendInviteInput{
		FromAddr:     "noreply@aerly.test",
		ToAddr:       "newbie@example.com",
		PublicURL:    "https://aerly.test/",
		InviterName:  "Test User",
		InviterLogin: "testuser",
		Message:      "Come and join me!",
	})
	decoded := decodeQP(msg)
	if !strings.Contains(decoded, "Test User") {
		t.Error("missing inviter name")
	}
	if !strings.Contains(decoded, "Come and join me!") {
		t.Error("missing personal message")
	}
	if !strings.Contains(decoded, "https://aerly.test/") {
		t.Error("missing sign-in link")
	}

	// Without a name (falls back to login) and without a message.
	msg2 := buildFriendInviteEmail(friendInviteInput{
		FromAddr:     "noreply@aerly.test",
		ToAddr:       "newbie2@example.com",
		PublicURL:    "https://aerly.test",
		InviterName:  "   ",
		InviterLogin: "fallbacklogin",
	})
	decoded2 := decodeQP(msg2)
	if !strings.Contains(decoded2, "fallbacklogin") {
		t.Error("expected username fallback when name blank")
	}
	if strings.Contains(decoded2, "Message from") {
		t.Error("no message label expected when message empty")
	}
}

// TestBuildFriendRequestEmailWithMessageG1 covers the personal-message branches
// of buildFriendRequestEmail (the existing test exercises the no-message path).
func TestBuildFriendRequestEmailWithMessageG1(t *testing.T) {
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr:     "noreply@aerly.test",
		ToAddr:       "bob@example.com",
		PublicURL:    "https://aerly.test",
		InviterName:  "Test User",
		InviterLogin: "testuser",
		Message:      "Hello from the synthetic test!",
		Token:        "tok123",
	})
	decoded := decodeQP(msg)
	if !strings.Contains(decoded, "Hello from the synthetic test!") {
		t.Error("missing personal message in friend request email")
	}
	if !strings.Contains(decoded, "Message from Test User") {
		t.Error("missing message attribution")
	}
}
