package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestRunWiringThenLLMError drives run() through the optional-feature wiring
// branches (GitHub + Google auth providers, AeroDataBox resolver, filesystem
// attachments) and then forces an early return via an unknown LLM provider.
// LLM_API_KEY makes LLMConfigured() true so the extractor branch is entered,
// and the unknown provider makes emailingest.NewRealLLM fail, returning before
// the blocking HTTP server starts. This covers the run() branches that sit
// after migration but before the server loop without needing a SIGTERM.
func TestRunWiringThenLLMError(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	devEnv(t, dbURL, "127.0.0.1:0")
	t.Setenv("GITHUB_CLIENT_ID", "gh-test-id")
	t.Setenv("GITHUB_CLIENT_SECRET", "gh-test-secret")
	t.Setenv("GOOGLE_CLIENT_ID", "google-test-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "google-test-secret")
	t.Setenv("AERODATABOX_RAPIDAPI_KEY", "adb-test-key")
	t.Setenv("ATTACHMENTS_STORE", filepath.Join(t.TempDir(), "blobs"))
	// Enter the extractor branch, then fail building the LLM client.
	t.Setenv("LLM_PROVIDER", "not-a-real-provider")
	t.Setenv("LLM_API_KEY", "test-llm-key")

	if err := run(""); err == nil {
		t.Fatal("expected run() to fail building an unknown LLM provider")
	}
}

// TestRunWiringS3AttachmentsThenLLMError mirrors the above but selects the S3
// attachments backend, covering the AttachmentsIsS3 wiring branch in run().
func TestRunWiringS3AttachmentsThenLLMError(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	devEnv(t, dbURL, "127.0.0.1:0")
	t.Setenv("ATTACHMENTS_STORE", "s3://test-bucket/prefix")
	t.Setenv("ATTACHMENTS_S3_ENDPOINT", "127.0.0.1:9000")
	t.Setenv("ATTACHMENTS_S3_REGION", "us-east-1")
	t.Setenv("ATTACHMENTS_S3_ACCESS_KEY", "AKIATESTONLY000000")
	t.Setenv("ATTACHMENTS_S3_SECRET_KEY", "test-secret-key-not-real")
	t.Setenv("ATTACHMENTS_S3_USE_SSL", "0")
	t.Setenv("LLM_PROVIDER", "not-a-real-provider")
	t.Setenv("LLM_API_KEY", "test-llm-key")

	if err := run(""); err == nil {
		t.Fatal("expected run() to fail building an unknown LLM provider")
	}
}

// TestRunEmailIngestRequiresResolver covers the run() guard that rejects
// EMAIL_INGEST_ENABLED when no flight resolver is configured (no AeroDataBox
// key). A valid keyless LLM (ollama) satisfies both config.Load's email-ingest
// LLM requirement and run()'s extractor build, so execution reaches the
// resolver-nil check and returns its error.
func TestRunEmailIngestRequiresResolver(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	devEnv(t, dbURL, "127.0.0.1:0")
	// No AERODATABOX key -> resolver stays nil.
	t.Setenv("LLM_PROVIDER", "ollama")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("EMAIL_INGEST_ENABLED", "1")
	t.Setenv("EMAIL_INGEST_MAILDIR", filepath.Join(t.TempDir(), "maildir"))
	t.Setenv("EMAIL_INGEST_ADDRESS", "ingest@example.com")
	t.Setenv("EMAIL_INGEST_REQUIRE_DKIM", "0")

	if err := run(""); err == nil {
		t.Fatal("expected run() to reject email ingest without a resolver")
	}
}

// TestRunConfigFileLoaded covers the configPath != "" branch of run(): a valid
// 0400 YAML config file is loaded, after which an unknown LLM provider (set in
// the file) forces an early return. This exercises the config.LoadFile success
// path and the subsequent slog.Info line.
func TestRunConfigFileLoaded(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	// Provide only the variables that must come from the environment for the
	// test DB; everything else comes from the config file. Clear the env keys
	// the file supplies so LoadFile's "env wins" rule doesn't shadow them.
	t.Setenv("DATABASE_URL", dbURL)
	t.Setenv("SESSION_KEY", "")
	t.Setenv("PUBLIC_URL", "")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("DEV_AUTH_BYPASS", "")
	t.Setenv("GITHUB_CLIENT_ID", "")
	t.Setenv("GITHUB_CLIENT_SECRET", "")
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	t.Setenv("AERODATABOX_RAPIDAPI_KEY", "")
	t.Setenv("OPENSKY_ENABLED", "")
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_API_KEY", "")

	cfgFile := filepath.Join(t.TempDir(), "aerly.yaml")
	body := "" +
		"SESSION_KEY: \"0123456789abcdef0123456789abcdef\"\n" +
		"PUBLIC_URL: \"http://localhost\"\n" +
		"LISTEN_ADDR: \"127.0.0.1:0\"\n" +
		"POLL_INTERVAL: \"60s\"\n" +
		"LLM_PROVIDER: \"not-a-real-provider\"\n" +
		"LLM_API_KEY: \"test-llm-key\"\n"
	if err := os.WriteFile(cfgFile, []byte(body), 0o400); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	if err := run(cfgFile); err == nil {
		t.Fatal("expected run() to fail on the unknown LLM provider from the config file")
	}
}

// TestRunAttachmentStoreBuildError covers run()'s handling of a
// buildAttachmentStore failure: the configured filesystem path is absolute (so
// config.Load accepts it) but uncreatable because a parent component is a
// regular file, so run() returns the builder's error before the server starts.
func TestRunAttachmentStoreBuildError(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	dir := t.TempDir()
	notDir := filepath.Join(dir, "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	devEnv(t, dbURL, "127.0.0.1:0")
	t.Setenv("ATTACHMENTS_STORE", filepath.Join(notDir, "child"))

	if err := run(""); err == nil {
		t.Fatal("expected run() to fail building an uncreatable attachments store")
	}
}

// TestRunMalformedDotenv covers the run() branch that warns (but does not
// fail) when a .env in the working directory cannot be parsed. We chdir into a
// temp dir holding a malformed .env, then let run() fail on an invalid config
// so it returns promptly after emitting the parse warning. t.Chdir restores
// the original working directory on cleanup.
func TestRunMalformedDotenv(t *testing.T) {
	dir := t.TempDir()
	// A line with no '=' is not a valid env assignment; godotenv reports it.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("this is not valid env syntax\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Chdir(dir)

	// Force an invalid config so run() returns straight after the .env warning.
	t.Setenv("SESSION_KEY", "short")
	t.Setenv("DATABASE_URL", "")
	if err := run(""); err == nil {
		t.Fatal("expected run() to fail with an invalid config after the .env warning")
	}
}

// TestRunConfigFileError covers the configPath != "" branch where LoadFile
// fails (insecure file permissions), returning before config.Load runs.
func TestRunConfigFileError(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "insecure.yaml")
	if err := os.WriteFile(cfgFile, []byte("SESSION_KEY: x\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	if err := run(cfgFile); err == nil {
		t.Fatal("expected run() to reject a config file with insecure permissions")
	}
}
