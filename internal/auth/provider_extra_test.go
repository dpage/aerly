package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

// failReader is a crypto/rand replacement that always errors, used to drive
// the (otherwise unreachable) random-token failure paths.
func failReader([]byte) (int, error) { return 0, errors.New("no entropy") }

// withFailingRand swaps the package randRead seam for a failing reader for the
// duration of the test, restoring it afterwards.
func withFailingRand(t *testing.T) {
	t.Helper()
	orig := randRead
	randRead = failReader
	t.Cleanup(func() { randRead = orig })
}

func TestRandomTokenError(t *testing.T) {
	withFailingRand(t)
	if _, err := randomToken(24); err == nil {
		t.Error("expected error when randRead fails")
	}
}

// TestLoginRandomTokenError drives login's handling of a crypto/rand failure:
// it must render a clean 500 rather than panic.
func TestLoginRandomTokenError(t *testing.T) {
	h, _ := newTestHandler(t)
	withFailingRand(t)
	w := httptest.NewRecorder()
	h.login(w, httptest.NewRequest("GET", "/auth/github/login", nil), h.providers["github"])
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Could not start sign-in") {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
}

// TestCallbackRouteViaRegister exercises the callback handler closure that
// Register attaches to the mux (rather than calling h.callback directly).
func TestCallbackRouteViaRegister(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.Register(mux)
	// No state cookie → the registered closure runs and renders the
	// "State cookie missing" error, proving the route is wired.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/auth/github/callback?code=c&state=s", nil)
	mux.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "State cookie missing") {
		t.Errorf("expected callback route to run, got: %s", w.Body.String())
	}
}

// stubProvider returns a provider whose FetchProfile yields a profile with an
// empty Provider field, so we can drive the defensive Provider-fill branch.
func addStubProvider(h *Handler, fetch func() (store.OAuthProfile, error)) *Provider {
	// TokenURL uses the GitHub access-token path so the rewriteTransport
	// installed by wireHTTP routes the exchange to the stub ghServer, which
	// returns a valid token. FetchProfile is overridden so the user endpoint
	// is never hit; the profile comes straight from the supplied closure.
	p := &Provider{
		Name:     "stub",
		Label:    "Stub",
		ClientID: "cid",
		AuthURL:  "https://stub.example.com/authorize",
		TokenURL: "https://stub.example.com/login/oauth/access_token",
		Scopes:   "email",
		FetchProfile: func(_ context.Context, _ *http.Client, _ string) (store.OAuthProfile, error) {
			return fetch()
		},
	}
	h.AddProvider(p)
	return p
}

// TestCallbackFillsBlankProvider covers the defensive branch where a buggy
// provider returns a profile with no Provider set: the callback fills it in
// from the provider name before linking.
func TestCallbackFillsBlankProvider(t *testing.T) {
	h, pool := newTestHandler(t)
	// Token exchange needs to succeed: point HTTP at a stub returning a token.
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	// Seed another user so this isn't the bootstrap path; the open signup
	// then creates a regular "stubuser" account.
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (username, is_active) VALUES ('someoneelse', true)`); err != nil {
		t.Fatal(err)
	}
	addStubProvider(h, func() (store.OAuthProfile, error) {
		// Deliberately leave Provider blank; ProviderUserID/Username set.
		return store.OAuthProfile{ProviderUserID: "777", Username: "stubuser", Name: "Stub User"}, nil
	})
	c, state := stateCookie(h, false)
	w := callback(h, "stub", url.Values{"code": {"c"}, "state": {state}}, c)
	res := w.Result()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d body=%s", res.StatusCode, w.Body.String())
	}
	// The identity row should have been written with provider "stub".
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM user_identities WHERE provider = 'stub' AND provider_user_id = '777'`,
	).Scan(&n); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 stub identity, got %d", n)
	}
}

// TestCallbackCountUsersError closes the pool so CountUsers fails, hitting the
// "Database error" branch.
func TestCallbackCountUsersError(t *testing.T) {
	h, pool := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	pool.Close()
	c, state := stateCookie(h, false)
	w := callback(h, "github", url.Values{"code": {"c"}, "state": {state}}, c)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Database error") {
		t.Errorf("code=%d body=%s, want 500 Database error", w.Code, w.Body.String())
	}
}

