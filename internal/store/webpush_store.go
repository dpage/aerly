package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// WebPushSubscription is one device's push subscription: the endpoint URL the
// browser's push service handed us plus the client encryption keys. The failure
// bookkeeping drives pruning of dead/flaky subscriptions in the push sender.
type WebPushSubscription struct {
	ID           int64
	UserID       int64
	Endpoint     string
	P256dh       string
	Auth         string
	UserAgent    string
	FailureCount int
}

// UpsertWebPushSubscription registers (or refreshes) a subscription, keyed on
// the globally-unique endpoint. If the same endpoint reappears — the browser
// re-subscribed, or a different user signed in on that device — the row is
// reassigned to the current user and its keys/failure state are reset. Returns
// the stored row's ID.
func (s *Store) UpsertWebPushSubscription(ctx context.Context, sub WebPushSubscription) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO webpush_subscriptions (user_id, endpoint, p256dh, auth, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (endpoint) DO UPDATE
		SET user_id = EXCLUDED.user_id,
		    p256dh = EXCLUDED.p256dh,
		    auth = EXCLUDED.auth,
		    user_agent = EXCLUDED.user_agent,
		    last_failure_at = NULL,
		    failure_count = 0
		RETURNING id`,
		sub.UserID, sub.Endpoint, sub.P256dh, sub.Auth, sub.UserAgent).Scan(&id)
	return id, err
}

// WebPushSubscriptionsFor returns all of a user's device subscriptions, oldest
// first. Empty (not an error) when the user has none.
func (s *Store) WebPushSubscriptionsFor(ctx context.Context, userID int64) ([]WebPushSubscription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, endpoint, p256dh, auth, user_agent, failure_count
		FROM webpush_subscriptions WHERE user_id = $1
		ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebPushSubscription
	for rows.Next() {
		var sub WebPushSubscription
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.Endpoint, &sub.P256dh,
			&sub.Auth, &sub.UserAgent, &sub.FailureCount); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// DeleteWebPushSubscriptionByEndpoint removes a subscription by endpoint, scoped
// to its owner so one user can't unregister another's device. A no-op (no error)
// when no such row exists.
func (s *Store) DeleteWebPushSubscriptionByEndpoint(ctx context.Context, userID int64, endpoint string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM webpush_subscriptions WHERE user_id = $1 AND endpoint = $2`,
		userID, endpoint)
	return err
}

// DeleteWebPushSubscription removes a subscription by ID. Used by the sender to
// prune a subscription the push service reported as permanently gone (404/410).
func (s *Store) DeleteWebPushSubscription(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webpush_subscriptions WHERE id = $1`, id)
	return err
}

// MarkWebPushSuccess records a successful send: stamps last_success_at and
// clears the transient-failure counter.
func (s *Store) MarkWebPushSuccess(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webpush_subscriptions
		 SET last_success_at = now(), last_failure_at = NULL, failure_count = 0
		 WHERE id = $1`, id)
	return err
}

// BumpWebPushFailure records a transient send failure and returns the new
// failure count, so the sender can prune a subscription that keeps failing.
func (s *Store) BumpWebPushFailure(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`UPDATE webpush_subscriptions
		 SET last_failure_at = now(), failure_count = failure_count + 1
		 WHERE id = $1
		 RETURNING failure_count`, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return n, err
}

// PushKindEnabled reports whether a user wants push for a given kind. The
// absence of a row means enabled, so granting permission opts the user into
// every kind until they explicitly toggle one off.
func (s *Store) PushKindEnabled(ctx context.Context, userID int64, kind string) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx,
		`SELECT enabled FROM push_kind_prefs WHERE user_id = $1 AND kind = $2`,
		userID, kind).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return enabled, nil
}

// PushKindPrefsFor returns the user's explicit per-kind settings as a map. Kinds
// without a row are absent from the map and default to enabled (see
// PushKindEnabled); the API layer fills defaults for the known kinds.
func (s *Store) PushKindPrefsFor(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT kind, enabled FROM push_kind_prefs WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var kind string
		var enabled bool
		if err := rows.Scan(&kind, &enabled); err != nil {
			return nil, err
		}
		out[kind] = enabled
	}
	return out, rows.Err()
}

// SetPushKindPref upserts a single per-kind push setting for a user.
func (s *Store) SetPushKindPref(ctx context.Context, userID int64, kind string, enabled bool) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO push_kind_prefs (user_id, kind, enabled)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, kind) DO UPDATE SET enabled = EXCLUDED.enabled`,
		userID, kind, enabled)
	return err
}
