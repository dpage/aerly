package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	OverpassURL     string
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

	// Web Push (optional). The VAPID key pair authenticates Aerly to the
	// browser push services; the public half is handed to clients so they can
	// subscribe, the private half signs each push and is a secret. Both empty
	// disables push end-to-end (the endpoints report disabled, the sender
	// no-ops, the UI hides the toggle). WebPushSubject is the VAPID "subject"
	// (a mailto: or https: URL identifying this deployment), defaulting to
	// PublicURL when WEBPUSH_VAPID_SUBJECT is unset.
	WebPushVAPIDPublic  string
	WebPushVAPIDPrivate string
	WebPushSubject      string

	// Email ingest (optional). All EmailIngest* fields are zero when
	// EmailIngestEnabled is false. When enabled, the rest are populated
	// from env vars with the defaults documented in README.
	EmailIngestEnabled         bool
	EmailIngestMaildir         string
	EmailIngestAddress         string
	EmailIngestPollInterval    time.Duration
	EmailIngestRequireDKIM     bool
	EmailIngestDKIMAuthServID  string
	EmailIngestRateLimitPerDay int
	EmailIngestMaxBodyBytes    int
	EmailIngestMaxAttachments  int
	EmailIngestMaxAttachBytes  int64
	EmailIngestSendmail        string
	LLMProvider                string
	LLMModel                   string
	LLMAPIKey                  string

	// Attachments (optional, issue #91). AttachmentsStore gates the feature:
	// empty/blank means off (upload endpoints 503, the UI hides the affordance).
	// Otherwise it is either an absolute filesystem path under which blobs are
	// stored (in a sharded directory structure), or an "s3://bucket[/prefix]" URL
	// naming an S3 (or S3-compatible) bucket. For S3 the AttachmentsS3* fields
	// carry the endpoint/region/credentials; they are zero for the filesystem
	// backend. AttachmentsMaxBytes caps a single upload.
	AttachmentsStore       string
	AttachmentsMaxBytes    int64
	AttachmentsS3Endpoint  string
	AttachmentsS3Region    string
	AttachmentsS3AccessKey string
	AttachmentsS3SecretKey string
	AttachmentsS3UseSSL    bool
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
		OverpassURL:     getenv("OVERPASS_URL", "https://overpass-api.de/api/interpreter"),
		PollInterval:    pollInterval,
		DevAuthBypass:   os.Getenv("DEV_AUTH_BYPASS") == "1",
		MailFromAddress: os.Getenv("MAIL_FROM_ADDRESS"),
		SendmailPath:    getenv("MAIL_SENDMAIL_PATH", "/usr/sbin/sendmail"),
	}

	cfg.WebPushVAPIDPublic = strings.TrimSpace(os.Getenv("WEBPUSH_VAPID_PUBLIC_KEY"))
	cfg.WebPushVAPIDPrivate = strings.TrimSpace(os.Getenv("WEBPUSH_VAPID_PRIVATE_KEY"))
	cfg.WebPushSubject = getenv("WEBPUSH_VAPID_SUBJECT", cfg.PublicURL)

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
	// Web Push is optional, but a half-configured key pair would let a client
	// subscribe (public key present) against a server that can't sign a push
	// (private key absent), or vice versa — fail closed at startup instead.
	if (cfg.WebPushVAPIDPublic == "") != (cfg.WebPushVAPIDPrivate == "") {
		problems = append(problems, "WEBPUSH_VAPID_PUBLIC_KEY and WEBPUSH_VAPID_PRIVATE_KEY must be set together")
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
		dkimReq, err := parseBool01("EMAIL_INGEST_REQUIRE_DKIM", true)
		if err != nil {
			return nil, err
		}
		cfg.EmailIngestRequireDKIM = dkimReq
		cfg.EmailIngestDKIMAuthServID = strings.TrimSpace(os.Getenv("EMAIL_INGEST_DKIM_AUTHSERV_ID"))
		// DKIM enforcement is meaningless unless we know which authserv-id our
		// own MTA stamps: otherwise any Authentication-Results header the sender
		// injected would be trusted. Fail closed at startup rather than ship a
		// spoofable trust check.
		if cfg.EmailIngestRequireDKIM && cfg.EmailIngestDKIMAuthServID == "" {
			return nil, fmt.Errorf("EMAIL_INGEST_REQUIRE_DKIM requires EMAIL_INGEST_DKIM_AUTHSERV_ID (the authserv-id your boundary MTA stamps on Authentication-Results)")
		}
		cfg.EmailIngestRateLimitPerDay = 50
		if v := os.Getenv("EMAIL_INGEST_RATE_LIMIT_PER_DAY"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("EMAIL_INGEST_RATE_LIMIT_PER_DAY must be a non-negative integer (0 disables the limit)")
			}
			cfg.EmailIngestRateLimitPerDay = n
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

	if err := loadAttachments(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadAttachments parses the optional attachments storage config. Blank
// ATTACHMENTS_STORE leaves the feature disabled. An absolute path selects the
// filesystem backend; an s3:// URL selects S3 and requires credentials. Either
// way ATTACHMENTS_MAX_BYTES bounds a single upload (default 25 MiB).
func loadAttachments(cfg *Config) error {
	cfg.AttachmentsStore = strings.TrimSpace(os.Getenv("ATTACHMENTS_STORE"))
	if cfg.AttachmentsStore == "" {
		return nil
	}

	cfg.AttachmentsMaxBytes = 25 << 20
	if v := os.Getenv("ATTACHMENTS_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return fmt.Errorf("ATTACHMENTS_MAX_BYTES must be a positive integer")
		}
		cfg.AttachmentsMaxBytes = n
	}

	if strings.HasPrefix(cfg.AttachmentsStore, "s3://") {
		bucket, _ := parseS3Bucket(cfg.AttachmentsStore)
		if bucket == "" {
			return fmt.Errorf("ATTACHMENTS_STORE s3:// URL must include a bucket (e.g. s3://my-bucket/prefix)")
		}
		cfg.AttachmentsS3Endpoint = strings.TrimSpace(os.Getenv("ATTACHMENTS_S3_ENDPOINT"))
		cfg.AttachmentsS3Region = getenv("ATTACHMENTS_S3_REGION", "us-east-1")
		cfg.AttachmentsS3AccessKey = os.Getenv("ATTACHMENTS_S3_ACCESS_KEY")
		cfg.AttachmentsS3SecretKey = os.Getenv("ATTACHMENTS_S3_SECRET_KEY")
		if cfg.AttachmentsS3AccessKey == "" || cfg.AttachmentsS3SecretKey == "" {
			return fmt.Errorf("ATTACHMENTS_STORE s3:// requires ATTACHMENTS_S3_ACCESS_KEY and ATTACHMENTS_S3_SECRET_KEY")
		}
		useSSL, err := parseBool01("ATTACHMENTS_S3_USE_SSL", true)
		if err != nil {
			return err
		}
		cfg.AttachmentsS3UseSSL = useSSL
		return nil
	}

	if !filepath.IsAbs(cfg.AttachmentsStore) {
		return fmt.Errorf("ATTACHMENTS_STORE must be an absolute filesystem path or an s3://bucket URL (got %q)", cfg.AttachmentsStore)
	}
	return nil
}

// parseS3Bucket extracts the bucket from an "s3://bucket[/prefix]" URL. It
// mirrors attachments.ParseS3URL but lives here so config has no dependency on
// the storage package.
func parseS3Bucket(raw string) (bucket, prefix string) {
	rest := strings.TrimPrefix(raw, "s3://")
	rest = strings.TrimPrefix(rest, "/")
	bucket, prefix, _ = strings.Cut(rest, "/")
	return bucket, strings.Trim(prefix, "/")
}

// LLMConfigured reports whether an LLM-backed extractor can be built: either an
// API key is present, or the keyless local `ollama` provider is selected. It
// drives whether the paste/upload ingest endpoints are active (and is required
// for email ingest).
func (c *Config) LLMConfigured() bool {
	return c.LLMAPIKey != "" || c.LLMProvider == "ollama"
}

// AttachmentsEnabled reports whether a plan-attachments store is configured.
// When false the upload/download endpoints report disabled and the UI hides the
// attachments affordance.
func (c *Config) AttachmentsEnabled() bool {
	return c.AttachmentsStore != ""
}

// AttachmentsIsS3 reports whether the configured store is an S3 bucket (rather
// than a local filesystem path).
func (c *Config) AttachmentsIsS3() bool {
	return strings.HasPrefix(c.AttachmentsStore, "s3://")
}

// WebPushEnabled reports whether Web Push is configured: both halves of the
// VAPID key pair are present. When false the push endpoints report disabled,
// the sender no-ops, and the UI hides the enable-push toggle.
func (c *Config) WebPushEnabled() bool {
	return c.WebPushVAPIDPublic != "" && c.WebPushVAPIDPrivate != ""
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

// HTTPS reports whether the deployment is served over HTTPS, inferred from the
// PublicURL scheme. It mirrors the condition that marks session cookies Secure,
// and gates HSTS: emitting Strict-Transport-Security over plain HTTP (e.g. local
// dev on http://localhost) is pointless and a footgun if the host is reused.
func (c *Config) HTTPS() bool {
	return strings.HasPrefix(c.PublicURL, "https://")
}

func getenv(k, dflt string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return dflt
}

// parseBool01 reads a strict 0/1 boolean env var, returning dflt when unset or
// empty. Unlike the loose `== "1"` pattern, an unrecognised value (e.g. a typo
// like "true") is a hard error rather than a silent false — important for the
// auth-gate flags, where silently disabling enforcement would be a security
// regression.
func parseBool01(k string, dflt bool) (bool, error) {
	switch strings.TrimSpace(os.Getenv(k)) {
	case "":
		return dflt, nil
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be 0 or 1", k)
	}
}
