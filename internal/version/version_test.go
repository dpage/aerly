package version

import (
	"runtime"
	"runtime/debug"
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

func TestApplyBuildSettingsFillsFromVCSStamp(t *testing.T) {
	got := applyBuildSettings(Info{}, []debug.BuildSetting{
		{Key: "vcs.revision", Value: "deadbeefcafef00d"},
		{Key: "vcs.time", Value: "2026-06-08T12:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
		{Key: "GOARCH", Value: "ignored"},
	})
	if got.Commit != "deadbeefcafef00d" {
		t.Errorf("Commit = %q, want the VCS revision", got.Commit)
	}
	if got.BuildTime != "2026-06-08T12:00:00Z" {
		t.Errorf("BuildTime = %q, want the VCS time", got.BuildTime)
	}
	if !got.Modified {
		t.Error("Modified = false, want true for vcs.modified=true")
	}
}

func TestApplyBuildSettingsKeepsLdflagsOverrides(t *testing.T) {
	// Commit and BuildTime are already populated (as if from -ldflags), so the
	// VCS stamp must not clobber them; vcs.modified=false leaves Modified false.
	seed := Info{Commit: "fromldflags", BuildTime: "fromldflags-time"}
	got := applyBuildSettings(seed, []debug.BuildSetting{
		{Key: "vcs.revision", Value: "vcsrevision"},
		{Key: "vcs.time", Value: "vcstime"},
		{Key: "vcs.modified", Value: "false"},
	})
	if got.Commit != "fromldflags" {
		t.Errorf("Commit = %q, want the ldflags value preserved", got.Commit)
	}
	if got.BuildTime != "fromldflags-time" {
		t.Errorf("BuildTime = %q, want the ldflags value preserved", got.BuildTime)
	}
	if got.Modified {
		t.Error("Modified = true, want false for vcs.modified=false")
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
