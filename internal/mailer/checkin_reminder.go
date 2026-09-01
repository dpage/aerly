package mailer

import (
	"fmt"
	"strings"
	"time"
)

// CheckinReminderInput is the data needed to render a check-in reminder
// (issue #119). Ident names the flight; StartsAt/StartTZ give the scheduled
// departure in the departure airport's own zone; Route, when set, is the
// "LHR → LIS" line that tells two same-numbered legs apart; TripID targets the
// "Open Aerly" link at the trip timeline.
type CheckinReminderInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	TripID    int64
	Ident     string
	Route     string // "LHR → LIS", or "" when the route isn't known
	StartsAt  time.Time
	StartTZ   string // IANA; falls back to UTC when empty/invalid
}

// CheckinReminderSubject returns the Subject line, e.g.
// "Check-in opens soon: BA286".
func CheckinReminderSubject(ident string) string {
	return "Check-in opens soon: " + ident
}

// BuildCheckinReminderEmail renders the complete RFC822 message (plain +
// branded HTML alternative). It leads with the headline so it reads well in a
// notification preview, and names the departure so a traveller with two legs
// the same day knows which one just opened.
func BuildCheckinReminderEmail(in CheckinReminderInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := CheckinReminderSubject(in.Ident)
	when := reminderLocalTime(in.StartsAt, in.StartTZ)

	flight := "Flight " + in.Ident
	if in.Route != "" {
		flight += " (" + in.Route + ")"
	}
	lead := fmt.Sprintf(
		"Online check-in for %s opens in five minutes. It departs %s",
		flight, when)
	link := fmt.Sprintf("%s/trips/%d", site, in.TripID)

	plain := fmt.Sprintf(
		"%s.\r\n\r\nOpen Aerly to see the details: %s\r\n\r\n— Aerly\r\n",
		lead, link)

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 16px;font-size:15px;">%s.</p>`+
			`<p style="margin:0;"><a href="%s" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		HTMLEscape(lead), HTMLEscape(link), BrandColor)

	return AssembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain, HTMLShell(subject, htmlBody, in.PublicURL))
}
