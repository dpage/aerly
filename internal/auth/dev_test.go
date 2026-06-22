package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// devReq builds a dev-endpoint request that appears to come from loopback, as
// the request-time guard requires.
func devReq(target string) *http.Request {
	r := httptest.NewRequest("GET", target, nil)
	r.RemoteAddr = "127.0.0.1:54321"
	return r
}

func TestDevSyntheticIDStable(t *testing.T) {
	a := devSyntheticID("Alice")
	b := devSyntheticID("alice") // case-insensitive
	c := devSyntheticID("bob")
	if a == 0 {
		t.Errorf("synthetic id should be non-zero, got %d", a)
	}
	if a != b {
		t.Errorf("synthetic id should be case-insensitive: %d vs %d", a, b)
	}
	if a == c {
		t.Error("different logins should yield different ids")
	}
}

func TestRegisterDevLoginRoute(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterDevLogin(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, devReq("/auth/dev-login?login=alice"))
	if w.Code != http.StatusFound {
		t.Errorf("dev-login code = %d, want 302", w.Code)
	}
}

func TestDevInfoRoute(t *testing.T) {
	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterDevLogin(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, devReq("/auth/dev-info"))
	if w.Code != http.StatusOK {
		t.Fatalf("dev-info code = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("dev-info Content-Type = %q, want application/json", got)
	}
	if body := strings.TrimSpace(w.Body.String()); body != `{"enabled":true}` {
		t.Errorf("dev-info body = %q, want %q", body, `{"enabled":true}`)
	}
}

func TestDevEndpointsRejectNonLoopback(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, target := range []string{"/auth/dev-login?login=alice", "/auth/dev-info"} {
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = "203.0.113.7:443" // a non-loopback (public) peer
		w := httptest.NewRecorder()
		if strings.HasPrefix(target, "/auth/dev-login") {
			h.devLogin(w, r)
		} else {
			h.devInfo(w, r)
		}
		if w.Code != http.StatusNotFound {
			t.Errorf("%s from non-loopback: code = %d, want 404", target, w.Code)
		}
	}
}

func TestDevLoginMissingLogin(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.devLogin(w, devReq("/auth/dev-login"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", w.Code)
	}
}

func TestDevLoginBootstrapsAndSetsSession(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.devLogin(w, devReq("/auth/dev-login?login=firstuser"))
	res := w.Result()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %d", res.StatusCode)
	}
	var sawSession bool
	for _, c := range res.Cookies() {
		if c.Name == SessionCookie && c.Value != "" {
			sawSession = true
		}
	}
	if !sawSession {
		t.Error("expected session cookie")
	}
}

// TestFromLoopbackBareAddr drives the SplitHostPort-error fallback in
// fromLoopback: when RemoteAddr has no "host:port" shape, the whole string is
// treated as the host. A bare loopback IP must still be recognised.
func TestFromLoopbackBareAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/auth/dev-info", nil)
	r.RemoteAddr = "127.0.0.1" // no port → SplitHostPort errors, host falls back
	if !fromLoopback(r) {
		t.Error("bare loopback IP should be treated as loopback")
	}
	r.RemoteAddr = "203.0.113.7" // bare, but public
	if fromLoopback(r) {
		t.Error("bare public IP must not be treated as loopback")
	}
}

// TestDevLoginCountUsersError closes the pool so CountUsers fails, exercising
// the dev-login "count users" error branch (HTTP 500).
func TestDevLoginCountUsersError(t *testing.T) {
	h, pool := newTestHandler(t)
	pool.Close() // subsequent queries fail with "closed pool"
	w := httptest.NewRecorder()
	h.devLogin(w, devReq("/auth/dev-login?login=alice"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", w.Code)
	}
}

// TestDevLoginLinkLoginError forces LinkLogin to fail by feeding a login that
// makes the synthetic profile invalid. A login of only whitespace is rejected
// earlier, so instead we close the pool only after CountUsers — not possible
// without a seam — so we drive the LinkLogin error via a deactivated user
// whose dev identity already exists.
func TestDevLoginLinkLoginError(t *testing.T) {
	h, pool := newTestHandler(t)
	// Seed a user, link the dev identity for login "ghost", then deactivate
	// it. LinkLogin matches the existing (inactive) identity and returns
	// ErrNotFound, which dev-login surfaces as 403.
	ctx := context.Background()
	id := testsupport.InsertUser(t, pool, "ghost", false, true)
	pid := strconv.FormatUint(devSyntheticID("ghost"), 10)
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_identities (user_id, provider, provider_user_id) VALUES ($1, 'dev', $2)`,
		id, pid); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_active = false WHERE id = $1`, id); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	w := httptest.NewRecorder()
	h.devLogin(w, devReq("/auth/dev-login?login=ghost"))
	if w.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403", w.Code)
	}
}

func TestDevLoginOpenSignup(t *testing.T) {
	h, pool := newTestHandler(t)
	// Seed a user so the new sign-in isn't the bootstrap-superuser path.
	testsupport.InsertUser(t, pool, "existing", false, true)
	w := httptest.NewRecorder()
	h.devLogin(w, devReq("/auth/dev-login?login=stranger"))
	// Open signups: the unknown login should be accepted and a session
	// cookie issued, just like a normal first-time OAuth sign-in.
	if w.Code != http.StatusFound {
		t.Errorf("code = %d, want 302 (redirect on success)", w.Code)
	}
	var sawSession bool
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie && c.Value != "" {
			sawSession = true
		}
	}
	if !sawSession {
		t.Error("expected session cookie")
	}
}
