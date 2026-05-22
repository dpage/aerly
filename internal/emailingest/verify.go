package emailingest

import (
	"fmt"
	"strings"
)

// VerifyInput is everything BuildVerifyEmail needs to render an RFC822
// verification message.
type VerifyInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string // server base URL; the verification link is appended
	Token     string // raw token returned by store.InsertUnverifiedEmail / ResendVerification
}

// BuildVerifyEmail renders a plaintext RFC822 verification email. The
// verification link is the only actionable content; the body is short on
// purpose so it doesn't trip aggressive spam filters.
func BuildVerifyEmail(in VerifyInput) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", in.FromAddr)
	fmt.Fprintf(&sb, "To: %s\r\n", in.ToAddr)
	sb.WriteString("Subject: Verify your email for flight-tracker\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")

	base := strings.TrimRight(in.PublicURL, "/")
	link := fmt.Sprintf("%s/auth/verify-email?token=%s", base, in.Token)
	sb.WriteString("Click the link below to verify this email address for flight-tracker:\r\n\r\n")
	fmt.Fprintf(&sb, "  %s\r\n\r\n", link)
	sb.WriteString("This link expires in 24 hours. If you didn't ask for it, you can ignore this message.\r\n")
	sb.WriteString("\r\n— flight-tracker\r\n")
	return sb.String()
}
