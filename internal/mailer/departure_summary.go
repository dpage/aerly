package mailer

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// delayThreshold is how large the departure delay must be before the summary
// calls the flight delayed.
const delayThreshold = 5 * time.Minute

// rainLikelyPct is the chance of rain from which the push weather line says
// "rain likely".
const rainLikelyPct = 50

// ForecastDay is one day of destination weather. Date is a civil date (only
// year, month and day are used); the mailer deliberately has its own type so
// it does not depend on the weather provider package.
type ForecastDay struct {
	Date      time.Time
	Text      string // "Partly cloudy"
	Emoji     string
	MaxC      float64
	MinC      float64
	PrecipPct *int // nil when the provider gave no figure
}

// DepartureSummaryInput is the data needed to render the departure summary
// (issue #130). ScheduledOut is the timetable departure and Departs the
// effective one (actual, else estimated, else scheduled); DepartureDelay
// drives the on time / delayed wording. HomeTZ is the recipient's home zone,
// "" for none. Terminal, Gate, Arrives, ContactPhone and Forecast are all
// optional.
type DepartureSummaryInput struct {
	FromAddr     string
	ToAddr       string
	PublicURL    string
	TripID       int64
	Ident        string
	Route        string // "LHR → ORD", or "" when unknown
	DestLabel    string // destination city, else IATA; "" for no subject suffix
	DestIATA     string
	ScheduledOut time.Time
	Departs      timing.Block
	// DepartureDelay is the off-block estimate minus the timetable, zero
	// without an estimate; it alone decides the on time / delayed wording.
	DepartureDelay time.Duration
	Arrives        *timing.Block
	HomeTZ         string
	Terminal       string
	Gate           string
	SupplierName   string // airline name; "the airline" when empty
	ContactPhone   string
	Forecast       []ForecastDay
	ForecastPlace  string // heads the forecast block, e.g. "Chicago"
}

// DepartureSummarySubject returns the Subject line, e.g.
// "Departure summary: BA171 to Chicago".
func DepartureSummarySubject(ident, destLabel string) string {
	s := "Departure summary: " + ident
	if destLabel != "" {
		s += " to " + destLabel
	}
	return s
}

