package emailingest

import (
	"context"
	"strings"
	"testing"
)

func TestBuildReply_AllAdded(t *testing.T) {
	in := ReplyInput{
		FromAddr:  "flights@flights.example",
		ToAddr:    "devrim@example.com",
		InReplyTo: "<msg1@example.com>",
		Subject:   "Fwd: TK1980 confirmation",
		Added:     []ReplyItem{{Label: "TK1980", Detail: "2026-06-12"}},
		PublicURL: "https://flights.example",
	}
	body := BuildReply(in)
	// QP soft-line-breaks (=\r\n) get inserted into the HTML body part by
	// the quoted-printable encoder; strip them so substring assertions
	// don't accidentally straddle a wrap.
	stripped := strings.ReplaceAll(body, "=\r\n", "")
	for _, want := range []string{
		"From: flights@flights.example",
		"To: devrim@example.com",
		"In-Reply-To: <msg1@example.com>",
		"References: <msg1@example.com>",
		"Subject: Re: Fwd: TK1980 confirmation",
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=",
		// plain part
		"Content-Type: text/plain; charset=utf-8",
		"TK1980 — 2026-06-12",
		// html part
		"Content-Type: text/html; charset=utf-8",
		"<!doctype html>",
		"Aerly",
		"#1f5fa8",
		">added<",
		">TK1980<",
	} {
		if !strings.Contains(stripped, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
}

func TestBuildReply_HTMLEscapesUserContent(t *testing.T) {
	body := BuildReply(ReplyInput{
		FromAddr: "x", ToAddr: "y", PublicURL: "https://flights.example",
		Failed: []ReplyFailure{{
			Label:  "AA<script>",
			Detail:   "2026-06-13",
			Reason: "no schedule <b>oops</b>",
		}},
	})
	// User-supplied content is fine to appear raw in the text/plain
	// part — escaping only matters in the HTML part. Slice from
	// "<!doctype html>" onward to scope the check, and strip QP soft
	// line breaks so the escaped-substring assertions don't straddle
	// a wrap.
	htmlIdx := strings.Index(body, "<!doctype html>")
	if htmlIdx < 0 {
		t.Fatalf("HTML part missing:\n%s", body)
	}
	htmlPart := strings.ReplaceAll(body[htmlIdx:], "=\r\n", "")
	for _, evil := range []string{"<script>", "<b>oops</b>"} {
		if strings.Contains(htmlPart, evil) {
			t.Errorf("unescaped %q present in HTML part:\n%s", evil, htmlPart)
		}
	}
	for _, want := range []string{"AA&lt;script&gt;", "&lt;b&gt;oops&lt;/b&gt;"} {
		if !strings.Contains(htmlPart, want) {
			t.Errorf("expected escaped %q in HTML part:\n%s", want, htmlPart)
		}
	}
}

// TestBuildReply_SubjectHeaderInjection verifies that a forwarded Subject
// carrying CRLF (mime word-decoding can preserve it) cannot inject extra
// headers into the reply — the header block must end at the first blank line
// and no attacker header (Bcc:) may appear among the real headers.
func TestBuildReply_SubjectHeaderInjection(t *testing.T) {
	body := BuildReply(ReplyInput{
		FromAddr:  "flights@flights.example",
		ToAddr:    "victim@example.com",
		Subject:   "hi\r\nBcc: attacker@evil.example",
		PublicURL: "https://flights.example",
	})
	// The header section is everything before the first blank line.
	headerEnd := strings.Index(body, "\r\n\r\n")
	if headerEnd < 0 {
		t.Fatalf("no header/body separator in:\n%s", body)
	}
	headers := body[:headerEnd]
	// No header line may *start* with Bcc: (the injected value is allowed to
	// survive inside the encoded Subject word, just not as its own header).
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Errorf("injected Bcc header survived as its own line:\n%s", headers)
		}
	}
	// The Subject must be a single header line: find it and confirm its value
	// contains no raw CRLF (it should be Q-encoded into one =?utf-8?...?= word).
	subjIdx := strings.Index(headers, "Subject:")
	if subjIdx < 0 {
		t.Fatalf("no Subject header:\n%s", headers)
	}
	subjLine := headers[subjIdx:]
	if i := strings.Index(subjLine, "\r\n"); i >= 0 {
		subjLine = subjLine[:i] // first line only
	}
	if !strings.Contains(subjLine, "=?") {
		t.Errorf("Subject with control chars was not encoded: %q", subjLine)
	}
}

func TestBuildReply_PartialFailure(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example/",
		Added:  []ReplyItem{{Label: "TK1980", Detail: "2026-06-12"}},
		Failed: []ReplyFailure{{Label: "XX9999", Detail: "2026-06-13", Reason: "no schedule"}},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "TK1980 — 2026-06-12") {
		t.Error("missing success line")
	}
	if !strings.Contains(body, "XX9999 — 2026-06-13 — no schedule") {
		t.Error("missing failure line")
	}
	// Trailing slash on PublicURL must not be doubled.
	if strings.Contains(body, "flights.example//") {
		t.Error("PublicURL trailing slash doubled")
	}
}

