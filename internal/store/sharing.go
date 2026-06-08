package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// SetTripShareAllFriends sets (or clears with "") the trip-level all-friends
// default role. "viewer"/"editor" enable the grant; "" disables it.
func (s *Store) SetTripShareAllFriends(ctx context.Context, tripID int64, role string) error {
	var arg any
	if role == "" {
		arg = nil
	} else {
		arg = role
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE trips SET share_all_friends_role = $2, updated_at = NOW() WHERE id = $1`,
		tripID, arg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PendingShare is a pre-share to an email address with no account yet.
type PendingShare struct {
	EmailLower string
	Kind       string // "trip" | "plan"
	TargetID   int64
	Role       string // "viewer"|"editor" for trip; "" for plan
	InviterID  int64
}

// InsertPendingShare records a pre-share, idempotent on (email, kind, target).
func (s *Store) InsertPendingShare(ctx context.Context, ps PendingShare) error {
	var role any
	if ps.Role == "" {
		role = nil
	} else {
		role = ps.Role
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pending_shares (email_lower, kind, target_id, role, inviter_id)
		VALUES (lower($1), $2, $3, $4, $5)
		ON CONFLICT (email_lower, kind, target_id) DO UPDATE SET role = EXCLUDED.role`,
		ps.EmailLower, ps.Kind, ps.TargetID, role, ps.InviterID)
	return err
}

// consumePendingSharesTx converts pending_shares addressed to any of userID's
// verified emails into real grant rows (trip_members for 'trip', a
// plan_passenger for 'plan'), then deletes them. Runs inside the same tx as
// consumePendingInvitesTx so a new user's pre-shares and friendships land
// atomically; the friend gate then makes them visible.
func consumePendingSharesTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `
		DELETE FROM pending_shares
		 WHERE email_lower IN (
		   SELECT lower(address) FROM user_emails
		   WHERE user_id = $1 AND verified = TRUE)
		RETURNING kind, target_id, role`, userID)
	if err != nil {
		return err
	}
	type ps struct {
		kind   string
		target int64
		role   *string
	}
	var claimed []ps
	for rows.Next() {
		var p ps
		if err := rows.Scan(&p.kind, &p.target, &p.role); err != nil {
			rows.Close()
			return err
		}
		claimed = append(claimed, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range claimed {
		switch p.kind {
		case "trip":
			role := "viewer"
			if p.role != nil && *p.role != "" {
				role = *p.role
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)
				ON CONFLICT (trip_id, user_id) DO NOTHING`, p.target, userID, role); err != nil {
				return err
			}
		case "plan":
			if _, err := tx.Exec(ctx, `
				INSERT INTO plan_passengers (plan_id, user_id, via_trip) VALUES ($1, $2, false)
				ON CONFLICT (plan_id, user_id) DO NOTHING`, p.target, userID); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetPlanShareAllFriends toggles the per-plan all-friends grant.
func (s *Store) SetPlanShareAllFriends(ctx context.Context, planID int64, enabled bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE plans SET share_all_friends = $2, updated_at = NOW() WHERE id = $1`,
		planID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
