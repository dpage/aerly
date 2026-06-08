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
	UserLow      int64
	UserHigh     int64
	Status       string // "pending" | "accepted"
	RequestedBy  int64
	RequestedAt  time.Time
	AcceptedAt   *time.Time
	InvitedEmail string // empty for non-pending rows; the email the inviter typed
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
		SELECT user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email
		FROM friendships WHERE user_low = $1 AND user_high = $2`,
		low, high))
}

// AreAcceptedFriends reports whether a and b have an accepted friendship.
// Argument order doesn't matter; the query normalises to the canonical pair.
// Returns false (no error) when a == b — callers may legitimately ask about
// the creator/passenger being the same user.
func (s *Store) AreAcceptedFriends(ctx context.Context, a, b int64) (bool, error) {
	if a == b {
		return false, nil
	}
	low, high := pairOrder(a, b)
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM friendships
			WHERE user_low = $1 AND user_high = $2 AND status = 'accepted')`,
		low, high).Scan(&ok)
	return ok, err
}

// ListFriendships returns every friendship row involving viewerID,
// regardless of status. Pending rows come first ('p' > 'a' alphabetically
// under status DESC) so the UI surfaces action items (incoming requests)
// above the already-accepted list, then each group is sorted by most
// recent activity first.
func (s *Store) ListFriendships(ctx context.Context, viewerID int64) ([]*Friendship, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email
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

// ListOutgoingPendingInvites returns the pending_friend_invites rows
// authored by inviterID — the email-only outgoing invites whose targets
// haven't (yet) verified the address. The list endpoint unions these with
// friendship rows so an outgoing pending invite looks identical regardless
// of whether the target is already a registered user.
func (s *Store) ListOutgoingPendingInvites(ctx context.Context, inviterID int64) ([]*PendingFriendInvite, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT email_lower, inviter_id, message, created_at
		FROM pending_friend_invites
		WHERE inviter_id = $1
		ORDER BY created_at DESC`,
		inviterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PendingFriendInvite
	for rows.Next() {
		var p PendingFriendInvite
		if err := rows.Scan(&p.EmailLower, &p.InviterID, &p.Message, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func scanFriendship(row pgx.Row) (*Friendship, error) {
	var f Friendship
	var invited *string
	if err := row.Scan(&f.UserLow, &f.UserHigh, &f.Status,
		&f.RequestedBy, &f.RequestedAt, &f.AcceptedAt, &invited); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if invited != nil {
		f.InvitedEmail = *invited
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
//
// Concurrent cross-direction requests are handled atomically: the INSERT
// uses ON CONFLICT DO NOTHING so the PRIMARY KEY arbitrates the race,
// then on conflict we fall through to a locked SELECT and apply the same
// no-op / accept-cross-direction logic. Two simultaneous A→B and B→A
// calls can no longer both miss the lock and one of them fail on the
// PRIMARY KEY constraint.
func (s *Store) RequestFriendship(ctx context.Context, requesterID, recipientID int64, invitedEmail string) (*Friendship, error) {
	if requesterID == recipientID {
		return nil, errors.New("cannot friend yourself")
	}
	// Lowercase at the store boundary so the wire response can't leak
	// target existence via casing: both this path (known target) and
	// UpsertPendingFriendInvite (unknown target) must produce byte-identical
	// output for the same typed email. Defensive against direct test callers
	// and any future writer too.
	invitedEmail = strings.ToLower(invitedEmail)
	low, high := pairOrder(requesterID, recipientID)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Fast path: try to claim the (low, high) slot. RETURNING is empty
	// when another transaction already holds the row — fall through to
	// the existing-row branch below.
	inserted, err := scanFriendship(tx.QueryRow(ctx, `
		INSERT INTO friendships (user_low, user_high, status, requested_by, invited_email)
		VALUES ($1, $2, 'pending', $3, $4)
		ON CONFLICT (user_low, user_high) DO NOTHING
		RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email`,
		low, high, requesterID, invitedEmail))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return inserted, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Conflict: a row exists. Lock it and decide between no-op (already
	// accepted, or duplicate same-direction pending) and cross-direction
	// implicit accept.
	existing, err := scanFriendship(tx.QueryRow(ctx, `
		SELECT user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email
		FROM friendships WHERE user_low = $1 AND user_high = $2
		FOR UPDATE`,
		low, high))
	if err != nil {
		return nil, err
	}
	if existing.Status == "accepted" || existing.RequestedBy == requesterID {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}
	upd, err := scanFriendship(tx.QueryRow(ctx, `
		UPDATE friendships
		SET status = 'accepted', accepted_at = NOW()
		WHERE user_low = $1 AND user_high = $2
		RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email`,
		low, high))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return upd, nil
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
		RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email`,
		low, high, viewerID))
}

