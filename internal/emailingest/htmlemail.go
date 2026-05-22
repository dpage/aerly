package emailingest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"
	"strings"
)

// brandColor is the primary brand colour shared with the SPA's MUI theme
// (`palette.primary.main` in web/src/theme.ts). Keep these in sync.
const brandColor = "#1f5fa8"

// htmlShell wraps body content in the branded HTML shell — a header bar
// with the brand mark and wordmark, the body in a white card, and a
// muted footer linking back to publicURL. All styling is inline so
// rendering survives Gmail / Outlook / Apple Mail's CSS stripping.
//
// The body argument must be HTML — callers escape user-supplied content
// themselves using htmlEscape() before composing.
func htmlShell(title, body, publicURL string) string {
	site := strings.TrimRight(publicURL, "/")
	host := site
	if u, err := url.Parse(site); err == nil && u.Host != "" {
		host = u.Host
	}
	safeTitle := html.EscapeString(title)
	safeSite := html.EscapeString(site)
	safeHost := html.EscapeString(host)
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + safeTitle + `</title>
</head>
<body style="margin:0;padding:0;background:#f5f6fa;font-family:system-ui,-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1a1a1a;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="background:#f5f6fa;padding:24px 12px;">
<tr><td align="center">
<table role="presentation" cellpadding="0" cellspacing="0" border="0" width="100%" style="max-width:560px;background:#ffffff;border-radius:8px;overflow:hidden;border:1px solid #e5e7eb;">
<tr><td style="background:` + brandColor + `;padding:18px 24px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0">
<tr>
<td valign="middle" style="padding-right:12px;">
<div style="width:36px;height:36px;border-radius:8px;background:rgba(255,255,255,0.18);color:#ffffff;font-size:20px;line-height:36px;text-align:center;font-family:Arial,sans-serif;">&#9992;&#xFE0E;</div>
</td>
<td valign="middle" style="color:#ffffff;font-size:18px;font-weight:600;letter-spacing:0.2px;">Flight Tracker</td>
</tr>
</table>
</td></tr>
<tr><td style="padding:24px;font-size:15px;line-height:1.55;color:#1a1a1a;">
` + body + `
</td></tr>
<tr><td style="padding:14px 24px;background:#fafafa;border-top:1px solid #eaeaea;color:#666;font-size:12px;line-height:1.4;">
Sent by Flight Tracker · <a href="` + safeSite + `" style="color:` + brandColor + `;text-decoration:none;">` + safeHost + `</a>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`
}

// htmlEscape escapes user-supplied content for safe inclusion in HTML.
func htmlEscape(s string) string { return html.EscapeString(s) }

// brandLinkStyle is the inline style for in-body links.
const brandLinkStyle = "color:" + brandColor + ";text-decoration:none;"

// multipartBody renders the multipart/alternative body (boundary + two
// parts) and returns the Content-Type header value and the body. The
// text/plain part comes first; text/html last, so MIME-aware clients
// prefer the HTML alternative (RFC 2046 §5.1.4).
func multipartBody(plain, htmlBody string) (contentType, body string) {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	boundary := "ft-" + hex.EncodeToString(b)

	var sb strings.Builder
	sb.WriteString("This is a multipart message in MIME format.\r\n\r\n")
	fmt.Fprintf(&sb, "--%s\r\n", boundary)
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	sb.WriteString(plain)
	if !strings.HasSuffix(plain, "\r\n") {
		sb.WriteString("\r\n")
	}
	fmt.Fprintf(&sb, "\r\n--%s\r\n", boundary)
	sb.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	sb.WriteString(htmlBody)
	if !strings.HasSuffix(htmlBody, "\r\n") {
		sb.WriteString("\r\n")
	}
	fmt.Fprintf(&sb, "\r\n--%s--\r\n", boundary)

	return fmt.Sprintf("multipart/alternative; boundary=\"%s\"", boundary), sb.String()
}
