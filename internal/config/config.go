package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr      string
	PublicURL       string
	DatabaseURL     string
	GitHubID        string
	GitHubSecret    string
	GoogleID        string
	GoogleSecret    string
	SessionKey      []byte
	OpenSkyUsername string
	OpenSkyPassword string
	OpenSkyEnabled  bool // true if we should query OpenSky even without creds
	AeroDataBoxKey  string
	PollInterval    time.Duration
	DevAuthBypass   bool

	// Outbound mail (always optional). Used for side-channel notifications
	// like "a new sign-in method was linked to your account" plus
	// friend-invite emails and other notification flows. When
	// MailFromAddress is empty those flows are skipped (and a warning
	// logged) — the in-app side of each feature keeps working.
	//
	// MailFromAddress doubles as the SMTP envelope sender, so its domain
	// should match the address used in the From: header so DMARC/SPF can
	// align. SendmailPath defaults to the distro-standard
	// /usr/sbin/sendmail when MAIL_SENDMAIL_PATH is empty.
	MailFromAddress string
	SendmailPath    string

	// Email ingest (optional). All EmailIngest* fields are zero when
	// EmailIngestEnabled is false. When enabled, the rest are populated
	// from env vars with the defaults documented in README.
	EmailIngestEnabled        bool
	EmailIngestMaildir        string
	EmailIngestAddress        string
	EmailIngestPollInterval   time.Duration
	EmailIngestRequireDKIM    bool
	EmailIngestDKIMAuthServID string
	EmailIngestMaxBodyBytes   int
	EmailIngestMaxAttachments int
	EmailIngestMaxAttachBytes int64
	EmailIngestSendmail       string
	LLMProvider               string
	LLMModel                  string
	LLMAPIKey                 string
}