// RemoveFriendship deletes the row joining viewerID and otherID. Covers
// both unfriending an accepted edge and declining / cancelling a pending
// request. Returns ErrNotFound when no friendship row matched.
//
// Prior to Wave 3 this also cleared legacy flight_passengers / flight_shares
// grants linking the pair; those tables were dropped with the legacy flight
// surface. Plan-level visibility is trip-membership based and is not affected
// by unfriending, so no cross-table cleanup is needed here anymore.
func (s *Store) RemoveFriendship(ctx context.Context, viewerID, otherID int64) error {
	low, high := pairOrder(viewerID, otherID)

	var status string
	err := s.pool.QueryRow(ctx, `
		DELETE FROM friendships
		WHERE user_low = $1 AND user_high = $2
		  AND $3 IN (user_low, user_high)
		RETURNING status`,
		low, high, viewerID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
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

// CancelOutgoingInvite removes whatever outgoing pending invite inviterID
// has open for the given email — across both pending_friend_invites (for
// unknown targets) and friendships (for known targets where the inviter
// is requested_by). Returns nil even when nothing matched, so the handler
// can serve an identical 204 regardless of whether the target email
// belongs to a registered user.
func (s *Store) CancelOutgoingInvite(ctx context.Context, inviterID int64, email string) error {
	addr := strings.ToLower(strings.TrimSpace(email))
	if addr == "" {
		return errors.New("email required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_friend_invites
		 WHERE inviter_id = $1 AND email_lower = $2`,
		inviterID, addr); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM friendships
		 WHERE status = 'pending'
		   AND requested_by = $1
		   AND lower(invited_email) = $2`,
		inviterID, addr); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// consumePendingInvitesTx converts every pending_friend_invite addressed at
// any of the verified email addresses owned by userID into an accepted
// friendship. The DELETE and the read of the resulting inviter_ids are a
// single statement (DELETE ... RETURNING inside a CTE) so a concurrent
// INSERT into pending_friend_invites can't be silently dropped — under
// READ COMMITTED, the DELETE's snapshot determines exactly which rows we
// claim, and any invite that arrives after that stays queued for the
// next sign-in. Idempotent: an existing friendship in any state is
// promoted to accepted (the invitation implicitly accepts an outstanding
// cross-direction request); preserving accepted_at via COALESCE keeps the
// original acceptance timestamp.
//
// Returns the IDs of users who became newly-accepted friends so the caller
// can notify them. Self-invite rows (inviter_id == userID, possible if a
// user invited their own then-unverified address) are deleted by the CTE
// but excluded from the returned set.
func consumePendingInvitesTx(ctx context.Context, tx pgx.Tx, userID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		WITH claimed AS (
			DELETE FROM pending_friend_invites
			WHERE email_lower IN (
				SELECT lower(address) FROM user_emails
				WHERE user_id = $1 AND verified = TRUE)
			RETURNING inviter_id
		)
		SELECT DISTINCT inviter_id FROM claimed WHERE inviter_id <> $1`,
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
	if err := consumePendingSharesTx(ctx, tx, userID); err != nil {
		return nil, err
	}
	return inviters, nil
}

// CountIncomingFriendRequests returns the number of friendships in
// 'pending' state where viewerID is the recipient (not the requester).
// Used by /api/notifications and the SSE notifications.updated payload.
func (s *Store) CountIncomingFriendRequests(ctx context.Context, viewerID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM friendships
		WHERE status = 'pending'
		  AND requested_by <> $1
		  AND $1 IN (user_low, user_high)`,
		viewerID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
