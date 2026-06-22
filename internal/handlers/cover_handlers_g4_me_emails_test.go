package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/config"
)

// TestG4MyEmailsDisabled covers the email-ingest-disabled 503 branch on every
// /api/me/emails endpoint (list, add, resend, delete) when ingest is off.
func TestG4MyEmailsDisabled(t *testing.T) {
	e := setup(t, nil, &config.Config{}) // EmailIngestEnabled = false
	uid := e.user(t, "g4disabled", false)

	if w := e.req(t, "GET", "/api/me/emails", nil, uid); w.Code != http.StatusServiceUnavailable {
		t.Errorf("list disabled = %d, want 503", w.Code)
	}
	if w := e.req(t, "POST", "/api/me/emails/1/resend", nil, uid); w.Code != http.StatusServiceUnavailable {
		t.Errorf("resend disabled = %d, want 503", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/me/emails/1", nil, uid); w.Code != http.StatusServiceUnavailable {
		t.Errorf("delete disabled = %d, want 503", w.Code)
	}
}

// TestG4ListMyEmailsStoreErr covers the store-error 500 branch of listMyEmails.
func TestG4ListMyEmailsStoreErr(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "g4liststore", false)
	g1dropTable(t, e, "user_emails")
	if w := e.req(t, "GET", "/api/me/emails", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("list store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4AddMyEmailBadBody covers the decode-error 400 branch of addMyEmail.
func TestG4AddMyEmailBadBody(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "g4addbad", false)
	if w := e.req(t, "POST", "/api/me/emails", map[string]any{"bogus": 1}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("add bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4AddMyEmailStoreErr covers the generic store-error 500 branch of
// addMyEmail (InsertUnverifiedEmail failing for a reason other than a taken
// address) by dropping the user_emails table.
func TestG4AddMyEmailStoreErr(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "g4addstore", false)
	e.api.SendVerifyEmail = func(context.Context, string, string) error { return nil }
	g1dropTable(t, e, "user_emails")
	if w := e.req(t, "POST", "/api/me/emails", map[string]string{"address": "g4@example.com"}, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("add store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ResendMyEmailBadID covers the invalid-id 400 branch of resendMyEmail.
func TestG4ResendMyEmailBadID(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "g4resendbad", false)
	if w := e.req(t, "POST", "/api/me/emails/abc/resend", nil, uid); w.Code != http.StatusBadRequest {
		t.Errorf("resend bad id = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ResendMyEmailSendFailure covers the 502 branch of resendMyEmail when
// the verification email cannot be sent.
func TestG4ResendMyEmailSendFailure(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "g4resendfail", false)
	row, _, err := e.store.InsertUnverifiedEmail(context.Background(), uid, "g4resend@example.com")
	if err != nil {
		t.Fatal(err)
	}
	e.api.SendVerifyEmail = func(context.Context, string, string) error {
		return errors.New("smtp down")
	}
	w := e.req(t, "POST", "/api/me/emails/"+itoa(row.ID)+"/resend", nil, uid)
	if w.Code != http.StatusBadGateway {
		t.Errorf("resend send failure = %d, want 502; body=%s", w.Code, w.Body.String())
	}
}

// TestG4DeleteMyEmailBadID covers the invalid-id 400 branch of deleteMyEmail.
func TestG4DeleteMyEmailBadID(t *testing.T) {
	e := setup(t, nil, &config.Config{EmailIngestEnabled: true})
	uid := e.user(t, "g4delbad", false)
	if w := e.req(t, "DELETE", "/api/me/emails/abc", nil, uid); w.Code != http.StatusBadRequest {
		t.Errorf("delete bad id = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}
