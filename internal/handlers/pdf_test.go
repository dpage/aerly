package handlers

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

// mustTime parses an RFC3339 instant for fixtures.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}

func TestRenderItineraryPDFStructure(t *testing.T) {
	start := mustTime(t, "2026-06-15T12:30:00Z")
	end := mustTime(t, "2026-06-15T14:45:00Z")
	trip := &store.Trip{Name: "Paris (2026)", Destination: "Paris", StartsOn: &start, EndsOn: &end}
	plans := []api.PlanDTO{
		{
			Type: "flight", Title: "BA303", ConfirmationRef: "ABC123", TicketNumber: "125-4567",
			SupplierName: "British Airways", ContactPhone: "+44 20 1234", ContactEmail: "help@ba.com",
			Website: "https://ba.com", Notes: "Seat 14A,\nwindow.",
			Parts: []api.PlanPartDTO{{
				StartsAt: start, EndsAt: &end, StartTZ: "Europe/London", EndTZ: "Europe/Paris",
				StartLabel: "London Heathrow T5", EndLabel: "Paris CDG",
				StartAddress: "Heathrow Airport, Longford TW6 1QG",
				EndAddress:   "95731 Roissy-en-France, France", Status: "confirmed",
			}},
		},
	}

	s := string(renderItineraryPDF(trip, plans, nil, "a4"))

	if !strings.HasPrefix(s, "%PDF-1.4") {
		t.Errorf("missing PDF header: %q", s[:min(16, len(s))])
	}
	for _, want := range []string{
		"/Type /Catalog", "/Type /Pages", "/BaseFont /Helvetica",
		"/BaseFont /Helvetica-Bold", "xref", "trailer", "startxref", "%%EOF",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF missing %q", want)
		}
	}
	// Content is uncompressed, so itinerary text appears literally (with the
	// PDF string escaping for parentheses).
	for _, want := range []string{
		`Paris \(2026\)`, "Paris", "Flight: BA303", "London Heathrow T5 -> Paris CDG",
		"From: Heathrow Airport, Longford TW6 1QG", "To: 95731 Roissy-en-France, France",
		"Confirmation: ABC123", "Ticket: 125-4567", "Booked with: British Airways",
		"Tel: +44 20 1234", "Email: help@ba.com", "https://ba.com",
		"Seat 14A, window.", "Monday, 15 June 2026", "Page 1 of",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF content missing %q", want)
		}
	}
	// A4 media box dimensions.
	if !strings.Contains(s, "595.28 841.89") {
		t.Errorf("A4 MediaBox missing from:\n%s", s)
	}
}

