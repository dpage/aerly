package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
)

func newTestHandler(t *testing.T) (*Handler, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)
	h := NewHandler(key, "http://localhost:8080", s)
	h.AddProvider(NewGitHubProvider("cid", "csecret"))
	return h, pool
}

func sessionReq(t *testing.T, uid int64) *http.Request {
	t.Helper()
	return sessionReqV(t, uid, 0)
}

// sessionReqV mints a request whose session cookie carries a specific session
// version (epoch). Freshly-inserted users default to version 0.
func sessionReqV(t *testing.T, uid int64, version int) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/x", nil)
	r.AddCookie(&http.Cookie{
		Name:  SessionCookie,
		Value: SignSession(key, uid, version, time.Now().Add(time.Hour)),
	})
	return r
}

func TestUserFromEmpty(t *testing.T) {
	if u := UserFrom(context.Background()); u != nil {
		t.Errorf("expected nil user, got %+v", u)
	}
}

func TestRequireNoCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next should not be called")
	})).ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestRequireBadCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	r := httptest.NewRequest("GET", "/x", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "garbage"})
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestRequireInactiveUserRejected(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "inactive", false, false)
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("inactive user must not pass")
	})).ServeHTTP(w, sessionReq(t, id))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestRequireValidUserPasses(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "alice", false, true)
	var gotID int64
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID = UserFrom(r.Context()).ID
	})).ServeHTTP(w, sessionReq(t, id))
	if w.Code != http.StatusOK || gotID != id {
		t.Errorf("code=%d gotID=%d want 200/%d", w.Code, gotID, id)
	}
}

func TestRequireRejectsStaleSessionVersion(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "rotate", false, true)

	// Bump the user's session epoch — a cookie minted at the old version 0 must
	// now be rejected (stateless "sign out everywhere").
	if err := h.Store.BumpSessionVersion(context.Background(), id); err != nil {
		t.Fatalf("bump: %v", err)
	}
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("stale-version cookie must not pass")
	})).ServeHTTP(w, sessionReqV(t, id, 0))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("stale version: code=%d, want 401", w.Code)
	}

	// A cookie at the new version passes.
	w = httptest.NewRecorder()
	called := false
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, sessionReqV(t, id, 1))
	if w.Code != http.StatusOK || !called {
		t.Errorf("fresh version: code=%d called=%v", w.Code, called)
	}
}

func TestLogoutAllBumpsEpochAndClearsCookie(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "byebye", false, true)

	// The endpoint runs behind Require, so present a valid (v0) session.
	w := httptest.NewRecorder()
	h.Require(http.HandlerFunc(h.LogoutAll)).ServeHTTP(w, sessionReqV(t, id, 0))
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout-all code = %d, want 204", w.Code)
	}
	// The response clears the caller's cookie.
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie && c.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("logout-all should clear the session cookie")
	}
	// The epoch was bumped, so the v0 session no longer authenticates.
	w = httptest.NewRecorder()
	h.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("session must be revoked after logout-all")
	})).ServeHTTP(w, sessionReqV(t, id, 0))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("revoked session: code=%d, want 401", w.Code)
	}
}

func TestRequireSuperuser(t *testing.T) {
	h, pool := newTestHandler(t)
	plain := testsupport.InsertUser(t, pool, "plain", false, true)
	super := testsupport.InsertUser(t, pool, "boss", true, true)

	w := httptest.NewRecorder()
	h.RequireSuperuser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("non-superuser must be blocked")
	})).ServeHTTP(w, sessionReq(t, plain))
	if w.Code != http.StatusForbidden {
		t.Errorf("plain user: code=%d want 403", w.Code)
	}

	w = httptest.NewRecorder()
	called := false
	h.RequireSuperuser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, sessionReq(t, super))
	if w.Code != http.StatusOK || !called {
		t.Errorf("superuser: code=%d called=%v", w.Code, called)
	}
}

func TestOptional(t *testing.T) {
	h, pool := newTestHandler(t)
	id := testsupport.InsertUser(t, pool, "opt", false, true)

	// Anonymous: passes through, no user.
	w := httptest.NewRecorder()
	sawUser := true
	h.Optional(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sawUser = UserFrom(r.Context()) != nil
	})).ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusOK || sawUser {
		t.Errorf("anon optional: code=%d sawUser=%v", w.Code, sawUser)
	}

	// Authenticated: user attached.
	w = httptest.NewRecorder()
	h.Optional(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if UserFrom(r.Context()) == nil {
			t.Error("expected user in context")
		}
	})).ServeHTTP(w, sessionReq(t, id))
}
