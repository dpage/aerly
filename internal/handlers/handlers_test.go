package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

var sessKey = []byte("handlers-test-session-key-32chars!!")

type fakeResolver struct {
	rf  *providers.ResolvedFlight
	err error
	// calls is incremented on every Resolve invocation so tests can
	// assert the known-IATA fast path bypasses the resolver entirely.
	calls int
}

func (f *fakeResolver) Resolve(context.Context, string, time.Time) (*providers.ResolvedFlight, error) {
	f.calls++
	return f.rf, f.err
}

type testEnv struct {
	mux   *http.ServeMux
	api   *API
	store *store.Store
	cfg   *config.Config
	hub   *sse.Hub
	pool  *pgxpool.Pool
}

func setup(t *testing.T, resolver providers.Resolver, cfg *config.Config) *testEnv {
	t.Helper()
	pool := testsupport.NewPool(t)
	s := store.New(pool)
	a := auth.NewHandler(sessKey, "http://localhost:8080", s)
	a.AddProvider(auth.NewGitHubProvider("cid", "csec"))
	hub := sse.NewHub()
	if cfg == nil {
		cfg = &config.Config{}
	}
	// Ensure the Config's SessionKey matches the test signing key so that
	// handlers that call auth.VerifyFriendAcceptToken(a.Config.SessionKey, ...)
	// can authenticate tokens minted with sessKey.
	if len(cfg.SessionKey) == 0 {
		cfg.SessionKey = sessKey
	}
	api := New(s, a, hub, cfg, resolver)
	mux := http.NewServeMux()
	api.Register(mux)
	return &testEnv{mux: mux, api: api, store: s, cfg: cfg, hub: hub, pool: pool}
}
func (e *testEnv) req(t *testing.T, method, path string, body any, asUser int64) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rdr)
	if asUser != 0 {
		r.AddCookie(&http.Cookie{
			Name:  auth.SessionCookie,
			Value: auth.SignSession(sessKey, asUser, time.Now().Add(time.Hour)),
		})
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}
func (e *testEnv) user(t *testing.T, username string, super bool) int64 {
	t.Helper()
	u, err := e.store.InviteUser(context.Background(), store.InvitePayload{
		Username: username, Name: username, IsSuperuser: super,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}
// befriend establishes an accepted friendship between a and b, so one may be
// added as the other's trip member / plan passenger (the handlers enforce this,
// matching the FE picker).
func (e *testEnv) befriend(t *testing.T, a, b int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.store.RequestFriendship(ctx, a, b, ""); err != nil {
		t.Fatalf("request friendship: %v", err)
	}
	if _, err := e.store.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("accept friendship: %v", err)
	}
}
func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return v
}
func TestRequiresAuth(t *testing.T) {
	e := setup(t, nil, nil)
	if w := e.req(t, "GET", "/api/trips", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous /api/trips = %d, want 401", w.Code)
	}
}
func TestGetMeAndConfig(t *testing.T) {
	cfg := &config.Config{AeroDataBoxKey: "k"} // ResolverAvailable → true
	e := setup(t, &fakeResolver{}, cfg)
	uid := e.user(t, "me", false)

	w := e.req(t, "GET", "/api/me", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/me = %d", w.Code)
	}
	me := decodeBody[map[string]any](t, w)
	if me["username"] != "me" {
		t.Errorf("unexpected me: %v", me)
	}

	w = e.req(t, "GET", "/api/config", nil, uid)
	caps := decodeBody[map[string]any](t, w)
	if caps["resolver_available"] != true {
		t.Errorf("resolver_available should be true, got %v", caps)
	}
	// The DTO grew a poll_interval_sec field; just assert it's present so
	// future shape changes are caught here. The value is whatever the test
	// fixture's Config sets — zero by default, which is fine for the wire
	// format.
	if _, ok := caps["poll_interval_sec"]; !ok {
		t.Errorf("poll_interval_sec missing from /api/config response: %v", caps)
	}
	if _, ok := caps["email_ingest_enabled"]; !ok {
		t.Errorf("email_ingest_enabled missing from /api/config response: %v", caps)
	}

	// No resolver / nil config → false. Address omitted when ingest disabled.
	e2 := setup(t, nil, &config.Config{})
	w = e2.req(t, "GET", "/api/config", nil, e2.user(t, "u", false))
	body := decodeBody[map[string]any](t, w)
	if body["resolver_available"] != false {
		t.Error("resolver_available should be false")
	}
	if _, ok := body["email_ingest_address"]; ok {
		t.Error("email_ingest_address should be omitted when ingest is disabled")
	}

	// Ingest enabled → both flags are exposed.
	e3 := setup(t, nil, &config.Config{
		EmailIngestEnabled: true,
		EmailIngestAddress: "flights@example.test",
	})
	w = e3.req(t, "GET", "/api/config", nil, e3.user(t, "u2", false))
	caps3 := decodeBody[map[string]any](t, w)
	if caps3["email_ingest_enabled"] != true {
		t.Error("email_ingest_enabled should be true when EmailIngestEnabled is set")
	}
	if got := caps3["email_ingest_address"]; got != "flights@example.test" {
		t.Errorf("email_ingest_address = %v, want flights@example.test", got)
	}
}

// drainEvents reads every event currently buffered for the subscriber, then
// returns. Tests use it to assert which SSE events a mutation published.
func drainEvents(ch <-chan sse.Event) []sse.Event {
	var out []sse.Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}
