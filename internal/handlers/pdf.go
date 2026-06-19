package handlers

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

// Hand-rolled PDF itinerary renderer. No external deps — like the iCal renderer
// in ics.go we generate the file directly. PDF is verbose but its core is
// simple: a handful of indirect objects (catalog, page tree, two base-14 fonts,
// and one content stream per page) followed by a cross-reference table. We use
// the standard Helvetica / Helvetica-Bold fonts so nothing has to be embedded,
// and lay text out top-down in points (1/72") with the Helvetica metrics below
// driving word-wrap. Output is deterministic for a given input.

// Page dimensions in PostScript points (72 per inch). A4 is the default; US
// Letter is offered as a per-user preference (issue #90).
const (
	paperA4W     = 595.28
	paperA4H     = 841.89
	paperLetterW = 612.0
	paperLetterH = 792.0
)

// pageSize maps a paper-size preference to its point dimensions, defaulting to
// A4 for the empty/unknown value so a legacy or malformed setting still renders.
func pageSize(paper string) (w, h float64) {
	if paper == "letter" {
		return paperLetterW, paperLetterH
	}
	return paperA4W, paperA4H
}

// pdfLayout accumulates page content streams as it lays an itinerary out from
// the top of the page downward. y is the current baseline cursor (PDF's origin
// is bottom-left, so it counts down toward the bottom margin).
type pdfLayout struct {
	pageW, pageH float64
	margin       float64
	footer       float64 // height reserved at the bottom for the page footer
	y            float64
	pages        []*bytes.Buffer
	cur          *bytes.Buffer
}

func newPDFLayout(paper string) *pdfLayout {
	w, h := pageSize(paper)
	l := &pdfLayout{pageW: w, pageH: h, margin: 56, footer: 40}
	l.newPage()
	return l
}

// newPage starts a fresh page and resets the cursor to just below the top
// margin.
func (l *pdfLayout) newPage() {
	b := &bytes.Buffer{}
	l.pages = append(l.pages, b)
	l.cur = b
	l.y = l.pageH - l.margin
}

// contentWidth is the usable width between the left and right margins.
func (l *pdfLayout) contentWidth() float64 { return l.pageW - 2*l.margin }

// ensure starts a new page when h points of vertical space no longer fit above
// the bottom margin (plus the reserved footer band).
func (l *pdfLayout) ensure(h float64) {
	if l.y-h < l.margin+l.footer {
		l.newPage()
	}
}

// text draws one line of text at (x, y) in the given font ("F1" Helvetica or
// "F2" Helvetica-Bold), size, and grayscale level (0 black … 1 white).
func (l *pdfLayout) text(x, y float64, font string, size, gray float64, s string) {
	fmt.Fprintf(l.cur, "%.3f g\nBT\n/%s %.2f Tf\n1 0 0 1 %.2f %.2f Tm\n(%s) Tj\nET\n",
		gray, font, size, x, y, pdfString(s))
}

// hrule draws a horizontal line across the content width at the cursor.
func (l *pdfLayout) hrule(gray float64) {
	fmt.Fprintf(l.cur, "%.3f G\n0.5 w\n%.2f %.2f m %.2f %.2f l S\n",
		gray, l.margin, l.y, l.pageW-l.margin, l.y)
}

// line writes a left-aligned wrapped paragraph in the body column starting at x,
// advancing the cursor by the consumed height (with page breaks as needed), and
// returns nothing — the cursor reflects the new position.
func (l *pdfLayout) line(x float64, font string, size, gray, leading float64, s string) {
	maxW := l.pageW - l.margin - x
	for _, ln := range wrapText(s, size, maxW) {
		l.ensure(leading)
		l.text(x, l.y, font, size, gray, ln)
		l.y -= leading
	}
}

// typeLabel turns a plan type into a human heading word. Unknown types are
// title-cased so a new plan type still reads sensibly.
func typeLabel(t string) string {
	switch t {
	case "flight":
		return "Flight"
	case "train":
		return "Train"
	case "hotel":
		return "Hotel"
	case "car":
		return "Car hire"
	case "ferry":
		return "Ferry"
	case "bus", "coach":
		return "Bus"
	case "":
		return "Plan"
	default:
		return strings.ToUpper(t[:1]) + t[1:]
	}
}

// itinPart pairs a visible plan_part with its owning plan, the unit the
// itinerary renders one row per. The plan carries the booking-level details
// (confirmation, ticket, supplier, contact); the part carries the leg's times,
// labels and addresses.
type itinPart struct {
	plan *api.PlanDTO
	part *api.PlanPartDTO
}

