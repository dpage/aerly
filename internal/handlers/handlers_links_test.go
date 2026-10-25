package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
)

// planLinks reads a plan's links as uid sees them through the trip read.
func planLinks(t *testing.T, e *testEnv, tid, pid, uid int64) []api.PlanLinkDTO {
	t.Helper()
	w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("get trip: %d %s", w.Code, w.Body.String())
	}
	body := decodeBody[struct {
		Plans []api.PlanDTO `json:"plans"`
	}](t, w)
	for _, p := range body.Plans {
		if p.ID == pid {
			if p.Links == nil {
				t.Fatal("links should always be present")
			}
			return p.Links
		}
	}
	t.Fatalf("plan %d not visible to %d", pid, uid)
	return nil
}

// hotelPart is a minimal valid part list for a hotel plan.
var hotelPart = []map[string]any{{"type": "hotel", "starts_at": "2026-06-01T15:00:00Z", "start_tz_source": "floating"}}

func linkPairs(ls []api.PlanLinkDTO) string {
	parts := make([]string, 0, len(ls))
	for _, l := range ls {
		parts = append(parts, l.Title+"="+l.URL)
	}
	return strings.Join(parts, ",")
}

// TestPlanLinksCreateAndEdit covers issue #109: links are saved with the plan
// on create, replaced wholesale by an edit that carries them, left alone by one
// that doesn't, and cleared by an empty list.
func TestPlanLinksCreateAndEdit(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "lkowner", false)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), map[string]any{
		"type": "event", "title": "Concert",
		"links": []map[string]any{
			{"title": " E-ticket ", "url": " https://tickets.example.com/abc.pdf "},
			{"title": "", "url": "http://example.org/venue"},
		},
		"parts": []map[string]any{{"type": "event", "starts_at": "2026-06-01T19:00:00Z", "start_tz_source": "floating"}},
	}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decodeBody[api.PlanDTO](t, w)
	want := "E-ticket=https://tickets.example.com/abc.pdf,example.org=http://example.org/venue"
	if got := linkPairs(created.Links); got != want {
		t.Errorf("created links = %q, want %q", got, want)
	}
	pid := created.ID

	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{
		"links": []map[string]any{{"title": "Boarding pass", "url": "https://example.com/bp.png"}},
	}, owner); w.Code != http.StatusOK {
		t.Fatalf("patch links: %d %s", w.Code, w.Body.String())
	}
	if got := linkPairs(planLinks(t, e, tid, pid, owner)); got != "Boarding pass=https://example.com/bp.png" {
		t.Errorf("after replace = %q", got)
	}

	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"title": "Gig"}, owner); w.Code != http.StatusOK {
		t.Fatalf("patch title: %d %s", w.Code, w.Body.String())
	}
	if got := linkPairs(planLinks(t, e, tid, pid, owner)); got != "Boarding pass=https://example.com/bp.png" {
		t.Errorf("an edit without links changed them: %q", got)
	}

	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"links": []any{}}, owner); w.Code != http.StatusOK {
		t.Fatalf("clear links: %d %s", w.Code, w.Body.String())
	}
	if got := planLinks(t, e, tid, pid, owner); len(got) != 0 {
		t.Errorf("after clear = %v, want none", got)
	}
}

// TestPlanLinksValidation checks the URL scheme and the size caps on both the
// create and edit paths.
func TestPlanLinksValidation(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "lkval", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)

	many := make([]map[string]any, maxPlanLinks+1)
	for i := range many {
		many[i] = map[string]any{"title": "x", "url": "https://example.com/"}
	}
	cases := map[string][]map[string]any{
		"javascript": {{"title": "x", "url": "javascript:alert(1)"}},
		"data":       {{"title": "x", "url": "data:text/html,hi"}},
		"relative":   {{"title": "x", "url": "/api/trips"}},
		"no host":    {{"title": "x", "url": "https://"}},
		"bad parse":  {{"title": "x", "url": "https://exa mple.com/%zz"}},
		"long url":   {{"title": "x", "url": "https://example.com/" + strings.Repeat("a", maxPlanLinkURLLen)}},
		"long title": {{"title": strings.Repeat("t", maxPlanLinkTitleLen+1), "url": "https://example.com/"}},
		"too many":   many,
	}
	for name, links := range cases {
		if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"links": links}, owner); w.Code != http.StatusBadRequest {
			t.Errorf("patch %s = %d, want 400", name, w.Code)
		}
		if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), map[string]any{
			"type": "hotel", "title": "Hotel", "links": links, "parts": hotelPart,
		}, owner); w.Code != http.StatusBadRequest {
			t.Errorf("create %s = %d, want 400; body=%s", name, w.Code, w.Body.String())
		}
	}
	if got := planLinks(t, e, tid, pid, owner); len(got) != 0 {
		t.Errorf("rejected links were stored: %v", got)
	}
}

