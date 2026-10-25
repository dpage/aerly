package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Per-plan links (issue #109): title/URL pairs pointing at tickets, boarding
// passes or anything else kept elsewhere. Aerly stores the link, never the
// target. The set is replaced wholesale on every save, so callers always send
// the complete list in display order.

// PlanLink is one stored link on a plan.
type PlanLink struct {
	ID       int64
	PlanID   int64
	Position int
	Title    string
	URL      string
}

// PlanLinkInput is one link as the client sends it; its position is its index.
type PlanLinkInput struct {
	Title string
	URL   string
}

// LinksByPlan returns a plan's links in display order.
func (s *Store) LinksByPlan(ctx context.Context, planID int64) ([]*PlanLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, plan_id, position, title, url
		FROM plan_links WHERE plan_id = $1
		ORDER BY position, id`, planID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*PlanLink, error) {
		l := &PlanLink{}
		return l, row.Scan(&l.ID, &l.PlanID, &l.Position, &l.Title, &l.URL)
	})
}

// replacePlanLinksTx swaps a plan's links for links, inside the caller's
// transaction so the plan save and its links land together.
func replacePlanLinksTx(ctx context.Context, tx pgx.Tx, planID int64, links []PlanLinkInput) error {
	titles := make([]string, len(links))
	urls := make([]string, len(links))
	for i, l := range links {
		titles[i], urls[i] = l.Title, l.URL
	}
	// One statement: the CTE's DELETE and the INSERT share a snapshot, and the
	// table has no unique constraint for the old and new rows to collide on.
	_, err := tx.Exec(ctx, `
		WITH gone AS (DELETE FROM plan_links WHERE plan_id = $1)
		INSERT INTO plan_links (plan_id, position, title, url)
		SELECT $1, x.ord - 1, x.title, x.url
		  FROM unnest($2::text[], $3::text[]) WITH ORDINALITY AS x(title, url, ord)`,
		planID, titles, urls)
	return err
}