// flattenPlans expands the trip's plans into time-ordered itinerary rows, one
// per non-dismissed part, sorted by start time (then part id for stable
// ordering of parts that share an instant).
func flattenPlans(plans []api.PlanDTO) []itinPart {
	var items []itinPart
	for i := range plans {
		pl := &plans[i]
		for j := range pl.Parts {
			pt := &pl.Parts[j]
			if pt.DismissedAt != nil {
				continue // superseded/tidied-away leg, as the calendar feed omits
			}
			items = append(items, itinPart{plan: pl, part: pt})
		}
	}
	sort.SliceStable(items, func(a, b int) bool {
		ai, bi := items[a].part, items[b].part
		if !ai.StartsAt.Equal(bi.StartsAt) {
			return ai.StartsAt.Before(bi.StartsAt)
		}
		return ai.ID < bi.ID
	})
	return items
}

// renderItineraryPDF lays out the trip's visible plans as a printable PDF and
// returns the encoded file. Parts are grouped under a header per local day, in
// time order, each with its route, addresses, booking references and contact
// details. paper is the user's page-size preference.
func renderItineraryPDF(trip *store.Trip, plans []api.PlanDTO, paper string) []byte {
	l := newPDFLayout(paper)

	// Title block: trip name, then destination/date meta, then a rule.
	name := trip.Name
	if name == "" {
		name = "Trip"
	}
	l.ensure(26)
	l.text(l.margin, l.y, "F2", 20, 0.1, name)
	l.y -= 26
	if meta := tripMeta(trip); meta != "" {
		l.text(l.margin, l.y, "F1", 11, 0.4, meta)
		l.y -= 18
	}
	l.hrule(0.75)
	l.y -= 18

	items := flattenPlans(plans)
	if len(items) == 0 {
		l.text(l.margin, l.y, "F1", 11, 0.4, "No plans to show.")
		l.y -= 16
	}

	bodyX := l.margin + 64
	var lastDay string
	for _, it := range items {
		loc := eventLoc(it.part.StartTZ)
		start := it.part.StartsAt.In(loc)
		day := start.Format("Monday, 2 January 2006")
		if day != lastDay {
			l.ensure(34)
			l.y -= 6
			l.text(l.margin, l.y, "F2", 12, 0.15, day)
			l.y -= 18
			lastDay = day
		}

		// Each row: a bold time in the left column, the title beside it, then
		// indented detail lines. Keep the time+title pair together on a page.
		l.ensure(28)
		l.text(l.margin, l.y, "F2", 10, 0.1, start.Format("15:04"))
		title := typeLabel(it.plan.Type) + ": " + nonEmpty(it.plan.Title, typeLabel(it.plan.Type))
		l.text(bodyX, l.y, "F2", 11, 0.1, title)
		l.y -= 16

		for _, d := range partDetails(it) {
			l.line(bodyX, "F1", 9.5, 0.4, 13, d)
		}
		l.y -= 8
	}

	return l.encode()
}

// tripMeta is the secondary header line: destination and, when set, the trip's
// date span. Either part may be absent.
func tripMeta(trip *store.Trip) string {
	var parts []string
	if trip.Destination != "" {
		parts = append(parts, trip.Destination)
	}
	if span := dateSpan(trip.StartsOn, trip.EndsOn); span != "" {
		parts = append(parts, span)
	}
	return strings.Join(parts, "  ·  ")
}

// dateSpan formats a trip's from/to dates. Returns "" when neither is set.
func dateSpan(start, end *time.Time) string {
	const f = "2 Jan 2006"
	switch {
	case start != nil && end != nil:
		return start.Format(f) + " – " + end.Format(f)
	case start != nil:
		return "from " + start.Format(f)
	case end != nil:
		return "until " + end.Format(f)
	default:
		return ""
	}
}

