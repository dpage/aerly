package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// signedToken builds a token whose signature validly covers an arbitrary
// (raw, undecoded) payload string, so we can drive the post-signature
// decode/parse branches of VerifyFriendAcceptToken with crafted contents.
func signedToken(rawPayload string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(rawPayload))
	return payload + "." + sign(tokenKey, payload)
}

// TestVerifyRejectsUndecodablePayload exercises the branch where the
// signature is valid for the payload segment but that segment is not valid
// base64url, so it can't be decoded back into the field string. The
// signature is computed over the literal payload segment, so signing a
// deliberately non-base64 string yields a matching signature.
func TestVerifyRejectsUndecodablePayload(t *testing.T) {
	payload := "!!not-base64!!"
	tok := payload + "." + sign(tokenKey, payload)
	if _, _, err := VerifyFriendAcceptToken(tokenKey, tok); !errors.Is(err, ErrMalformedAcceptToken) {
		t.Errorf("err = %v, want ErrMalformedAcceptToken", err)
	}
}

// TestVerifyRejectsWrongFieldCount exercises the "payload decodes but doesn't
// split into exactly three fields" branch.
func TestVerifyRejectsWrongFieldCount(t *testing.T) {
	for _, raw := range []string{"1.2", "1.2.3.4", "lonely"} {
		if _, _, err := VerifyFriendAcceptToken(tokenKey, signedToken(raw)); !errors.Is(err, ErrMalformedAcceptToken) {
			t.Errorf("verify(%q) err = %v, want ErrMalformedAcceptToken", raw, err)
		}
	}
}

// TestVerifyRejectsNonNumericFields exercises the strconv.ParseInt failure
// branch where the field count is right but a field isn't an integer.
func TestVerifyRejectsNonNumericFields(t *testing.T) {
	for _, raw := range []string{"x.2.3", "1.y.3", "1.2.z"} {
		if _, _, err := VerifyFriendAcceptToken(tokenKey, signedToken(raw)); !errors.Is(err, ErrMalformedAcceptToken) {
			t.Errorf("verify(%q) err = %v, want ErrMalformedAcceptToken", raw, err)
		}
	}
}

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
