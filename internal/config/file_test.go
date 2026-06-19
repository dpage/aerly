package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfigFile writes content to a temp file with the given permissions and
// returns its path.
func writeConfigFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aerly.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Chmod separately so the umask doesn't interfere with the requested mode.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}

func TestLoadFileAppliesValues(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("POLL_INTERVAL", "")
	t.Setenv("DEV_AUTH_BYPASS", "")
	path := writeConfigFile(t, "LISTEN_ADDR: \":9000\"\nPOLL_INTERVAL: 90s\nDEV_AUTH_BYPASS: true\n", 0o400)
	if err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := os.Getenv("LISTEN_ADDR"); got != ":9000" {
		t.Errorf("LISTEN_ADDR = %q, want :9000", got)
	}
	if got := os.Getenv("POLL_INTERVAL"); got != "90s" {
		t.Errorf("POLL_INTERVAL = %q, want 90s", got)
	}
	// Booleans become the 0/1 form the env-var loaders expect.
	if got := os.Getenv("DEV_AUTH_BYPASS"); got != "1" {
		t.Errorf("DEV_AUTH_BYPASS = %q, want 1", got)
	}
}

func TestLoadFileEnvOverridesFile(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":7777")
	path := writeConfigFile(t, "LISTEN_ADDR: \":9000\"\n", 0o400)
	if err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := os.Getenv("LISTEN_ADDR"); got != ":7777" {
		t.Errorf("LISTEN_ADDR = %q, want :7777 (env should win over file)", got)
	}
}

func TestLoadFileNumberStringified(t *testing.T) {
	t.Setenv("EMAIL_INGEST_RATE_LIMIT_PER_DAY", "")
	path := writeConfigFile(t, "EMAIL_INGEST_RATE_LIMIT_PER_DAY: 10\n", 0o400)
	if err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if got := os.Getenv("EMAIL_INGEST_RATE_LIMIT_PER_DAY"); got != "10" {
		t.Errorf("rate limit = %q, want 10", got)
	}
}

func TestLoadFileNullSkipped(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "")
	path := writeConfigFile(t, "LISTEN_ADDR: null\n", 0o400)
	if err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if _, ok := os.LookupEnv("LISTEN_ADDR"); ok && os.Getenv("LISTEN_ADDR") != "" {
		t.Errorf("LISTEN_ADDR should be left unset by a null value")
	}
}

func TestLoadFileRejectsLoosePermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0o444, 0o440, 0o600, 0o640, 0o644, 0o777} {
		path := writeConfigFile(t, "LISTEN_ADDR: \":9000\"\n", mode)
		err := LoadFile(path)
		if err == nil || !strings.Contains(err.Error(), "permissions") {
			t.Errorf("mode %#o: expected permissions error, got %v", mode, err)
		}
	}
}

func TestLoadFileAcceptsExactly0400(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "")
	path := writeConfigFile(t, "LISTEN_ADDR: \":9000\"\n", 0o400)
	if err := LoadFile(path); err != nil {
		t.Fatalf("0400 should be accepted: %v", err)
	}
}

func TestLoadFileMissingFile(t *testing.T) {
	err := LoadFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

func TestLoadFileRejectsNonScalar(t *testing.T) {
	path := writeConfigFile(t, "LISTEN_ADDR:\n  nested: value\n", 0o400)
	if err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "LISTEN_ADDR") {
		t.Errorf("expected non-scalar error mentioning the key, got %v", err)
	}
}

func TestLoadFileRejectsMalformedYAML(t *testing.T) {
	path := writeConfigFile(t, "this: is: not: valid\n", 0o400)
	if err := LoadFile(path); err == nil {
		t.Fatal("expected a parse error for malformed YAML")
	}
}
