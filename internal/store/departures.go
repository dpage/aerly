package store

import (
	"context"
	"time"
)

// Flight departure summaries (issue #130). Modelled on the check-in reminders
// in checkins.go: opt-in, once per flight leg per user, and found by the same
// query, differing only in the lead time, the preference column and the table
// recording what has been sent.

// DepartureSummaryLead is how far ahead of departure a departure summary fires.
const DepartureSummaryLead = 4 * time.Hour

// DueDepartureSummary is one resolved departure summary the poller should
// dispatch. It carries the same fields as DueCheckin, so the alias keeps the
// two paths on one scan.
type DueDepartureSummary = DueCheckin

// DueDepartureSummaries returns every (flight part, user) departure summary
// ready to fire at `now`: the part is an active, still-upcoming flight whose
// departure is inside the lead, the user is on the plan's alert-recipient set
// with the departure-summary preference on, and none was sent for that pair yet.
//
// As with DueCheckins, visibility is NOT applied here; the caller filters each
// candidate through VisiblePlanUserIDs.
func (s *Store) DueDepartureSummaries(ctx context.Context, now time.Time) ([]DueDepartureSummary, error) {
	return s.dueFlightNotices(ctx, now, DepartureSummaryLead, "departure_summary", "flight_departure_summary_sent")
}

// MarkDepartureSummarySent records that a departure summary for (part, user)
// has been dispatched so it never fires again. Idempotent.
func (s *Store) MarkDepartureSummarySent(ctx context.Context, partID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO flight_departure_summary_sent (plan_part_id, user_id) VALUES ($1, $2)
		ON CONFLICT (plan_part_id, user_id) DO NOTHING`, partID, userID)
	return err
}
