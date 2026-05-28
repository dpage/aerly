package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var tokenKey = []byte("accept-token-test-key-thirty-two-bytes!")

func TestMintVerifyRoundTrip(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	tok := MintFriendAcceptToken(tokenKey, 42, 99, exp)
	if tok == "" {
		t.Fatal("MintFriendAcceptToken returned empty string")
	}
	r, i, err := VerifyFriendAcceptToken(tokenKey, tok)
	if err != nil {
		t.Fatalf("VerifyFriendAcceptToken: %v", err)
	}
	if r != 42 || i != 99 {
		t.Errorf("recipient/inviter = %d/%d, want 42/99", r, i)
	}
}

func TestVerifyRejectsExpiredAcceptToken(t *testing.T) {
	tok := MintFriendAcceptToken(tokenKey, 1, 2, time.Now().Add(-time.Second))
	_, _, err := VerifyFriendAcceptToken(tokenKey, tok)
	if !errors.Is(err, ErrExpiredAcceptToken) {
		t.Errorf("err = %v, want ErrExpiredAcceptToken", err)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	tok := MintFriendAcceptToken(tokenKey, 1, 2, time.Now().Add(time.Hour))
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("token shape = %q", tok)
	}
	// Flip the recipient id in the payload by re-base64'ing a new value
	// with the original signature; verification must reject.
	bad := encodePayload(99, 2, time.Now().Add(time.Hour).Unix()) + "." + parts[1]
	_, _, err := VerifyFriendAcceptToken(tokenKey, bad)
	if !errors.Is(err, ErrMalformedAcceptToken) {
		t.Errorf("err = %v, want ErrMalformedAcceptToken", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	tok := MintFriendAcceptToken(tokenKey, 1, 2, time.Now().Add(time.Hour))
	_, _, err := VerifyFriendAcceptToken([]byte("different-key-thirty-two-bytes!!"), tok)
	if !errors.Is(err, ErrMalformedAcceptToken) {
		t.Errorf("err = %v, want ErrMalformedAcceptToken", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"not-base64",
		"only-one-segment",
		"a.b.c", // too many segments
	}
	for _, c := range cases {
		if _, _, err := VerifyFriendAcceptToken(tokenKey, c); !errors.Is(err, ErrMalformedAcceptToken) {
			t.Errorf("verify(%q) err = %v, want ErrMalformedAcceptToken", c, err)
		}
	}
}