// partDetails builds the indented detail lines under one itinerary row: the
// from→to route, the departure/arrival (or single) address, the full time
// range, the booking references (confirmation + ticket), the supplier contact
// block, a cancelled flag, and any notes. Empty fields are skipped.
func partDetails(it itinPart) []string {
	pl, pt := it.plan, it.part
	var out []string

	if route := routeLine(pt.StartLabel, pt.EndLabel); route != "" {
		out = append(out, route)
	}
	// Addresses. A part with a destination (a journey: flight/train/transfer)
	// labels them From/To; a single-location plan (hotel/dining/excursion) shows
	// one "Address" line.
	twoEnded := pt.EndLabel != "" || pt.EndAddress != ""
	if pt.StartAddress != "" {
		if twoEnded {
			out = append(out, "From: "+oneLine(pt.StartAddress))
		} else {
			out = append(out, "Address: "+oneLine(pt.StartAddress))
		}
	}
	if pt.EndAddress != "" {
		out = append(out, "To: "+oneLine(pt.EndAddress))
	}

	out = append(out, timeRange(pt.StartsAt, pt.EndsAt, pt.StartTZ, pt.EndTZ))

	if refs := joinNonEmpty("   ", labelled("Confirmation", pl.ConfirmationRef),
		labelled("Ticket", pl.TicketNumber)); refs != "" {
		out = append(out, refs)
	}
	if contact := joinNonEmpty("   ", labelled("Booked with", pl.SupplierName),
		labelled("Tel", pl.ContactPhone), labelled("Email", pl.ContactEmail)); contact != "" {
		out = append(out, contact)
	}
	if pl.Website != "" {
		out = append(out, oneLine(pl.Website))
	}
	if pt.Status == "cancelled" {
		out = append(out, "Status: cancelled")
	}
	if pl.Notes != "" {
		out = append(out, oneLine(pl.Notes))
	}
	return out
}

// labelled returns "label: value" when value is non-blank, else "".
func labelled(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return label + ": " + value
}

// joinNonEmpty joins the non-empty parts with sep.
func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

// oneLine collapses any internal newlines and runs of whitespace in free text
// (addresses, notes) into single spaces so it flows through the wrapper as one
// paragraph rather than breaking the layout.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// routeLine joins a start and end label with an arrow, tolerating either being
// blank.
func routeLine(from, to string) string {
	switch {
	case from != "" && to != "":
		return from + " -> " + to
	case from != "":
		return from
	case to != "":
		return to
	default:
		return ""
	}
}

// timeRange renders a part's local time span, e.g. "Mon 2 Jan, 14:30 – 16:45"
// or, across days, with the end's date too. End-less parts show just the start.
func timeRange(startsAt time.Time, endsAt *time.Time, startTZ, endTZ string) string {
	start := startsAt.In(eventLoc(startTZ))
	s := start.Format("Mon 2 Jan, 15:04")
	if endsAt == nil {
		return s
	}
	endLoc := eventLoc(startTZ)
	if endTZ != "" {
		endLoc = eventLoc(endTZ)
	}
	end := endsAt.In(endLoc)
	if sameDay(start, end) {
		return s + " – " + end.Format("15:04")
	}
	return s + " – " + end.Format("Mon 2 Jan, 15:04")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// eventLoc loads an IANA zone, falling back to UTC for an empty or unknown name
// so rendering never fails on bad tz data.
func eventLoc(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// --- Low-level PDF encoding ---

// encode serializes the laid-out pages into a complete PDF file. It appends a
// footer to every page first (page numbers need the final total), then writes
// the object table and cross-reference section.
func (l *pdfLayout) encode() []byte {
	n := len(l.pages)
	for i, p := range l.pages {
		l.writeFooter(p, i+1, n)
	}

	// Object numbering: 1 catalog, 2 page tree, 3/4 fonts, then the page
	// objects, then their content streams.
	const fontF1, fontF2 = 3, 4
	pageObj := func(i int) int { return 5 + i }
	contentObj := func(i int) int { return 5 + n + i }

	bodies := make([]string, 0, 4+2*n)
	bodies = append(bodies,
		"<< /Type /Catalog /Pages 2 0 R >>",
		pagesTreeBody(n, pageObj),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>",
	)
	for i := 0; i < n; i++ {
		bodies = append(bodies, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] "+
				"/Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> >> /Contents %d 0 R >>",
			l.pageW, l.pageH, fontF1, fontF2, contentObj(i)))
	}
	for i := 0; i < n; i++ {
		data := l.pages[i].Bytes()
		bodies = append(bodies, fmt.Sprintf(
			"<< /Length %d >>\nstream\n%s\nendstream", len(data), data))
	}

	return assemblePDF(bodies)
}

// pagesTreeBody renders the /Pages object listing every page as a kid.
func pagesTreeBody(n int, pageObj func(int) int) string {
	var kids strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			kids.WriteByte(' ')
		}
		fmt.Fprintf(&kids, "%d 0 R", pageObj(i))
	}
	return fmt.Sprintf("<< /Type /Pages /Kids [ %s ] /Count %d >>", kids.String(), n)
}

