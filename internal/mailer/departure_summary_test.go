package mailer

import (
	"io"
	"mime/quotedprintable"
	"strings"
	"testing"
	"time"
)

func intp(v int) *int { return &v }

// decodedHTML returns the quoted-printable HTML part of msg, decoded.
func decodedHTML(t *testing.T, msg string) string {
	t.Helper()
	_, rest, ok := strings.Cut(msg, "Content-Type: text/html")
	if !ok {
		t.Fatalf("no HTML part\n%s", msg)
	}
	_, body, _ := strings.Cut(rest, "\r\n\r\n")
	body, _, _ = strings.Cut(body, "\r\n--ae-")
	b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("decode HTML: %v", err)
	}
	return string(b)
}

func depInput() DepartureSummaryInput {
	sched := time.Date(2026, 10, 15, 10, 10, 0, 0, time.UTC) // 11:10 BST
	arr := time.Date(2026, 10, 15, 18, 50, 0, 0, time.UTC)   // 13:50 CDT
	return DepartureSummaryInput{
		FromAddr:      "aerly@example.com",
		ToAddr:        "alice@example.com",
		PublicURL:     "https://aerly.test/",
		TripID:        7,
		Ident:         "BA171",
		Route:         "LHR → ORD",
		DestLabel:     "Chicago",
		DestIATA:      "ORD",
		ScheduledOut:  sched,
		DepartsAt:     sched,
		OriginTZ:      "Europe/London",
		ArrivesAt:     &arr,
		DestTZ:        "America/Chicago",
		Terminal:      "5",
		Gate:          "B32",
		SupplierName:  "Example Airways",
		ContactPhone:  "+44 20 7946 0000",
		ForecastPlace: "Chicago",
		Forecast: []ForecastDay{
			{Date: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), Text: "Partly cloudy", Emoji: "⛅", MaxC: 24.4, MinC: 17.6, PrecipPct: intp(20)},
			{Date: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), Text: "Rain", Emoji: "🌧", MaxC: 19, MinC: 12, PrecipPct: nil},
		},
	}
}

