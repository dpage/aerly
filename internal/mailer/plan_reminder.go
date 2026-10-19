package mailer

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dpage/aerly/internal/timing"
)

// PlanReminderInput is the data needed to render an upcoming-plan reminder
// (issue #11). Label is the human name for the plan (flight ident, plan title,
// or a type fallback); Start is the part's start as a label needs it, and
// HomeTZ the recipient's home zone ("" for none); TripID targets the "Open
// Aerly" link at the trip timeline.
type PlanReminderInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	TripID    int64
	Label     string
	Start     timing.Block
	HomeTZ    string
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
		return "your check-in"
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
	case "vehicle_hire":
		return "your car hire"
	default:
		return "your plan"
	}
}

// PlanReminderSubject returns the Subject line, e.g. "Upcoming: flight BA123".
func PlanReminderSubject(label string) string {
	return "Upcoming: " + label
}

// FullTime is how an email quotes a time: the web's full label, then the
// recipient's own time when their home zone reads differently, e.g. "Sun 18
// Oct, 14:00 Tashkent (UTC+5) (10:00 your time)". Exported so the push and
// inbox copy that reuses an email's wording quotes the time the same way.
func FullTime(b timing.Block, home string) string {
	return WithYourTime(timing.Full(b, home), b, home)
}

// WithYourTime appends the recipient's own time to s, separated by a space,
// when there is one to add, and returns s unchanged otherwise.
func WithYourTime(s string, b timing.Block, home string) string {
	if y := timing.YourTime(b, home); y != "" {
		return s + " " + y
	}
	return s
}

// BuildPlanReminderEmail renders the complete RFC822 reminder message (plain +
// branded HTML alternative), leading with the headline so it reads well in a
// notification preview.
func BuildPlanReminderEmail(in PlanReminderInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := PlanReminderSubject(in.Label)
	when := FullTime(in.Start, in.HomeTZ)
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
