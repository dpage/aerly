package mailer

import (
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// FlightAlertInput is the data needed to render a flight-change alert email
// (spec §9). Kind selects the headline ("delayed" / "cancelled" / "diverted" /
// "reinstated" / "gate" / "gate_assigned" / "belt" / "terminal"). Detail is a
// short human phrase describing the change, e.g. "now departing 14:35 London
// (UTC+1)", "to a different airport", "now departs gate B32", "departs gate
// B32", or "bags on belt 34". The route and time fields describe which flight changed, since a traveller with several
// flights booked can't tell them apart from the ident alone (issue #144).
type FlightAlertInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	Ident     string // flight ident, e.g. "BA123"
	Kind      string // delayed|cancelled|diverted|reinstated|gate|gate_assigned|belt|terminal
	Detail    string // ready-to-render change phrase

	Origin  string        // departure airport label, e.g. "London Heathrow (LHR)"
	Dest    string        // arrival airport label
	Departs timing.Block  // best known departure; a zero At leaves it out
	Arrives *timing.Block // best known arrival; nil leaves it out
	HomeTZ  string        // the recipient's home zone, "" for none
}

// FlightAlertDetails returns the sentence identifying the flight, e.g.
// "Flight from London Heathrow (LHR) to Sydney (SYD), departing Mon 1 Mar,
// 10:00 London (UTC), arriving Tue 2 Mar, 19:00 Sydney (UTC+11)." Each time is
// quoted on its own airport's clock, followed by the recipient's own time when
// it differs. Parts that aren't known are left out, and "" is returned when
// nothing is.
func FlightAlertDetails(in FlightAlertInput) string {
	var route string
	switch {
	case in.Origin != "" && in.Dest != "":
		route = "Flight from " + in.Origin + " to " + in.Dest
	case in.Origin != "":
		route = "Flight from " + in.Origin
	case in.Dest != "":
		route = "Flight to " + in.Dest
	}
	var parts []string
	if route != "" {
		parts = append(parts, route)
	}
	if !in.Departs.At.IsZero() {
		parts = append(parts, "departing "+FullTime(in.Departs, in.HomeTZ))
	}
	if in.Arrives != nil {
		parts = append(parts, "arriving "+FullTime(*in.Arrives, in.HomeTZ))
	}
	if len(parts) == 0 {
		return ""
	}
	s := strings.Join(parts, ", ")
	if route == "" {
		// A bare time list still needs to read as a sentence.
		s = "Flight " + s
	}
	return s + "."
}

// FlightAlertSubject returns the Subject line for a flight-change alert, e.g.
// "Your flight BA123 is now delayed". Exposed so callers can log / reuse it.
func FlightAlertSubject(ident, kind string) string {
	switch kind {
	case "cancelled":
		return fmt.Sprintf("Your flight %s has been cancelled", ident)
	case "diverted":
		return fmt.Sprintf("Your flight %s has been diverted", ident)
	case "reinstated":
		// A cancellation the provider has since withdrawn.
		return fmt.Sprintf("Your flight %s is no longer cancelled", ident)
	case "gate":
		// Lead with "Gate change" so the notification preview is actionable,
		// e.g. "Gate change: BA286" + " — now departs gate B32".
		return fmt.Sprintf("Gate change: %s", ident)
	case "gate_assigned":
		// A gate published where there was none: not a change, so it must not
		// read as one, e.g. "Gate assigned: XX100" + " — departs gate A12".
		return fmt.Sprintf("Gate assigned: %s", ident)
	case "belt":
		// Arrival baggage belt assigned/changed, e.g. "Baggage belt: BA286"
		// + " — bags on belt 34".
		return fmt.Sprintf("Baggage belt: %s", ident)
	case "terminal":
		// A known departure/arrival terminal confirmed as moved, e.g.
		// "Terminal change: BA286" + " — now departs terminal 3".
		return fmt.Sprintf("Terminal change: %s", ident)
	default:
		return fmt.Sprintf("Your flight %s is now delayed", ident)
	}
}

// BuildFlightAlertEmail renders the complete RFC822 message (plain + branded
// HTML alternative) for a flight-change alert. The body leads with the
// headline ("Your flight BA123 is now delayed to …") so it reads well in a
// notification preview, per spec §9.
func BuildFlightAlertEmail(in FlightAlertInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := FlightAlertSubject(in.Ident, in.Kind)

	// Lead line mirrors the spec's example phrasing.
	lead := subject
	if in.Detail != "" {
		lead = subject + " — " + in.Detail
	}

	details := FlightAlertDetails(in)
	plainDetails, htmlDetails := "", ""
	if details != "" {
		plainDetails = details + "\r\n\r\n"
		htmlDetails = `<p style="margin:0 0 16px;font-size:15px;">` + HTMLEscape(details) + `</p>`
	}

	plain := fmt.Sprintf(
		"%s.\r\n\r\n"+
			"%s"+
			"Open Aerly to see the latest on this flight: %s/\r\n\r\n"+
			"— Aerly\r\n",
		lead, plainDetails, site)

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 16px;font-size:15px;">%s.</p>`+
			`%s`+
			`<p style="margin:0;"><a href="%s/" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		HTMLEscape(lead), htmlDetails, HTMLEscape(site), BrandColor)

	return AssembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain, HTMLShell(subject, htmlBody, in.PublicURL))
}

// AssembleRFC822 wraps a plain + html pair into a complete multipart/alternative
// RFC822 message with the standard Aerly headers (From/To/Date/Subject). The
// subject is RFC 2047 Q-encoded so non-ASCII content survives strict MTAs (and
// any CR/LF is neutralised); pure-ASCII subjects round-trip unchanged. Shared
// by the poller alert, friend-invite, account-link, and ingest-reply mailers.
func AssembleRFC822(fromAddr, toAddr, subject, plain, htmlBody string) string {
	contentType, body := MultipartBody(plain, htmlBody)
	encodedSubject := mime.QEncoding.Encode("utf-8", subject)
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", SanitizeHeaderValue(FormatFrom(fromAddr)))
	fmt.Fprintf(&sb, "To: %s\r\n", SanitizeHeaderValue(toAddr))
	fmt.Fprintf(&sb, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&sb, "Subject: %s\r\n", encodedSubject)
	sb.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&sb, "Content-Type: %s\r\n\r\n", contentType)
	sb.WriteString(body)
	return sb.String()
}
