package emailingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/planops"
)

func TestClip_TruncatesToMaxLen(t *testing.T) {
	long := strings.Repeat("x", maxFieldLen+50)
	got := clip("  " + long + "  ")
	if len([]rune(got)) != maxFieldLen {
		t.Errorf("clip length = %d, want %d", len([]rune(got)), maxFieldLen)
	}
}

func TestTruncateForError_Bounds(t *testing.T) {
	short := truncateForError("hi")
	if short != "hi" {
		t.Errorf("short blob altered: %q", short)
	}
	long := truncateForError(strings.Repeat("y", maxErrorBlobBytes+100))
	if !strings.HasSuffix(long, "…(truncated)") {
		t.Errorf("long blob not truncated: ...%q", long[len(long)-20:])
	}
}

func TestExtract_InvalidCalendarDate(t *testing.T) {
	// 2026-13-40 matches the date regex but isn't a real calendar date, so the
	// leg is dropped at the time.Parse gate.
	x, _ := newExtractor(`{"flights":[{"ident":"TK1980","date":"2026-13-40","confidence":"high"}]}`)
	legs, err := x.Extract(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) != 0 {
		t.Errorf("legs = %+v, want empty (impossible date)", legs)
	}
}

func TestValidateFlightPart_Drops(t *testing.T) {
	x := NewExtractor(&fakeLLM{}, "test")
	x.Now = fixedNow
	cases := []struct {
		name        string
		ident, date string
	}{
		{"bad ident", "lowercase", "2026-06-12"},
		{"bad date format", "TK1980", "12/06/2026"},
		{"impossible date", "TK1980", "2026-13-40"},
		{"out of window past", "TK1980", "2000-01-01"},
		{"out of window future", "TK1980", "2099-01-01"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := x.validateFlightPart(c.ident, c.date, "high", "", "", "", "", ""); ok {
				t.Errorf("validateFlightPart(%q,%q) ok=true, want false", c.ident, c.date)
			}
		})
	}
}

func TestExtractPlans_BadJSON(t *testing.T) {
	x, _ := newExtractor("not json at all")
	if _, err := x.ExtractPlans(context.Background(), "body", nil); err == nil {
		t.Error("expected a JSON error")
	}
}

func TestExtractPlans_LLMError(t *testing.T) {
	x, l := newExtractor("")
	l.err = errors.New("boom")
	if _, err := x.ExtractPlans(context.Background(), "body", nil); err == nil {
		t.Error("expected the LLM error to propagate")
	}
}

func TestExtractPlans_ForwardsDocs(t *testing.T) {
	x, l := newExtractor(`{"plans":[]}`)
	docs := []planops.Document{{Data: []byte("%PDF synthetic"), MediaType: "application/pdf", Filename: "t.pdf"}}
	if _, err := x.ExtractPlans(context.Background(), "body", docs); err != nil {
		t.Fatal(err)
	}
	if len(l.lastDocs) != 1 || l.lastDocs[0].Filename != "t.pdf" {
		t.Errorf("docs not forwarded into emailingest.Document: %+v", l.lastDocs)
	}
}

