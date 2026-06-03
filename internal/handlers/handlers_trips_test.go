package handlers

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/store"
)

func TestTripCRUDEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	stranger := e.user(t, "stranger", false)

	// Create requires a name.
	if w := e.req(t, "POST", "/api/trips", map[string]any{}, owner); w.Code != 400 {
		t.Errorf("create without name = %d, want 400", w.Code)
	}
	w := e.req(t, "POST", "/api/trips", map[string]any{
		"name": "Italy", "destination": "Rome", "starts_on": "2026-06-01",
	}, owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	trip := decodeBody[map[string]any](t, w)
	tid := int64(trip["id"].(float64))
	if trip["my_role"] != "owner" {
		t.Errorf("my_role = %v, want owner", trip["my_role"])
	}
	if trip["starts_on"] != "2026-06-01" {
		t.Errorf("starts_on = %v", trip["starts_on"])
	}

	// List shows it to the owner, not the stranger.
	if w := e.req(t, "GET", "/api/trips", nil, owner); len(decodeBody[[]map[string]any](t, w)) != 1 {
		t.Error("owner should list 1 trip")
	}
	if w := e.req(t, "GET", "/api/trips", nil, stranger); len(decodeBody[[]map[string]any](t, w)) != 0 {
		t.Error("stranger should list 0 trips")
	}

	// Get embeds a plans array (FE contract: Trip & { plans: Plan[] }).
	w = e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, owner)
	if w.Code != 200 {
		t.Fatalf("get = %d %s", w.Code, w.Body.String())
	}
	full := decodeBody[map[string]any](t, w)
	if _, ok := full["plans"]; !ok {
		t.Error("trip detail must embed a plans array")
	}

	// Stranger gets 404 (existence not leaked).
	if w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tid), nil, stranger); w.Code != 404 {
		t.Errorf("stranger get = %d, want 404", w.Code)
	}

	// Patch (owner).
	w = e.req(t, "PATCH", fmt.Sprintf("/api/trips/%d", tid), map[string]any{"name": "Italy 26"}, owner)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["name"] != "Italy 26" {
		t.Errorf("patch = %d %s", w.Code, w.Body.String())
	}
	// Stranger can't patch.
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/trips/%d", tid), map[string]any{"name": "x"}, stranger); w.Code != 403 {
		t.Errorf("stranger patch = %d, want 403", w.Code)
	}

	// Delete (stranger 403, owner 204).
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d", tid), nil, stranger); w.Code != 403 {
		t.Errorf("stranger delete = %d, want 403", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d", tid), nil, owner); w.Code != 204 {
		t.Errorf("owner delete = %d, want 204", w.Code)
	}
}

func TestTripMemberEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	editor := e.user(t, "editor", false)
	viewer := e.user(t, "viewer", false)

	w := e.req(t, "POST", "/api/trips", map[string]any{"name": "T"}, owner)
	tid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// Owner adds an editor; response is the trip with the new member.
	w = e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
		map[string]any{"user_id": editor, "role": "editor"}, owner)
	if w.Code != 200 {
		t.Fatalf("add editor = %d %s", w.Code, w.Body.String())
	}
	if len(decodeBody[map[string]any](t, w)["members"].([]any)) != 2 {
		t.Error("expected 2 members after add")
	}

	// Bad role rejected.
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
		map[string]any{"user_id": viewer, "role": "boss"}, owner); w.Code != 400 {
		t.Errorf("bad role = %d, want 400", w.Code)
	}

	// An editor is NOT allowed to manage members (owner-only).
	if w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/members", tid),
		map[string]any{"user_id": viewer, "role": "viewer"}, editor); w.Code != 403 {
		t.Errorf("editor add member = %d, want 403", w.Code)
	}

	// Owner removes the editor.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/trips/%d/members/%d", tid, editor), nil, owner); w.Code != 204 {
		t.Errorf("remove member = %d, want 204", w.Code)
	}
}

func TestTagEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	w := e.req(t, "POST", "/api/trips", map[string]any{"name": "Beach trip"}, owner)
	tid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// PUT tags (FE sends { labels: [...] }).
	w = e.req(t, "PUT", fmt.Sprintf("/api/trips/%d/tags", tid),
		map[string]any{"labels": []string{"Beach", "Summer"}}, owner)
	if w.Code != 200 {
		t.Fatalf("set tags = %d %s", w.Code, w.Body.String())
	}
	tags := decodeBody[map[string]any](t, w)["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2", tags)
	}

	// Suggest autocompletes over visible tags.
	w = e.req(t, "GET", "/api/tags/suggest?q=bea", nil, owner)
	if w.Code != 200 {
		t.Fatalf("suggest = %d", w.Code)
	}
	sug := decodeBody[[]map[string]any](t, w)
	if len(sug) != 1 || sug[0]["label"] != "Beach" {
		t.Errorf("suggest = %v, want [Beach]", sug)
	}
}

func TestListTripsSuperuserIncludeScopes(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "admin", true) // superuser
	stranger := e.user(t, "stranger2", false)
	ctx := context.Background()

	if _, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Admin trip"}, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Stranger trip"}, stranger); err != nil {
		t.Fatal(err)
	}

	// Superuser ?include=all sees every trip (both).
	all := decodeBody[[]map[string]any](t, e.req(t, "GET", "/api/trips?include=all", nil, admin))
	if len(all) < 2 {
		t.Errorf("superuser include=all = %d trips, want >= 2", len(all))
	}

	// Non-superuser passing include=all is ignored — only their own trip.
	mine := decodeBody[[]map[string]any](t, e.req(t, "GET", "/api/trips?include=all", nil, stranger))
	if len(mine) != 1 {
		t.Errorf("non-superuser include=all = %d trips, want 1 (ignored)", len(mine))
	}
}