// formatDuration renders a positive duration compactly: "45 min", "1 h",
// "1 h 20 min". Sub-minute remainders are dropped.
func formatDuration(d time.Duration) string {
	mins := int(d / time.Minute)
	h, m := mins/60, mins%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

// DepartureSummaryHeadline returns the first sentence of the summary, reused
// as the in-app inbox row and the start of the push body, e.g.
// "BA171 (LHR → ORD) is on time, departing terminal 5, gate B32 at Thu 15
// Oct, 11:10 London (UTC+1)."
func DepartureSummaryHeadline(in DepartureSummaryInput) string {
	flight := in.Ident
	if in.Route != "" {
		flight += " (" + in.Route + ")"
	}

	status, departing := "is on time,", "departing"
	if in.DepartureDelay >= delayThreshold {
		status = "is delayed by " + formatDuration(in.DepartureDelay) + ","
		departing = "now departing"
	}

	var where []string
	if in.Terminal != "" {
		where = append(where, "terminal "+in.Terminal)
	}
	if in.Gate != "" {
		where = append(where, "gate "+in.Gate)
	}
	at := " at "
	if len(where) > 0 {
		at = " " + strings.Join(where, ", ") + " at "
	}
	return fmt.Sprintf("%s %s %s%s%s.", flight, status, departing, at,
		FullTime(in.Departs, in.HomeTZ))
}

// arrivalLine is the "Arrival at ORD is ..." sentence, or "" when the arrival
// time is unknown.
func arrivalLine(in DepartureSummaryInput) string {
	if in.Arrives == nil {
		return ""
	}
	dest := in.DestIATA
	if dest == "" {
		dest = "the destination"
	}
	return fmt.Sprintf("Arrival at %s is %s.", dest, FullTime(*in.Arrives, in.HomeTZ))
}

// contactLine is the "For help or changes" sentence, or "" without a phone.
func contactLine(in DepartureSummaryInput) string {
	if in.ContactPhone == "" {
		return ""
	}
	who := in.SupplierName
	if who == "" {
		who = "the airline"
	}
	return fmt.Sprintf("For help or changes, contact %s: %s", who, in.ContactPhone)
}

// celsius renders a temperature as a whole number of degrees, e.g. "24°C".
// It rounds half away from zero (fmt's %.0f rounds half to even, so 0.5 would
// read 0 but 1.5 would read 2), and a reading that rounds to zero from below
// prints as "0°C" rather than "-0°C".
func celsius(v float64) string {
	r := math.Round(v)
	if r == 0 {
		r = 0 // drops the sign of a negative zero
	}
	return fmt.Sprintf("%.0f°C", r)
}

// tempRange renders a day's high and low as "24°C / 18°C".
func tempRange(d ForecastDay) string {
	return celsius(d.MaxC) + " / " + celsius(d.MinC)
}

// forecastPlace names the place the forecast is for, falling back to the
// destination label.
func forecastPlace(in DepartureSummaryInput) string {
	if in.ForecastPlace != "" {
		return in.ForecastPlace
	}
	return in.DestLabel
}

// DepartureSummaryWeatherLine returns the short weather sentence the push body
// appends, e.g. "Chicago: 24°C / 18°C, rain likely.", from the first forecast
// day; "" when there is no forecast.
func DepartureSummaryWeatherLine(in DepartureSummaryInput) string {
	if len(in.Forecast) == 0 {
		return ""
	}
	d := in.Forecast[0]
	line := tempRange(d)
	if place := forecastPlace(in); place != "" {
		line = place + ": " + line
	}
	switch {
	case d.PrecipPct != nil && *d.PrecipPct >= rainLikelyPct:
		line += ", rain likely"
	case d.Text != "":
		line += ", " + strings.ToLower(d.Text)
	}
	return line + "."
}

// forecastHeading heads the forecast block.
func forecastHeading(in DepartureSummaryInput) string {
	if place := forecastPlace(in); place != "" {
		return "Weather in " + place
	}
	return "Weather at your destination"
}

func precipText(d ForecastDay) string {
	if d.PrecipPct == nil {
		return "-"
	}
	return fmt.Sprintf("%d%% rain", *d.PrecipPct)
}

const (
	openMeteoURL       = "https://open-meteo.com/"
	weatherAttribution = "Weather data by Open-Meteo.com"
)

// BuildDepartureSummaryEmail renders the complete RFC822 message (plain +
// branded HTML alternative). The headline leads so it reads well in a
// notification preview; the weather block is left out when there is no
// forecast.
func BuildDepartureSummaryEmail(in DepartureSummaryInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := DepartureSummarySubject(in.Ident, in.DestLabel)
	link := fmt.Sprintf("%s/trips/%d", site, in.TripID)

	var sentences []string
	sentences = append(sentences, DepartureSummaryHeadline(in))
	if a := arrivalLine(in); a != "" {
		sentences = append(sentences, a)
	}
	contact := contactLine(in)

	var plain strings.Builder
	plain.WriteString(strings.Join(sentences, "\r\n\r\n") + "\r\n\r\n")
	if contact != "" {
		plain.WriteString(contact + "\r\n\r\n")
	}
	plain.WriteString("Open Aerly to see the details: " + link + "\r\n\r\n")

	var html strings.Builder
	for _, s := range sentences {
		fmt.Fprintf(&html, `<p style="margin:0 0 16px;font-size:15px;">%s</p>`, HTMLEscape(s))
	}
	if contact != "" {
		fmt.Fprintf(&html, `<p style="margin:0 0 16px;">%s</p>`, HTMLEscape(contact))
	}
	fmt.Fprintf(&html,
		`<p style="margin:0 0 20px;"><a href="%s" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		HTMLEscape(link), BrandColor)

	if len(in.Forecast) > 0 {
		heading := forecastHeading(in)
		plain.WriteString(heading + "\r\n")
		fmt.Fprintf(&html, `<p style="margin:0 0 8px;font-weight:600;">%s</p>`+
			`<table role="presentation" cellspacing="0" cellpadding="0" style="border-collapse:collapse;font-size:14px;">`,
			HTMLEscape(heading))
		for _, d := range in.Forecast {
			date := d.Date.Format("2 Jan")
			temps := tempRange(d)
			fmt.Fprintf(&plain, "%s  %s %s  %s  %s\r\n",
				date, d.Emoji, d.Text, precipText(d), temps)
			cell := `<td style="padding:4px 12px 4px 0;border-bottom:1px solid #e5e7eb;">%s</td>`
			html.WriteString("<tr>")
			for _, c := range []string{date, d.Emoji, d.Text, precipText(d), temps} {
				fmt.Fprintf(&html, cell, HTMLEscape(c))
			}
			html.WriteString("</tr>")
		}
		html.WriteString("</table>")
		// Open-Meteo's data is CC-BY 4.0: credit it wherever it is shown.
		plain.WriteString("\r\n" + weatherAttribution + "\r\n\r\n")
		fmt.Fprintf(&html, `<p style="margin:8px 0 0;font-size:12px;color:#6b7280;">Weather data by <a href="%s" style="color:#6b7280;">Open-Meteo.com</a></p>`,
			openMeteoURL)
	}
	plain.WriteString("— Aerly\r\n")

	return AssembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain.String(), HTMLShell(subject, html.String(), in.PublicURL))
}
