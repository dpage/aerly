package store

import "testing"

func linkSummary(ls []*PlanLink) string {
	out := ""
	for _, l := range ls {
		out += l.Title + "=" + l.URL + ";"
	}
	return out
}

// TestPlanLinksRoundTrip covers create, replace, leave-alone and clear.
func TestPlanLinksRoundTrip(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)
	tid := mkTrip(t, s, uid)
	p, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: tid, Type: "event", Title: "Gig",
		Links: []PlanLinkInput{{Title: "A", URL: "https://example.com/a"}, {Title: "B", URL: "https://example.com/b"}},
	}, uid)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ls, err := s.LinksByPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("LinksByPlan: %v", err)
	}
	if got := linkSummary(ls); got != "A=https://example.com/a;B=https://example.com/b;" {
		t.Errorf("created = %q", got)
	}
	if ls[0].PlanID != p.ID || ls[0].Position != 0 || ls[1].Position != 1 {
		t.Errorf("unexpected plan/position: %+v %+v", ls[0], ls[1])
	}

	title := "Concert"
	if _, err := s.UpdatePlan(ctx, p.ID, UpdatePlanPayload{Title: &title}); err != nil {
		t.Fatalf("UpdatePlan title: %v", err)
	}
	if ls, _ = s.LinksByPlan(ctx, p.ID); len(ls) != 2 {
		t.Errorf("nil Links changed the links: %d", len(ls))
	}

	repl := []PlanLinkInput{{Title: "C", URL: "https://example.com/c"}}
	if _, err := s.UpdatePlan(ctx, p.ID, UpdatePlanPayload{Links: &repl}); err != nil {
		t.Fatalf("UpdatePlan links: %v", err)
	}
	if ls, _ = s.LinksByPlan(ctx, p.ID); linkSummary(ls) != "C=https://example.com/c;" {
		t.Errorf("replaced = %q", linkSummary(ls))
	}

	none := []PlanLinkInput{}
	if _, err := s.UpdatePlan(ctx, p.ID, UpdatePlanPayload{Links: &none}); err != nil {
		t.Fatalf("UpdatePlan clear: %v", err)
	}
	if ls, _ = s.LinksByPlan(ctx, p.ID); len(ls) != 0 {
		t.Errorf("cleared = %q", linkSummary(ls))
	}

	if _, err := s.UpdatePlan(ctx, p.ID+1000, UpdatePlanPayload{Links: &repl}); err != ErrNotFound {
		t.Errorf("UpdatePlan missing plan = %v, want ErrNotFound", err)
	}
}

// TestPlanLinksStoreErrors drives the failure branches with plan_links gone.
func TestPlanLinksStoreErrors(t *testing.T) {
	s := newStore(t)
	uid := mkUser(t, s)
	tid := mkTrip(t, s, uid)
	pid := mkPlan(t, s, tid, uid)
	if _, err := s.pool.Exec(ctx, `DROP TABLE plan_links`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.LinksByPlan(ctx, pid); err == nil {
		t.Error("LinksByPlan should fail without its table")
	}
	links := []PlanLinkInput{{Title: "A", URL: "https://example.com/a"}}
	if _, err := s.UpdatePlan(ctx, pid, UpdatePlanPayload{Links: &links}); err == nil {
		t.Error("UpdatePlan should fail without plan_links")
	}
	if _, err := s.CreatePlan(ctx, CreatePlanPayload{TripID: tid, Type: "event", Title: "x", Links: links}, uid); err == nil {
		t.Error("CreatePlan should fail without plan_links")
	}
}