func TestDepartureSummarySubject(t *testing.T) {
	if got := DepartureSummarySubject("BA171", "Chicago"); got != "Departure summary: BA171 to Chicago" {
		t.Errorf("subject = %q", got)
	}
	if got := DepartureSummarySubject("BA171", ""); got != "Departure summary: BA171" {
		t.Errorf("subject = %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		45 * time.Minute:            "45 min",
		time.Hour:                   "1 h",
		80 * time.Minute:            "1 h 20 min",
		2*time.Hour + 5*time.Minute: "2 h 5 min",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDepartureSummaryHeadline(t *testing.T) {
	in := depInput()
	if got, want := DepartureSummaryHeadline(in),
		"BA171 (LHR → ORD) is on time, departing terminal 5, gate B32 at Thu 15 Oct, 11:10 BST."; got != want {
		t.Errorf("on time:\n got %q\nwant %q", got, want)
	}

	// Under the threshold still counts as on time.
	in.DepartsAt = in.ScheduledOut.Add(4 * time.Minute)
	in.DepartureDelay = 4 * time.Minute
	if !strings.Contains(DepartureSummaryHeadline(in), "is on time") {
		t.Errorf("4 min late should be on time")
	}

	// Wheels-off a taxi-time after the timetable, with no off-block estimate,
	// is not a delay: the delay field decides, not DepartsAt.
	in.DepartsAt = in.ScheduledOut.Add(12 * time.Minute)
	in.DepartureDelay = 0
	if got := DepartureSummaryHeadline(in); !strings.Contains(got, "is on time, departing") {
		t.Errorf("wheels-off without an estimate should be on time, got %q", got)
	}

	in.DepartsAt = in.ScheduledOut.Add(80 * time.Minute)
	in.DepartureDelay = 80 * time.Minute
	if got, want := DepartureSummaryHeadline(in),
		"BA171 (LHR → ORD) is delayed by 1 h 20 min, now departing terminal 5, gate B32 at Thu 15 Oct, 12:30 BST."; got != want {
		t.Errorf("delayed:\n got %q\nwant %q", got, want)
	}
}

func TestDepartureSummaryHeadline_Missing(t *testing.T) {
	in := depInput()
	in.Route, in.Terminal, in.Gate = "", "", ""
	if got, want := DepartureSummaryHeadline(in),
		"BA171 is on time, departing at Thu 15 Oct, 11:10 BST."; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	in.Gate = "B32"
	if got := DepartureSummaryHeadline(in); !strings.Contains(got, "departing gate B32 at") || strings.Contains(got, "terminal") {
		t.Errorf("gate only: %q", got)
	}
	in.Gate, in.Terminal = "", "5"
	if got := DepartureSummaryHeadline(in); !strings.Contains(got, "departing terminal 5 at") || strings.Contains(got, "gate") {
		t.Errorf("terminal only: %q", got)
	}
}

func TestDepartureSummaryWeatherLine(t *testing.T) {
	in := depInput()
	if got, want := DepartureSummaryWeatherLine(in), "Chicago: 24°C / 18°C, partly cloudy."; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	in.Forecast[0].PrecipPct = intp(70)
	if got, want := DepartureSummaryWeatherLine(in), "Chicago: 24°C / 18°C, rain likely."; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	in.Forecast[0].PrecipPct, in.Forecast[0].Text = nil, ""
	in.ForecastPlace = ""
	if got, want := DepartureSummaryWeatherLine(in), "Chicago: 24°C / 18°C."; got != want {
		t.Errorf("label fallback: got %q want %q", got, want)
	}
	in.DestLabel = ""
	if got, want := DepartureSummaryWeatherLine(in), "24°C / 18°C."; got != want {
		t.Errorf("no place: got %q want %q", got, want)
	}
	in.Forecast = nil
	if got := DepartureSummaryWeatherLine(in); got != "" {
		t.Errorf("no forecast should give empty line, got %q", got)
	}
}

func TestBuildDepartureSummaryEmail(t *testing.T) {
	msg := BuildDepartureSummaryEmail(depInput())
	for _, want := range []string{
		"Subject: Departure summary: BA171 to Chicago",
		"To: alice@example.com",
		"BA171 (LHR",
		"is on time, departing terminal 5, gate B32 at Thu 15 Oct, 11:10 BST.",
		"Arrival at ORD is Thu 15 Oct, 13:50 CDT.",
		"For help or changes, contact Example Airways: +44 20 7946 0000",
		"https://aerly.test/trips/7",
		"Weather in Chicago",
		"17 Oct  ⛅ Partly cloudy  20% rain  24°C / 18°C",
		"18 Oct  🌧 Rain  -  19°C / 12°C",
		"border-collapse",
		"— Aerly",
		"Weather data by Open-Meteo.com\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("email missing %q\n---\n%s", want, msg)
		}
	}
	html := decodedHTML(t, msg)
	for _, want := range []string{
		// The nil PrecipPct day renders as "-" in the HTML table too.
		`border-bottom:1px solid #e5e7eb;">-</td>`,
		`Weather data by <a href="https://open-meteo.com/" style="color:#6b7280;">Open-Meteo.com</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q\n---\n%s", want, html)
		}
	}
}

func TestBuildDepartureSummaryEmail_NoForecastNoAttribution(t *testing.T) {
	in := depInput()
	in.Forecast = nil
	msg := BuildDepartureSummaryEmail(in)
	if strings.Contains(msg, "Open-Meteo") {
		t.Errorf("attribution present without a forecast\n---\n%s", msg)
	}
}

func TestBuildDepartureSummaryEmail_Sparse(t *testing.T) {
	in := depInput()
	in.ArrivesAt, in.ContactPhone, in.Forecast = nil, "", nil
	in.DestLabel, in.DestIATA = "", ""
	msg := BuildDepartureSummaryEmail(in)
	if !strings.Contains(msg, "Subject: Departure summary: BA171\r\n") {
		t.Errorf("subject wrong:\n%s", msg)
	}
	for _, bad := range []string{"Arrival at", "For help or changes", "Weather", "border-collapse"} {
		if strings.Contains(msg, bad) {
			t.Errorf("email should omit %q:\n%s", bad, msg)
		}
	}

	// Supplier name and destination IATA fall back to generic wording.
	in = depInput()
	in.SupplierName, in.DestIATA, in.ForecastPlace, in.DestLabel = "", "", "", ""
	msg = BuildDepartureSummaryEmail(in)
	for _, want := range []string{"contact the airline: +44", "Arrival at the destination is", "Weather at your destination"} {
		if !strings.Contains(msg, want) {
			t.Errorf("email missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildDepartureSummaryEmail_Escaping(t *testing.T) {
	in := depInput()
	in.SupplierName = `<b>Evil</b> & Co`
	in.Forecast[0].Text = `<script>x</script>`
	msg := BuildDepartureSummaryEmail(in)
	// Only the HTML alternative needs escaping; the plain part is literal.
	html := msg[strings.Index(msg, "text/html"):]
	if strings.Contains(html, "<script>") || strings.Contains(html, "<b>Evil</b>") {
		t.Errorf("HTML not escaped:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("escaped form missing:\n%s", html)
	}
}

func TestCelsiusRounding(t *testing.T) {
	for in, want := range map[float64]string{
		0.5:  "1°C", // half away from zero, not half to even
		2.5:  "3°C",
		-0.4: "0°C", // never "-0°C"
		-0.5: "-1°C",
		-3.6: "-4°C",
		18.4: "18°C",
	} {
		if got := celsius(in); got != want {
			t.Errorf("celsius(%v) = %q, want %q", in, got, want)
		}
	}
	d := ForecastDay{MaxC: 0.5, MinC: -0.2}
	if got := tempRange(d); got != "1°C / 0°C" {
		t.Errorf("tempRange = %q", got)
	}
}
