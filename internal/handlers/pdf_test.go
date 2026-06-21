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
	// "(Hotel: <name> \(Check-in\))" while a stray label line would render as the
	// bare "(<name>)".
	if strings.Contains(s, "("+name+")") {
		t.Errorf("the place label should not repeat as a line when it echoes the title")
	}
	if !strings.Contains(s, `(Hotel: `+name+` \(Check-in\))`) {
		t.Errorf("the title should still name the hotel and mark the check-in:\n%s", s)
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
		`Hotel: Radisson Blu \(Check-in\)`,
		`Hotel: Radisson Blu \(Check-out\)`,
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
		"flight": "Flight", "train": "Train", "hotel": "Hotel", "car": "Car hire",
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
