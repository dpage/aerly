package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/config"
)

func TestAdminInfoRequiresSuperuser(t *testing.T) {
	cfg := &config.Config{
		PublicURL:       "https://aerly.example",
		PollInterval:    60 * time.Second,
		OpenSkyUsername: "flyer",
		AeroDataBoxKey:  "k",
		GitHubID:        "gh",
		LLMProvider:     "anthropic",
		LLMModel:        "claude-haiku-4-5",
		LLMAPIKey:       "secret",
		MailFromAddress: "noreply@aerly.example",
	}
	e := setup(t, &fakeResolver{}, cfg)
	normal := e.user(t, "normal", false)
	admin := e.user(t, "admin", true)

	// Non-superuser is forbidden.
	if w := e.req(t, "GET", "/api/admin/info", nil, normal); w.Code != http.StatusForbidden {
		t.Fatalf("normal user /api/admin/info = %d, want 403", w.Code)
	}
	// Anonymous is unauthorized.
	if w := e.req(t, "GET", "/api/admin/info", nil, 0); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/admin/info = %d, want 401", w.Code)
	}

	// Superuser gets the diagnostics payload.
	w := e.req(t, "GET", "/api/admin/info", nil, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("admin /api/admin/info = %d, want 200", w.Code)
	}
	body := decodeBody[map[string]any](t, w)

	ver, ok := body["version"].(map[string]any)
	if !ok {
		t.Fatalf("version block missing: %v", body)
	}
	if _, ok := ver["commit"]; !ok {
		t.Errorf("version.commit missing: %v", ver)
	}
	if _, ok := ver["go_version"]; !ok {
		t.Errorf("version.go_version missing: %v", ver)
	}

	rt, ok := body["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime block missing: %v", body)
	}
	if _, ok := rt["uptime_sec"]; !ok {
		t.Errorf("runtime.uptime_sec missing: %v", rt)
	}

	conf, ok := body["config"].(map[string]any)
	if !ok {
		t.Fatalf("config block missing: %v", body)
	}
	if conf["public_url"] != "https://aerly.example" {
		t.Errorf("config.public_url = %v, want the configured URL", conf["public_url"])
	}
	if conf["tracker"] != "opensky" {
		t.Errorf("config.tracker = %v, want opensky", conf["tracker"])
	}
	if conf["tracker_authed"] != true {
		t.Errorf("config.tracker_authed = %v, want true", conf["tracker_authed"])
	}
	if conf["llm_configured"] != true {
		t.Errorf("config.llm_configured = %v, want true", conf["llm_configured"])
	}
	if conf["auth_github"] != true {
		t.Errorf("config.auth_github = %v, want true", conf["auth_github"])
	}
	if conf["mail_configured"] != true {
		t.Errorf("config.mail_configured = %v, want true", conf["mail_configured"])
	}
	// No secret values must leak into the payload.
	for _, secret := range []string{"secret", "k"} {
		for k, v := range conf {
			if s, ok := v.(string); ok && s == secret {
				t.Errorf("config.%s leaked a secret value %q", k, s)
			}
		}
	}
}

// TestVersionEndpointOpenToAnyUser: /api/version is the lightweight build probe
// the SPA polls to prompt a refresh after a deploy. Unlike /api/admin/info it is
// NOT superuser-gated (every signed-in client needs it), but it still requires a
// session and exposes only the commit/build-time — no secrets.
func TestVersionEndpointOpenToAnyUser(t *testing.T) {
	e := setup(t, nil, &config.Config{LLMAPIKey: "secret"})
	normal := e.user(t, "normal", false)

	// Anonymous is unauthorized.
	if w := e.req(t, "GET", "/api/version", nil, 0); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/version = %d, want 401", w.Code)
	}

	// A normal (non-superuser) signed-in user gets the build identifier.
	w := e.req(t, "GET", "/api/version", nil, normal)
	if w.Code != http.StatusOK {
		t.Fatalf("normal user /api/version = %d, want 200", w.Code)
	}
	body := decodeBody[map[string]any](t, w)
	for _, key := range []string{"commit", "short", "build_time"} {
		if _, ok := body[key]; !ok {
			t.Errorf("version.%s missing: %v", key, body)
		}
	}
	// The superuser-only diagnostic fields must not appear here.
	for _, key := range []string{"go_version", "config", "runtime"} {
		if _, ok := body[key]; ok {
			t.Errorf("version payload leaked %q to a non-admin: %v", key, body)
		}
	}
}

func TestAdminInfoStubTrackerWhenUnconfigured(t *testing.T) {
	e := setup(t, nil, &config.Config{})
	admin := e.user(t, "admin", true)
	w := e.req(t, "GET", "/api/admin/info", nil, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("admin /api/admin/info = %d, want 200", w.Code)
	}
	conf := decodeBody[map[string]any](t, w)["config"].(map[string]any)
	if conf["tracker"] != "stub" {
		t.Errorf("config.tracker = %v, want stub", conf["tracker"])
	}
	if conf["resolver_available"] != false {
		t.Errorf("config.resolver_available = %v, want false", conf["resolver_available"])
	}
	if _, ok := conf["email_ingest_address"]; ok {
		t.Error("config.email_ingest_address should be omitted when ingest is disabled")
	}
}
