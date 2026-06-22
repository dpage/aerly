package main

import (
	"net/http"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestMainEmailIngestStartup exercises the full email-ingest service startup
// branch of run() (the goroutine that constructs and runs emailingest.Service)
// together with the LLM extractor success branch. A keyless ollama LLM
// satisfies LLMConfigured() and builds a real client without contacting any
// server, and an AeroDataBox key provides the resolver email ingest requires.
// run() then reaches the blocking server loop, which we wait for via /healthz
// and tear down with SIGTERM. It calls run() directly (not main()) because
// main() registers a -config flag on the default CommandLine, which panics if
// invoked twice in the same test binary (TestMainGracefulShutdown owns the
// single main() invocation).
func TestMainEmailIngestStartup(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	if dbURL == "" {
		t.Skip("no database available")
	}
	addr := freePort(t)
	devEnv(t, dbURL, addr)
	t.Setenv("AERODATABOX_RAPIDAPI_KEY", "adb-test-key")
	t.Setenv("LLM_PROVIDER", "ollama")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("EMAIL_INGEST_ENABLED", "1")
	t.Setenv("EMAIL_INGEST_MAILDIR", filepath.Join(t.TempDir(), "maildir"))
	t.Setenv("EMAIL_INGEST_ADDRESS", "ingest@example.com")
	t.Setenv("EMAIL_INGEST_REQUIRE_DKIM", "0")
	t.Setenv("EMAIL_INGEST_POLL_INTERVAL", "60s")

	done := make(chan struct{})
	go func() {
		if err := run(""); err != nil {
			t.Errorf("run() returned error: %v", err)
		}
		close(done)
	}()

	healthOK := false
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				healthOK = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !healthOK {
		t.Fatal("server never became healthy")
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("main() did not shut down after SIGTERM")
	}
}
