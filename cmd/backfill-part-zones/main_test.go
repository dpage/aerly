package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/db"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/testsupport"
	"github.com/dpage/aerly/migrations"
)

func fp(v float64) *float64 { return &v }

func noTrip(int64) ([]store.ZonedEnd, error) { return nil, nil }

func TestPlanCoordsConfirmed(t *testing.T) {
	at := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	fixes, rep, err := plan([]store.FloatingEnd{
		{PartID: 1, TripID: 1, End: "start", At: at, Lat: fp(51.5), Lon: fp(-0.12)},
	}, noTrip)
	if err != nil || len(fixes) != 1 {
		t.Fatalf("fixes=%v err=%v", fixes, err)
	}
	f := fixes[0]
	if f.TZ != "Europe/London" || f.Source != "confirmed" || !f.At.Equal(at.Add(-time.Hour)) {
		t.Errorf("fix = %+v", f)
	}
	if rep.Confirmed != 1 || len(rep.Old) != 1 || !rep.Old[0].Equal(at) {
		t.Errorf("report = %+v", rep)
	}
}

func TestPlanIATAConfirmed(t *testing.T) {
	at := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	fixes, rep, _ := plan([]store.FloatingEnd{
		{PartID: 2, TripID: 1, End: "end", At: at, IATA: "JFK"},
	}, noTrip)
	if len(fixes) != 1 || fixes[0].TZ != "America/New_York" || fixes[0].Source != "confirmed" || rep.Confirmed != 1 {
		t.Fatalf("fixes=%+v rep=%+v", fixes, rep)
	}
}

func TestPlanNeighbourByNearestTime(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 7, day, 12, 0, 0, 0, time.UTC) }
	zoned := []store.ZonedEnd{
		{At: d(1), TZ: "Europe/London", Dwell: 100 * time.Hour},
		{At: d(10), TZ: "Europe/Paris"},
	}
	fixes, rep, _ := plan([]store.FloatingEnd{
		{PartID: 3, TripID: 1, End: "start", At: d(8)},
	}, func(int64) ([]store.ZonedEnd, error) { return zoned, nil })
	if len(fixes) != 1 || fixes[0].TZ != "Europe/Paris" || fixes[0].Source != "inferred" || rep.Inferred != 1 {
		t.Fatalf("fixes=%+v rep=%+v", fixes, rep)
	}
}

func TestNearestTieGoesEarlier(t *testing.T) {
	a := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	zoned := []store.ZonedEnd{
		{At: a.Add(2 * time.Hour), TZ: "Europe/Paris"},
		{At: a, TZ: "Europe/London"},
	}
	if got := nearest(zoned, a.Add(time.Hour)); got != "Europe/London" {
		t.Errorf("nearest = %q", got)
	}
	if got := nearest(nil, a); got != "" {
		t.Errorf("nearest(nil) = %q", got)
	}
}

func TestDominantByDwell(t *testing.T) {
	zoned := []store.ZonedEnd{
		{TZ: "Europe/Paris", Dwell: 48 * time.Hour},
		{TZ: "Europe/London", Dwell: 24 * time.Hour},
		{TZ: "Europe/London", Dwell: 30 * time.Hour},
	}
	if got := dominant(zoned); got != "Europe/London" {
		t.Errorf("dominant = %q", got)
	}
	tie := []store.ZonedEnd{{TZ: "Europe/Paris"}, {TZ: "Europe/London"}}
	if got := dominant(tie); got != "Europe/London" {
		t.Errorf("tie dominant = %q", got)
	}
}

func TestPlanBorrowsFromNeighbour(t *testing.T) {
	// The only zoned end in the trip is the neighbour, and it is also the
	// dominant zone: the zone is borrowed, so the result is inferred. (The
	// trip fallback in Infer is reached only if the neighbour's zone is invalid.)
	zoned := []store.ZonedEnd{{At: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), TZ: "Europe/Paris", Dwell: time.Hour}}
	fixes, _, _ := plan([]store.FloatingEnd{
		{PartID: 4, TripID: 9, End: "end", At: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)},
	}, func(int64) ([]store.ZonedEnd, error) { return zoned, nil })
	if len(fixes) != 1 || fixes[0].Source != "inferred" {
		t.Fatalf("fixes=%+v", fixes)
	}
}

// TestPlanSettlesInOneRun: a coordinate-less end borrows from a neighbour
// confirmed earlier in the same run, and a second plan over the resulting
// state finds nothing left to fix.
func TestPlanSettlesInOneRun(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 7, day, 12, 0, 0, 0, time.UTC) }
	ends := []store.FloatingEnd{
		{PartID: 1, TripID: 1, End: "start", At: d(1), Lat: fp(48.85), Lon: fp(2.35)},
		{PartID: 2, TripID: 1, End: "start", At: d(2)}, // no evidence of its own
	}
	fixes, rep, err := plan(ends, noTrip)
	if err != nil || len(fixes) != 2 || rep.Confirmed != 1 || rep.Inferred != 1 || rep.Floating != 0 {
		t.Fatalf("fixes=%+v rep=%+v err=%v", fixes, rep, err)
	}
	for _, f := range fixes {
		if f.TZ != "Europe/Paris" {
			t.Errorf("fix = %+v", f)
		}
	}
	// Second run: nothing is floating any more.
	again, rep2, _ := plan(nil, noTrip)
	if len(again) != 0 || rep2.Floating != 0 {
		t.Errorf("second plan = %+v %+v", again, rep2)
	}
}

