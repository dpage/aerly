package store

import "context"

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
