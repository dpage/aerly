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
		Added:     []ReplyLeg{{Ident: "TK1980", Date: "2026-06-12"}},
		PublicURL: "https://flights.example",
	}
	body := BuildReply(in)
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
		"TK1980 on 2026-06-12",
		// html part
		"Content-Type: text/html; charset=utf-8",
		"<!doctype html>",
		"Aerly",
		"#1f5fa8",
		">added<",
		">TK1980<",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
}

func TestBuildReply_HTMLEscapesUserContent(t *testing.T) {
	body := BuildReply(ReplyInput{
		FromAddr: "x", ToAddr: "y", PublicURL: "https://flights.example",
		Failed: []ReplyFailure{{
			Ident:  "AA<script>",
			Date:   "2026-06-13",
			Reason: "no schedule <b>oops</b>",
		}},
	})
	// User-supplied content is fine to appear raw in the text/plain
	// part — escaping only matters in the HTML part. Slice from
	// "<!doctype html>" onward to scope the check.
	htmlIdx := strings.Index(body, "<!doctype html>")
	if htmlIdx < 0 {
		t.Fatalf("HTML part missing:\n%s", body)
	}
	htmlPart := body[htmlIdx:]
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

func TestBuildReply_PartialFailure(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example/",
		Added:  []ReplyLeg{{Ident: "TK1980", Date: "2026-06-12"}},
		Failed: []ReplyFailure{{Ident: "XX9999", Date: "2026-06-13", Reason: "no schedule"}},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "TK1980 on 2026-06-12") {
		t.Error("missing success line")
	}
	if !strings.Contains(body, "XX9999 on 2026-06-13 — no schedule") {
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
		Failed: []ReplyFailure{{Ident: "XX9", Date: "2026-06-13", Reason: "nope"}},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "couldn't add any of the flights") {
		t.Errorf("missing all-failed lead-in: %s", body)
	}
}

func TestBuildReply_NothingFound(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example",
	}
	body := BuildReply(in)
	if !strings.Contains(body, "couldn't find any flight") {
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
	if !strings.Contains(body, "Subject: Re: Your forwarded flight email") {
		t.Errorf("missing fallback subject: %s", body)
	}
}

func TestBuildReply_ManualNote(t *testing.T) {
	in := ReplyInput{
		FromAddr: "flights@flights.example", ToAddr: "u@x", PublicURL: "https://flights.example",
		Added: []ReplyLeg{
			{Ident: "TK1980", Date: "2026-06-12"},
			{Ident: "TK1981", Date: "2026-06-13", ManualNote: true},
		},
	}
	body := BuildReply(in)
	if !strings.Contains(body, "TK1980 on 2026-06-12\r\n") {
		t.Errorf("resolver-driven line should NOT have manual suffix: %s", body)
	}
	if !strings.Contains(body, "TK1981 on 2026-06-13 (from the email — please verify the times)") {
		t.Errorf("manual line missing suffix: %s", body)
	}
	if !strings.Contains(body, "please check the departure and arrival times") {
		t.Errorf("manual trailer missing: %s", body)
	}
}

func TestSend_BinaryDoesNotExist(t *testing.T) {
	err := Send(context.Background(), "/tmp/does-not-exist-aerly", "From: a\r\n\r\n")
	if err == nil {
		t.Error("expected error when sendmail binary doesn't exist")
	}
}