// TestPlanOrderIndependent: second-pass ends infer only from pre-existing
// and confirmed zones, never from each other, so reordering them gives the
// same fixes. Chained, end 2 would take London from end 1's guess on day 7.
func TestPlanOrderIndependent(t *testing.T) {
	d := func(day int, hour int) time.Time { return time.Date(2026, 7, day, hour, 0, 0, 0, time.UTC) }
	zoned := []store.ZonedEnd{
		{At: d(1, 0), TZ: "Europe/Paris"},
		{At: d(11, 0), TZ: "Europe/London"},
	}
	trip := func(int64) ([]store.ZonedEnd, error) { return zoned, nil }
	a := store.FloatingEnd{PartID: 1, TripID: 1, End: "start", At: d(8, 0)}  // nearest London
	b := store.FloatingEnd{PartID: 2, TripID: 1, End: "start", At: d(5, 12)} // nearest Paris
	zones := func(ends ...store.FloatingEnd) map[int64]string {
		fixes, _, err := plan(ends, trip)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]string{}
		for _, f := range fixes {
			out[f.PartID] = f.TZ
		}
		return out
	}
	ab, ba := zones(a, b), zones(b, a)
	if ab[1] != "Europe/London" || ab[2] != "Europe/Paris" {
		t.Errorf("a then b = %v", ab)
	}
	if ba[1] != ab[1] || ba[2] != ab[2] {
		t.Errorf("order changed the result: a,b = %v; b,a = %v", ab, ba)
	}
}

func TestPlanNothingLeftFloating(t *testing.T) {
	fixes, rep, err := plan([]store.FloatingEnd{
		{PartID: 5, TripID: 1, End: "start", At: time.Now()},
	}, noTrip)
	if err != nil || len(fixes) != 0 || rep.Floating != 1 {
		t.Fatalf("fixes=%v rep=%+v err=%v", fixes, rep, err)
	}
	if len(rep.LeftFloating) != 1 || rep.LeftFloating[0].PartID != 5 || rep.LeftFloating[0].End != "start" {
		t.Errorf("left floating = %+v", rep.LeftFloating)
	}
}

