package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/db"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
	"github.com/dpage/aerly/migrations"
)

// TestIsQuotaExhausted exercises the pure quota predicate: any error whose
// message contains "quota" (case-insensitive) is the monthly wall; everything
// else (transient throttles, not-found, etc.) is not.
func TestIsQuotaExhausted(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"lowercase quota", errors.New("monthly quota exceeded"), true},
		{"mixed case", errors.New("API Quota Reached"), true},
		{"upper case", errors.New("QUOTA"), true},
		{"transient throttle", errors.New("429 too many requests per second"), false},
		{"not found", errors.New("flight not found"), false},
		{"empty message", errors.New(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isQuotaExhausted(tc.err); got != tc.want {
				t.Fatalf("isQuotaExhausted(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestSleep covers both arms of the select: a timer that fires, and an
// already-cancelled context that returns immediately.
func TestSleep(t *testing.T) {
	// Timer fires (short duration, live context).
	start := time.Now()
	sleep(context.Background(), 5*time.Millisecond)
	if elapsed := time.Since(start); elapsed < 2*time.Millisecond {
		t.Fatalf("sleep returned too early: %v", elapsed)
	}

	// Cancelled context returns immediately, well before the long timer.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	sleep(ctx, time.Hour)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("sleep did not honour cancelled context: %v", elapsed)
	}
}

// seedFlightPart inserts a synthetic trip + flight plan + plan_part +
// flight_details (unresolved, bare IATA labels) and returns the plan_part id.
// All values are made up; no real people, flights, or coordinates.
func seedFlightPart(t *testing.T, s *store.Store, ident, originIATA, destIATA string) int64 {
	t.Helper()
	ctx := context.Background()
	userID := testsupport.InsertUser(t, s.Pool(),
		fmt.Sprintf("seeduser-%d", time.Now().UnixNano()), false, true)

	var tripID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ($1, $2) RETURNING id`,
		"Synthetic Trip", userID).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`,
		tripID, userID); err != nil {
		t.Fatalf("insert trip member: %v", err)
	}
	var planID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'flight', $2) RETURNING id`,
		tripID, userID).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(2 * time.Hour)
	var partID int64
	if err := s.Pool().QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_label, end_label, status)
		VALUES ($1, $2, $3, $4, $5, 'confirmed') RETURNING id`,
		planID, out, in, originIATA, destIATA).Scan(&partID); err != nil {
		t.Fatalf("insert plan_part: %v", err)
	}
	if _, err := s.Pool().Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in,
			origin_iata, dest_iata, flight_status, resolved)
		VALUES ($1, $2, $3, $4, $5, $6, 'Scheduled', false)`,
		partID, ident, out, in, originIATA, destIATA); err != nil {
		t.Fatalf("insert flight_details: %v", err)
	}
	return partID
}

// TestTableRelabel covers each branch of the table-only relabel: an on-table
// origin upgrades the labels (returns true), an entirely off-table pair is a
// no-op (returns false), and a part id that does not exist surfaces the store
// error path (returns false).
func TestTableRelabel(t *testing.T) {
	s := store.New(testsupport.NewPool(t))
	ctx := context.Background()

	t.Run("on-table leg upgrades labels", func(t *testing.T) {
		partID := seedFlightPart(t, s, "QQ100", "LHR", "ZZZ")
		r := store.FlightPartRow{
			PartID: partID, Ident: "QQ100",
			OriginIATA: "LHR", DestIATA: "ZZZ",
		}
		if !tableRelabel(ctx, s, r) {
			t.Fatal("tableRelabel returned false for an on-table origin")
		}
		var startLabel string
		if err := s.Pool().QueryRow(ctx,
			`SELECT start_label FROM plan_parts WHERE id = $1`, partID).Scan(&startLabel); err != nil {
			t.Fatalf("read start_label: %v", err)
		}
		if startLabel != "London Heathrow (LHR)" {
			t.Fatalf("start_label = %q, want upgraded LHR label", startLabel)
		}
	})

	t.Run("off-table pair is a no-op", func(t *testing.T) {
		partID := seedFlightPart(t, s, "QQ200", "ZZZ", "XXY")
		r := store.FlightPartRow{
			PartID: partID, Ident: "QQ200",
			OriginIATA: "ZZZ", DestIATA: "XXY",
		}
		if tableRelabel(ctx, s, r) {
			t.Fatal("tableRelabel returned true for an off-table pair")
		}
	})

	t.Run("store error returns false", func(t *testing.T) {
		// A non-existent part id makes UpdateFlightPartRoute return ErrNotFound,
		// so the on-table branch still hits the error path and returns false.
		r := store.FlightPartRow{
			PartID: 9_999_999, Ident: "QQ300",
			OriginIATA: "LHR", DestIATA: "ZZZ",
		}
		if tableRelabel(ctx, s, r) {
			t.Fatal("tableRelabel returned true despite a store error")
		}
	})
}

// withEnv sets env vars for the duration of the test and restores them after.
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// resetFlags swaps in a fresh flag.CommandLine and os.Args so run() can call
// flag.Bool / flag.Parse without "flag redefined" panics across invocations,
// then restores the originals.
func resetFlags(t *testing.T, args []string) {
	t.Helper()
	oldFlags := flag.CommandLine
	oldArgs := os.Args
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = append([]string{"backfill-flight-labels"}, args...)
	t.Cleanup(func() {
		flag.CommandLine = oldFlags
		os.Args = oldArgs
	})
}

// TestRunMissingKey: with no AeroDataBox key configured, run() returns the
// "key required" error before ever touching the database or network.
func TestRunMissingKey(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	withEnv(t, map[string]string{
		"DATABASE_URL":             dbURL,
		"SESSION_KEY":              "0123456789abcdef0123456789abcdef",
		"DEV_AUTH_BYPASS":          "1",
		"AERODATABOX_RAPIDAPI_KEY": "",
	})
	resetFlags(t, nil)

	err := run()
	if err == nil || err.Error() != "AERODATABOX_RAPIDAPI_KEY is required to re-resolve flights" {
		t.Fatalf("run() error = %v, want missing-key error", err)
	}
}

// TestRunMalformedDotEnv covers the godotenv warning branch: a .env that exists
// in the working directory but fails to parse is logged (not fatal). run() then
// proceeds and fails on the missing key, which we assert to confirm we got past
// the .env load. The malformed .env lives in a throwaway temp dir, never the
// repo, so nothing private or real is involved.
func TestRunMalformedDotEnv(t *testing.T) {
	dir := t.TempDir()
	// A bare token with no '=' is not valid dotenv syntax, so Load() errors
	// with something other than os.ErrNotExist, hitting the warn branch.
	if err := os.WriteFile(dir+"/.env", []byte("THIS_IS_NOT_VALID_ENV_SYNTAX\n"), 0o600); err != nil {
		t.Fatalf("write malformed .env: %v", err)
	}
	t.Chdir(dir)

	withEnv(t, map[string]string{
		"DATABASE_URL":             "postgres://example/db",
		"SESSION_KEY":              "0123456789abcdef0123456789abcdef",
		"DEV_AUTH_BYPASS":          "1",
		"AERODATABOX_RAPIDAPI_KEY": "",
	})
	resetFlags(t, nil)

	if err := run(); err == nil {
		t.Fatal("run() returned nil; expected the missing-key error after the .env warning")
	}
}

// TestRunConfigError: a config failure (e.g. SESSION_KEY too short) is returned
// straight out of run() before the key check.
func TestRunConfigError(t *testing.T) {
	withEnv(t, map[string]string{
		"DATABASE_URL":             "postgres://example/db",
		"SESSION_KEY":              "tooshort",
		"DEV_AUTH_BYPASS":          "1",
		"AERODATABOX_RAPIDAPI_KEY": "synthetic-test-key",
	})
	resetFlags(t, nil)

	if err := run(); err == nil {
		t.Fatal("run() returned nil for an invalid SESSION_KEY")
	}
}

// TestRunDryRun drives run() end-to-end in dry-run mode against a real (but
// synthetic) database. Dry-run never calls the resolver, so no network is
// touched: it covers config load, db open/migrate, store construction,
// resolver construction, listing unresolved parts, the dry-run loop branch,
// and the final summary/return.
func TestRunDryRun(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)

	// Migrate and seed up front via our own pool; run() re-runs migrations
	// idempotently when it opens its own pool against the same URL.
	ctx := context.Background()
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open seed pool: %v", err)
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate seed pool: %v", err)
	}
	s := store.New(pool)
	seedFlightPart(t, s, "QQ400", "LHR", "JFK")
	seedFlightPart(t, s, "QQ401", "ZZZ", "XXY")
	pool.Close()

	withEnv(t, map[string]string{
		"DATABASE_URL":             dbURL,
		"SESSION_KEY":              "0123456789abcdef0123456789abcdef",
		"DEV_AUTH_BYPASS":          "1",
		"AERODATABOX_RAPIDAPI_KEY": "synthetic-test-key",
	})
	resetFlags(t, []string{"-n", "-throttle", "1ms"})

	if err := run(); err != nil {
		t.Fatalf("run() in dry-run mode returned error: %v", err)
	}
}

// TestRunDryRunEmpty drives the same dry-run path against a database with no
// unresolved flights, covering the zero-rows summary path.
func TestRunDryRunEmpty(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	withEnv(t, map[string]string{
		"DATABASE_URL":             dbURL,
		"SESSION_KEY":              "0123456789abcdef0123456789abcdef",
		"DEV_AUTH_BYPASS":          "1",
		"AERODATABOX_RAPIDAPI_KEY": "synthetic-test-key",
	})
	resetFlags(t, []string{"-n"})

	if err := run(); err != nil {
		t.Fatalf("run() against empty db returned error: %v", err)
	}
}

// TestRunDBOpenError: a valid config but an unreachable database surfaces the
// db.Open error from run().
func TestRunDBOpenError(t *testing.T) {
	withEnv(t, map[string]string{
		// Port 1 is never a Postgres server, so db.Open fails fast.
		"DATABASE_URL":             "postgres://aerly:aerly@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=2",
		"SESSION_KEY":              "0123456789abcdef0123456789abcdef",
		"DEV_AUTH_BYPASS":          "1",
		"AERODATABOX_RAPIDAPI_KEY": "synthetic-test-key",
	})
	resetFlags(t, nil)

	if err := run(); err == nil {
		t.Fatal("run() returned nil for an unreachable database")
	}
}
