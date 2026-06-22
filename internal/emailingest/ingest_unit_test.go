package emailingest

import (
	"errors"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
)

func TestPlanTypeLabel_AllTypes(t *testing.T) {
	cases := map[string]string{
		"flight":    "Flight",
		"hotel":     "Hotel",
		"train":     "Train",
		"ground":    "Ground transport",
		"dining":    "Dining",
		"excursion": "Excursion",
		"meeting":   "Meeting",
		"event":     "Event",
		"unknown":   "Booking",
		"":          "Booking",
	}
	for in, want := range cases {
		if got := planTypeLabel(in); got != want {
			t.Errorf("planTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlanLabel_TitleThenTypeFallback(t *testing.T) {
	withTitle := planops.ProposedPlan{Type: "hotel", Title: "  The Plaza  "}
	if got := planLabel(withTitle); got != "The Plaza" {
		t.Errorf("planLabel = %q, want The Plaza", got)
	}
	noTitle := planops.ProposedPlan{Type: "hotel"}
	if got := planLabel(noTitle); got != "Hotel" {
		t.Errorf("planLabel fallback = %q, want Hotel", got)
	}
}

func TestPlanDetail_WithAndWithoutDate(t *testing.T) {
	start := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	withDate := planops.ProposedPlan{
		Type:  "hotel",
		Parts: []planops.ProposedPart{{Type: "hotel", StartsAt: start}},
	}
	got := planDetail(withDate)
	if got != "Hotel · 12 Jun 2026" {
		t.Errorf("planDetail = %q, want %q", got, "Hotel · 12 Jun 2026")
	}
	noDate := planops.ProposedPlan{Type: "dining"}
	if got := planDetail(noDate); got != "Dining" {
		t.Errorf("planDetail (no date) = %q, want Dining", got)
	}
}

func TestPlanReplyFailure_Flight(t *testing.T) {
	p := planops.ProposedPlan{
		Type: "flight",
		Parts: []planops.ProposedPart{{Type: "flight", Flight: &store.FlightDetail{
			Ident:        "BA171",
			ScheduledOut: time.Date(2026, 7, 20, 15, 40, 0, 0, time.UTC),
		}}},
	}
	f := planReplyFailure(p, errors.New("upstream down"))
	if f.Label != "BA171" {
		t.Errorf("label = %q, want BA171", f.Label)
	}
	if f.Detail != "2026-07-20" {
		t.Errorf("detail = %q, want 2026-07-20", f.Detail)
	}
	if f.Reason == "" {
		t.Error("reason should be populated")
	}
}

func TestPlanReplyFailure_NonFlight(t *testing.T) {
	start := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	p := planops.ProposedPlan{
		Type:  "hotel",
		Title: "The Plaza",
		Parts: []planops.ProposedPart{{Type: "hotel", StartsAt: start}},
	}
	f := planReplyFailure(p, errors.New("commit failed"))
	if f.Label != "The Plaza" {
		t.Errorf("label = %q, want The Plaza", f.Label)
	}
	if f.Detail != "Hotel · 12 Jun 2026" {
		t.Errorf("detail = %q", f.Detail)
	}
}

func TestPlanReplyItem_NonFlight(t *testing.T) {
	start := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	p := planops.ProposedPlan{
		Type:  "hotel",
		Title: "The Plaza",
		Parts: []planops.ProposedPart{{Type: "hotel", StartsAt: start}},
	}
	item := planReplyItem(p)
	if item.Label != "The Plaza" || item.Detail != "Hotel · 12 Jun 2026" {
		t.Errorf("item = %+v", item)
	}
	if item.ManualNote {
		t.Error("non-flight item should not carry a manual note")
	}
}
