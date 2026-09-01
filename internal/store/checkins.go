package store

import (
	"context"
	"time"
)

// Flight check-in reminders (issue #119). Separate from both the
// flight-status-change alert path (which fires on what the airline does) and
// the upcoming-plan reminder path (which fires a configurable lead before any
// plan): this one fires once per flight, at a fixed offset chosen so the
// traveller is at the keyboard when check-in opens.

// CheckinLead is how far ahead of departure a check-in reminder fires: the 24
// hours before departure at which most airlines open online check-in, plus the
// five minutes' notice asked for in issue #119, so the reminder lands just
// before the window rather than just after it.
const CheckinLead = 24*time.Hour + 5*time.Minute

// DueCheckin is one resolved check-in reminder the poller should dispatch. The
// channel flags and address come from the recipient's alert_prefs, so the
// poller can decide delivery without a second query, exactly as it does for
// change alerts.
type DueCheckin struct {
	PlanPartID int64
	PlanID     int64
	TripID     int64
	UserID     int64
	Ident      string
	StartsAt   time.Time
	StartTZ    string
	OriginIATA string
	DestIATA   string
	StartLat   *float64
	StartLon   *float64
	InApp      bool
	Email      bool
	EmailAddr  string // newest verified address, "" when none
}

// DueCheckins returns every (flight part, user) check-in reminder ready to fire
// at `now`: the part is an active, still-upcoming flight whose departure is
// inside the check-in lead, the user is on the plan's alert-recipient set with
// the check-in preference on, and no reminder was sent for that pair yet.
//
// Visibility is NOT applied here — the caller filters each candidate through
// VisiblePlanUserIDs, reusing the tested predicate rather than duplicating it
// in SQL, which is the same division of labour as DueReminders.
func (s *Store) DueCheckins(ctx context.Context, now time.Time) ([]DueCheckin, error) {
	rows, err := s.pool.Query(ctx, `
		WITH recip AS (
			SELECT pl.id AS plan_id, pl.trip_id, pl.created_by AS user_id
			FROM plans pl
			WHERE pl.type = 'flight' AND pl.created_by IS NOT NULL
			UNION
			SELECT pl.id, pl.trip_id, pp.user_id
			FROM plan_passengers pp JOIN plans pl ON pl.id = pp.plan_id
			WHERE pl.type = 'flight'
			UNION
			SELECT pl.id, pl.trip_id, pao.user_id
			FROM plan_alert_optin pao JOIN plans pl ON pl.id = pao.plan_id
			WHERE pl.type = 'flight'
		)
		SELECT part.id, r.plan_id, r.trip_id, r.user_id,
		       fd.ident, part.starts_at, part.start_tz,
		       fd.origin_iata, fd.dest_iata, part.start_lat, part.start_lon,
		       COALESCE(ap.in_app, TRUE), COALESCE(ap.email, TRUE),
		       COALESCE((
		           SELECT e.address FROM user_emails e
		           WHERE e.user_id = r.user_id AND e.verified = TRUE
		           ORDER BY e.verified_at DESC NULLS LAST, e.id DESC
		           LIMIT 1
		       ), '')
		FROM recip r
		JOIN plan_parts part ON part.plan_id = r.plan_id
		JOIN flight_details fd ON fd.plan_part_id = part.id
		LEFT JOIN alert_prefs ap ON ap.user_id = r.user_id
		WHERE COALESCE(ap.checkin, FALSE)
		  AND part.status <> 'cancelled'
		  AND part.dismissed_at IS NULL
		  AND fd.flight_status NOT IN ('Cancelled', 'Diverted')
		  AND part.starts_at > $1
		  AND $1 >= part.starts_at - $2::interval
		  AND NOT EXISTS (
		      SELECT 1 FROM flight_checkin_sent cs
		      WHERE cs.plan_part_id = part.id AND cs.user_id = r.user_id
		  )
		ORDER BY part.starts_at, r.user_id`, now, CheckinLead)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueCheckin
	for rows.Next() {
		var d DueCheckin
		if err := rows.Scan(&d.PlanPartID, &d.PlanID, &d.TripID, &d.UserID,
			&d.Ident, &d.StartsAt, &d.StartTZ, &d.OriginIATA, &d.DestIATA,
			&d.StartLat, &d.StartLon, &d.InApp, &d.Email, &d.EmailAddr); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkCheckinSent records that a check-in reminder for (part, user) has been
// dispatched so it never fires again. Idempotent.
func (s *Store) MarkCheckinSent(ctx context.Context, partID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO flight_checkin_sent (plan_part_id, user_id) VALUES ($1, $2)
		ON CONFLICT (plan_part_id, user_id) DO NOTHING`, partID, userID)
	return err
}
