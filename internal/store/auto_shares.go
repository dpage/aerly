package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// AutoShare is one "always share with" default: when UserID creates a new trip,
// ShareWithID is automatically granted Role on it. Role is one of
// "viewer"/"editor" (a trip_members role) or "passenger" (a trip-level
// passenger — a traveller on every plan).
type AutoShare struct {
	UserID      int64
	ShareWithID int64
	Role        string
}

// ListAutoShares returns userID's configured auto-share defaults, ordered by
// the target user id for stable output.
func (s *Store) ListAutoShares(ctx context.Context, userID int64) ([]AutoShare, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, share_with_id, role FROM user_auto_shares
		 WHERE user_id = $1 ORDER BY share_with_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutoShare{}
	for rows.Next() {
		var a AutoShare
		if err := rows.Scan(&a.UserID, &a.ShareWithID, &a.Role); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAutoShare creates or updates an auto-share default for userID → shareWithID
// at the given role. role must be "viewer", "editor", or "passenger" (validated
// in the handler; the DB CHECK is the backstop).
func (s *Store) SetAutoShare(ctx context.Context, userID, shareWithID int64, role string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_auto_shares (user_id, share_with_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, share_with_id) DO UPDATE SET role = EXCLUDED.role`,
		userID, shareWithID, role)
	return err
}

// RemoveAutoShare deletes the auto-share default for userID → shareWithID.
// Returns ErrNotFound when no such default existed.
func (s *Store) RemoveAutoShare(ctx context.Context, userID, shareWithID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_auto_shares WHERE user_id = $1 AND share_with_id = $2`,
		userID, shareWithID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// applyAutoSharesTx grants owner's "always share with" defaults on a freshly
// created trip, inside the trip-creation transaction. viewer/editor become
// trip_members rows; passenger becomes a trip-level passenger (a trip_passengers
// row plus a viewer membership). The trip has no plans yet, so there is nothing
// to materialise onto plan_passengers here — later plan creation inherits each
// trip passenger automatically (see CreatePlan). All inserts are idempotent and
// never disturb the owner row.
func applyAutoSharesTx(ctx context.Context, tx pgx.Tx, tripID, ownerID int64) error {
	rows, err := tx.Query(ctx,
		`SELECT share_with_id, role FROM user_auto_shares WHERE user_id = $1`, ownerID)
	if err != nil {
		return err
	}
	type share struct {
		userID int64
		role   string
	}
	var shares []share
	for rows.Next() {
		var sh share
		if err := rows.Scan(&sh.userID, &sh.role); err != nil {
			rows.Close()
			return err
		}
		shares = append(shares, sh)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, sh := range shares {
		if sh.role == "passenger" {
			if _, err := tx.Exec(ctx,
				`INSERT INTO trip_passengers (trip_id, user_id) VALUES ($1, $2)
				 ON CONFLICT DO NOTHING`, tripID, sh.userID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'viewer')
				 ON CONFLICT DO NOTHING`, tripID, sh.userID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)
			 ON CONFLICT DO NOTHING`, tripID, sh.userID, sh.role); err != nil {
			return err
		}
	}
	return nil
}
