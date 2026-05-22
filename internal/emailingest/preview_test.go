//go:build preview

// Run with: go test -tags=preview ./internal/emailingest -run TestPreviewEmails
// Writes sample HTML renderings to /tmp/email-*.html for visual inspection.

package emailingest

import (
	"os"
	"strings"
	"testing"
)

func extractHTML(msg string) string {
	i := strings.Index(msg, "<!doctype html>")
	if i < 0 {
		return msg
	}
	rest := msg[i:]
	j := strings.Index(rest, "</html>")
	if j < 0 {
		return rest
	}
	return rest[:j+len("</html>")]
}

func writePreview(t *testing.T, name, html string) {
	t.Helper()
	path := "/tmp/" + name
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d bytes)", path, len(html))
}

func TestPreviewEmails(t *testing.T) {
	verify := BuildVerifyEmail(VerifyInput{
		FromAddr: "flights@example.com", ToAddr: "alice@example.com",
		PublicURL: "https://flights.example.com", Token: "demo-token-1234",
	})
	writePreview(t, "email-verify.html", extractHTML(verify))

	writePreview(t, "email-reply-added.html", extractHTML(BuildReply(ReplyInput{
		FromAddr: "flights@example.com", ToAddr: "alice@example.com",
		PublicURL: "https://flights.example.com",
		Added: []ReplyLeg{
			{Ident: "TK1980", Date: "2026-06-12"},
			{Ident: "BA286", Date: "2026-06-15"},
		},
	})))

	writePreview(t, "email-reply-mixed.html", extractHTML(BuildReply(ReplyInput{
		FromAddr: "flights@example.com", ToAddr: "alice@example.com",
		PublicURL: "https://flights.example.com",
		Added:    []ReplyLeg{{Ident: "TK1980", Date: "2026-06-12"}},
		Failed:   []ReplyFailure{{Ident: "XX9999", Date: "2026-06-13", Reason: "no schedule found"}},
	})))

	writePreview(t, "email-reply-failed.html", extractHTML(BuildReply(ReplyInput{
		FromAddr: "flights@example.com", ToAddr: "alice@example.com",
		PublicURL: "https://flights.example.com",
		Failed: []ReplyFailure{
			{Ident: "XX9999", Date: "2026-06-13", Reason: "no schedule found"},
		},
	})))

	writePreview(t, "email-reply-none.html", extractHTML(BuildReply(ReplyInput{
		FromAddr: "flights@example.com", ToAddr: "alice@example.com",
		PublicURL: "https://flights.example.com",
	})))
}