func TestBuildReply_AllFailed(t *testing.T) {
	in := ReplyInput{
		FromAddr: "x", ToAddr: "y", PublicURL: "https://flights.example",
		Failed: []ReplyFailure{{Label: "XX9", Detail: "2026-06-13", Reason: "nope"}},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "couldn't add any of the bookings") {
		t.Errorf("missing all-failed lead-in: %s", body)
	}
}

func TestBuildReply_NothingFound(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example",
	}
	body := BuildReply(in)
	if !strings.Contains(body, "couldn't find any travel") {
		t.Errorf("missing fallback copy, got: %s", body)
	}
}

func TestBuildReply_SubjectAlreadyHasRe(t *testing.T) {
	in := ReplyInput{FromAddr: "x@y", ToAddr: "u@x", Subject: "Re: hello"}
	body := BuildReply(in)
	if strings.Contains(body, "Re: Re: hello") {
		t.Error("subject double-prefixed")
	}
	if !strings.Contains(body, "Subject: Re: hello") {
		t.Error("expected single Re: prefix")
	}
}

func TestBuildReply_EmptySubject(t *testing.T) {
	in := ReplyInput{FromAddr: "x@y", ToAddr: "u@x"}
	body := BuildReply(in)
	if !strings.Contains(body, "Subject: Re: Your forwarded travel email") {
		t.Errorf("missing fallback subject: %s", body)
	}
}

func TestBuildReply_ManualNote(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example",
		Added: []ReplyItem{
			{Label: "TK1980", Detail: "2026-06-12"},
			{Label: "TK1981", Detail: "2026-06-13", ManualNote: true},
		},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "TK1980 — 2026-06-12\r\n") {
		t.Errorf("resolver-driven line should NOT have manual suffix: %s", body)
	}
	if !strings.Contains(body, "TK1981 — 2026-06-13 (from the email — please verify the times)") {
		t.Errorf("manual line missing suffix: %s", body)
	}
	if !strings.Contains(body, "please check the departure and arrival times") {
		t.Errorf("manual trailer missing: %s", body)
	}
}

func TestSend_BinaryDoesNotExist(t *testing.T) {
	err := Send(context.Background(), "/tmp/does-not-exist-aerly", "x@example.com", "From: a\r\n\r\n")
	if err == nil {
		t.Error("expected error when sendmail binary doesn't exist")
	}
}

// TestBuildReply_NoLineExceeds998 guards against the
// "DKIM body hash broken by Postfix's >998-byte soft-wrap" bug.
// RFC 5321 §4.5.3.1.6 caps SMTP lines at 998 octets; any compliant MTA
// rewrites longer lines with CRLF<SP>, which happens *after* opendkim
// signs and invalidates the body hash at the receiver. The worst-case
// payload is a reply with several flight rows — legBlockHTML concatenates
// row HTML without newlines, so the entire body becomes one logical line.
func TestBuildReply_NoLineExceeds998(t *testing.T) {
	in := ReplyInput{
		FromAddr:  "flights@aerly.me",
		ToAddr:    "user@example.com",
		InReplyTo: "<msg@example.com>",
		Subject:   "Fwd: lots of flights",
		PublicURL: "https://aerly.me",
		Added: []ReplyItem{
			{Label: "TK1980", Detail: "2026-06-12"},
			{Label: "BA286", Detail: "2026-06-13"},
			{Label: "AF1234", Detail: "2026-06-14", ManualNote: true},
			{Label: "LH456", Detail: "2026-06-15"},
			{Label: "UA900", Detail: "2026-06-16", ManualNote: true},
			{Label: "EK203", Detail: "2026-06-17"},
		},
		Failed: []ReplyFailure{
			{Label: "XX9999", Detail: "2026-06-18", Reason: "no schedule found in resolver"},
			{Label: "YY1111", Detail: "2026-06-19", Reason: "ident not recognised by AeroDataBox"},
		},
	}
	mustHaveShortLines(t, BuildReply(in))
}

func TestBuildVerifyEmail_NoLineExceeds998(t *testing.T) {
	msg := BuildVerifyEmail(VerifyInput{
		FromAddr:  "flights@aerly.me",
		ToAddr:    "user@example.com",
		PublicURL: "https://aerly.me",
		Token:     "demo-token-1234567890abcdef",
	})
	mustHaveShortLines(t, msg)
}

func mustHaveShortLines(t *testing.T, msg string) {
	t.Helper()
	for i, line := range strings.Split(msg, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line %d is %d bytes (>998), will be rewritten by any RFC-compliant MTA:\n%s",
				i, len(line), line)
		}
	}
}

func TestBuildReply_NonFlightBookingReported(t *testing.T) {
	in := ReplyInput{
		FromAddr: "a@x", ToAddr: "b@y", PublicURL: "https://x",
		Added: []ReplyItem{{Label: "Marriott Tysons", Detail: "Hotel · 12 Jun 2026"}},
	}
	body := strings.ReplaceAll(BuildReply(in), "=\r\n", "")
	for _, want := range []string{
		"added the following booking(s)",
		"Marriott Tysons",
		"Hotel · 12 Jun 2026",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reply missing %q\n%s", want, body)
		}
	}
	// A successful hotel ingest must NOT claim it found no flight information.
	if strings.Contains(body, "flight information") {
		t.Errorf("reply wrongly mentions flight information:\n%s", body)
	}
}
