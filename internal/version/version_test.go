package version

import (
	"runtime"
	"testing"
)

func TestGetPopulatesRuntimeFacts(t *testing.T) {
	got := Get()
	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", got.GoVersion, runtime.Version())
	}
	if got.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", got.OS, runtime.GOOS)
	}
	if got.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", got.Arch, runtime.GOARCH)
	}
}

func TestGetPrefersLdflagsOverrides(t *testing.T) {
	// Save and restore the package vars so this test doesn't leak into others.
	origCommit, origBuild := commit, buildTime
	t.Cleanup(func() { commit, buildTime = origCommit, origBuild })

	commit = "0123456789abcdef0123456789abcdef01234567"
	buildTime = "2026-06-08T00:00:00Z"

	got := Get()
	if got.Commit != commit {
		t.Errorf("Commit = %q, want the ldflags override %q", got.Commit, commit)
	}
	if got.BuildTime != buildTime {
		t.Errorf("BuildTime = %q, want the ldflags override %q", got.BuildTime, buildTime)
	}
	if got.Short != "0123456789ab" {
		t.Errorf("Short = %q, want first 12 chars", got.Short)
	}
}

func TestShortSHAHandlesShortInput(t *testing.T) {
	if s := shortSHA("abc"); s != "abc" {
		t.Errorf("shortSHA(\"abc\") = %q, want \"abc\"", s)
	}
	if s := shortSHA(""); s != "" {
		t.Errorf("shortSHA(\"\") = %q, want \"\"", s)
	}
}
