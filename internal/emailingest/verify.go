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

// BuildVerifyEmail renders an RFC822 verification email as
// multipart/alternative — a plain-text body for legacy clients and a
// branded HTML body for everything else. Both carry the same
// verification link; the body is short on purpose so the message
// doesn't trip aggressive spam filters.
func BuildVerifyEmail(in VerifyInput) string {
	link := strings.TrimRight(in.PublicURL, "/") + "/auth/verify-email?token=" + in.Token

	plain := verifyPlainBody(link)
	htmlPart := htmlShell("Verify your email", verifyHTMLBody(link), in.PublicURL)
	contentType, body := multipartBody(plain, htmlPart)

	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", in.FromAddr)
	fmt.Fprintf(&sb, "To: %s\r\n", in.ToAddr)
	sb.WriteString("Subject: Verify your email for flight-tracker\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&sb, "Content-Type: %s\r\n\r\n", contentType)
	sb.WriteString(body)
	return sb.String()
}

func verifyPlainBody(link string) string {
	return "Click the link below to verify this email address for flight-tracker:\r\n\r\n" +
		"  " + link + "\r\n\r\n" +
		"This link expires in 24 hours. If you didn't ask for it, you can ignore this message.\r\n\r\n" +
		"— flight-tracker\r\n"
}

func verifyHTMLBody(link string) string {
	safe := htmlEscape(link)
	return `<p style="margin:0 0 16px;font-size:15px;">Click the button below to verify this email address for Flight Tracker.</p>
<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:8px 0 20px;">
<tr><td style="border-radius:8px;background:` + brandColor + `;">
<a href="` + safe + `" style="display:inline-block;padding:12px 22px;color:#ffffff;text-decoration:none;font-weight:600;font-size:15px;">Verify email</a>
</td></tr>
</table>
<p style="margin:0 0 8px;font-size:13px;color:#555;">Or paste this URL into your browser:</p>
<p style="margin:0 0 20px;font-size:13px;word-break:break-all;"><a href="` + safe + `" style="` + brandLinkStyle + `">` + safe + `</a></p>
<p style="margin:0;font-size:13px;color:#666;">This link expires in 24 hours. If you didn't ask for it, you can ignore this message.</p>`
}