func TestUserAdminEndpoints(t *testing.T) {
	e := setup(t, nil, nil)
	super := e.user(t, "boss", true)
	plain := e.user(t, "plain", false)

	// Non-superuser is forbidden from user mutations.
	if w := e.req(t, "POST", "/api/users", map[string]any{"username": "x"}, plain); w.Code != http.StatusForbidden {
		t.Errorf("non-super invite = %d, want 403", w.Code)
	}

	// listUsers (any authed user).
	w := e.req(t, "GET", "/api/users", nil, plain)
	if w.Code != 200 || len(decodeBody[[]map[string]any](t, w)) != 2 {
		t.Errorf("listUsers = %d %s", w.Code, w.Body.String())
	}

	// invite: bad body, store error (empty login), success.
	if w := e.req(t, "POST", "/api/users", "??", super); w.Code != 400 {
		t.Errorf("invite bad body = %d", w.Code)
	}
	if w := e.req(t, "POST", "/api/users", map[string]any{"username": "  "}, super); w.Code != 400 {
		t.Errorf("invite empty login = %d", w.Code)
	}
	w = e.req(t, "POST", "/api/users", map[string]any{"username": "newbie", "name": "N"}, super)
	if w.Code != http.StatusCreated {
		t.Fatalf("invite = %d %s", w.Code, w.Body.String())
	}
	newbie := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// Duplicate username should surface as 409, not the raw pg error.
	if w := e.req(t, "POST", "/api/users", map[string]any{"username": "newbie"}, super); w.Code != http.StatusConflict {
		t.Errorf("duplicate invite = %d, want 409 (body=%s)", w.Code, w.Body.String())
	}

	// update: bad id, bad body, not found, self-guards, success.
	if w := e.req(t, "PATCH", "/api/users/x", map[string]any{}, super); w.Code != 400 {
		t.Errorf("update bad id = %d", w.Code)
	}
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", newbie), "??", super); w.Code != 400 {
		t.Errorf("update bad body = %d", w.Code)
	}
	if w := e.req(t, "PATCH", "/api/users/999999", map[string]any{"name": "z"}, super); w.Code != 404 {
		t.Errorf("update missing = %d, want 404", w.Code)
	}
	no := false
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", super), map[string]any{"is_superuser": no}, super); w.Code != 400 {
		t.Errorf("self-demote should be 400, got %d", w.Code)
	}
	if w := e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", super), map[string]any{"is_active": no}, super); w.Code != 400 {
		t.Errorf("self-deactivate should be 400, got %d", w.Code)
	}
	nm := "Renamed"
	w = e.req(t, "PATCH", fmt.Sprintf("/api/users/%d", newbie), map[string]any{"name": nm}, super)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["name"] != nm {
		t.Errorf("update = %d %s", w.Code, w.Body.String())
	}

	// delete: bad id, self-guard, not found, success.
	if w := e.req(t, "DELETE", "/api/users/x", nil, super); w.Code != 400 {
		t.Errorf("delete bad id = %d", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/users/%d", super), nil, super); w.Code != 400 {
		t.Errorf("self-delete should be 400, got %d", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/users/999999", nil, super); w.Code != 404 {
		t.Errorf("delete missing = %d, want 404", w.Code)
	}
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/users/%d", newbie), nil, super); w.Code != 204 {
		t.Errorf("delete = %d, want 204", w.Code)
	}
}
func TestResolveFlight(t *testing.T) {
	// No resolver configured → 501.
	e := setup(t, nil, nil)
	uid := e.user(t, "u", false)
	if w := e.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "BA1", "date": "2026-05-19"}, uid); w.Code != http.StatusNotImplemented {
		t.Errorf("no resolver = %d, want 501", w.Code)
	}

	rf := &providers.ResolvedFlight{
		Ident: "BA286", OriginIATA: "LHR", DestIATA: "SFO",
		ScheduledOut: time.Now(), ScheduledIn: time.Now().Add(11 * time.Hour),
	}
	e2 := setup(t, &fakeResolver{rf: rf}, nil)
	u2 := e2.user(t, "u2", false)

	if w := e2.req(t, "POST", "/api/flights/resolve", "??", u2); w.Code != 400 {
		t.Errorf("resolve bad body = %d", w.Code)
	}
	if w := e2.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "", "date": ""}, u2); w.Code != 400 {
		t.Errorf("resolve missing fields = %d", w.Code)
	}
	if w := e2.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "BA286", "date": "19/05/2026"}, u2); w.Code != 400 {
		t.Errorf("resolve bad date = %d", w.Code)
	}
	w := e2.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "BA286", "date": "2026-05-19"}, u2)
	if w.Code != 200 || decodeBody[map[string]any](t, w)["ident"] != "BA286" {
		t.Errorf("resolve = %d %s", w.Code, w.Body.String())
	}

	// Resolver returns an error → 422.
	e3 := setup(t, &fakeResolver{err: errors.New("not found upstream")}, nil)
	u3 := e3.user(t, "u3", false)
	if w := e3.req(t, "POST", "/api/flights/resolve", map[string]any{"ident": "ZZ9", "date": "2026-05-19"}, u3); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("resolver error = %d, want 422", w.Code)
	}
}
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
func TestWriteHelpers(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusTeapot, map[string]int{"a": 1})
	if w.Code != http.StatusTeapot || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("writeJSON wrong: %d %s", w.Code, w.Header().Get("Content-Type"))
	}

	w = httptest.NewRecorder()
	handleStoreErr(w, store.ErrNotFound)
	if w.Code != http.StatusNotFound {
		t.Errorf("ErrNotFound → %d, want 404", w.Code)
	}
	w = httptest.NewRecorder()
	handleStoreErr(w, errors.New("boom: relation \"users\" column \"secret\""))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("generic err → %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "boom") || strings.Contains(w.Body.String(), "users") {
		t.Errorf("500 body leaked the raw store error: %s", w.Body.String())
	}
}
