package handlers

import (
	"net/http"
	"runtime"
	"time"

	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/version"
)

// AdminInfoDTO is the superuser-only "About" payload. It surfaces the running
// build (commit hash, build time) plus the runtime and configuration facts an
// operator needs to confirm what's deployed and how it's wired. It deliberately
// carries NO secrets — only whether each integration is enabled.
type AdminInfoDTO struct {
	Version version.Info   `json:"version"`
	Runtime RuntimeInfoDTO `json:"runtime"`
	Config  AdminConfigDTO `json:"config"`
}

// RuntimeInfoDTO reports live process facts.
type RuntimeInfoDTO struct {
	StartedAt  time.Time `json:"started_at"`
	UptimeSec  int64     `json:"uptime_sec"`
	Goroutines int       `json:"goroutines"`
	NumCPU     int       `json:"num_cpu"`
}

// AdminConfigDTO mirrors the effective server configuration without leaking any
// credentials: each integration is reported as on/off, plus the non-secret
// values (public URL, poll cadence, model names) that help diagnose behaviour.
type AdminConfigDTO struct {
	PublicURL          string `json:"public_url"`
	Tracker            string `json:"tracker"` // "opensky" or "stub"
	TrackerAuthed      bool   `json:"tracker_authed"`
	ResolverAvailable  bool   `json:"resolver_available"`
	PollIntervalSec    int    `json:"poll_interval_sec"`
	EmailIngestEnabled bool   `json:"email_ingest_enabled"`
	EmailIngestAddress string `json:"email_ingest_address,omitempty"`
	LLMConfigured      bool   `json:"llm_configured"`
	LLMProvider        string `json:"llm_provider"`
	LLMModel           string `json:"llm_model"`
	MailConfigured     bool   `json:"mail_configured"`
	DevAuthBypass      bool   `json:"dev_auth_bypass"`
	AuthGitHub         bool   `json:"auth_github"`
	AuthGoogle         bool   `json:"auth_google"`
}

func (a *API) getAdminInfo(w http.ResponseWriter, r *http.Request) {
	_ = r
	out := AdminInfoDTO{
		Version: version.Get(),
		Runtime: RuntimeInfoDTO{
			StartedAt:  a.StartedAt,
			UptimeSec:  int64(time.Since(a.StartedAt).Seconds()),
			Goroutines: runtime.NumGoroutine(),
			NumCPU:     runtime.NumCPU(),
		},
	}
	if c := a.Config; c != nil {
		out.Config = AdminConfigDTO{
			PublicURL:          c.PublicURL,
			Tracker:            trackerName(c),
			TrackerAuthed:      c.OpenSkyUsername != "",
			ResolverAvailable:  c.ResolverAvailable(),
			PollIntervalSec:    int(c.PollInterval.Seconds()),
			EmailIngestEnabled: c.EmailIngestEnabled,
			LLMConfigured:      c.LLMConfigured(),
			LLMProvider:        c.LLMProvider,
			LLMModel:           c.LLMModel,
			MailConfigured:     c.MailFromAddress != "",
			DevAuthBypass:      c.DevAuthBypass,
			AuthGitHub:         c.GitHubID != "",
			AuthGoogle:         c.GoogleID != "",
		}
		if c.EmailIngestEnabled {
			out.Config.EmailIngestAddress = c.EmailIngestAddress
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func trackerName(c *config.Config) string {
	if c.UseOpenSky() {
		return "opensky"
	}
	return "stub"
}

// VersionDTO is the build identifier any authenticated client can poll to detect
// that a newer build has been deployed (the SPA is embedded in the binary, so a
// commit change means a new frontend is being served and a browser running the
// old bundle should refresh). It carries only the commit + build time — no
// secrets — so unlike AdminInfoDTO it isn't gated to superusers.
type VersionDTO struct {
	Commit    string `json:"commit"`
	Short     string `json:"short"`
	BuildTime string `json:"build_time"`
}

func (a *API) getVersion(w http.ResponseWriter, r *http.Request) {
	_ = r
	v := version.Get()
	writeJSON(w, http.StatusOK, VersionDTO{Commit: v.Commit, Short: v.Short, BuildTime: v.BuildTime})
}