// A hotel carries the same place in start_* and end_*; the itinerary must show
// it once (one Address line, no "X -> X" route, no duplicated From/To) and must
// not repeat the place label when it just echoes the title. A multi-night stay
// splits into a check-in row and a check-out row, so the title carries the
// "(Check-in)" / "(Check-out)" suffix.
func TestRenderItineraryPDFSingleAddress(t *testing.T) {
	in := mustTime(t, "2026-07-20T16:00:00Z")
	out := mustTime(t, "2026-07-23T12:00:00Z")
	name := "Courtyard by Marriott Pittsburgh University Center"
	addr := "100 Lytton Avenue, Pittsburgh, Pennsylvania 15213, USA"
	trip := &store.Trip{Name: "Stay"}
	plans := []api.PlanDTO{{
		Type: "hotel", Title: name, ConfirmationRef: "97703742",
		Parts: []api.PlanPartDTO{{
			StartsAt: in, EndsAt: &out, StartTZ: "America/New_York", EndTZ: "America/New_York",
			StartLabel: name, EndLabel: name, StartAddress: addr, EndAddress: addr,
		}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))
	if !strings.Contains(s, "Address: "+addr) {
		t.Errorf("hotel should show one Address line:\n%s", s)
	}
	if strings.Contains(s, "From: ") || strings.Contains(s, "To: ") {
		t.Errorf("hotel must not render From/To addresses")
	}
	if strings.Contains(s, name+" -> "+name) {
		t.Errorf("hotel must not render a redundant X -> X route")
	}
	// The place label repeats the title, so it must not also appear as its own
	// detail line. In the content stream the title renders as
	// "(Accommodation: <name> \(Check-in\))" while a stray label line would render
	// as the bare "(<name>)".
	if strings.Contains(s, "("+name+")") {
		t.Errorf("the place label should not repeat as a line when it echoes the title")
	}
	if !strings.Contains(s, `(Accommodation: `+name+` \(Check-in\))`) {
		t.Errorf("the title should still name the accommodation and mark the check-in:\n%s", s)
	}
}

// A multi-night hotel stay must render as two rows — a check-in on its first day
// and a check-out on its last — so the check-out time appears on its own day,
// not only inside a span line on the check-in day (the reported bug).
func TestRenderItineraryPDFHotelCheckOut(t *testing.T) {
	in := mustTime(t, "2026-09-07T14:00:00Z")  // 15:00 BST check-in, Mon 7 Sep
	out := mustTime(t, "2026-09-09T11:00:00Z") // 12:00 BST check-out, Wed 9 Sep
	trip := &store.Trip{Name: "PGDay UK 2026"}
	plans := []api.PlanDTO{{
		Type: "hotel", Title: "Radisson Blu", ConfirmationRef: "3BD48BMG",
		Parts: []api.PlanPartDTO{{
			StartsAt: in, EndsAt: &out, StartTZ: "Europe/London",
		}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))

	for _, want := range []string{
		`Accommodation: Radisson Blu \(Check-in\)`,
		`Accommodation: Radisson Blu \(Check-out\)`,
		"Check-in: Mon 7 Sep, 15:00",
		"Check-out: Wed 9 Sep, 12:00",
		"Monday, 7 September 2026",
		"Wednesday, 9 September 2026",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("split hotel PDF missing %q:\n%s", want, s)
		}
	}
	// The confirmation belongs to the check-in row only, not duplicated onto the
	// check-out reminder.
	if got := strings.Count(s, "Confirmation: 3BD48BMG"); got != 1 {
		t.Errorf("confirmation should appear once (on check-in), got %d", got)
	}
}

// A same-day hotel booking (no overnight) is not a band: it stays one row with
// the usual span line, never split into check-in/check-out.
func TestRenderItineraryPDFHotelSameDay(t *testing.T) {
	in := mustTime(t, "2026-09-07T09:00:00Z")
	out := mustTime(t, "2026-09-07T17:00:00Z")
	trip := &store.Trip{Name: "Day room"}
	plans := []api.PlanDTO{{
		Type: "hotel", Title: "Yotel",
		Parts: []api.PlanPartDTO{{StartsAt: in, EndsAt: &out, StartTZ: "Europe/London"}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))
	if strings.Contains(s, "Check-in)") || strings.Contains(s, "Check-out)") {
		t.Errorf("a same-day hotel should not split into check-in/check-out rows:\n%s", s)
	}
	// The en-dash in the span is folded to ASCII "-" in the content stream.
	if !strings.Contains(s, "Mon 7 Sep, 10:00 - 18:00") {
		t.Errorf("a same-day hotel should show its full span:\n%s", s)
	}
}

// A single-location plan whose place label differs from the title still shows
// the place (and address) as detail lines.
func TestRenderItineraryPDFSingleLocationLabel(t *testing.T) {
	at := mustTime(t, "2026-06-15T19:30:00Z")
	trip := &store.Trip{Name: "Trip"}
	plans := []api.PlanDTO{{
		Type: "dining", Title: "Anniversary dinner",
		Parts: []api.PlanPartDTO{{
			StartsAt: at, StartTZ: "America/Los_Angeles",
			StartLabel: "The French Laundry", StartAddress: "6640 Washington St, Yountville, CA",
		}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))
	if !strings.Contains(s, "(The French Laundry)") {
		t.Errorf("a place label distinct from the title should be shown:\n%s", s)
	}
	if !strings.Contains(s, "Address: 6640 Washington St, Yountville, CA") {
		t.Errorf("single-location dining should show its address")
	}
}

func TestRenderItineraryPDFLetterAndEmpty(t *testing.T) {
	trip := &store.Trip{Name: "Empty"}
	out := renderItineraryPDF(trip, nil, nil, "letter")
	s := string(out)
	if !strings.Contains(s, "612.00 792.00") {
		t.Errorf("Letter MediaBox missing")
	}
	if !strings.Contains(s, "No plans to show.") {
		t.Errorf("empty itinerary should note there are no plans")
	}
}

// A trip with no name and a single cancelled, end-less part still renders, and
// the cancelled flag and fallback title appear.
func TestRenderItineraryPDFFallbacks(t *testing.T) {
	at := mustTime(t, "2026-01-02T08:00:00Z")
	trip := &store.Trip{} // no name, no destination, no dates
	plans := []api.PlanDTO{{
		Type: "", Title: "",
		Parts: []api.PlanPartDTO{{StartsAt: at, StartTZ: "", Status: "cancelled"}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, ""))
	if !strings.Contains(s, "Plan: Plan") {
		t.Errorf("untyped/untitled plan should fall back to Plan: Plan")
	}
	if !strings.Contains(s, "Status: cancelled") {
		t.Errorf("cancelled status should be shown")
	}
}

// Many plans force pagination; the page tree Count and footers must reflect it.
func TestRenderItineraryPDFPaginates(t *testing.T) {
	trip := &store.Trip{Name: "Long"}
	var plans []api.PlanDTO
	base := mustTime(t, "2026-03-01T09:00:00Z")
	for i := 0; i < 60; i++ {
		ts := base.Add(time.Duration(i) * 24 * time.Hour)
		plans = append(plans, api.PlanDTO{
			Type: "hotel", Title: "Stay",
			Notes: strings.Repeat("A long note that should wrap across the body column width. ", 4),
			Parts: []api.PlanPartDTO{{StartsAt: ts, StartTZ: "UTC"}},
		})
	}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))
	if strings.Contains(s, "/Count 1 ") {
		t.Errorf("expected multiple pages, got a single-page tree")
	}
	if !strings.Contains(s, "Page 2 of") {
		t.Errorf("expected a second page footer")
	}
}

// renderItinerariesPDF lays several trips into one document: every trip's title
// and plans appear, each trip starts a new page (so a two-trip doc has at least
// two pages), and the page-size preference is honoured.
func TestRenderItinerariesPDF(t *testing.T) {
	at := mustTime(t, "2026-06-15T12:30:00Z")
	parts := []api.PlanPartDTO{{StartsAt: at, StartTZ: "UTC", StartLabel: "Gate"}}
	sections := []tripItinerary{
		{
			trip:  &store.Trip{Name: "Paris (2026)", Destination: "Paris"},
			plans: []api.PlanDTO{{Type: "flight", Title: "BA303", Parts: parts}},
		},
		{
			trip:  &store.Trip{Name: "Berlin Trip"},
			plans: []api.PlanDTO{{Type: "train", Title: "ICE 123", Parts: parts}},
		},
	}
	s := string(renderItinerariesPDF(sections, "a4"))

	if !strings.HasPrefix(s, "%PDF-1.4") {
		t.Fatalf("missing PDF header")
	}
	for _, want := range []string{
		`Paris \(2026\)`, "Flight: BA303", "Berlin Trip", "Train: ICE 123",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("multi-trip PDF missing %q", want)
		}
	}
	// One page per trip → at least two pages, and a second-page footer.
	if strings.Contains(s, "/Count 1 ") {
		t.Errorf("expected at least two pages for two trips, got a single-page tree")
	}
	if !strings.Contains(s, "Page 2 of") {
		t.Errorf("expected a second-page footer for the second trip")
	}
}

// An empty section list still produces a valid (single, empty) PDF rather than
// panicking — the handler guards against this, but the renderer is defensive.
func TestRenderItinerariesPDFEmpty(t *testing.T) {
	s := string(renderItinerariesPDF(nil, "a4"))
	if !strings.HasPrefix(s, "%PDF-1.4") || !strings.Contains(s, "%%EOF") {
		t.Errorf("empty multi-trip export should still be a valid PDF")
	}
}

// flattenItinerary drops dismissed parts and orders the rest by start time.
func TestFlattenItinerary(t *testing.T) {
	t1 := mustTime(t, "2026-03-01T09:00:00Z")
	t2 := mustTime(t, "2026-03-01T12:00:00Z")
	t3 := mustTime(t, "2026-03-02T08:00:00Z")
	dismissed := t2
	plans := []api.PlanDTO{
		{Type: "hotel", Parts: []api.PlanPartDTO{
			{ID: 3, StartsAt: t3},
			{ID: 2, StartsAt: t2, DismissedAt: &dismissed}, // tidied away → skipped
		}},
		{Type: "flight", Parts: []api.PlanPartDTO{{ID: 1, StartsAt: t1}}},
	}
	rows := flattenItinerary(plans, nil)
	if len(rows) != 2 {
		t.Fatalf("expected 2 live rows, got %d", len(rows))
	}
	if rows[0].part == nil || rows[1].part == nil ||
		rows[0].part.part.ID != 1 || rows[1].part.part.ID != 3 {
		t.Errorf("parts not ordered by start time")
	}
}

// External feed events interleave with plan parts in start-time order.
func TestFlattenItineraryExternals(t *testing.T) {
	t1 := mustTime(t, "2026-03-01T09:00:00Z")
	t2 := mustTime(t, "2026-03-01T12:00:00Z")
	plans := []api.PlanDTO{
		{Type: "flight", Parts: []api.PlanPartDTO{{ID: 1, StartsAt: t1}}},
	}
	externals := []api.ExternalEventDTO{
		{ID: 7, Title: "Keynote", StartsAt: t2},
	}
	rows := flattenItinerary(plans, externals)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].part == nil || rows[1].ev == nil {
		t.Errorf("expected booking then external event by time, got %+v", rows)
	}
}

