package mailer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHTMLShellEscapesTitleAndSite(t *testing.T) {
	out := HTMLShell("Hello <world>", "<p>body</p>", "https://flights.example.com")
	if !strings.Contains(out, "Hello &lt;world&gt;") {
		t.Errorf("title not escaped: %s", out)
	}
	if !strings.Contains(out, "flights.example.com") {
		t.Errorf("host not rendered: %s", out)
	}
	if !strings.Contains(out, "<p>body</p>") {
		t.Errorf("body not embedded: %s", out)
	}
}

func TestHTMLShell_NonURLPublicURLFallsBackToString(t *testing.T) {
	// A "site" without a parseable host should still render — we fall back
	// to the full string for the footer label.
	out := HTMLShell("t", "b", "not a url")
	if !strings.Contains(out, "not a url") {
		t.Errorf("expected footer to contain the literal site string: %s", out)
	}
}

func TestHTMLEscape(t *testing.T) {
	if got := HTMLEscape(`<a "x">&'`); got != "&lt;a &#34;x&#34;&gt;&amp;&#39;" {
		t.Errorf("HTMLEscape = %q", got)
	}
}

func TestMultipartBody_HasBothPartsAndBoundary(t *testing.T) {
	ct, body := MultipartBody("hi\r\n", "<p>hi</p>")
	if !strings.HasPrefix(ct, "multipart/alternative; boundary=\"ae-") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(body, "Content-Type: text/plain") {
		t.Errorf("missing text/plain part: %s", body)
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Errorf("missing text/html part: %s", body)
	}
	if !strings.Contains(body, "Content-Transfer-Encoding: quoted-printable") {
		t.Errorf("html part should be quoted-printable: %s", body)
	}
}

func TestMultipartBody_AddsTrailingCRLFToPlain(t *testing.T) {
	// Caller passes plain text without trailing CRLF — the assembler must
	// add one so the next boundary isn't smashed against the body.
	_, body := MultipartBody("no trailing newline", "<p>x</p>")
	if !strings.Contains(body, "no trailing newline\r\n") {
		t.Errorf("expected synthetic CRLF after plain body: %q", body)
	}
}

func TestQuotedPrintable(t *testing.T) {
	out := QuotedPrintable("hello = world")
	if !strings.Contains(out, "=3D") {
		t.Errorf("'=' should be QP-encoded as =3D, got %q", out)
	}
}

func TestSend_NoPath(t *testing.T) {
	if err := Send(context.Background(), "", "", "msg"); err == nil {
		t.Error("expected error when sendmail path unset")
	}
}

func TestSend_BinaryMissing(t *testing.T) {
	if err := Send(context.Background(), "/no/such/sendmail", "x@y", "msg"); err == nil {
		t.Error("expected error when sendmail binary doesn't exist")
	}
}

func TestSend_NonZeroExitReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sendmail pipe is POSIX-shaped; skip on Windows")
	}
	// A stub that drains stdin but exits non-zero must surface as a Wait()
	// error: the message was written and stdin closed cleanly, so the only
	// failure is the child's exit status.
	dir := t.TempDir()
	stub := filepath.Join(dir, "sendmail.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat >/dev/null\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if err := Send(context.Background(), stub, "x@y", "msg"); err == nil {
		t.Error("expected error when sendmail exits non-zero")
	}
}

func TestSend_WriteToStdinFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sendmail pipe is POSIX-shaped; skip on Windows")
	}
	// A stub that exits immediately without reading stdin closes the read end
	// of the pipe; writing a payload larger than the OS pipe buffer then fails
	// with EPIPE, exercising the write-error branch (Close + Wait + return).
	dir := t.TempDir()
	stub := filepath.Join(dir, "sendmail.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	// 8 MiB comfortably exceeds the typical 64 KiB pipe buffer.
	big := strings.Repeat("x", 8<<20)
	// Either the write fails (EPIPE) or, if the child hadn't exited yet, Wait
	// succeeds; in practice on Linux the child exits before we finish writing,
	// so we expect an error. Accept an error as the success condition.
	if err := Send(context.Background(), stub, "x@y", big); err == nil {
		t.Error("expected an error when the child closes stdin mid-write")
	}
}

func TestSend_PassesMessageToBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sendmail pipe is POSIX-shaped; skip on Windows")
	}
	// Drop a stub script onto disk that drains stdin and exits 0. This
	// exercises the build / start / write-stdin / close / wait path
	// end-to-end without relying on coreutils behaviour for sendmail's
	// -t / -f flags (which `cat` mis-interprets as filenames).
	dir := t.TempDir()
	stub := filepath.Join(dir, "sendmail.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if err := Send(context.Background(), stub, "x@y", "msg"); err != nil {
		t.Errorf("Send(stub) = %v, want nil", err)
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"alice@example.com", "alice@example.com"},
		{"alice@example.com\r\nBcc: victim@evil", "alice@example.com"},
		{"alice@example.com\ninjected", "alice@example.com"},
		{"alice@example.com\rinjected", "alice@example.com"},
		{"", ""},
	}
	for _, c := range cases {
		if got := SanitizeHeaderValue(c.in); got != c.want {
			t.Errorf("SanitizeHeaderValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAssembleRFC822_NeutralisesHeaderInjection(t *testing.T) {
	msg := AssembleRFC822("from@example.com", "to@example.com\r\nBcc: victim@evil",
		"Subject line", "plain", "<p>html</p>")
	if strings.Contains(msg, "Bcc:") {
		t.Errorf("injected Bcc survived:\n%s", msg)
	}
	if !strings.Contains(msg, "To: to@example.com\r\n") {
		t.Errorf("To header not sanitised as expected:\n%s", msg)
	}
}
