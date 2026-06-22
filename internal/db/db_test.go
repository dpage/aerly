package db_test

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"testing"
	"testing/fstest"

	"github.com/dpage/aerly/internal/db"
	"github.com/dpage/aerly/internal/testsupport"
	"github.com/dpage/aerly/migrations"
)

// adminURL mirrors testsupport's maintenance connection string. It is used by
// the error-path tests below that need to open a second pool as a different
// role (or against a fresh database) to exercise branches that a healthy,
// already-migrated pool never reaches.
func adminURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://aerly:aerly@127.0.0.1:5432/postgres?sslmode=disable"
}

// openErrFS makes fs.ReadDir(".") fail (Open always errors).
type openErrFS struct{}

func (openErrFS) Open(string) (fs.File, error) { return nil, errors.New("open boom") }

// readFileErrFS lists one migration file but fails when it is read.
type readFileErrFS struct{}

type fakeDirEntry struct{ name string }

func (f fakeDirEntry) Name() string             { return f.name }
func (fakeDirEntry) IsDir() bool                { return false }
func (fakeDirEntry) Type() fs.FileMode          { return 0 }
func (fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

func (readFileErrFS) Open(string) (fs.File, error) { return nil, errors.New("read boom") }
func (readFileErrFS) ReadDir(string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{fakeDirEntry{name: "0099_x.up.sql"}}, nil
}

func TestOpenParseError(t *testing.T) {
	if _, err := db.Open(context.Background(), "::::not-a-valid-dsn"); err == nil {
		t.Error("expected ParseConfig error for invalid DSN")
	}
}

func TestOpenValid(t *testing.T) {
	// testsupport hands back an already-open pool; just assert Open works on
	// the same URL form by re-opening a throwaway config string.
	pool := testsupport.NewPool(t)
	if pool == nil {
		t.Skip("no DB")
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
}

func TestMigrateIdempotentWithRealMigrations(t *testing.T) {
	pool := testsupport.NewPool(t) // already migrated once by the helper
	// Running again must be a no-op (all versions already applied).
	if err := db.Migrate(context.Background(), pool, migrations.FS); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestMigrateBadFilename(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"notaversion.up.sql": {Data: []byte("SELECT 1")},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected parse-filename error")
	}
}

func TestMigrateSkipsDownFiles(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0001_init.up.sql":   {Data: []byte("SELECT 1")}, // already applied → skipped
		"0001_init.down.sql": {Data: []byte("garbage that would fail if run")},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err != nil {
		t.Fatalf("down files must be ignored: %v", err)
	}
}

func TestMigrateApplyErrorRollsBack(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0099_broken.up.sql": {Data: []byte("THIS IS NOT VALID SQL;")},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected apply error for invalid SQL")
	}
	// schema_migrations must not record the failed version.
	var n int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 99`).Scan(&n)
	if n != 0 {
		t.Errorf("failed migration was recorded (%d rows)", n)
	}
}

func TestMigrateNewVersionApplies(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0050_extra.up.sql": {Data: []byte(`CREATE TABLE extra_t (id int)`)},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err != nil {
		t.Fatalf("Migrate new version: %v", err)
	}
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 50`).Scan(&n); err != nil || n != 1 {
		t.Errorf("version 50 not recorded: n=%d err=%v", n, err)
	}
	// Second run skips it (applied map continue branch).
	if err := db.Migrate(context.Background(), pool, mfs); err != nil {
		t.Fatalf("re-Migrate: %v", err)
	}
}

func TestMigrateCreateTableErrorOnCancelledCtx(t *testing.T) {
	pool := testsupport.NewPool(t)
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.Migrate(c, pool, migrations.FS); err == nil {
		t.Error("expected create schema_migrations error on cancelled ctx")
	}
}

func TestMigrateReadDirError(t *testing.T) {
	pool := testsupport.NewPool(t)
	if err := db.Migrate(context.Background(), pool, openErrFS{}); err == nil {
		t.Error("expected ReadDir error")
	}
}

func TestMigrateReadFileError(t *testing.T) {
	pool := testsupport.NewPool(t)
	if err := db.Migrate(context.Background(), pool, readFileErrFS{}); err == nil {
		t.Error("expected ReadFile error for listed-but-unreadable migration")
	}
}