func TestPageSize(t *testing.T) {
	if w, h := pageSize("letter"); w != paperLetterW || h != paperLetterH {
		t.Errorf("letter = %v,%v", w, h)
	}
	if w, h := pageSize("a4"); w != paperA4W || h != paperA4H {
		t.Errorf("a4 = %v,%v", w, h)
	}
	if w, h := pageSize("bogus"); w != paperA4W || h != paperA4H {
		t.Errorf("unknown should default to A4, got %v,%v", w, h)
	}
}

func TestTypeLabel(t *testing.T) {
	cases := map[string]string{
		"flight": "Flight", "train": "Train", "hotel": "Accommodation", "vehicle_hire": "Car hire",
		"ferry": "Ferry", "bus": "Bus", "coach": "Bus", "": "Plan", "spaceflight": "Spaceflight",
	}
	for in, want := range cases {
		if got := typeLabel(in); got != want {
			t.Errorf("typeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDateSpan(t *testing.T) {
	a := mustTime(t, "2026-06-01T00:00:00Z")
	b := mustTime(t, "2026-06-09T00:00:00Z")
	if got := dateSpan(&a, &b); got != "1 Jun 2026 – 9 Jun 2026" {
		t.Errorf("both = %q", got)
	}
	if got := dateSpan(&a, nil); got != "from 1 Jun 2026" {
		t.Errorf("start only = %q", got)
	}
	if got := dateSpan(nil, &b); got != "until 9 Jun 2026" {
		t.Errorf("end only = %q", got)
	}
	if got := dateSpan(nil, nil); got != "" {
		t.Errorf("none = %q, want empty", got)
	}
}

func TestSingleLocation(t *testing.T) {
	// Stationary types are always single-location, even if end_* is populated.
	for _, ty := range []string{"hotel", "dining", "excursion"} {
		pt := &api.PlanPartDTO{StartLabel: "X", EndLabel: "Y", StartAddress: "a", EndAddress: "b"}
		if !singleLocation(ty, pt) {
			t.Errorf("%s should be single-location", ty)
		}
	}
	// A journey with distinct ends is not single-location.
	journey := &api.PlanPartDTO{StartLabel: "LHR", EndLabel: "PIT"}
	if singleLocation("flight", journey) {
		t.Errorf("a flight with distinct ends should not be single-location")
	}
	// A non-stationary type collapses when its ends coincide (or are blank).
	same := &api.PlanPartDTO{StartLabel: "Office", EndLabel: "Office", StartAddress: "1 St", EndAddress: "1 St"}
	if !singleLocation("ground", same) {
		t.Errorf("a part whose ends coincide should be single-location")
	}
	blank := &api.PlanPartDTO{StartLabel: "Park"}
	if !singleLocation("ground", blank) {
		t.Errorf("a part with a blank end should be single-location")
	}
}

func TestRouteLine(t *testing.T) {
	if got := routeLine("A", "B"); got != "A -> B" {
		t.Errorf("both = %q", got)
	}
	if got := routeLine("A", ""); got != "A" {
		t.Errorf("from only = %q", got)
	}
	if got := routeLine("", "B"); got != "B" {
		t.Errorf("to only = %q", got)
	}
	if got := routeLine("", ""); got != "" {
		t.Errorf("none = %q", got)
	}
}

func TestTimeRange(t *testing.T) {
	start := mustTime(t, "2026-06-15T08:00:00Z")
	endSame := mustTime(t, "2026-06-15T10:30:00Z")
	endNext := mustTime(t, "2026-06-16T07:00:00Z")

	if got := timeRange(start, nil, "UTC", ""); got != "Mon 15 Jun, 08:00" {
		t.Errorf("no end = %q", got)
	}
	if got := timeRange(start, &endSame, "UTC", ""); got != "Mon 15 Jun, 08:00 – 10:30" {
		t.Errorf("same day = %q", got)
	}
	if got := timeRange(start, &endNext, "UTC", "UTC"); got != "Mon 15 Jun, 08:00 – Tue 16 Jun, 07:00" {
		t.Errorf("cross day = %q", got)
	}
}

func TestLabelledAndJoin(t *testing.T) {
	if got := labelled("Confirmation", "X1"); got != "Confirmation: X1" {
		t.Errorf("labelled = %q", got)
	}
	if got := labelled("Confirmation", "   "); got != "" {
		t.Errorf("blank value should yield empty, got %q", got)
	}
	if got := joinNonEmpty("   ", "", "a", "", "b"); got != "a   b" {
		t.Errorf("joinNonEmpty = %q", got)
	}
	if got := joinNonEmpty(" · ", "", ""); got != "" {
		t.Errorf("all-empty join = %q", got)
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("a\nb   c\n\n d "); got != "a b c d" {
		t.Errorf("oneLine = %q", got)
	}
}

func TestEventLoc(t *testing.T) {
	if eventLoc("") != time.UTC {
		t.Errorf("empty tz should be UTC")
	}
	if eventLoc("Not/AZone") != time.UTC {
		t.Errorf("bad tz should fall back to UTC")
	}
	if loc := eventLoc("Europe/Paris"); loc == nil || loc.String() != "Europe/Paris" {
		t.Errorf("valid tz not loaded: %v", loc)
	}
}

func TestWinAnsiByte(t *testing.T) {
	cases := map[rune]byte{
		'A': 'A', ' ': ' ', '~': '~', 0xE9: 0xE9, // é (Latin-1)
		'’': '\'', '“': '"', '”': '"', '–': '-', '—': '-', '•': '*', '→': '>',
		'☃': '?', // outside any mapping
	}
	for in, want := range cases {
		if got := winAnsiByte(in); got != want {
			t.Errorf("winAnsiByte(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPDFString(t *testing.T) {
	if got := pdfString(`a(b)c\d`); got != `a\(b\)c\\d` {
		t.Errorf("escaping = %q", got)
	}
	if got := pdfString("é→"); got != "\xE9>" {
		t.Errorf("encoding = %q", got)
	}
}

func TestWrapText(t *testing.T) {
	// Empty input still yields one (empty) line.
	if got := wrapText("", 10, 100); len(got) != 1 || got[0] != "" {
		t.Errorf("empty = %v", got)
	}
	// Non-positive width returns the whole string untouched.
	if got := wrapText("a b c", 10, 0); len(got) != 1 || got[0] != "a b c" {
		t.Errorf("zero width = %v", got)
	}
	// A normal paragraph wraps to more than one line at a narrow width.
	lines := wrapText("the quick brown fox jumps over the lazy dog", 10, 60)
	if len(lines) < 2 {
		t.Errorf("expected wrapping, got %v", lines)
	}
	for _, ln := range lines {
		if helveticaTextWidth(ln, 10) > 60 {
			t.Errorf("line over width: %q (%.1f)", ln, helveticaTextWidth(ln, 10))
		}
	}
	// A single word longer than the line is hard-broken into pieces.
	broken := wrapText("supercalifragilisticexpialidocious", 12, 40)
	if len(broken) < 2 {
		t.Errorf("expected hard break, got %v", broken)
	}
}

func TestHelveticaTextWidth(t *testing.T) {
	// Wider glyphs measure wider than narrow ones at the same size.
	if helveticaTextWidth("WWWW", 10) <= helveticaTextWidth("iiii", 10) {
		t.Errorf("W should be wider than i")
	}
	// Out-of-table bytes use the fallback width (non-zero).
	if helveticaWidth(0x80) != 556 {
		t.Errorf("fallback width = %d", helveticaWidth(0x80))
	}
	if helveticaWidth(' ') != 278 {
		t.Errorf("space width = %d", helveticaWidth(' '))
	}
}

func TestAssemblePDFOffsets(t *testing.T) {
	out := assemblePDF([]string{"<< /Type /Catalog >>"})
	s := string(out)
	if !bytes.HasPrefix(out, []byte("%PDF-1.4")) {
		t.Errorf("no header")
	}
	if !strings.Contains(s, "1 0 obj") || !strings.Contains(s, "/Size 2") {
		t.Errorf("object/trailer wrong:\n%s", s)
	}
}

// TestPDFHotelOutputUnchangedByBandingRefactor is the golden test for the
// banding unification on the PDF side: a two-night hotel stay must keep
// producing exactly the same ordered run of drawn text (day headers, times, row
// titles and detail lines) that it produced before the refactor, since those
// labels are user-visible on a printed itinerary.
func TestPDFHotelOutputUnchangedByBandingRefactor(t *testing.T) {
	in := mustTime(t, "2026-09-07T14:00:00Z")
	out := mustTime(t, "2026-09-09T11:00:00Z")
	trip := &store.Trip{Name: "Test Trip"}
	plans := []api.PlanDTO{{
		Type: "hotel", Title: "Test Hotel", ConfirmationRef: "TESTREF1",
		SupplierName: "Test Bookings", ContactPhone: "+44 20 7946 0100",
		Parts: []api.PlanPartDTO{{
			StartsAt: in, EndsAt: &out, StartTZ: "Europe/London",
			StartLabel: "Test Hotel", EndLabel: "Test Hotel",
			StartAddress: "1 Test Street, Testville TE1 1ST",
		}},
	}}

	got := strings.Join(pdfTextRuns(string(renderItineraryPDF(trip, plans, nil, "a4"))), "\n")
	want := strings.Join([]string{
		"Test Trip",
		"Monday, 7 September 2026",
		"15:00",
		`Accommodation: Test Hotel \(Check-in\)`,
		"Address: 1 Test Street, Testville TE1 1ST",
		"Check-in: Mon 7 Sep, 15:00",
		"Confirmation: TESTREF1",
		"Booked with: Test Bookings Tel: +44 20 7946 0100",
		"Wednesday, 9 September 2026",
		"12:00",
		`Accommodation: Test Hotel \(Check-out\)`,
		"Address: 1 Test Street, Testville TE1 1ST",
		"Check-out: Wed 9 Sep, 12:00",
		"Aerly itinerary  \xb7  Page 1 of 1",
	}, "\n")
	if got != want {
		t.Fatalf("hotel itinerary text changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// pdfTextRuns extracts, in order, every string the layout drew with a Tj
// operator. The renderer writes one "(text) Tj" per line, so this reads back
// the visible itinerary text without decoding the whole PDF.
func pdfTextRuns(pdf string) []string {
	var out []string
	for _, line := range strings.Split(pdf, "\n") {
		if strings.HasPrefix(line, "(") && strings.HasSuffix(line, ") Tj") {
			out = append(out, line[1:len(line)-len(") Tj")])
		}
	}
	return out
}

// A multi-day vehicle hire splits into a pickup row on its first day and a
// return row on its last, the same rule a multi-night hotel follows, but
// labelled for a hire.
func TestRenderItineraryPDFVehicleHireBands(t *testing.T) {
	pickup := mustTime(t, "2026-09-09T08:00:00Z")  // 09:00 BST, Wed 9 Sep
	dropoff := mustTime(t, "2026-09-11T09:00:00Z") // 10:00 BST, Fri 11 Sep
	trip := &store.Trip{Name: "Test Trip"}
	plans := []api.PlanDTO{{
		Type: "vehicle_hire", Title: "Test Car Hire", ConfirmationRef: "TESTREF2",
		Parts: []api.PlanPartDTO{{
			StartsAt: pickup, EndsAt: &dropoff, StartTZ: "Europe/London",
			StartLabel: "Testport Terminal 1", EndLabel: "Testville Central",
		}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))

	for _, want := range []string{
		`\(Pickup\)`,
		`\(Return\)`,
		"Pickup: Wed 9 Sep, 09:00",
		"Return: Fri 11 Sep, 10:00",
		"Wednesday, 9 September 2026",
		"Friday, 11 September 2026",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("banded hire PDF missing %q:\n%s", want, s)
		}
	}
	// As with a hotel, the booking reference belongs to the first row only; the
	// return row is just a "when to bring it back" reminder.
	if got := strings.Count(s, "Confirmation: TESTREF2"); got != 1 {
		t.Errorf("confirmation should appear once (on the pickup row), got %d", got)
	}
}

// TestRenderItineraryPDFOneWayHireEdgesShowOwnEnd is a regression test for a
// Task 7 review Minor: the route/From/To block used to be emitted before the
// edge switch, so both the pickup and return rows of a one-way hire repeated
// the full pickup->drop-off route and both addresses, meaning the return row
// told the reader to head for the collection desk. Each edge must now show
// only its own end: the pickup row its place and address, the return row its
// own.
func TestRenderItineraryPDFOneWayHireEdgesShowOwnEnd(t *testing.T) {
	pickup := mustTime(t, "2026-09-09T08:00:00Z")
	dropoff := mustTime(t, "2026-09-11T09:00:00Z")
	trip := &store.Trip{Name: "Test Trip"}
	plans := []api.PlanDTO{{
		Type: "vehicle_hire", Title: "Test Car Hire",
		Parts: []api.PlanPartDTO{{
			StartsAt: pickup, EndsAt: &dropoff, StartTZ: "Europe/London",
			StartLabel:   "Testport Desk",
			EndLabel:     "Testville Depot",
			StartAddress: "1 Airport Road, Testport",
			EndAddress:   "2 Depot Street, Testville",
		}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))

	if !strings.Contains(s, "Address: 1 Airport Road, Testport") {
		t.Errorf("pickup row should show the pickup address:\n%s", s)
	}
	if !strings.Contains(s, "Address: 2 Depot Street, Testville") {
		t.Errorf("return row should show the return address:\n%s", s)
	}
	// The old code printed both addresses (as From:/To:) on both rows. Each
	// address, and each place label, must now appear exactly once: on its own
	// edge's row only. In particular the return row must NOT carry the pickup
	// address.
	if got := strings.Count(s, "1 Airport Road, Testport"); got != 1 {
		t.Errorf("pickup address should appear once (pickup row only), got %d occurrences:\n%s", got, s)
	}
	if got := strings.Count(s, "2 Depot Street, Testville"); got != 1 {
		t.Errorf("return address should appear once (return row only), got %d occurrences:\n%s", got, s)
	}
	if got := strings.Count(s, "Testport Desk"); got != 1 {
		t.Errorf("pickup place should appear once (pickup row only), got %d occurrences:\n%s", got, s)
	}
	if got := strings.Count(s, "Testville Depot"); got != 1 {
		t.Errorf("return place should appear once (return row only), got %d occurrences:\n%s", got, s)
	}
	if strings.Contains(s, "Testport Desk -> Testville Depot") {
		t.Errorf("a banded edge row should not print the whole route:\n%s", s)
	}
}

// TestRenderItineraryPDFHotelAddressUnaffectedByOneWayHireFix guards the
// singleLocation branch (hotel/dining/excursion) against the per-edge address
// change made for one-way hires: a hotel keeps showing the same single address
// on both its check-in and check-out rows, exactly as before.
func TestRenderItineraryPDFHotelAddressUnaffectedByOneWayHireFix(t *testing.T) {
	in := mustTime(t, "2026-09-07T14:00:00Z")
	out := mustTime(t, "2026-09-09T11:00:00Z")
	trip := &store.Trip{Name: "Test Trip"}
	plans := []api.PlanDTO{{
		Type: "hotel", Title: "Test Hotel",
		Parts: []api.PlanPartDTO{{
			StartsAt: in, EndsAt: &out, StartTZ: "Europe/London",
			StartLabel:   "Test Hotel",
			EndLabel:     "Test Hotel",
			StartAddress: "1 Test Street, Testville TE1 1ST",
		}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))
	if got := strings.Count(s, "Address: 1 Test Street, Testville TE1 1ST"); got != 2 {
		t.Errorf("hotel address should appear on both the check-in and check-out rows (unchanged), got %d occurrences:\n%s", got, s)
	}
}

// A hire picked up and returned on the same day is not a band: one row, with
// the usual span line.
func TestRenderItineraryPDFVehicleHireSameDay(t *testing.T) {
	pickup := mustTime(t, "2026-09-09T08:00:00Z")
	dropoff := mustTime(t, "2026-09-09T16:00:00Z")
	trip := &store.Trip{Name: "Test Trip"}
	plans := []api.PlanDTO{{
		Type: "vehicle_hire", Title: "Test Day Hire",
		Parts: []api.PlanPartDTO{{StartsAt: pickup, EndsAt: &dropoff, StartTZ: "Europe/London"}},
	}}
	s := string(renderItineraryPDF(trip, plans, nil, "a4"))
	if strings.Contains(s, "Pickup)") || strings.Contains(s, "Return)") {
		t.Errorf("a same-day hire should not split into pickup/return rows:\n%s", s)
	}
	if !strings.Contains(s, "Wed 9 Sep, 09:00 - 17:00") {
		t.Errorf("a same-day hire should show its full span:\n%s", s)
	}
}

// Banding is opt-in by type: an overnight journey ends on a later local day but
// must still render as a single row, never split into two.
func TestRenderItineraryPDFOvernightJourneyNeverBands(t *testing.T) {
	depart := mustTime(t, "2026-09-09T21:00:00Z")
	arrive := mustTime(t, "2026-09-10T05:30:00Z")
	for _, typ := range []string{"flight", "train", "ground"} {
		trip := &store.Trip{Name: "Test Trip"}
		plans := []api.PlanDTO{{
			Type: typ, Title: "Overnight",
			Parts: []api.PlanPartDTO{{
				StartsAt: depart, EndsAt: &arrive, StartTZ: "Europe/London",
				StartLabel: "Testville", EndLabel: "Testburg",
			}},
		}}
		s := string(renderItineraryPDF(trip, plans, nil, "a4"))
		if got := strings.Count(s, ": Overnight)"); got != 1 {
			t.Errorf("%s: an overnight journey must render as one row, got %d:\n%s", typ, got, s)
		}
		if strings.Count(s, "September 2026") != 1 {
			t.Errorf("%s: an overnight journey must not open a second day header:\n%s", typ, s)
		}
	}
}
