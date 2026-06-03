package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Trip is the top-level container: a set of plans, a membership/visibility
// scope, and a tag bucket. starts_on / ends_on are nullable hints; the
// effective span is usually derived from the trip's plan_parts.
type Trip struct {
	ID          int64
	Name        string
	Destination string
	StartsOn    *time.Time
	EndsOn      *time.Time
	CreatedBy   *int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// CountryCode is the trip's main country as a lowercase ISO 3166-1 alpha-2
	// code (derived by geocoding the destination). "" = not yet derived; "zz" =
	// derived but no country found.
	CountryCode string
	// EffectiveStart/EffectiveEnd are the min/max of the trip's (non-dismissed)
	// part instants — used to show inferred dates when StartsOn/EndsOn aren't
	// set. Only populated by ListTrips (the detail payload carries parts, so the
	// client derives it there).
	EffectiveStart *time.Time
	EffectiveEnd   *time.Time
}

// TripMember is one (trip, user, role) edge — the sharing boundary. Role is
// one of owner / editor / viewer (DB CHECK).
type TripMember struct {
	TripID  int64
	UserID  int64
	Role    string
	AddedAt time.Time
}

// CreateTripPayload carries the fields a caller may set when creating a trip.
type CreateTripPayload struct {
	Name        string
	Destination string
	StartsOn    *time.Time
	EndsOn      *time.Time
	// TripItID is the source TripIt trip id when this trip comes from an .ics
	// import; empty otherwise. It makes re-import idempotent (see
	// TripByTripItID).
	TripItID string
}

// UpdateTripPayload carries the optionally-set fields of a trip edit. A nil
// pointer means "leave this column untouched".
type UpdateTripPayload struct {
	Name        *string
	Destination *string
	StartsOn    *time.Time
	EndsOn      *time.Time
}

// tripColumns is the shared SELECT list for trip rows.
const tripColumns = `id, name, destination, starts_on, ends_on, created_by, created_at, updated_at, country_code`