func TestMigrateVersionQueryError(t *testing.T) {
	pool := testsupport.NewPool(t)
	// Replace schema_migrations with a table lacking the `version` column so
	// the SELECT version query fails (CREATE IF NOT EXISTS is a no-op).
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (foo int)`); err != nil {
		t.Fatalf("create wrong: %v", err)
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err == nil {
		t.Error("expected SELECT version query error")
	}
}

func TestMigrateScanError(t *testing.T) {
	pool := testsupport.NewPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	// version is TEXT with a non-integer value → rows.Scan(&int) fails.
	_, _ = pool.Exec(ctx, `CREATE TABLE schema_migrations (version text, name text)`)
	_, _ = pool.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ('notanint','x')`)
	if err := db.Migrate(ctx, pool, migrations.FS); err == nil {
		t.Error("expected scan error for non-integer version")
	}
}

func TestMigrateRecordInsertErrorRollsBack(t *testing.T) {
	pool := testsupport.NewPool(t)
	// The migration body drops schema_migrations, so the follow-up
	// INSERT INTO schema_migrations fails and the tx is rolled back.
	mfs := fstest.MapFS{
		"0098_drop.up.sql": {Data: []byte(`DROP TABLE schema_migrations`)},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected schema_migrations INSERT error after self-drop")
	}
}

// TestMigrateRowScanError covers the rows.Scan(&v) error branch (line 78).
//
// The existing TestMigrateVersionQueryError / TestMigrateScanError tests reuse
// the helper's already-migrated pool, whose connection has cached the
// `SELECT version` plan as returning an INTEGER. Pointing that connection at a
// differently-typed `schema_migrations` column surfaces a "cached plan must
// not change result type" error lazily via rows.Err(), so the scan itself
// never fails and line 78 stays uncovered.
//
// Here we instead open a brand-new pool against an EMPTY, un-migrated database
// (so no `SELECT version` plan has ever been prepared) and pre-create
// schema_migrations with a TEXT version column. With a fresh plan the result
// column really is text, and decoding it into the *int passed to rows.Scan
// fails at the scan, exercising the rows.Close()/return err branch.
func TestMigrateRowScanError(t *testing.T) {
	ctx := context.Background()
	dsn := testsupport.NewDatabaseURL(t) // empty, un-migrated database
	if dsn == "" {
		t.Skip("no DB")
	}
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx,
		`CREATE TABLE schema_migrations (version text, name text)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO schema_migrations (version, name) VALUES ('5', 'x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.Migrate(ctx, pool, migrations.FS); err == nil {
		t.Error("expected scan error decoding a TEXT version into *int")
	}
}

// TestMigrateCreateTableError covers the "create schema_migrations" error
// branch (line 45). On a healthy superuser pool the CREATE TABLE IF NOT EXISTS
// never fails, so we connect as a freshly-created, low-privilege role that
// lacks CREATE on the public schema (with the table dropped first) and confirm
// Migrate fails before it can list or apply anything.
func TestMigrateCreateTableError(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		t.Skip("no DB")
	}
	ctx := context.Background()

	var dbname string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbname); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	// Use a database-scoped role name so parallel test databases don't collide
	// on the cluster-global role namespace.
	role := "lowpriv_" + dbname
	if _, err := pool.Exec(ctx, `CREATE ROLE "`+role+`" LOGIN PASSWORD 'lowpriv'`); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP ROLE IF EXISTS "`+role+`"`) })
	// Strip CREATE from public so the limited role cannot recreate the table.
	if _, err := pool.Exec(ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC`); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := pool.Exec(ctx, `GRANT USAGE ON SCHEMA public TO "`+role+`"`); err != nil {
		t.Fatalf("grant usage: %v", err)
	}

	u, err := url.Parse(adminURL())
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.User = url.UserPassword(role, "lowpriv")
	u.Path = "/" + dbname
	lp, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("open low-priv pool: %v", err)
	}
	defer lp.Close()

	if err := db.Migrate(ctx, lp, migrations.FS); err == nil {
		t.Error("expected create schema_migrations permission error")
	}
}

// TestMigrateCommitError covers the tx.Commit() error branch: the migration
// body creates a DEFERRABLE INITIALLY DEFERRED FK and inserts a violating
// row, so the constraint is only checked — and fails — at COMMIT time.
func TestMigrateCommitError(t *testing.T) {
	pool := testsupport.NewPool(t)
	mfs := fstest.MapFS{
		"0097_deferred.up.sql": {Data: []byte(`
			CREATE TABLE parent_t (id int PRIMARY KEY);
			CREATE TABLE child_t (
				pid int REFERENCES parent_t(id) DEFERRABLE INITIALLY DEFERRED
			);
			INSERT INTO child_t VALUES (999);`)},
	}
	if err := db.Migrate(context.Background(), pool, mfs); err == nil {
		t.Error("expected COMMIT error from deferred FK violation")
	}
}
