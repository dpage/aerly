package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/planops"
)

// TestToProposedPlanDTOZoneSources: an LLM proposal's sources reach the JSON the
// confirm dialog reads, under start_tz_source / end_tz_source.
func TestToProposedPlanDTOZoneSources(t *testing.T) {
	end := time.Date(2026, 7, 3, 11, 0, 0, 0, time.UTC)
	dto := toProposedPlanDTO(planops.ProposedPlan{Type: "hotel", Parts: []planops.ProposedPart{{
		Type: "hotel", StartsAt: time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC), EndsAt: &end,
		StartTZSource: "floating", EndTZSource: "floating",
	}}})
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, `"start_tz_source":"floating"`) || !strings.Contains(s, `"end_tz_source":"floating"`) {
		t.Errorf("JSON lacks the sources: %s", s)
	}
}

// TestICalProposalsZoneSources: an uploaded .ics previewed in the dialog keeps
// the importer's sources, so a zone-less UTC instant stays unknown on confirm.
func TestICalProposalsZoneSources(t *testing.T) {
	ics := "BEGIN:VCALENDAR\nPRODID:-//Test//Synthetic//EN\nBEGIN:VEVENT\nUID:bus-1@kayak.com\n" +
		"DTSTART:20260701T140000Z\nDTEND:20260701T160000Z\nSUMMARY:Bus Test Lines 100\n" +
		`DESCRIPTION:Test Lines Bus Number 100\n\nView trip: https://www.kayak.com/trips/!TESTBUS?ref=calendar` +
		"\nEND:VEVENT\nEND:VCALENDAR\n"
	out, ok := icalProposals([]byte(ics))
	if !ok || len(out.Proposals) != 1 {
		t.Fatalf("icalProposals ok=%v proposals=%d, want one", ok, len(out.Proposals))
	}
	p := out.Proposals[0].Parts[0]
	if p.StartTZSource != "unknown" || p.EndTZSource != "unknown" {
		t.Errorf("sources = %q/%q, want unknown/unknown", p.StartTZSource, p.EndTZSource)
	}
}

// icalPart previews an .ics through icalProposals and returns the first part
// of the plan with the given title.
func icalPart(t *testing.T, ics, title string) api.PlanPartDTO {
	t.Helper()
	out, ok := icalProposals([]byte(ics))
	if !ok {
		t.Fatal("icalProposals did not recognise the calendar")
	}
	for _, p := range out.Proposals {
		if p.Title == title {
			return p.Parts[0]
		}
	}
	t.Fatalf("no proposal titled %q in %+v", title, out.Proposals)
	return api.PlanPartDTO{}
}

func checkPart(t *testing.T, what string, at time.Time, tz, src, wantAt, wantTZ, wantSrc string) {
	t.Helper()
	if g := at.UTC().Format(time.RFC3339); g != wantAt || tz != wantTZ || src != wantSrc {
		t.Errorf("%s = %s %q/%q, want %s %q/%q", what, g, tz, src, wantAt, wantTZ, wantSrc)
	}
}

// TestICalProposalsOutlookZones: Outlook's own export has no booking source,
// so it is left for the LLM; the same Windows TZID and VTIMEZONE in a
// recognised calendar reach the proposal as Europe/Berlin, confirmed, and an
// unmapped TZID its VTIMEZONE settles keeps the instant with no zone.
func TestICalProposalsOutlookZones(t *testing.T) {
	outlook := readICS(t, "outlook_windows_tz.ics")
	if _, ok := icalProposals([]byte(outlook)); ok {
		t.Fatal("an Outlook export should not be recognised as a booking source")
	}
	view := `\n\nView trip: https://www.kayak.com/trips/!TESTOUT?ref=calendar`
	ics := strings.Replace(outlook, "END:VCALENDAR",
		"BEGIN:VEVENT\nUID:din-1@kayak.com\nSUMMARY:Test Bistro\n"+
			`DTSTART;TZID="W. Europe Standard Time":20260701T190000`+"\n"+
			`DTEND;TZID="W. Europe Standard Time":20260701T210000`+"\n"+
			"DESCRIPTION:Restaurant Name: Test Bistro"+view+"\nEND:VEVENT\n"+
			"BEGIN:VEVENT\nUID:din-2@kayak.com\nSUMMARY:Test Cafe\n"+
			"DTSTART;TZID=Example Zone:20260702T090000\nDTEND;TZID=Example Zone:20260702T100000\n"+
			"DESCRIPTION:Restaurant Name: Test Cafe"+view+"\nEND:VEVENT\n"+
			"BEGIN:VTIMEZONE\nTZID:Example Zone\nBEGIN:STANDARD\nDTSTART:19700101T000000\n"+
			"TZOFFSETFROM:+0530\nTZOFFSETTO:+0530\nEND:STANDARD\nEND:VTIMEZONE\nEND:VCALENDAR", 1)

	p := icalPart(t, ics, "Test Bistro")
	checkPart(t, "Windows start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-01T17:00:00Z", "Europe/Berlin", "confirmed")
	checkPart(t, "Windows end", *p.EndsAt, p.EndTZ, p.EndTZSource, "2026-07-01T19:00:00Z", "Europe/Berlin", "confirmed")
	p = icalPart(t, ics, "Test Cafe")
	checkPart(t, "VTIMEZONE start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-07-02T03:30:00Z", "", "unknown")
}

// TestICalProposalsCalendarZone: a floating time with no location of its own
// is anchored in the calendar's X-WR-TIMEZONE as inferred, and without one
// stays floating with its digits untouched.
func TestICalProposalsCalendarZone(t *testing.T) {
	ics := func(header string) string {
		return "BEGIN:VCALENDAR\nPRODID:-//Test//Synthetic//EN\n" + header +
			"BEGIN:VEVENT\nUID:item-1@tripit.com\nSUMMARY:Test Shuttle\nDESCRIPTION:[Ground Transportation]\n" +
			"DTSTART:20261018T140000\nDTEND:20261018T150000\nEND:VEVENT\nEND:VCALENDAR\n"
	}
	p := icalPart(t, ics("X-WR-TIMEZONE:Asia/Tashkent\n"), "Test Shuttle")
	checkPart(t, "start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-10-18T09:00:00Z", "Asia/Tashkent", "inferred")
	checkPart(t, "end", *p.EndsAt, p.EndTZ, p.EndTZSource, "2026-10-18T10:00:00Z", "Asia/Tashkent", "inferred")
	p = icalPart(t, ics(""), "Test Shuttle")
	checkPart(t, "zoneless start", p.StartsAt, p.StartTZ, p.StartTZSource, "2026-10-18T14:00:00Z", "", "floating")
}
