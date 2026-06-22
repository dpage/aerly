// Command coverage-gate is the Go counterpart to the web suite's per-file
// coverage threshold (vitest's `perFile: true` at 90%). Go's tooling only
// reports coverage per package, so this tool aggregates a standard cover
// profile down to per-file statement coverage and checks each file against a
// minimum percentage.
//
// Go coverage is statement-based, so "statements" is the only metric we can
// meaningfully gate on (unlike v8, which also tracks branches/functions/lines);
// 90% statement coverage per file is the natural equivalent of the web gate.
//
// Usage:
//
//	go test -covermode=set -coverprofile=coverage.out ./cmd/... ./internal/... ./migrations ./web
//	go run ./cmd/coverage-gate -profile=coverage.out -min=90 [-enforce]
//
// Without -enforce the tool always exits 0 (report-only); with -enforce it
// exits 1 if any non-excluded file falls below the threshold. The Makefile
// `coverage-gate` target runs it report-only for now; wiring it into CI with
// -enforce is a later step once the per-file gaps have been closed.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// fileCov accumulates statement counts for a single source file across all of
// the blocks the cover profile reports for it.
type fileCov struct {
	total   int
	covered int
}

func (f fileCov) pct() float64 {
	if f.total == 0 {
		return 100
	}
	return 100 * float64(f.covered) / float64(f.total)
}

func main() {
	profile := flag.String("profile", "coverage.out", "path to the go cover profile to analyse")
	min := flag.Float64("min", 90, "minimum per-file statement coverage percentage")
	excludePath := flag.String("exclude", ".coverage-exclude", "path to the exclusion list (one repo-relative pattern per line)")
	enforce := flag.Bool("enforce", false, "exit non-zero when a non-excluded file is below the threshold")
	flag.Parse()

	module, err := modulePath("go.mod")
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-gate: %v\n", err)
		os.Exit(2)
	}

	files, err := parseProfile(*profile, module)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-gate: %v\n", err)
		os.Exit(2)
	}

	patterns, err := loadExclusions(*excludePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-gate: %v\n", err)
		os.Exit(2)
	}

	report(os.Stdout, files, patterns, *min)

	if *enforce && countFailures(files, patterns, *min) > 0 {
		os.Exit(1)
	}
}

// modulePath extracts the module import path from go.mod so we can strip it
// from the profile's file keys and report repo-relative paths.
func modulePath(goMod string) (string, error) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", goMod, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("no module directive found in %s", goMod)
}

// parseProfile reads a cover profile and returns per-file coverage keyed by
// the repo-relative path (module prefix stripped). The profile format is one
// header line ("mode: ...") followed by lines of:
//
//	import/path/file.go:startLine.col,endLine.col numStmts count
func parseProfile(path, module string) (map[string]*fileCov, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening profile: %w", err)
	}
	defer f.Close()

	prefix := module + "/"
	files := map[string]*fileCov{}

	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				continue
			}
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Split into "<file>:<block>", "<numStmts>", "<count>".
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed profile line: %q", line)
		}
		// The file path itself never contains a colon in practice; the last
		// colon separates the path from the block's line/column range.
		colon := strings.LastIndex(fields[0], ":")
		if colon < 0 {
			return nil, fmt.Errorf("malformed profile line: %q", line)
		}
		name := strings.TrimPrefix(fields[0][:colon], prefix)

		numStmts, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("bad statement count in %q: %w", line, err)
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("bad execution count in %q: %w", line, err)
		}

		fc := files[name]
		if fc == nil {
			fc = &fileCov{}
			files[name] = fc
		}
		fc.total += numStmts
		if count > 0 {
			fc.covered += numStmts
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading profile: %w", err)
	}
	return files, nil
}

// loadExclusions reads the exclusion file, ignoring blank lines and '#'
// comments. A missing file is not an error: it simply means nothing is
// excluded.
func loadExclusions(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading exclusions: %w", err)
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, nil
}

// excluded reports whether a repo-relative file path is covered by any
// exclusion pattern. A pattern ending in "/" matches that directory and
// everything beneath it; otherwise we try an exact match and a filepath.Match
// glob (so "internal/**" style globbing and single-file entries both work).
func excluded(name string, patterns []string) bool {
	for _, p := range patterns {
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(name, p) {
				return true
			}
			continue
		}
		if name == p {
			return true
		}
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

func countFailures(files map[string]*fileCov, patterns []string, min float64) int {
	n := 0
	for name, fc := range files {
		if excluded(name, patterns) {
			continue
		}
		if fc.pct() < min {
			n++
		}
	}
	return n
}

// report prints a per-file table sorted by ascending coverage, with the failing
// files grouped at the top where they are easiest to act on, followed by a
// summary.
func report(out *os.File, files map[string]*fileCov, patterns []string, min float64) {
	type row struct {
		name string
		pct  float64
		excl bool
	}
	rows := make([]row, 0, len(files))
	for name, fc := range files {
		rows = append(rows, row{name: name, pct: fc.pct(), excl: excluded(name, patterns)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].pct != rows[j].pct {
			return rows[i].pct < rows[j].pct
		}
		return rows[i].name < rows[j].name
	})

	pass, fail, excl := 0, 0, 0
	w := bufio.NewWriter(out)
	defer w.Flush()
	for _, r := range rows {
		var status string
		switch {
		case r.excl:
			status = "EXCL"
			excl++
		case r.pct < min:
			status = "FAIL"
			fail++
		default:
			status = "pass"
			pass++
		}
		fmt.Fprintf(w, "%-4s %6.1f%%  %s\n", status, r.pct, r.name)
	}
	fmt.Fprintf(w, "\n%d files: %d pass, %d below %.0f%%, %d excluded\n",
		len(rows), pass, fail, min, excl)
	if fail > 0 {
		fmt.Fprintf(w, "%d file(s) below the %.0f%% target (report-only; pass -enforce to fail the build).\n", fail, min)
	}
}
