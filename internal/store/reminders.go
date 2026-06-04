package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Upcoming-plan reminders (issue #11). This surface is wholly separate from the
// flight-status-change alert path (alert_prefs / plan_alert_optin / flight
// alerts): it drives scheduled "your plan starts soon" emails + in-app notices.

// TripReminder is a user's trip-level reminder opt-in. OptedIn is false when no
// row exists (the default).
type TripReminder struct {
	OptedIn   bool
	LeadHours int
}

// TripReminderFor returns the viewer's trip-level reminder opt-in, defaulting
// to not-opted-in (lead 24) when no row exists.
func (s *Store) TripReminderFor(ctx context.Context, tripID, userID int64) (TripReminder, error) {
	tr := TripReminder{OptedIn: false, LeadHours: 24}
	err := s.pool.QueryRow(ctx,
		`SELECT lead_hours FROM trip_reminder_optin WHERE trip_id = $1 AND user_id = $2`,
		tripID, userID).Scan(&tr.LeadHours)
	if errors.Is(err, pgx.ErrNoRows) {
		return tr, nil
	}
	if err != nil {
		return TripReminder{}, err
	}
	tr.OptedIn = true
	return tr, nil
}

// SetTripReminder upserts the trip-level opt-in (opt in / change lead).
func (s *Store) SetTripReminder(ctx context.Context, tripID, userID int64, leadHours int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO trip_reminder_optin (trip_id, user_id, lead_hours)
		VALUES ($1, $2, $3)
		ON CONFLICT (trip_id, user_id) DO UPDATE SET lead_hours = EXCLUDED.lead_hours`,
		tripID, userID, leadHours)
	return err
}

// RemoveTripReminder opts the user out at the trip level. No-op when absent.
func (s *Store) RemoveTripReminder(ctx context.Context, tripID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM trip_reminder_optin WHERE trip_id = $1 AND user_id = $2`, tripID, userID)
	return err
}

// PlanReminder is a user's per-plan reminder override. Override is "inherit"
// (no row), "on" (enabled), or "off" (explicit opt-out).
type PlanReminder struct {
	Override  string // inherit|on|off
	LeadHours int
}

// PlanReminderFor returns the viewer's per-plan override, defaulting to
// "inherit" (lead 24) when no row exists.
func (s *Store) PlanReminderFor(ctx context.Context, planID, userID int64) (PlanReminder, error) {
	pr := PlanReminder{Override: "inherit", LeadHours: 24}
	var enabled bool
	err := s.pool.QueryRow(ctx,
		`SELECT enabled, lead_hours FROM plan_reminder_optin WHERE plan_id = $1 AND user_id = $2`,
		planID, userID).Scan(&enabled, &pr.LeadHours)
	if errors.Is(err, pgx.ErrNoRows) {
		return pr, nil
	}
	if err != nil {
		return PlanReminder{}, err
	}
	if enabled {
		pr.Override = "on"
	} else {
		pr.Override = "off"
	}
	return pr, nil
}

// SetPlanReminder upserts a per-plan override (enabled = opt in / out).
func (s *Store) SetPlanReminder(ctx context.Context, planID, userID int64, enabled bool, leadHours int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plan_reminder_optin (plan_id, user_id, enabled, lead_hours)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (plan_id, user_id) DO UPDATE
		SET enabled = EXCLUDED.enabled, lead_hours = EXCLUDED.lead_hours`,
		planID, userID, enabled, leadHours)
	return err
}

// RemovePlanReminder clears a per-plan override (revert to inheriting the
// trip). No-op when absent.
func (s *Store) RemovePlanReminder(ctx context.Context, planID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM plan_reminder_optin WHERE plan_id = $1 AND user_id = $2`, planID, userID)
	return err
}

// MarkReminderSent records that a reminder for (part, user) has been dispatched
// so it never fires again. Idempotent.
func (s *Store) MarkReminderSent(ctx context.Context, partID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plan_reminder_sent (plan_part_id, user_id) VALUES ($1, $2)
		ON CONFLICT (plan_part_id, user_id) DO NOTHING`, partID, userID)
	return err
}

// DueReminder is one resolved reminder the poller should dispatch.
type DueReminder struct {
	PlanPartID int64
	PlanID     int64
	TripID     int64
	UserID     int64
	LeadHours  int
	StartsAt   time.Time
	StartTZ    string
	StartLabel string
	PlanType   string
	PlanTitle  string
	Ident      string // flight ident when the part is a flight, else ""
	Email      string // newest verified address, "" when none
}

// DueReminders returns every (plan_part, user) reminder ready to fire at `now`:
// the user is effectively opted in (a per-plan override beats the trip), the
// part is active and still upcoming, now is within the lead window, and no
// reminder was sent for that pair yet. Visibility is NOT applied here — the
// caller filters each candidate through VisiblePlanUserIDs so we reuse the
// tested §4 predicate rather than duplicate it in SQL.
func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]DueReminder, error) {
	rows, err := s.pool.Query(ctx, `
		WITH pairs AS (
			-- trip-level opt-in expands to every plan in the trip
			SELECT DISTINCT pl.id AS plan_id, pl.trip_id, tro.user_id
			FROM trip_reminder_optin tro
			JOIN plans pl ON pl.trip_id = tro.trip_id
			UNION
			-- per-plan override (enabled resolved below)
			SELECT DISTINCT pro.plan_id, pl.trip_id, pro.user_id
			FROM plan_reminder_optin pro
			JOIN plans pl ON pl.id = pro.plan_id
		),
		resolved AS (
			SELECT p.plan_id, p.trip_id, p.user_id,
			       COALESCE(pro.enabled, TRUE) AS enabled,
			       COALESCE(pro.lead_hours, tro.lead_hours) AS lead_hours
			FROM pairs p
			LEFT JOIN plan_reminder_optin pro
			       ON pro.plan_id = p.plan_id AND pro.user_id = p.user_id
			LEFT JOIN trip_reminder_optin tro
			       ON tro.trip_id = p.trip_id AND tro.user_id = p.user_id
		)
		SELECT pp.id, r.plan_id, r.trip_id, r.user_id, r.lead_hours,
		       pp.starts_at, pp.start_tz, pp.start_label, pl.type, pl.title,
		       COALESCE(fd.ident, ''),
		       COALESCE((
		           SELECT e.address FROM user_emails e
		           WHERE e.user_id = r.user_id AND e.verified = TRUE
		           ORDER BY e.verified_at DESC NULLS LAST, e.id DESC
		           LIMIT 1
		       ), '')
		FROM resolved r
		JOIN plans pl ON pl.id = r.plan_id
		JOIN plan_parts pp ON pp.plan_id = r.plan_id
		LEFT JOIN flight_details fd ON fd.plan_part_id = pp.id
		WHERE r.enabled
		  AND pp.status <> 'cancelled'
		  AND pp.dismissed_at IS NULL
		  AND pp.starts_at > $1
		  AND $1 >= pp.starts_at - make_interval(hours => r.lead_hours)
		  AND NOT EXISTS (
		      SELECT 1 FROM plan_reminder_sent prs
		      WHERE prs.plan_part_id = pp.id AND prs.user_id = r.user_id
		  )
		ORDER BY pp.starts_at, r.user_id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueReminder
	for rows.Next() {
		var d DueReminder
		if err := rows.Scan(&d.PlanPartID, &d.PlanID, &d.TripID, &d.UserID, &d.LeadHours,
			&d.StartsAt, &d.StartTZ, &d.StartLabel, &d.PlanType, &d.PlanTitle,
			&d.Ident, &d.Email); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
