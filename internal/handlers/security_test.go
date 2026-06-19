package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_SetOnEveryResponse(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	})
	h := SecurityHeaders(next, false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d (wrapper must not swallow the handler)", rec.Code, http.StatusTeapot)
	}
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header missing")
	}
	// Spot-check the directives that matter most for the review finding.
	for _, frag := range []string{
		"default-src 'self'",
		"frame-ancestors 'none'",
		"object-src 'none'",
		"script-src 'self'",
		"https://tile.openstreetmap.org",
		"https://demotiles.maplibre.org",
		// Image hosts the SPA loads from; omitting flagcdn silently breaks the
		// trip-card country flags.
		"https://flagcdn.com",
	} {
		if !strings.Contains(csp, frag) {
			t.Errorf("CSP missing %q\nfull policy: %s", frag, csp)
		}
	}
	// script-src must NOT permit inline or eval — the whole point of the policy.
	if strings.Contains(csp, "'unsafe-inline'") && !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Error("unexpected 'unsafe-inline' outside style-src")
	}
	if strings.Contains(csp, "'unsafe-eval'") {
		t.Error("CSP must not allow 'unsafe-eval'")
	}
}

func TestSecurityHeaders_HSTSGatedOnHTTPS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	recPlain := httptest.NewRecorder()
	SecurityHeaders(next, false).ServeHTTP(recPlain, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := recPlain.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS set on a non-HTTPS deployment: %q", got)
	}

	recTLS := httptest.NewRecorder()
	SecurityHeaders(next, true).ServeHTTP(recTLS, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := recTLS.Header().Get("Strict-Transport-Security"); got != hstsValue {
		t.Errorf("HSTS = %q, want %q", got, hstsValue)
	}
}
