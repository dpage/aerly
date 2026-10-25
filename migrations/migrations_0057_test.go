package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestMigration0057PlanLinksCascade verifies plan_links rows go with their plan.
func TestMigration0057PlanLinksCascade(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	uid := testsupport.InsertUser(t, pool, "m57user", false, true)
	var tid, pid int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, uid).Scan(&tid); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, title, created_by) VALUES ($1, 'event', 'E', $2) RETURNING id`,
		tid, uid).Scan(&pid); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_links (plan_id, position, title, url) VALUES ($1, 0, 'Ticket', 'https://example.com/t')`,
		pid); err != nil {
		t.Fatalf("insert link: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM plans WHERE id = $1`, pid); err != nil {
		t.Fatalf("delete plan: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM plan_links`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("plan_links rows = %d after plan delete, want 0", n)
	}
}
