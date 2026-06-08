package handlers

import (
	"fmt"
	"strings"

	"github.com/dpage/aerly/internal/mailer"
)

// shareEmailInput is the data needed to render the "X shared Y with you" email
// sent to a newly-added sharee — either an existing Aerly user (looked up by
// verified email) or a pre-shared address.
type shareEmailInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	ActorName string
	ItemName  string
	Path      string // e.g. "/trips/42"
}

func buildShareEmail(in shareEmailInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	link := site + in.Path
	subject := fmt.Sprintf("%s shared %q with you on Aerly", in.ActorName, in.ItemName)
	plain := fmt.Sprintf("%s shared %q with you on Aerly.\r\n\r\nOpen it: %s\r\n\r\n— Aerly\r\n",
		in.ActorName, in.ItemName, link)
	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 16px;font-size:15px;"><strong>%s</strong> shared <strong>%s</strong> with you on Aerly.</p>`+
			`<p style="margin:0;"><a href="%s" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open it</a></p>`,
		mailer.HTMLEscape(in.ActorName), mailer.HTMLEscape(in.ItemName),
		mailer.HTMLEscape(link), mailer.BrandColor)
	return mailer.AssembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain, mailer.HTMLShell(subject, htmlBody, in.PublicURL))
}