func Load() (*Config, error) {
	pollInterval, pollErr := time.ParseDuration(getenv("POLL_INTERVAL", "60s"))
	if pollErr != nil {
		return nil, fmt.Errorf("POLL_INTERVAL must be a positive duration (e.g. 60s, 5m): %w", pollErr)
	}
	if pollInterval <= 0 {
		return nil, fmt.Errorf("POLL_INTERVAL must be a positive duration (e.g. 60s, 5m)")
	}

	cfg := &Config{
		ListenAddr:      getenv("LISTEN_ADDR", ":8080"),
		PublicURL:       strings.TrimRight(getenv("PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		GitHubID:        os.Getenv("GITHUB_CLIENT_ID"),
		GitHubSecret:    os.Getenv("GITHUB_CLIENT_SECRET"),
		GoogleID:        os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleSecret:    os.Getenv("GOOGLE_CLIENT_SECRET"),
		OpenSkyUsername: os.Getenv("OPENSKY_USERNAME"),
		OpenSkyPassword: os.Getenv("OPENSKY_PASSWORD"),
		OpenSkyEnabled:  os.Getenv("OPENSKY_ENABLED") == "1",
		AeroDataBoxKey:  os.Getenv("AERODATABOX_RAPIDAPI_KEY"),
		PollInterval:    pollInterval,
		DevAuthBypass:   os.Getenv("DEV_AUTH_BYPASS") == "1",
		MailFromAddress: os.Getenv("MAIL_FROM_ADDRESS"),
		SendmailPath:    getenv("MAIL_SENDMAIL_PATH", "/usr/sbin/sendmail"),
	}

	sessKey := os.Getenv("SESSION_KEY")
	if len(sessKey) < 32 {
		return nil, fmt.Errorf("SESSION_KEY must be set to at least 32 chars (got %d)", len(sessKey))
	}
	cfg.SessionKey = []byte(sessKey)

	// Collect every configuration problem we can detect so the operator
	// sees them all in one go rather than fixing them one restart at a time.
	var problems []string
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL must be set")
	}
	// OAuth: each provider is optional, but at least one must be fully
	// configured (or DEV_AUTH_BYPASS must be on). A half-configured
	// provider — ID without secret or vice versa — is an error since the
	// flow would 500 on first sign-in.
	if (cfg.GitHubID == "") != (cfg.GitHubSecret == "") {
		problems = append(problems, "GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET must be set together")
	}
	if (cfg.GoogleID == "") != (cfg.GoogleSecret == "") {
		problems = append(problems, "GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set together")
	}
	if !cfg.DevAuthBypass && cfg.GitHubID == "" && cfg.GoogleID == "" {
		problems = append(problems, "at least one OAuth provider must be configured "+
			"(set GITHUB_CLIENT_ID+SECRET and/or GOOGLE_CLIENT_ID+SECRET)")
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	if cfg.DevAuthBypass && !strings.HasPrefix(cfg.PublicURL, "http://localhost") &&
		!strings.HasPrefix(cfg.PublicURL, "http://127.0.0.1") {
		return nil, fmt.Errorf("DEV_AUTH_BYPASS may only be used with a localhost PUBLIC_URL (got %q)", cfg.PublicURL)
	}

	// LLM config is independent of email ingest. A configured LLM enables the
	// paste/upload ingest extractor (the HTTP propose/confirm endpoints) on its
	// own; email ingest additionally requires it (checked below).
	cfg.LLMProvider = getenv("LLM_PROVIDER", "anthropic")
	cfg.LLMModel = getenv("LLM_MODEL", "claude-haiku-4-5")
	cfg.LLMAPIKey = os.Getenv("LLM_API_KEY")

	cfg.EmailIngestEnabled = os.Getenv("EMAIL_INGEST_ENABLED") == "1"
	if cfg.EmailIngestEnabled {
		cfg.EmailIngestMaildir = os.Getenv("EMAIL_INGEST_MAILDIR")
		cfg.EmailIngestAddress = os.Getenv("EMAIL_INGEST_ADDRESS")
		if cfg.EmailIngestMaildir == "" || cfg.EmailIngestAddress == "" {
			return nil, fmt.Errorf("EMAIL_INGEST_ENABLED=1 requires EMAIL_INGEST_MAILDIR and EMAIL_INGEST_ADDRESS")
		}
		pi, err := time.ParseDuration(getenv("EMAIL_INGEST_POLL_INTERVAL", "30s"))
		if err != nil || pi <= 0 {
			return nil, fmt.Errorf("EMAIL_INGEST_POLL_INTERVAL must be a positive duration")
		}
		cfg.EmailIngestPollInterval = pi
		cfg.EmailIngestRequireDKIM = getenv("EMAIL_INGEST_REQUIRE_DKIM", "1") == "1"
		cfg.EmailIngestDKIMAuthServID = strings.TrimSpace(os.Getenv("EMAIL_INGEST_DKIM_AUTHSERV_ID"))
		// DKIM enforcement is meaningless unless we know which authserv-id our
		// own MTA stamps: otherwise any Authentication-Results header the sender
		// injected would be trusted. Fail closed at startup rather than ship a
		// spoofable trust check.
		if cfg.EmailIngestRequireDKIM && cfg.EmailIngestDKIMAuthServID == "" {
			return nil, fmt.Errorf("EMAIL_INGEST_REQUIRE_DKIM=1 requires EMAIL_INGEST_DKIM_AUTHSERV_ID (the authserv-id your boundary MTA stamps on Authentication-Results)")
		}
		cfg.EmailIngestMaxBodyBytes = 1 << 20
		if v := os.Getenv("EMAIL_INGEST_MAX_BODY_BYTES"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("EMAIL_INGEST_MAX_BODY_BYTES must be a positive integer")
			}
			cfg.EmailIngestMaxBodyBytes = n
		}
		cfg.EmailIngestMaxAttachments = 5
		if v := os.Getenv("EMAIL_INGEST_MAX_ATTACHMENTS"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("EMAIL_INGEST_MAX_ATTACHMENTS must be a positive integer")
			}
			cfg.EmailIngestMaxAttachments = n
		}
		cfg.EmailIngestMaxAttachBytes = 10 << 20
		if v := os.Getenv("EMAIL_INGEST_MAX_ATTACH_BYTES"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("EMAIL_INGEST_MAX_ATTACH_BYTES must be a positive integer")
			}
			cfg.EmailIngestMaxAttachBytes = n
		}
		cfg.EmailIngestSendmail = getenv("EMAIL_INGEST_SENDMAIL", cfg.SendmailPath)
		if !cfg.LLMConfigured() {
			return nil, fmt.Errorf("EMAIL_INGEST_ENABLED=1 requires an LLM (set LLM_API_KEY, or LLM_PROVIDER=ollama)")
		}
	}
	return cfg, nil
}

// LLMConfigured reports whether an LLM-backed extractor can be built: either an
// API key is present, or the keyless local `ollama` provider is selected. It
// drives whether the paste/upload ingest endpoints are active (and is required
// for email ingest).
func (c *Config) LLMConfigured() bool {
	return c.LLMAPIKey != "" || c.LLMProvider == "ollama"
}

// UseOpenSky reports whether the OpenSky tracker should be used. We turn it
// on whenever OpenSky credentials are configured, or whenever the operator
// explicitly opts into anonymous OpenSky (heavily rate-limited).
func (c *Config) UseOpenSky() bool {
	return c.OpenSkyUsername != "" || c.OpenSkyEnabled
}

// ResolverAvailable reports whether a Resolver is wired — i.e. whether the
// frontend can offer the minimal "ident + date" Add Flight dialog.
func (c *Config) ResolverAvailable() bool {
	return c.AeroDataBoxKey != ""
}

func getenv(k, dflt string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return dflt
}
