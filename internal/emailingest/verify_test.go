package emailingest

import (
	"strings"
	"testing"
)

func TestBuildVerifyEmail_HeadersAndBody(t *testing.T) {
	got := BuildVerifyEmail(VerifyInput{
		FromAddr:  "flights@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://flights.example.com",
		Token:     "abc123",
	})

	mustContain(t, got, "From: flights@example.com\r\n")
	mustContain(t, got, "To: alice@example.com\r\n")
	mustContain(t, got, "Subject: Verify your email for flight-tracker\r\n")
	mustContain(t, got, "Content-Type: text/plain; charset=utf-8\r\n")
	mustContain(t, got, "https://flights.example.com/auth/verify-email?token=abc123")
	mustContain(t, got, "24 hours")
}

func TestBuildVerifyEmail_TrimsPublicURLTrailingSlash(t *testing.T) {
	got := BuildVerifyEmail(VerifyInput{
		FromAddr:  "f@example.com",
		ToAddr:    "t@example.com",
		PublicURL: "https://flights.example.com/",
		Token:     "tok",
	})
	if strings.Contains(got, "//auth/verify-email") {
		t.Errorf("trailing slash not trimmed: %q", got)
	}
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("missing %q in:\n%s", want, got)
	}
}
