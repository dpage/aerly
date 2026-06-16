package mailer

import (
	"fmt"
	"strings"
	"time"
)

// QuotaAlertInput is the data needed to render an operator quota/rate-limit
// alert (the "let me know when we've been rate limited or hit our API quotas"
// heads-up sent to admins). Provider is the upstream's label ("OpenSky",
// "AeroDataBox"); Detail is a short human phrase — the upstream's own message
// when available, otherwise a remediation hint; OccurredAt is when the limit
// was hit (zero is rendered without a timestamp).
type QuotaAlertInput struct {
	FromAddr   string
	ToAddr     string
	PublicURL  string
	Provider   string
	Detail     string
	OccurredAt time.Time
}

// QuotaAlertSubject returns the Subject line for an operator quota alert, e.g.
// "Aerly hit a rate limit on OpenSky". Exposed so callers can log / reuse it.
func QuotaAlertSubject(provider string) string {
	return fmt.Sprintf("Aerly hit a rate limit on %s", provider)
}

// BuildQuotaAlertEmail renders the complete RFC822 message (plain + branded
// HTML alternative) telling the admins that an upstream API rejected a request
// for rate-limit / quota reasons, so they can raise the plan tier or widen the
// poll interval. The lead line names the provider so it reads well in a
// notification preview.
func BuildQuotaAlertEmail(in QuotaAlertInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := QuotaAlertSubject(in.Provider)
	detail := strings.TrimSpace(in.Detail)

	var when string
	if !in.OccurredAt.IsZero() {
		when = in.OccurredAt.UTC().Format("2006-01-02 15:04 MST")
	}

	var pb strings.Builder
	fmt.Fprintf(&pb, "Aerly's %s integration returned a rate-limit / quota response", in.Provider)
	if when != "" {
		fmt.Fprintf(&pb, " at %s", when)
	}
	pb.WriteString(".\r\n\r\n")
	if detail != "" {
		fmt.Fprintf(&pb, "Detail: %s\r\n\r\n", detail)
	}
	pb.WriteString("Flight tracking and lookups may be degraded until the limit clears. " +
		"Consider raising the provider's plan tier or widening POLL_INTERVAL.\r\n\r\n")
	fmt.Fprintf(&pb, "Open Aerly: %s/\r\n\r\n— Aerly\r\n", site)

	var hb strings.Builder
	fmt.Fprintf(&hb,
		`<p style="margin:0 0 16px;font-size:15px;">Aerly's <strong>%s</strong> integration returned a rate-limit / quota response%s.</p>`,
		HTMLEscape(in.Provider), htmlWhenSuffix(when))
	if detail != "" {
		fmt.Fprintf(&hb,
			`<p style="margin:0 0 16px;font-size:14px;color:#555;">Detail: %s</p>`,
			HTMLEscape(detail))
	}
	hb.WriteString(`<p style="margin:0 0 20px;font-size:15px;">Flight tracking and lookups may be degraded until the limit clears. Consider raising the provider's plan tier or widening the poll interval.</p>`)
	fmt.Fprintf(&hb,
		`<p style="margin:0;"><a href="%s/" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		HTMLEscape(site), BrandColor)

	return AssembleRFC822(in.FromAddr, in.ToAddr, subject,
		pb.String(), HTMLShell(subject, hb.String(), in.PublicURL))
}

// htmlWhenSuffix renders the " at <time>" clause for the HTML lead line,
// escaped, or "" when no timestamp is available.
func htmlWhenSuffix(when string) string {
	if when == "" {
		return ""
	}
	return " at " + HTMLEscape(when)
}
