package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const userEmailColumns = `id, user_id, address, verified, verify_token,
	verify_sent_at, verified_at, created_at, is_primary`

func scanUserEmail(row pgx.Row) (*UserEmail, error) {
	var e UserEmail
	if err := row.Scan(
		&e.ID, &e.UserID, &e.Address, &e.Verified,
		&e.VerifyToken, &e.VerifySentAt, &e.VerifiedAt, &e.CreatedAt, &e.IsPrimary,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

// UpsertVerifiedEmail inserts (or marks-verified) the given address for userID.
// Returns an error if the address is already owned by a different user.
func (s *Store) UpsertVerifiedEmail(ctx context.Context, userID int64, address string) error {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return errors.New("address required")
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO user_emails (user_id, address, verified, verified_at)
		VALUES ($1, $2, TRUE, NOW())
		ON CONFLICT (lower(address)) DO UPDATE
		SET verified = TRUE, verified_at = NOW(), verify_token = NULL
		WHERE user_emails.user_id = EXCLUDED.user_id`,
		userID, addr)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("address already owned by another user")
	}
	return nil
}

// UserByVerifiedEmail looks up a user by a case-insensitive match on a
// verified email address. Returns ErrNotFound if no verified row matches.
func (s *Store) UserByVerifiedEmail(ctx context.Context, address string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT `+prefixed(userColumns, "u.")+`
		FROM users u
		JOIN user_emails e ON e.user_id = u.id
		WHERE lower(e.address) = lower($1) AND e.verified = TRUE
		LIMIT 1`,
		address))
}

// SuperuserEmails returns the verified email addresses of every active
// superuser — the "admins" who receive operational alerts (e.g. an upstream
// API hitting its rate limit / quota). Addresses are de-duplicated and
// returned in a stable order; a superuser with no verified address simply
// doesn't appear. An empty result is not an error (the caller decides whether
// having nobody to notify is worth a warning).
func (s *Store) SuperuserEmails(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT e.address
		FROM users u
		JOIN user_emails e ON e.user_id = u.id
		WHERE u.is_superuser AND u.is_active AND e.verified
		ORDER BY e.address`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}
		out = append(out, addr)
	}
	return out, rows.Err()
}

// EmailsByUser returns all email rows for a user, newest first.
func (s *Store) EmailsByUser(ctx context.Context, userID int64) ([]*UserEmail, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userEmailColumns+`
		FROM user_emails WHERE user_id = $1 ORDER BY created_at DESC, id DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*UserEmail
	for rows.Next() {
		e, err := scanUserEmail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PrimaryEmail returns the address that user-facing notifications should be
// sent to: the user's explicit primary email if one is set and verified,
// otherwise their oldest verified address as a fallback. Returns ErrNotFound
// if the user has no verified email at all.
func (s *Store) PrimaryEmail(ctx context.Context, userID int64) (string, error) {
	var addr string
	err := s.pool.QueryRow(ctx,
		`SELECT address FROM user_emails
		WHERE user_id = $1 AND verified
		ORDER BY is_primary DESC, created_at ASC, id ASC
		LIMIT 1`,
		userID).Scan(&addr)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return addr, nil
}

// SetPrimaryEmail makes emailID the user's primary notification address,
// clearing any previous primary. The target must be a verified address owned by
// userID: ErrNotFound if it isn't theirs (or doesn't exist), ErrNotVerified if
// it exists but hasn't been verified. Idempotent — setting the current primary
// again is a no-op success.
func (s *Store) SetPrimaryEmail(ctx context.Context, userID, emailID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Promote the target only if it's owned by the user and verified. The
	// partial unique index (one primary per user) means we must clear the old
	// primary first, in the same transaction.
	if _, err := tx.Exec(ctx,
		`UPDATE user_emails SET is_primary = FALSE WHERE user_id = $1 AND is_primary`,
		userID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE user_emails SET is_primary = TRUE
		WHERE id = $1 AND user_id = $2 AND verified`,
		emailID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Nothing was promoted: either the row isn't the user's (or is missing),
		// or it exists but isn't verified. Distinguish the two for the caller.
		var verified bool
		qErr := tx.QueryRow(ctx,
			`SELECT verified FROM user_emails WHERE id = $1 AND user_id = $2`,
			emailID, userID).Scan(&verified)
		if errors.Is(qErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if qErr != nil {
			return qErr
		}
		return ErrNotVerified
	}
	return tx.Commit(ctx)
}

// generateToken returns a 32-byte cryptographically-random URL-safe token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ResendVerification regenerates verify_token and bumps verify_sent_at for
// an unverified row owned by userID. Returns ErrNotFound when the row
// doesn't exist or is owned by someone else; ErrAlreadyVerified when the
// row is already verified.
func (s *Store) ResendVerification(ctx context.Context, userID, emailID int64) (*UserEmail, string, error) {
	// First, fetch the row to distinguish "not yours" (ErrNotFound) from
	// "already verified" (ErrAlreadyVerified). The handler maps these to
	// different HTTP status codes.
	row, err := scanUserEmail(s.pool.QueryRow(ctx,
		`SELECT `+userEmailColumns+`
		FROM user_emails WHERE id = $1 AND user_id = $2`, emailID, userID))
	if err != nil {
		return nil, "", err
	}
	if row.Verified {
		return nil, "", ErrAlreadyVerified
	}
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	updated, err := scanUserEmail(s.pool.QueryRow(ctx, `
		UPDATE user_emails
		SET verify_token = $3, verify_sent_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING `+userEmailColumns,
		emailID, userID, token))
	if err != nil {
		return nil, "", err
	}
	return updated, token, nil
}

// VerifyEmailByToken flips an unverified row matching token to verified.
// Returns ErrNotFound when the token is unknown, already consumed, or
// older than 24 hours. The update is atomic — the token is cleared as
// part of the same statement so a second call returns ErrNotFound.
func (s *Store) VerifyEmailByToken(ctx context.Context, token string) (*UserEmail, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	row, err := scanUserEmail(s.pool.QueryRow(ctx, `
		UPDATE user_emails
		SET verified = TRUE, verified_at = NOW(), verify_token = NULL
		WHERE verify_token = $1
		  AND verify_sent_at > NOW() - INTERVAL '24 hours'
		RETURNING `+userEmailColumns,
		token))
	if err != nil {
		return nil, err
	}
	return row, nil
}

// DeleteUserEmail removes the row whose (id, user_id) match. Returns
// ErrNotFound when no row was deleted (unknown id or wrong owner).
func (s *Store) DeleteUserEmail(ctx context.Context, userID, emailID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_emails WHERE id = $1 AND user_id = $2`,
		emailID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertUnverifiedEmail inserts a new email row for userID with a fresh
// random verify_token and verify_sent_at = NOW(). Returns the row and the
// raw token (so the caller can embed it in a verification URL). The token
// is not exposed by the store outside of this return value.
//
// Returns ErrAddressTaken if the address (case-insensitive) is already
// owned by any user — including userID itself.
func (s *Store) InsertUnverifiedEmail(ctx context.Context, userID int64, address string) (*UserEmail, string, error) {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil, "", errors.New("address required")
	}
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	row, err := scanUserEmail(s.pool.QueryRow(ctx, `
		INSERT INTO user_emails (user_id, address, verified, verify_token, verify_sent_at)
		VALUES ($1, $2, FALSE, $3, NOW())
		RETURNING `+userEmailColumns,
		userID, addr, token))
	if err != nil {
		// Surface the unique-violation on lower(address) as ErrAddressTaken.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, "", ErrAddressTaken
		}
		return nil, "", err
	}
	return row, token, nil
}
