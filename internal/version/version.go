// Package version exposes build-time provenance for the running binary: the
// git commit it was built from, whether the working tree was dirty, and when
// it was built. The values are sourced from the VCS stamp the Go toolchain
// embeds automatically (debug.ReadBuildInfo), with optional -ldflags overrides
// for builds where stamping isn't available (e.g. a source tarball without a
// .git directory).
package version

import (
	"runtime"
	"runtime/debug"
)

// commit and buildTime may be injected at build time via
//
//	-ldflags "-X github.com/dpage/aerly/internal/version.commit=<sha> \
//	          -X github.com/dpage/aerly/internal/version.buildTime=<rfc3339>"
//
// When left empty, Get() falls back to the toolchain's VCS stamp.
var (
	commit    string
	buildTime string
)

// Info is the provenance of the running binary. It carries no secrets and is
// safe to surface to operators (the superuser-only "About" panel).
type Info struct {
	// Commit is the full git SHA the binary was built from, or "" if unknown.
	Commit string `json:"commit"`
	// Short is the first 12 characters of Commit (or all of it if shorter).
	Short string `json:"short"`
	// Modified is true when the working tree had uncommitted changes at build
	// time — a signal that the running code may not match any commit exactly.
	Modified bool `json:"modified"`
	// BuildTime is the commit/build timestamp in RFC3339, or "" if unknown.
	BuildTime string `json:"build_time"`
	// GoVersion, OS and Arch describe the toolchain and target platform.
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get assembles the build provenance, preferring -ldflags overrides and
// falling back to the embedded VCS stamp for anything not set that way.
func Get() Info {
	info := Info{
		Commit:    commit,
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.BuildTime == "" {
					info.BuildTime = s.Value
				}
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
	}
	info.Short = shortSHA(info.Commit)
	return info
}

func shortSHA(sha string) string {
	const n = 12
	if len(sha) > n {
		return sha[:n]
	}
	return sha
}
