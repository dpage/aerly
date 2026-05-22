package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/dpage/flight-tracker/internal/api"
	"github.com/dpage/flight-tracker/internal/config"
)

func TestListMyEmails_Empty(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)

	w := e.req(t, "GET", "/api/me/emails", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeBody[[]api.UserEmailDTO](t, w)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestListMyEmails_ShowsOwn(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	if _, _, err := e.store.InsertUnverifiedEmail(context.Background(), uid, "alice@example.com"); err != nil {
		t.Fatal(err)
	}

	w := e.req(t, "GET", "/api/me/emails", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeBody[[]api.UserEmailDTO](t, w)
	if len(got) != 1 || got[0].Address != "alice@example.com" {
		t.Errorf("got = %+v", got)
	}
	if got[0].Verified {
		t.Error("freshly inserted row should be unverified")
	}
}

func TestListMyEmails_HidesOthers(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "alice", false)
	other := e.user(t, "bob", false)
	if _, _, err := e.store.InsertUnverifiedEmail(context.Background(), other, "bob@example.com"); err != nil {
		t.Fatal(err)
	}

	w := e.req(t, "GET", "/api/me/emails", nil, uid)
	got := decodeBody[[]api.UserEmailDTO](t, w)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestMyEmails_RequiresAuth(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	if w := e.req(t, "GET", "/api/me/emails", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anon GET = %d, want 401", w.Code)
	}
}