func scanTrip(row pgx.Row) (*Trip, error) {
	var t Trip
	err := row.Scan(&t.ID, &t.Name, &t.Destination, &t.StartsOn, &t.EndsOn,
		&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// listTripsSelect is the shared SELECT for trip-list queries: the trip columns
// plus the effective start/end inferred from plan parts. Callers append a WHERE
// clause + ORDER BY.
var listTripsSelect = `
	SELECT ` + prefixed(tripColumns, "t.") + `,
		(SELECT min(p.starts_at) FROM plan_parts p
		   JOIN plans pl ON pl.id = p.plan_id
		  WHERE pl.trip_id = t.id AND p.dismissed_at IS NULL),
		(SELECT max(COALESCE(p.ends_at, p.starts_at)) FROM plan_parts p
		   JOIN plans pl ON pl.id = p.plan_id
		  WHERE pl.trip_id = t.id AND p.dismissed_at IS NULL)
	FROM trips t`

func (s *Store) scanTripList(rows pgx.Rows) ([]*Trip, error) {
	defer rows.Close()
	var out []*Trip
	for rows.Next() {
		var t Trip
		if err := rows.Scan(&t.ID, &t.Name, &t.Destination, &t.StartsOn, &t.EndsOn,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode,
			&t.EffectiveStart, &t.EffectiveEnd); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// ListTrips returns the trips the viewer can see (member of, or owner),
// newest-updated first.
func (s *Store) ListTrips(ctx context.Context, viewerID int64) ([]*Trip, error) {
	rows, err := s.pool.Query(ctx, listTripsSelect+`
		WHERE t.created_by = $1
		   OR EXISTS (SELECT 1 FROM trip_members tm
		              WHERE tm.trip_id = t.id AND tm.user_id = $1)
		ORDER BY t.updated_at DESC, t.id DESC`, viewerID)
	if err != nil {
		return nil, err
	}
	return s.scanTripList(rows)
}

// ListFriendsTrips returns every trip owned by one of the viewer's accepted
// friends — including trips not shared with the viewer. Superuser diagnostics
// only (gated in the handler).
func (s *Store) ListFriendsTrips(ctx context.Context, viewerID int64) ([]*Trip, error) {
	rows, err := s.pool.Query(ctx, listTripsSelect+`
		WHERE t.created_by IN (
			SELECT CASE WHEN user_low = $1 THEN user_high ELSE user_low END
			FROM friendships
			WHERE (user_low = $1 OR user_high = $1) AND status = 'accepted'
		)
		ORDER BY t.updated_at DESC, t.id DESC`, viewerID)
	if err != nil {
		return nil, err
	}
	return s.scanTripList(rows)
}

// ListAllTrips returns every trip in the system. Superuser diagnostics only
// (gated in the handler).
func (s *Store) ListAllTrips(ctx context.Context) ([]*Trip, error) {
	rows, err := s.pool.Query(ctx, listTripsSelect+`
		ORDER BY t.updated_at DESC, t.id DESC`)
	if err != nil {
		return nil, err
	}
	return s.scanTripList(rows)
}

// TripByID returns a single trip by id.
func (s *Store) TripByID(ctx context.Context, id int64) (*Trip, error) {
	return scanTrip(s.pool.QueryRow(ctx,
		`SELECT `+tripColumns+` FROM trips WHERE id = $1`, id))
}

// TripByTripItID returns the caller's trip imported from the given TripIt trip
// id, or ErrNotFound. Used to reuse a trip on re-import instead of duplicating
// it. tripitID must be non-empty.
func (s *Store) TripByTripItID(ctx context.Context, createdBy int64, tripitID string) (*Trip, error) {
	return scanTrip(s.pool.QueryRow(ctx,
		`SELECT `+tripColumns+` FROM trips WHERE created_by = $1 AND tripit_id = $2`,
		createdBy, tripitID))
}

// SetTripCountry sets the derived ISO country code (does not bump updated_at, so
// a background derivation doesn't reorder the trip list).
func (s *Store) SetTripCountry(ctx context.Context, tripID int64, code string) error {
	_, err := s.pool.Exec(ctx, `UPDATE trips SET country_code = $2 WHERE id = $1`, tripID, code)
	return err
}

// TripsNeedingCountry returns trips whose country hasn't been derived yet but
// which have something to geocode (a destination or at least a name). Used by
// the startup backfill.
func (s *Store) TripsNeedingCountry(ctx context.Context) ([]*Trip, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tripColumns+` FROM trips
		 WHERE country_code = '' AND (destination <> '' OR name <> '')
		 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Trip
	for rows.Next() {
		var t Trip
		if err := rows.Scan(&t.ID, &t.Name, &t.Destination, &t.StartsOn, &t.EndsOn,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// CreateTrip inserts a trip and an owner trip_members row for createdBy, in one
// transaction.
func (s *Store) CreateTrip(ctx context.Context, in CreateTripPayload, createdBy int64) (*Trip, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	t, err := scanTrip(tx.QueryRow(ctx, `
		INSERT INTO trips (name, destination, starts_on, ends_on, created_by, tripit_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+tripColumns,
		in.Name, in.Destination, in.StartsOn, in.EndsOn, createdBy, in.TripItID))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`,
		t.ID, createdBy); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateTrip applies the supplied fields to a trip. nil pointers are left
// untouched. ends_on/starts_on are set unconditionally when their pointer is
// non-nil (so a caller may also clear them by passing a zero-valued *time —
// the handler distinguishes "absent" from "present").
func (s *Store) UpdateTrip(ctx context.Context, id int64, in UpdateTripPayload) (*Trip, error) {
	t, err := scanTrip(s.pool.QueryRow(ctx, `
		UPDATE trips SET
			name        = COALESCE($2, name),
			destination = COALESCE($3, destination),
			starts_on   = CASE WHEN $4::boolean THEN $5 ELSE starts_on END,
			ends_on     = CASE WHEN $6::boolean THEN $7 ELSE ends_on END,
			updated_at  = NOW()
		WHERE id = $1
		RETURNING `+tripColumns,
		id, in.Name, in.Destination,
		in.StartsOn != nil, in.StartsOn,
		in.EndsOn != nil, in.EndsOn))
	return t, err
}

// DeleteTrip removes a trip and (via cascade) its plans, parts, and members.
func (s *Store) DeleteTrip(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM trips WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TripMembers returns the membership rows for a trip, ordered by role then
// user id for stable output.
func (s *Store) TripMembers(ctx context.Context, tripID int64) ([]*TripMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT trip_id, user_id, role, added_at
		FROM trip_members
		WHERE trip_id = $1
		ORDER BY
			CASE role WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END,
			user_id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TripMember
	for rows.Next() {
		var m TripMember
		if err := rows.Scan(&m.TripID, &m.UserID, &m.Role, &m.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// AddTripMember inserts or updates a (trip, user) membership at the given role.
func (s *Store) AddTripMember(ctx context.Context, tripID, userID int64, role string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO trip_members (trip_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (trip_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		tripID, userID, role)
	return err
}

// RemoveTripMember drops a (trip, user) membership.
func (s *Store) RemoveTripMember(ctx context.Context, tripID, userID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM trip_members WHERE trip_id = $1 AND user_id = $2`, tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TripRole returns the viewer's role on the trip ("owner"|"editor"|"viewer"),
// or ErrNotFound if they are not a member.
func (s *Store) TripRole(ctx context.Context, tripID, viewerID int64) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM trip_members WHERE trip_id = $1 AND user_id = $2`,
		tripID, viewerID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

// CanEditTrip reports whether the viewer may mutate the trip's plans/parts
// (owner or editor). A non-member returns (false, nil); a missing trip also
// returns (false, nil) — callers that need to distinguish "no such trip" use
// TripByID first.
func (s *Store) CanEditTrip(ctx context.Context, tripID, viewerID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM trip_members
			WHERE trip_id = $1 AND user_id = $2 AND role IN ('owner','editor')
		)`, tripID, viewerID).Scan(&ok)
	return ok, err
}

// CanViewTrip reports whether the viewer is on the trip in any role, or owns
// it. Used to gate the trip-detail read.
func (s *Store) CanViewTrip(ctx context.Context, tripID, viewerID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM trips t
			WHERE t.id = $1
			  AND (t.created_by = $2
			    OR EXISTS (SELECT 1 FROM trip_members tm
			               WHERE tm.trip_id = t.id AND tm.user_id = $2))
		)`, tripID, viewerID).Scan(&ok)
	return ok, err
}
