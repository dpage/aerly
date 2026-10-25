package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestMigration0056DepartureSummaryPrefDefaultsOn verifies the alert_prefs
// column now defaults to TRUE, so a new row opts the user in to departure
// summaries until they turn them off.
func TestMigration0056DepartureSummaryPrefDefaultsOn(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	uid := testsupport.InsertUser(t, pool, "m56user", false, true)
	if _, err := pool.Exec(ctx,
		`INSERT INTO alert_prefs (user_id) VALUES ($1)`, uid); err != nil {
		t.Fatalf("insert alert_prefs: %v", err)
	}
	var pref bool
	if err := pool.QueryRow(ctx,
		`SELECT departure_summary FROM alert_prefs WHERE user_id = $1`, uid).Scan(&pref); err != nil {
		t.Fatalf("select departure_summary: %v", err)
	}
	if !pref {
		t.Fatal("departure_summary should default to TRUE")
	}
}