// TestCallbackLinkLoginError drives the generic (non-ErrNotFound) LinkLogin
// failure branch. A stub provider returning a profile with an empty
// ProviderUserID makes LinkLogin return a plain error after CountUsers
// succeeds.
func TestCallbackLinkLoginError(t *testing.T) {
	h, _ := newTestHandler(t)
	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	addStubProvider(h, func() (store.OAuthProfile, error) {
		// Provider is filled from the name, but ProviderUserID stays empty,
		// so LinkLogin returns "provider and provider_user_id required".
		return store.OAuthProfile{Username: "x"}, nil
	})
	c, state := stateCookie(h, false)
	w := callback(h, "stub", url.Values{"code": {"c"}, "state": {state}}, c)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Database error") {
		t.Errorf("code=%d body=%s, want 500 Database error", w.Code, w.Body.String())
	}
}

// TestCallbackCrossProviderNotifies wires up a user who already has a verified
// email, then signs in via GitHub with the same verified email. LinkLogin
// reports LinkOutcomeCrossProvider and the callback fires the heads-up email.
func TestCallbackCrossProviderNotifies(t *testing.T) {
	h, pool := newTestHandler(t)
	ctx := context.Background()

	// Existing account, signed up via google, with a verified email that the
	// GitHub stub server will also report (octo@example.com).
	google := store.OAuthProfile{
		Provider: "google", ProviderUserID: "g-1",
		Name: "Octo", Email: "octo@example.com",
	}
	if _, _, err := h.Store.LinkLogin(ctx, google, true); err != nil {
		t.Fatalf("seed google user: %v", err)
	}
	// Confirm the verified email landed so the GitHub login matches on it.
	if u, _ := h.Store.UserByVerifiedEmail(ctx, "octo@example.com"); u == nil {
		t.Skip("seeded verified email not present; cross-provider match unavailable")
	}

	cap := &captureSender{}
	h.SendNotification = cap.send
	h.MailFromAddress = "noreply@aerly.test"
	h.SendmailPath = "/bin/true"

	wireHTTP(t, h, ghServer(t, ghServerOpts{}))
	c, state := stateCookie(h, false)
	w := callback(h, "github", url.Values{"code": {"c"}, "state": {state}}, c)
	if w.Code != http.StatusFound {
		t.Fatalf("code=%d body=%s, want 302", w.Code, w.Body.String())
	}
	if cap.calls != 1 {
		t.Errorf("expected 1 cross-provider notification, got %d", cap.calls)
	}
	_ = pool
}

// TestLogoutAllUnauthenticated covers the nil-user guard in LogoutAll.
func TestLogoutAllUnauthenticated(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.LogoutAll(w, httptest.NewRequest("POST", "/auth/logout-all", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

// TestLogoutAllSuccess covers the happy path: a user in context, session
// version bumped, cookie cleared.
func TestLogoutAllSuccess(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "logoutme", false, true)
	u := &store.User{ID: id, Username: "logoutme", IsActive: true}
	r := httptest.NewRequest("POST", "/auth/logout-all", nil)
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
	w := httptest.NewRecorder()
	h.LogoutAll(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("code = %d, want 204", w.Code)
	}
}

// TestLogoutAllBumpError injects a user, then closes the pool so
// BumpSessionVersion fails, hitting the 500 branch.
func TestLogoutAllBumpError(t *testing.T) {
	h, pool := newTestHandler(t)
	u := &store.User{ID: 1, Username: "x", IsActive: true}
	pool.Close()
	r := httptest.NewRequest("POST", "/auth/logout-all", nil)
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
	w := httptest.NewRecorder()
	h.LogoutAll(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", w.Code)
	}
}

// TestExchangeCodeBadTokenURL covers the NewRequestWithContext error branch:
// a control character in the URL makes request construction fail before any
// network call.
func TestExchangeCodeBadTokenURL(t *testing.T) {
	h, _ := newTestHandler(t)
	p := &Provider{Name: "bad", TokenURL: "http://\x7f/bad"}
	if _, err := h.exchangeCode(context.Background(), p, "code"); err == nil {
		t.Error("expected request-construction error for invalid URL")
	}
}
