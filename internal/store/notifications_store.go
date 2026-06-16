package store

import (
	"context"
	"time"
)

// Notification is one generic in-app inbox item (kind "share" today).
type Notification struct {
	ID        int64
	UserID    int64
	Kind      string
	ActorID   *int64
	TripID    *int64
	PlanID    *int64
	Message   string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// InsertNotification persists one notification row and returns it with
// ID/CreatedAt populated, so the caller can publish the persisted shape over
// SSE.
func (s *Store) InsertNotification(ctx context.Context, n Notification) (Notification, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, kind, actor_id, trip_id, plan_id, message)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at`,
		n.UserID, n.Kind, n.ActorID, n.TripID, n.PlanID, n.Message,
	).Scan(&n.ID, &n.CreatedAt)
	return n, err
}

// ListNotifications returns a user's most recent notifications, newest first,
// capped at limit.
func (s *Store) ListNotifications(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, kind, actor_id, trip_id, plan_id, message, created_at, read_at
		FROM notifications WHERE user_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.ActorID, &n.TripID,
			&n.PlanID, &n.Message, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkNotificationsRead stamps read_at on all of the user's unread notifications.
func (s *Store) MarkNotificationsRead(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}

// CountUnreadNotifications counts a user's unread notifications (for the badge).
func (s *Store) CountUnreadNotifications(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

// DeleteNotification removes a single notification, scoped to its owner so a
// user can't delete another's. A no-op (no error) when no such row exists.
func (s *Store) DeleteNotification(ctx context.Context, userID, id int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM notifications WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// DeleteAllNotifications removes all of a user's notifications (the inbox
// "clear all").
func (s *Store) DeleteAllNotifications(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID)
	return err
}
