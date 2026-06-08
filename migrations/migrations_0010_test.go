package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/testsupport"
	"github.com/dpage/aerly/migrations"
)

// readUpDown returns the bodies of a migration's .up.sql / .down.sql.
func readUpDown(t *testing.T, base string) (up, down string) {
	t.Helper()
	ub, err := migrations.FS.ReadFile(base + ".up.sql")
	if err != nil {
		t.Fatalf("read %s.up.sql: %v", base, err)
	}
	db, err := migrations.FS.ReadFile(base + ".down.sql")
	if err != nil {
		t.Fatalf("read %s.down.sql: %v", base, err)
	}
	return string(ub), string(db)
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		                WHERE table_schema='public' AND table_name=$1)`, name,
	).Scan(&ok); err != nil {
		t.Fatalf("table exists %q: %v", name, err)
	}
	return ok
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, col string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		                WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`,
		table, col,
	).Scan(&ok); err != nil {
		t.Fatalf("column exists %q.%q: %v", table, col, err)
	}
	return ok
}

// TestMigration0010UpDown verifies the trip-core migration creates its tables,
// re-keys positions, installs the passenger⇒viewer trigger, and that the down
// migration cleanly reverses it (leaving the surviving legacy tables intact and
// re-applies up afterwards).
func TestMigration0010UpDown(t *testing.T) {
	pool := testsupport.NewPool(t) // applies all .up.sql migrations, incl. 0010
	if pool == nil {
		return
	}
	ctx := context.Background()

	newTables := []string{
		"trips", "trip_members", "trip_tags", "plans", "plan_parts",
		"flight_details", "hotel_details", "train_details", "ground_details",
		"dining_details", "excursion_details", "plan_passengers",
		"plan_visibility", "plan_visibility_members", "alert_prefs",
		"plan_alert_optin", "calendar_tokens",
	}
	for _, tbl := range newTables {
		if !tableExists(t, pool, tbl) {
			t.Errorf("expected table %q after up", tbl)
		}
	}
	if !columnExists(t, pool, "positions", "plan_part_id") {
		t.Error("positions.plan_part_id missing after up")
	}

	// NOTE: the plan_passengers ⇒ trip_members 'viewer' trigger that migration
	// 0010 created is dropped by migration 0030 (passengers are now plan-scoped).
	// Since NewPool applies all migrations, that trigger no longer exists here;
	// its removal is covered by TestMigration0030. Other 0010 schema assertions
	// below remain valid.

	// Down then up again — exercises the reverse, and that the FK is restored.
	// A correct rollback chain unwinds the later migrations first:
	//   - 0030 created notifications/pending_shares tables with FKs to
	//     trips/plans/users, so those must be dropped before 0010 can drop them;
	//   - 0024 added reminder opt-in/sent tables → trips/plans/plan_parts/users,
	//     so they must be dropped before 0010 can drop those tables (0023 only
	//     adds columns to plans, so it needs no unwind here);
	//   - 0022 added trip_passengers → trips/users, so its table must be dropped
	//     before 0010 can drop trips;
	//   - 0021 added FKs from flight_alerts → trips/plans/plan_parts/users, so
	//     its constraints must be dropped before flight_alerts (and before
	//     0010 can drop plan_parts/plans/trips);
	//   - 0020-down then drops the flight_alerts table itself;
	//   - 0013 (applied by NewPool) dropped the legacy flights tables, so its
	//     down must run before 0010-down's positions→flights FK restore.
	_, down0030 := readUpDown(t, "0030_share_all_friends")
	_, down0024 := readUpDown(t, "0024_plan_reminders")
	_, down0022 := readUpDown(t, "0022_trip_passengers")
	_, down0021 := readUpDown(t, "0021_schema_hardening")
	_, down0020 := readUpDown(t, "0020_flight_alerts")
	up0013, down0013 := readUpDown(t, "0013_drop_legacy_flights")
	up, down := readUpDown(t, "0010_trip_core")
	if _, err := pool.Exec(ctx, down0030); err != nil {
		t.Fatalf("apply 0030 down: %v", err)
	}
	if _, err := pool.Exec(ctx, down0024); err != nil {
		t.Fatalf("apply 0024 down: %v", err)
	}
	if _, err := pool.Exec(ctx, down0022); err != nil {
		t.Fatalf("apply 0022 down: %v", err)
	}
	if _, err := pool.Exec(ctx, down0021); err != nil {
		t.Fatalf("apply 0021 down: %v", err)
	}
	if _, err := pool.Exec(ctx, down0020); err != nil {
		t.Fatalf("apply 0020 down: %v", err)
	}
	if _, err := pool.Exec(ctx, down0013); err != nil {
		t.Fatalf("apply 0013 down: %v", err)
	}
	// 0013-down recreates the legacy flights structure (empty).
	if !tableExists(t, pool, "flights") {
		t.Error("legacy flights table should be recreated by 0013 down")
	}
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("apply down: %v", err)
	}
	for _, tbl := range newTables {
		if tableExists(t, pool, tbl) {
			t.Errorf("table %q still present after down", tbl)
		}
	}
	if columnExists(t, pool, "positions", "plan_part_id") {
		t.Error("positions.plan_part_id still present after down")
	}
	// Legacy tables must survive the 0010 down (0010 never dropped them).
	if !tableExists(t, pool, "flights") {
		t.Error("legacy flights table should survive 0010 down")
	}
	// Re-apply up (0010 then 0013) to confirm reversibility of the chain.
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("re-apply up: %v", err)
	}
	if !tableExists(t, pool, "trips") {
		t.Error("trips missing after re-applying up")
	}
	if _, err := pool.Exec(ctx, up0013); err != nil {
		t.Fatalf("re-apply 0013 up: %v", err)
	}
	if tableExists(t, pool, "flights") {
		t.Error("flights should be dropped again after re-applying 0013 up")
	}
}
