package auth

import (
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/dpage/aerly/internal/store"
)

// RegisterDevLogin attaches GET /auth/dev-login?login=foo, which fabricates a
// synthetic identity and creates a session — bypassing OAuth entirely. It is
// the caller's responsibility to gate this on DEV_AUTH_BYPASS + localhost.
//
// Also attaches GET /auth/dev-info — an unauthenticated probe the SPA's login
// page uses to decide whether to render the dev-login form. When dev bypass is
// off the route isn't registered and the probe 404s.
//
// Synthetic identity rows use the "dev" provider, so they can never collide
// with real GitHub or Google identities.
func (h *Handler) RegisterDevLogin(mux *http.ServeMux) {
	slog.Warn("DEV_AUTH_BYPASS enabled — /auth/dev-login active. Do not use in production.")
	mux.HandleFunc("GET /auth/dev-login", h.devLogin)
	mux.HandleFunc("GET /auth/dev-info", h.devInfo)
}

// fromLoopback reports whether the request originated from the loopback
// interface, judged solely from the raw TCP peer (RemoteAddr) — never from
// forwarded headers, which a client can spoof. This is the request-time guard
// that keeps the dev-bypass endpoints from being reachable through a proxy even
// if DEV_AUTH_BYPASS is ever (mis)enabled in a non-local deployment.
func fromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *Handler) devInfo(w http.ResponseWriter, r *http.Request) {
	if !fromLoopback(r) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"enabled": true})
}

func (h *Handler) devLogin(w http.ResponseWriter, r *http.Request) {
	if !fromLoopback(r) {
		http.NotFound(w, r)
		return
	}
	login := strings.TrimSpace(r.URL.Query().Get("login"))
	if login == "" {
		http.Error(w, "missing ?login=<username>", http.StatusBadRequest)
		return
	}

	profile := store.OAuthProfile{
		Provider:       "dev",
		ProviderUserID: strconv.FormatUint(devSyntheticID(login), 10),
		Username:       login,
		Name:           login,
		AvatarURL:      "https://github.com/" + login + ".png",
	}
	count, err := h.Store.CountUsers(r.Context())
	if err != nil {
		slog.Error("dev-login: count users", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	user, _, err := h.Store.LinkLogin(r.Context(), profile, count == 0)
	if err != nil {
		slog.Error("dev-login: link login", "err", err)
		http.Error(w, "could not sign in", http.StatusForbidden)
		return
	}
	SetSessionCookie(w, h.SessionKey, user.ID, user.SessionVersion, h.Secure)
	http.Redirect(w, r, "/", http.StatusFound)
}

// devSyntheticID hashes the login into a stable identifier so the same dev
// login always maps to the same user_identities row across server restarts.
func devSyntheticID(login string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(login)))
	return h.Sum64()
}