func TestPlanTripError(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := plan([]store.FloatingEnd{{PartID: 1, TripID: 1}},
		func(int64) ([]store.ZonedEnd, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanCachesTrip(t *testing.T) {
	calls := 0
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, _, _ = plan([]store.FloatingEnd{
		{PartID: 1, TripID: 7, End: "start", At: at},
		{PartID: 2, TripID: 7, End: "end", At: at},
	}, func(int64) ([]store.ZonedEnd, error) { calls++; return nil, nil })
	if calls != 1 {
		t.Errorf("trip called %d times", calls)
	}
}

func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func resetFlags(t *testing.T, args []string) {
	t.Helper()
	oldFlags, oldArgs := flag.CommandLine, os.Args
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = append([]string{"backfill-part-zones"}, args...)
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
}

// seedFloating inserts a synthetic hotel part with floating ends at London
// coordinates and returns its id.
func seedFloating(t *testing.T, s *store.Store) int64 {
	t.Helper()
	ctx := context.Background()
	userID := testsupport.InsertUser(t, s.Pool(), fmt.Sprintf("zoneuser-%d", time.Now().UnixNano()), false, true)
	var tripID, planID, partID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('Test Trip', $1) RETURNING id`, userID).Scan(&tripID); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, 'hotel', $2) RETURNING id`,
		tripID, userID).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool().QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_label, status, start_lat, start_lon,
			start_tz, start_tz_source, end_tz, end_tz_source)
		VALUES ($1, '2026-07-01 15:00+00', '2026-07-03 11:00+00', 'Test Hotel', 'confirmed',
			51.5, -0.12, '', 'floating', '', 'floating') RETURNING id`, planID).Scan(&partID); err != nil {
		t.Fatal(err)
	}
	return partID
}

func TestRunDryRunThenApply(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	ctx := context.Background()
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	s := store.New(pool)
	partID := seedFloating(t, s)
	pool.Close()

	withEnv(t, map[string]string{"DATABASE_URL": dbURL})
	type snapshot struct {
		startTZ, startSrc, endTZ, endSrc string
		startsAt, endsAt                 time.Time
	}
	state := func() snapshot {
		p, err := db.Open(ctx, dbURL)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		var x snapshot
		if err := p.QueryRow(ctx,
			`SELECT start_tz, start_tz_source, starts_at, end_tz, end_tz_source, ends_at
			 FROM plan_parts WHERE id = $1`, partID).Scan(&x.startTZ, &x.startSrc, &x.startsAt,
			&x.endTZ, &x.endSrc, &x.endsAt); err != nil {
			t.Fatal(err)
		}
		return x
	}
	before := state()

	resetFlags(t, nil) // default is a dry run
	if err := run(); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if after := state(); after != before {
		t.Fatalf("dry run changed the row: %+v -> %+v", before, after)
	}

	resetFlags(t, []string{"-n=false"})
	if err := run(); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := state()
	want := time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC) // 15:00 London summer time
	if got.startTZ != "Europe/London" || got.startSrc != "confirmed" || !got.startsAt.Equal(want) {
		t.Fatalf("after apply: %+v", got)
	}
	// The end has no coordinates of its own, so it uses the start's.
	if got.endTZ != "Europe/London" || got.endSrc != "confirmed" {
		t.Fatalf("end after apply: %+v", got)
	}
	// A second apply is a fixed point.
	resetFlags(t, []string{"-n=false"})
	if err := run(); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if again := state(); again != got {
		t.Fatalf("second run changed the row: %+v -> %+v", got, again)
	}
}

func TestRunErrors(t *testing.T) {
	t.Run("no database", func(t *testing.T) {
		withEnv(t, map[string]string{"DATABASE_URL": ""})
		resetFlags(t, nil)
		if err := run(); err == nil || !strings.Contains(err.Error(), "no database") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("db flag wins over env", func(t *testing.T) {
		withEnv(t, map[string]string{"DATABASE_URL": "postgres://example/ignored"})
		resetFlags(t, []string{"-db", "postgres://aerly:aerly@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=2"})
		if err := run(); err == nil || strings.Contains(err.Error(), "no database") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("db open", func(t *testing.T) {
		withEnv(t, map[string]string{
			"DATABASE_URL": "postgres://aerly:aerly@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=2",
		})
		resetFlags(t, nil)
		if err := run(); err == nil {
			t.Fatal("want db error")
		}
	})
	t.Run("malformed dotenv", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/.env", []byte("NOT_VALID_ENV_SYNTAX\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		withEnv(t, map[string]string{"DATABASE_URL": ""})
		resetFlags(t, nil)
		if err := run(); err == nil {
			t.Fatal("want error after the .env warning")
		}
	})
}

type fakeStore struct {
	ends             []store.FloatingEnd
	endsErr, tripErr error
	applyErr         error
	applied          int
}

func (f *fakeStore) FloatingEnds(context.Context) ([]store.FloatingEnd, error) {
	return f.ends, f.endsErr
}
func (f *fakeStore) TripZonedEnds(context.Context, int64) ([]store.ZonedEnd, error) {
	return nil, f.tripErr
}
func (f *fakeStore) ApplyZoneFixes(_ context.Context, fx []store.ZoneFix) (int, error) {
	f.applied += len(fx)
	return len(fx), f.applyErr
}

func TestProcessErrors(t *testing.T) {
	boom := errors.New("boom")
	one := []store.FloatingEnd{{PartID: 1, TripID: 1, End: "start",
		At: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), Lat: fp(48.85), Lon: fp(2.35)}}
	ctx := context.Background()

	if err := process(ctx, &fakeStore{endsErr: boom}, true); !errors.Is(err, boom) {
		t.Errorf("ends error = %v", err)
	}
	if err := process(ctx, &fakeStore{ends: one, tripErr: boom}, true); !errors.Is(err, boom) {
		t.Errorf("trip error = %v", err)
	}
	if err := process(ctx, &fakeStore{ends: one, applyErr: boom}, false); !errors.Is(err, boom) {
		t.Errorf("apply error = %v", err)
	}
	f := &fakeStore{ends: one}
	if err := process(ctx, f, true); err != nil || f.applied != 0 {
		t.Errorf("dry run applied %d, err %v", f.applied, err)
	}
	if err := process(ctx, f, false); err != nil || f.applied != 1 {
		t.Errorf("apply applied %d, err %v", f.applied, err)
	}
}

func TestRunRefusesUnmigratedDatabase(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t) // created but not migrated
	withEnv(t, map[string]string{"DATABASE_URL": ""})
	resetFlags(t, []string{"-db", dbURL, "-n=false"})
	err := run()
	if err == nil {
		t.Fatal("want a refusal")
	}
	t.Log(err)
}

func TestRequireMigration(t *testing.T) {
	dbURL := testsupport.NewDatabaseURL(t)
	ctx := context.Background()
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Before any migration there is no schema_migrations table at all.
	if err := requireMigration(ctx, pool); err == nil {
		t.Error("want error with no schema_migrations table")
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if err := requireMigration(ctx, pool); err != nil {
		t.Errorf("migrated database refused: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version = 55`); err != nil {
		t.Fatal(err)
	}
	if err := requireMigration(ctx, pool); err == nil || !strings.Contains(err.Error(), "migration 0055 is not applied; deploy first") {
		t.Errorf("err = %v", err)
	}
}