func TestExtractPlans_BoundsPlansAndParts(t *testing.T) {
	// Build a response with more than maxPlansPerMessage plans, the first of
	// which has more than maxPartsPerPlan parts. Both are clamped.
	var sb strings.Builder
	sb.WriteString(`{"plans":[`)
	for i := 0; i < maxPlansPerMessage+5; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"type":"dining","title":"D","parts":[`)
		nParts := 1
		if i == 0 {
			nParts = maxPartsPerPlan + 5
		}
		for j := 0; j < nParts; j++ {
			if j > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(`{"type":"dining","confidence":"high","start_date":"2026-06-12","dining":{"reservation_name":"x"}}`)
		}
		sb.WriteString(`]}`)
	}
	sb.WriteString(`]}`)
	x, _ := newExtractor(sb.String())
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) > maxPlansPerMessage {
		t.Errorf("plans = %d, want <= %d", len(plans), maxPlansPerMessage)
	}
	if len(plans[0].Parts) > maxPartsPerPlan {
		t.Errorf("first plan parts = %d, want <= %d", len(plans[0].Parts), maxPartsPerPlan)
	}
}

func TestExtractPlans_TrainAndGround(t *testing.T) {
	resp := `{"plans":[
	  {"type":"train","title":"Eurostar","parts":[
	     {"type":"train","confidence":"high","start_date":"2026-06-12","end_time":"10:30","train":{"operator":"Eurostar","service_no":"9004","class":"Standard"}}
	  ]},
	  {"type":"ground","title":"Transfer","parts":[
	     {"type":"ground","confidence":"high","start_date":"2026-06-12","ground":{"provider":"Addison Lee","vehicle":"Saloon"}}
	  ]}
	]}`
	x, _ := newExtractor(resp)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]planops.ExtractedPart{}
	for _, p := range plans {
		byType[p.Type] = p.Parts[0]
	}
	tr := byType["train"]
	if tr.Operator != "Eurostar" || tr.ServiceNo != "9004" || tr.Class != "Standard" {
		t.Errorf("train fields wrong: %+v", tr)
	}
	if tr.EndTime != "10:30" {
		t.Errorf("train end_time = %q, want 10:30", tr.EndTime)
	}
	gr := byType["ground"]
	if gr.Provider != "Addison Lee" || gr.Vehicle != "Saloon" {
		t.Errorf("ground fields wrong: %+v", gr)
	}
}

func TestExtractPlans_PartTypeFallsBackToPlanType(t *testing.T) {
	// The part omits its own type, so it inherits the plan's type ("dining").
	resp := `{"plans":[
	  {"type":"dining","title":"Dinner","parts":[
	     {"confidence":"high","start_date":"2026-06-12","dining":{"reservation_name":"Test User"}}
	  ]}
	]}`
	x, _ := newExtractor(resp)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Parts[0].Type != "dining" {
		t.Fatalf("part type not inherited from plan: %+v", plans)
	}
	if plans[0].Parts[0].ReservationName != "Test User" {
		t.Errorf("dining reservation_name = %q", plans[0].Parts[0].ReservationName)
	}
}

func TestExtractPlans_DropsInvalidPartType(t *testing.T) {
	resp := `{"plans":[
	  {"type":"spaceship","title":"X","parts":[
	     {"type":"spaceship","confidence":"high","start_date":"2026-06-12"}
	  ]}
	]}`
	x, _ := newExtractor(resp)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Errorf("plans = %+v, want empty (unknown type dropped)", plans)
	}
}

func TestExtractPlans_DropsFlightPartFailingValidation(t *testing.T) {
	// A flight part whose ident fails validation is dropped, leaving the plan
	// with no parts so the whole plan is omitted.
	resp := `{"plans":[
	  {"type":"flight","title":"Bad","parts":[
	     {"type":"flight","confidence":"high","flight":{"ident":"nope","date":"2026-06-12"}}
	  ]}
	]}`
	x, _ := newExtractor(resp)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Errorf("plans = %+v, want empty (invalid flight dropped)", plans)
	}
}

func TestExtractPlans_PlanTypeDefaultsFromFirstPart(t *testing.T) {
	// The plan carries no top-level type; it should adopt the first part's type.
	resp := `{"plans":[
	  {"title":"Untyped","parts":[
	     {"type":"hotel","confidence":"high","start_date":"2026-06-12","hotel":{"property_name":"Plaza"}}
	  ]}
	]}`
	x, _ := newExtractor(resp)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Type != "hotel" {
		t.Fatalf("plan type not defaulted from part: %+v", plans)
	}
}

func TestRoundTripTitle_NoIdentUsesRouteOnly(t *testing.T) {
	// A round trip with no flight ident on the first leg falls back to a
	// route-only title built from the StartLabel/EndLabel.
	rt := planops.ExtractedPlan{Type: "flight", Parts: []planops.ExtractedPart{
		{Type: "flight", StartLabel: "lhr", EndLabel: "jfk"},
		{Type: "flight", StartLabel: "jfk", EndLabel: "lhr"},
	}}
	got, ok := roundTripTitle(rt)
	if !ok {
		t.Fatal("expected a round-trip title")
	}
	if got != "LHR ↔ JFK" {
		t.Errorf("title = %q, want %q", got, "LHR ↔ JFK")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("  ", "fallback"); got != "fallback" {
		t.Errorf("got %q, want fallback", got)
	}
	if got := firstNonEmpty("first", "fallback"); got != "first" {
		t.Errorf("got %q, want first", got)
	}
}