// writeFooter stamps a centred "Page i of n" plus the app name at the bottom
// margin of one page's content stream.
func (l *pdfLayout) writeFooter(p *bytes.Buffer, page, total int) {
	label := fmt.Sprintf("Aerly itinerary  ·  Page %d of %d", page, total)
	w := helveticaTextWidth(label, 8)
	x := (l.pageW - w) / 2
	fmt.Fprintf(p, "%.3f g\nBT\n/F1 8 Tf\n1 0 0 1 %.2f %.2f Tm\n(%s) Tj\nET\n",
		0.55, x, l.margin*0.55, pdfString(label))
}

// assemblePDF writes header, the numbered objects, the xref table and trailer
// for object bodies given in object-number order (bodies[0] is object 1).
func assemblePDF(bodies []string) []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(bodies)+1)
	for i, body := range bodies {
		num := i + 1
		offsets[num] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", len(bodies)+1)
	out.WriteString("0000000000 65535 f \n")
	for num := 1; num <= len(bodies); num++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[num])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(bodies)+1, xref)
	return out.Bytes()
}

// pdfString encodes a Go string for use inside a PDF literal string: characters
// are reduced to the WinAnsi byte range the base fonts can show, then the PDF
// string delimiters are escaped.
func pdfString(s string) string {
	var b strings.Builder
	for _, r := range s {
		c := winAnsiByte(r)
		switch c {
		case '\\', '(', ')':
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// winAnsiByte maps a rune to its WinAnsi byte. ASCII and the Latin-1 upper
// range pass through (WinAnsi agrees with Latin-1 across the printable 0xA0–0xFF
// band); a few common typographic runes are folded to ASCII equivalents, and
// anything else becomes '?' so the output stays renderable.
func winAnsiByte(r rune) byte {
	switch {
	case r >= 0x20 && r <= 0x7E:
		return byte(r)
	case r >= 0xA0 && r <= 0xFF:
		return byte(r)
	}
	switch r {
	case '‘', '’': // ‘ ’
		return '\''
	case '“', '”': // “ ”
		return '"'
	case '–', '—': // – —
		return '-'
	case '•': // •
		return '*'
	case '→': // →
		return '>'
	default:
		return '?'
	}
}

// wrapText greedily wraps text to lines no wider than maxW points at the given
// font size, hard-breaking any single word that is itself too long. A
// non-positive maxW yields the whole string as one line.
func wrapText(s string, size, maxW float64) []string {
	if maxW <= 0 {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
	}
	for _, word := range strings.Fields(s) {
		if cur.Len() == 0 {
			// Place the word, breaking it across lines if it alone overflows.
			for helveticaTextWidth(word, size) > maxW {
				cut := longestPrefix(word, size, maxW)
				if cut == 0 {
					cut = 1 // always make progress
				}
				lines = append(lines, word[:cut])
				word = word[cut:]
			}
			cur.WriteString(word)
			continue
		}
		if helveticaTextWidth(cur.String()+" "+word, size) <= maxW {
			cur.WriteByte(' ')
			cur.WriteString(word)
		} else {
			flush()
			cur.WriteString(word)
		}
	}
	if cur.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// longestPrefix returns the number of leading bytes of word that fit within
// maxW at the given size.
func longestPrefix(word string, size, maxW float64) int {
	for i := 1; i <= len(word); i++ {
		if helveticaTextWidth(word[:i], size) > maxW {
			return i - 1
		}
	}
	return len(word)
}

// helveticaTextWidth returns the rendered width in points of s at the given
// size, summing per-character Helvetica metrics over its WinAnsi bytes.
func helveticaTextWidth(s string, size float64) float64 {
	var units int
	for _, r := range s {
		units += helveticaWidth(winAnsiByte(r))
	}
	return float64(units) * size / 1000
}

// helveticaWidth is the advance width (per 1000 em) of one WinAnsi byte in
// Helvetica. The table covers printable ASCII (the bulk of itinerary text);
// other bytes fall back to a representative average so wrapping stays sane.
func helveticaWidth(b byte) int {
	if b >= 32 && b <= 126 {
		return helvAscii[b-32]
	}
	return 556
}

// helvAscii holds the standard Helvetica advance widths for ASCII codes
// 32 ('space') through 126 ('~'), in 1000-em units (from the Adobe AFM).
var helvAscii = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}
