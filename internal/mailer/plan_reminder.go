package mailer

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// PlanReminderInput is the data needed to render an upcoming-plan reminder
// (issue #11). Label is the human name for the plan (flight ident, plan title,
// or a type fallback); StartsAt/StartTZ give the local start time; TripID
// targets the "Open Aerly" link at the trip timeline.
type PlanReminderInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	TripID    int64
	Label     string
	StartsAt  time.Time
	StartTZ   string // IANA; falls back to UTC when empty/invalid
}

// PlanReminderLabel derives a concise label from a plan's type/title/ident.
// Flights lead with their ident; otherwise the title wins, then a type word.
func PlanReminderLabel(planType, title, ident string) string {
	if planType == "flight" && ident != "" {
		return "flight " + ident
	}
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	switch planType {
	case "flight":
		return "your flight"
	case "train":
		return "your train"
	case "hotel":
		return "your hotel check-in"
	case "ground":
		return "your transfer"
	case "dining":
		return "your reservation"
	case "excursion":
		return "your excursion"
	case "meeting":
		return "your meeting"
	case "event":
		return "your event"
	default:
		return "your plan"
	}
}

// PlanReminderSubject returns the Subject line, e.g. "Upcoming: flight BA123".
func PlanReminderSubject(label string) string {
	return "Upcoming: " + label
}

// reminderLocalTime renders StartsAt in the part's IANA zone, falling back to
// UTC when the zone is empty or unknown.
func reminderLocalTime(t time.Time, tz string) string {
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return t.In(loc).Format("Mon 2 Jan, 15:04 MST")
}

// BuildPlanReminderEmail renders the complete RFC822 reminder message (plain +
// branded HTML alternative), leading with the headline so it reads well in a
// notification preview.
func BuildPlanReminderEmail(in PlanReminderInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := PlanReminderSubject(in.Label)
	when := reminderLocalTime(in.StartsAt, in.StartTZ)
	lead := fmt.Sprintf("%s starts %s", capitalise(in.Label), when)
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

// capitalise upper-cases the first rune of s. Rune-aware so a user-entered
// plan title with a multi-byte leading character (e.g. "école") isn't corrupted.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + string(r[1:])
}
