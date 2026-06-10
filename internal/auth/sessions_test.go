package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var key = []byte("test-session-key-at-least-32-chars!!")

func TestSignVerifyRoundTrip(t *testing.T) {
	tok := SignSession(key, 42, 3, time.Now().Add(time.Hour))
	uid, ver, err := VerifySession(key, tok)
	if err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
	if uid != 42 || ver != 3 {
		t.Errorf("uid=%d ver=%d, want 42/3", uid, ver)
	}
}

func TestVerifyAcceptsLegacyV1AsVersionZero(t *testing.T) {
	// A pre-upgrade v1 cookie (no version field) must still verify, treated as
	// session version 0, so sessions survive the rollout.
	body := "v1.42.9999999999"
	raw := body + "." + sign(key, body)
	uid, ver, err := VerifySession(key, raw)
	if err != nil || uid != 42 || ver != 0 {
		t.Errorf("v1 token: uid=%d ver=%d err=%v, want 42/0/nil", uid, ver, err)
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	tok := SignSession(key, 42, 0, time.Now().Add(time.Hour))
	if _, _, err := VerifySession(key, tok+"x"); err == nil {
		t.Error("tampered signature should fail")
	}
	if _, _, err := VerifySession([]byte("different-key-also-32-chars-long!!!"), tok); err == nil {
		t.Error("wrong key should fail")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"v1.1",                         // too few parts
		"v3.1.0.2.sig",                 // unknown version tag
		"v2.1.0.9999999999",            // v2 missing the signature part
		"v2.notanint.0.9999999999.sig", // bad uid (sig also wrong)
	}
	for _, c := range cases {
		if _, _, err := VerifySession(key, c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	tok := SignSession(key, 7, 0, time.Now().Add(-time.Minute))
	if _, _, err := VerifySession(key, tok); err == nil {
		t.Error("expired token should fail")
	}
}

func TestVerifyRejectsBadExpiryField(t *testing.T) {
	// Valid signature over a body whose expiry isn't an integer.
	body := "v2.5.0.notanumber"
	raw := body + "." + sign(key, body)
	if _, _, err := VerifySession(key, raw); err == nil {
		t.Error("non-integer expiry should fail")
	}
}

func TestVerifyRejectsBadVersionField(t *testing.T) {
	body := "v2.5.notanint.9999999999"
	raw := body + "." + sign(key, body)
	if _, _, err := VerifySession(key, raw); err == nil {
		t.Error("non-integer version should fail")
	}
}

func TestVerifyRejectsBadUIDField(t *testing.T) {
	body := "v2.notanint.0.9999999999"
	raw := body + "." + sign(key, body)
	if _, _, err := VerifySession(key, raw); err == nil {
		t.Error("non-integer uid should fail")
	}
}

func TestSetSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, key, 99, 2, true)
	res := w.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != SessionCookie || !c.HttpOnly || !c.Secure {
		t.Errorf("unexpected cookie attrs: %+v", c)
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v", c.SameSite)
	}
	uid, ver, err := VerifySession(key, c.Value)
	if err != nil || uid != 99 || ver != 2 {
		t.Errorf("cookie value not a valid session: uid=%d ver=%d err=%v", uid, ver, err)
	}
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w, false)
	c := w.Result().Cookies()[0]
	if c.MaxAge != -1 || c.Value != "" {
		t.Errorf("clear cookie should expire immediately: %+v", c)
	}
	if c.Secure {
		t.Error("secure should be false here")
	}
}

func TestSignDeterministic(t *testing.T) {
	a := sign(key, "body")
	b := sign(key, "body")
	if a != b {
		t.Error("sign should be deterministic")
	}
	if strings.ContainsAny(a, "+/=") {
		t.Errorf("expected raw-url base64 (no +/=), got %q", a)
	}
}
