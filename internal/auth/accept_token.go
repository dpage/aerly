package auth

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrMalformedAcceptToken signals the token was missing, mis-encoded,
// truncated, or had a mismatched signature.
var ErrMalformedAcceptToken = errors.New("malformed friend-accept token")

// ErrExpiredAcceptToken signals the token verified but its expiry is in
// the past. Handler converts this to HTTP 410 so the caller can surface
// a "ask the sender to resend" message.
var ErrExpiredAcceptToken = errors.New("expired friend-accept token")

// encodePayload builds the ASCII payload that goes between the two
// base64url segments. Unexported but accessible to the test file (same
// package) so tests can construct tampered tokens that share the
// original signature.
func encodePayload(recipientID, inviterID, expiryUnix int64) string {
	raw := fmt.Sprintf("%d.%d.%d", recipientID, inviterID, expiryUnix)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// MintFriendAcceptToken returns an HMAC-signed token authorising a
// specific recipient → inviter friend-accept. Format is
// base64url(payload).base64url(hmac-sha256(payload)), where payload is
// "<recipientID>.<inviterID>.<expiryUnix>" — see sessions.go's sign()
// for the underlying primitive.
func MintFriendAcceptToken(key []byte, recipientID, inviterID int64, expiry time.Time) string {
	payload := encodePayload(recipientID, inviterID, expiry.Unix())
	return payload + "." + sign(key, payload)
}

// VerifyFriendAcceptToken decodes and authenticates a token previously
// returned by MintFriendAcceptToken. On success it returns the
// (recipientID, inviterID) pair encoded in the token. Returns
// ErrExpiredAcceptToken if the signature is valid but the expiry is in
// the past, ErrMalformedAcceptToken for every other failure (bad
// signature, wrong key, garbage, structural problems).
func VerifyFriendAcceptToken(key []byte, token string) (int64, int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, ErrMalformedAcceptToken
	}
	payload, sig := parts[0], parts[1]

	expected, err := base64.RawURLEncoding.DecodeString(sign(key, payload))
	if err != nil {
		return 0, 0, ErrMalformedAcceptToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, expected) {
		return 0, 0, ErrMalformedAcceptToken
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return 0, 0, ErrMalformedAcceptToken
	}
	fields := strings.Split(string(raw), ".")
	if len(fields) != 3 {
		return 0, 0, ErrMalformedAcceptToken
	}
	recipientID, err1 := strconv.ParseInt(fields[0], 10, 64)
	inviterID, err2 := strconv.ParseInt(fields[1], 10, 64)
	expiryUnix, err3 := strconv.ParseInt(fields[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, ErrMalformedAcceptToken
	}
	if time.Now().Unix() > expiryUnix {
		return 0, 0, ErrExpiredAcceptToken
	}
	return recipientID, inviterID, nil
}
