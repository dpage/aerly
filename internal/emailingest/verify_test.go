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
	mustContain(t, got, "MIME-Version: 1.0\r\n")
	mustContain(t, got, "Content-Type: multipart/alternative; boundary=")
	// Both parts present.
	mustContain(t, got, "Content-Type: text/plain; charset=utf-8")
	mustContain(t, got, "Content-Type: text/html; charset=utf-8")
	// Link rendered in both parts.
	mustContain(t, got, "https://flights.example.com/auth/verify-email?token=abc123")
	mustContain(t, got, "24 hours")
	// HTML part: brand mark, header, button.
	mustContain(t, got, "<!doctype html>")
	mustContain(t, got, "Flight Tracker") // wordmark in shell
	mustContain(t, got, "#1f5fa8")        // brand colour
	mustContain(t, got, ">Verify email<") // button label
}

func TestBuildVerifyEmail_MultipartBoundaryClosesProperly(t *testing.T) {
	got := BuildVerifyEmail(VerifyInput{
		FromAddr: "f@x", ToAddr: "t@y", PublicURL: "https://flights.example", Token: "tok",
	})
	// Extract boundary from the top-level Content-Type header.
	const ctMarker = `Content-Type: multipart/alternative; boundary="`
	i := strings.Index(got, ctMarker)
	if i == -1 {
		t.Fatalf("multipart Content-Type missing:\n%s", got)
	}
	rest := got[i+len(ctMarker):]
	j := strings.Index(rest, `"`)
	if j == -1 {
		t.Fatalf("boundary not quoted:\n%s", got)
	}
	boundary := rest[:j]
	if boundary == "" {
		t.Fatal("empty boundary")
	}
	// Each part starts with --<boundary>; closing is --<boundary>--.
	open := "--" + boundary + "\r\n"
	close := "--" + boundary + "--\r\n"
	if n := strings.Count(got, open); n != 2 {
		t.Errorf("expected 2 opening boundary markers, got %d", n)
	}
	if !strings.Contains(got, close) {
		t.Errorf("missing closing boundary %q", close)
	}
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
