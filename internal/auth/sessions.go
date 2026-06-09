// Package auth handles GitHub OAuth sign-in and HMAC-signed session cookies.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	SessionCookie = "flight_session"
	StateCookie   = "flight_oauth_state"
	SessionTTL    = 30 * 24 * time.Hour
	StateTTL      = 10 * time.Minute
)

var ErrInvalidSession = errors.New("invalid session")

// SignSession returns a cookie value of the form v1.<uid>.<expUnix>.<sig>.
func SignSession(key []byte, userID int64, expires time.Time) string {
	body := fmt.Sprintf("v1.%d.%d", userID, expires.Unix())
	return body + "." + sign(key, body)
}

// VerifySession parses a cookie value and returns the user ID if the signature
// is valid and the expiry is in the future.
func VerifySession(key []byte, raw string) (int64, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return 0, ErrInvalidSession
	}
	body := strings.Join(parts[:3], ".")
	if !hmac.Equal([]byte(parts[3]), []byte(sign(key, body))) {
		return 0, ErrInvalidSession
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, ErrInvalidSession
	}
	if time.Now().Unix() > expUnix {
		return 0, ErrInvalidSession
	}
	uid, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, ErrInvalidSession
	}
	return uid, nil
}

// SignState returns a signed OAuth-state cookie value of the form
// <nonce>.<expUnix>.<sig>. Unlike a bare double-submit token, the signature
// covers BOTH the random anti-CSRF nonce and the expiry, so the cookie is
// tamper-proof and the nonce can't be substituted. nonce must be dot-free
// (randomToken uses base64url, which is).
func SignState(key []byte, nonce string, expires time.Time) string {
	body := fmt.Sprintf("%s.%d", nonce, expires.Unix())
	return body + "." + sign(key, body)
}

// VerifyState validates an OAuth-state cookie value against the nonce echoed
// back from the provider. It checks the signature and the nonce match in
// constant time and that the state has not expired. Returns nil when valid.
func VerifyState(key []byte, raw, gotNonce string) error {
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) != 3 {
		return ErrInvalidSession
	}
	nonce, expStr, sig := parts[0], parts[1], parts[2]
	if !hmac.Equal([]byte(sig), []byte(sign(key, nonce+"."+expStr))) {
		return ErrInvalidSession
	}
	if !hmac.Equal([]byte(nonce), []byte(gotNonce)) {
		return ErrInvalidSession
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return ErrInvalidSession
	}
	if time.Now().Unix() > expUnix {
		return ErrInvalidSession
	}
	return nil
}

// SetSessionCookie writes a session cookie that expires in SessionTTL.
func SetSessionCookie(w http.ResponseWriter, key []byte, userID int64, secure bool) {
	expires := time.Now().Add(SessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    SignSession(key, userID, expires),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie invalidates the cookie on the client.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func sign(key []byte, body string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
