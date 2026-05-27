package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Friendship is one edge in the friend graph. The canonical-pair layout
// (user_low < user_high) is a storage detail; consumers should use the
// FriendID / Direction helpers, which orient the edge relative to the
// viewer.
type Friendship struct {
	UserLow     int64
	UserHigh    int64
	Status      string // "pending" | "accepted"
	RequestedBy int64
	RequestedAt time.Time
	AcceptedAt  *time.Time
}

// FriendID returns the *other* user's ID, given the viewer.
func (f *Friendship) FriendID(viewerID int64) int64 {
	if f.UserLow == viewerID {
		return f.UserHigh
	}
	return f.UserLow
}

// IncomingTo reports whether viewerID is the recipient of a pending request
// (i.e. the one who needs to accept/decline).
func (f *Friendship) IncomingTo(viewerID int64) bool {
	return f.Status == "pending" && f.RequestedBy != viewerID
}

// PendingFriendInvite is a queued friend request addressed at an email that
// isn't a verified user_emails row yet. Consumed on first sign-in.
type PendingFriendInvite struct {
	EmailLower string
	InviterID  int64
	Message    string
	CreatedAt  time.Time
}

// pairOrder returns the (low, high) ordering this table uses internally.
func pairOrder(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

// FriendshipBetween returns the row joining a and b, or ErrNotFound. Order
// of arguments doesn't matter — the query rewrites to the canonical pair.
func (s *Store) FriendshipBetween(ctx context.Context, a, b int64) (*Friendship, error) {
	if a == b {
		return nil, ErrNotFound
	}
	low, high := pairOrder(a, b)
	return scanFriendship(s.pool.QueryRow(ctx, `
		SELECT user_low, user_high, status, requested_by, requested_at, accepted_at
		FROM friendships WHERE user_low = $1 AND user_high = $2`,
		low, high))
}

// ListFriendships returns every friendship row involving viewerID,
// regardless of status, ordered by accepted-then-pending and most recent
// activity first.
func (s *Store) ListFriendships(ctx context.Context, viewerID int64) ([]*Friendship, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_low, user_high, status, requested_by, requested_at, accepted_at
		FROM friendships
		WHERE $1 IN (user_low, user_high)
		ORDER BY status DESC,
		         COALESCE(accepted_at, requested_at) DESC`,
		viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Friendship
	for rows.Next() {
		f, err := scanFriendship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanFriendship(row pgx.Row) (*Friendship, error) {
	var f Friendship
	if err := row.Scan(&f.UserLow, &f.UserHigh, &f.Status,
		&f.RequestedBy, &f.RequestedAt, &f.AcceptedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

// RequestFriendship inserts a pending row from requesterID → recipientID.
// If a row already exists:
//   - status='accepted' → returns the existing row (no-op).
//   - status='pending' with requesterID as the recipient of the original
//     request → upgrades to accepted (the second-direction request implicitly
//     accepts the first).
//   - status='pending' with requesterID as the original requester → returns
//     the existing row (no-op duplicate).
func (s *Store) RequestFriendship(ctx context.Context, requesterID, recipientID int64) (*Friendship, error) {
	if requesterID == recipientID {
		return nil, errors.New("cannot friend yourself")
	}
	low, high := pairOrder(requesterID, recipientID)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	existing, err := scanFriendship(tx.QueryRow(ctx, `
		SELECT user_low, user_high, status, requested_by, requested_at, accepted_at
		FROM friendships WHERE user_low = $1 AND user_high = $2
		FOR UPDATE`,
		low, high))
	switch {
	case err == nil:
		// Already accepted, or duplicate request from same side: no-op.
		if existing.Status == "accepted" || existing.RequestedBy == requesterID {
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return existing, nil
		}
		// Cross-direction pending request → accept.
		upd, err := scanFriendship(tx.QueryRow(ctx, `
			UPDATE friendships
			SET status = 'accepted', accepted_at = NOW()
			WHERE user_low = $1 AND user_high = $2
			RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at`,
			low, high))
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return upd, nil
	case errors.Is(err, ErrNotFound):
		// Insert fresh pending row.
		row, err := scanFriendship(tx.QueryRow(ctx, `
			INSERT INTO friendships (user_low, user_high, status, requested_by)
			VALUES ($1, $2, 'pending', $3)
			RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at`,
			low, high, requesterID))
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return row, nil
	default:
		return nil, err
	}
}

// AcceptFriendship transitions a pending row from requested_by ≠ viewerID
// to status='accepted'. Returns ErrNotFound if no pending row addressed to
// viewerID exists.
func (s *Store) AcceptFriendship(ctx context.Context, viewerID, otherID int64) (*Friendship, error) {
	low, high := pairOrder(viewerID, otherID)
	return scanFriendship(s.pool.QueryRow(ctx, `
		UPDATE friendships
		SET status = 'accepted', accepted_at = NOW()
		WHERE user_low = $1 AND user_high = $2
		  AND status = 'pending'
		  AND requested_by <> $3
		RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at`,
		low, high, viewerID))
}

// RemoveFriendship deletes the row joining viewerID and otherID. Covers
// both unfriending an accepted edge and declining / cancelling a pending
// request. Returns ErrNotFound when no row matched.
func (s *Store) RemoveFriendship(ctx context.Context, viewerID, otherID int64) error {
	low, high := pairOrder(viewerID, otherID)
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM friendships
		WHERE user_low = $1 AND user_high = $2
		  AND $3 IN (user_low, user_high)`,
		low, high, viewerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertPendingFriendInvite records that inviterID has invited an email
// address that doesn't (yet) belong to any verified user. Returns (created,
// nil) where created reports whether the row was new (caller uses this to
// decide whether to send the email — duplicate invites stay silent).
func (s *Store) UpsertPendingFriendInvite(ctx context.Context, inviterID int64, email, message string) (bool, error) {
	addr := strings.ToLower(strings.TrimSpace(email))
	if addr == "" {
		return false, errors.New("email required")
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO pending_friend_invites (email_lower, inviter_id, message)
		VALUES ($1, $2, $3)
		ON CONFLICT (email_lower, inviter_id) DO NOTHING`,
		addr, inviterID, message)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// consumePendingInvitesTx converts every pending_friend_invite addressed at
// any of the verified email addresses owned by userID into an accepted
// friendship, then deletes those pending rows. Idempotent: if the
// friendship already exists in any state it's promoted to accepted (the
// invitation implicitly accepts an outstanding cross-direction request).
//
// Returns the IDs of users who became newly-accepted friends so the caller
// can notify them.
func consumePendingInvitesTx(ctx context.Context, tx pgx.Tx, userID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT p.inviter_id
		FROM pending_friend_invites p
		JOIN user_emails e ON lower(e.address) = p.email_lower
		WHERE e.user_id = $1 AND e.verified = TRUE
		  AND p.inviter_id <> $1`,
		userID)
	if err != nil {
		return nil, err
	}
	var inviters []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		inviters = append(inviters, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, inviter := range inviters {
		low, high := pairOrder(userID, inviter)
		if _, err := tx.Exec(ctx, `
			INSERT INTO friendships (user_low, user_high, status, requested_by, accepted_at)
			VALUES ($1, $2, 'accepted', $3, NOW())
			ON CONFLICT (user_low, user_high) DO UPDATE
			SET status = 'accepted',
			    accepted_at = COALESCE(friendships.accepted_at, NOW())`,
			low, high, inviter); err != nil {
			return nil, err
		}
	}

	// Drop every matching pending row, including ones from the same inviter
	// that conflicted (no-op INSERT above) so the queue stays clean.
	if _, err := tx.Exec(ctx, `
		DELETE FROM pending_friend_invites
		WHERE email_lower IN (
			SELECT lower(address) FROM user_emails
			WHERE user_id = $1 AND verified = TRUE)`,
		userID); err != nil {
		return nil, err
	}
	return inviters, nil
}