// TestPlanLinksHiddenFromShares checks links follow the booking-details rule
// (#163): a viewer member the booking isn't for doesn't see them.
func TestPlanLinksHiddenFromShares(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "lkhowner", false)
	viewer := e.user(t, "lkhviewer", false)
	e.befriend(t, owner, viewer)
	tid := newTrip(t, e, owner, "Trip")
	addMember(t, e, tid, viewer, "viewer")
	pid := seedHotelPlan(t, e, tid, owner)
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{
		"links": []map[string]any{{"title": "Voucher", "url": "https://example.com/v"}},
	}, owner); w.Code != http.StatusOK {
		t.Fatalf("patch links: %d %s", w.Code, w.Body.String())
	}
	if got := planLinks(t, e, tid, pid, owner); len(got) != 1 {
		t.Errorf("owner sees %d links, want 1", len(got))
	}
	if got := planLinks(t, e, tid, pid, viewer); len(got) != 0 {
		t.Errorf("viewer sees %v, want none", got)
	}
}

// TestPlanLinksSurviveLinkingPlans checks linking plans into one booking keeps
// the absorbed plans' links, after the primary's own.
func TestPlanLinksSurviveLinkingPlans(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "lkjoin", false)
	tid := newTrip(t, e, owner, "Trip")
	out := newFlightPlan(t, e, tid, owner, "BA1", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))
	back := newFlightPlan(t, e, tid, owner, "BA2", time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC))
	for pid, links := range map[int64][]map[string]any{
		out:  {{"title": "Out", "url": "https://example.com/out"}},
		back: {{"title": "Back 1", "url": "https://example.com/b1"}, {"title": "Back 2", "url": "https://example.com/b2"}},
	} {
		if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"links": links}, owner); w.Code != http.StatusOK {
			t.Fatalf("patch links: %d %s", w.Code, w.Body.String())
		}
	}
	if w := e.req(t, "POST", "/api/plans/"+itoa(out)+"/link", map[string]any{"plan_ids": []int64{back}}, owner); w.Code >= 300 {
		t.Fatalf("link plans: %d %s", w.Code, w.Body.String())
	}
	want := "Out=https://example.com/out,Back 1=https://example.com/b1,Back 2=https://example.com/b2"
	if got := linkPairs(planLinks(t, e, tid, out, owner)); got != want {
		t.Errorf("linked plan links = %q, want %q", got, want)
	}
}

// TestPlanLinksStoreErrors drives the store failure branches: with plan_links
// gone, saving or reading links is a 500.
func TestPlanLinksStoreErrors(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "lkerr", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := seedHotelPlan(t, e, tid, owner)
	g1dropTable(t, e, "plan_links")
	links := []map[string]any{{"title": "x", "url": "https://example.com/"}}
	if w := e.req(t, "PATCH", "/api/plans/"+itoa(pid), map[string]any{"links": links}, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("patch = %d, want 500", w.Code)
	}
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), map[string]any{
		"type": "hotel", "title": "Hotel", "links": links, "parts": hotelPart,
	}, owner); w.Code != http.StatusBadRequest {
		// createPlan reports every store failure as a 400.
		t.Errorf("create = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if w := e.req(t, "GET", "/api/trips/"+itoa(tid), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("get trip = %d, want 500", w.Code)
	}
}
