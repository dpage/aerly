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
	// ShareAllFriendsRole is "" (off), "viewer", or "editor": the default trip
	// role granted to every accepted friend of the owner (read-time computed).
	ShareAllFriendsRole string
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
		&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode, &t.ShareAllFriendsRole)
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
	SELECT ` + prefixed(tripColumns, "t.") + `, COALESCE(t.share_all_friends_role, ''),
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
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode, &t.ShareAllFriendsRole,
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
		   OR (
		        EXISTS (SELECT 1 FROM friendships f
		                WHERE f.status = 'accepted'
		                  AND f.user_low = LEAST(t.created_by, $1)
		                  AND f.user_high = GREATEST(t.created_by, $1))
		        AND (
		             EXISTS (SELECT 1 FROM trip_members tm
		                     WHERE tm.trip_id = t.id AND tm.user_id = $1)
		          OR t.share_all_friends_role IS NOT NULL
		          OR EXISTS (
		               SELECT 1 FROM plans pl
		               WHERE pl.trip_id = t.id
		                 AND (
		                      pl.created_by = $1
		                   OR pl.share_all_friends
		                   OR EXISTS (SELECT 1 FROM plan_passengers pp
		                              WHERE pp.plan_id = pl.id AND pp.user_id = $1)
		                   OR EXISTS (SELECT 1 FROM plan_visibility pv
		                              JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                              WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
		                                AND m.user_id = $1)
		                     )
		             )
		           )
		      )
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
		`SELECT `+tripColumns+`, COALESCE(share_all_friends_role, '') FROM trips WHERE id = $1`, id))
}

// TripByTripItID returns the caller's trip imported from the given TripIt trip
// id, or ErrNotFound. Used to reuse a trip on re-import instead of duplicating
// it. tripitID must be non-empty.
func (s *Store) TripByTripItID(ctx context.Context, createdBy int64, tripitID string) (*Trip, error) {
	return scanTrip(s.pool.QueryRow(ctx,
		`SELECT `+tripColumns+`, COALESCE(share_all_friends_role, '') FROM trips WHERE created_by = $1 AND tripit_id = $2`,
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
		`SELECT `+tripColumns+`, COALESCE(share_all_friends_role, '') FROM trips
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
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.CountryCode, &t.ShareAllFriendsRole); err != nil {
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
		RETURNING `+tripColumns+`, COALESCE(share_all_friends_role, '')`,
		in.Name, in.Destination, in.StartsOn, in.EndsOn, createdBy, in.TripItID))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`,
		t.ID, createdBy); err != nil {
		return nil, err
	}
	// Apply the owner's "always share with" defaults to the new trip.
	if err := applyAutoSharesTx(ctx, tx, t.ID, createdBy); err != nil {
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
		RETURNING `+tripColumns+`, COALESCE(share_all_friends_role, '')`,
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the trip so the owner-count check and delete are atomic against a
	// concurrent role change / member removal that could also drop an owner.
	if err := lockTripTx(ctx, tx, tripID); err != nil {
		return err
	}
	// Refuse to remove the trip's last owner — that would leave no non-superuser
	// able to manage it (a self-inflicted lockout).
	var removingOwner bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM trip_members
		                 WHERE trip_id = $1 AND user_id = $2 AND role = 'owner')`,
		tripID, userID).Scan(&removingOwner); err != nil {
		return err
	}
	if removingOwner {
		var owners int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM trip_members WHERE trip_id = $1 AND role = 'owner'`,
			tripID).Scan(&owners); err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	tag, err := tx.Exec(ctx,
		`DELETE FROM trip_members WHERE trip_id = $1 AND user_id = $2`, tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// TripSpan is the effective date span of a trip computed from its non-dismissed
// plan parts: Start = min(starts_at), End = max(coalesce(ends_at, starts_at)).
type TripSpan struct {
	Start time.Time
	End   time.Time
}

// TripPartSpans returns, in one query, the part-derived span for every trip the
// user owns or is a member of that has at least one non-dismissed dated part.
// Trips with no dated parts are simply absent from the map (callers fall back to
// trips.starts_on/ends_on). This replaces a per-trip PlansByTrip+PartsByPlan
// fan-out when auto-selecting a trip for an ingested booking.
func (s *Store) TripPartSpans(ctx context.Context, userID int64) (map[int64]TripSpan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pl.trip_id,
		       min(part.starts_at)                              AS span_start,
		       max(COALESCE(part.ends_at, part.starts_at))      AS span_end
		  FROM plans pl
		  JOIN plan_parts part ON part.plan_id = pl.id AND part.dismissed_at IS NULL
		 WHERE EXISTS (SELECT 1 FROM trip_members tm
		                WHERE tm.trip_id = pl.trip_id AND tm.user_id = $1)
		 GROUP BY pl.trip_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]TripSpan{}
	for rows.Next() {
		var tripID int64
		var sp TripSpan
		if err := rows.Scan(&tripID, &sp.Start, &sp.End); err != nil {
			return nil, err
		}
		out[tripID] = sp
	}
	return out, rows.Err()
}

// lockTripTx serializes trip-passenger materialisation against concurrent plan
// creation / visibility changes for the same trip (so a new plan and a new
// passenger can't cross and miss each other). All paths that reconcile a trip's
// via_trip plan_passengers take this row lock first.
func lockTripTx(ctx context.Context, tx pgx.Tx, tripID int64) error {
	_, err := tx.Exec(ctx, `SELECT 1 FROM trips WHERE id = $1 FOR UPDATE`, tripID)
	return err
}

// AddTripPassenger records userID as a trip-level passenger (issue #20): a
// traveller on the whole trip. They become a trip viewer (so an empty trip is
// still visible to them and it surfaces under their "My trips", issue #19) and
// are materialised as a passenger on every plan they're allowed to see — plans
// hidden from them (plan_visibility) are skipped, so hiding still works. Manual
// per-plan passenger rows are left untouched. Idempotent.
func (s *Store) AddTripPassenger(ctx context.Context, tripID, userID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := lockTripTx(ctx, tx, tripID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO trip_passengers (trip_id, user_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`, tripID, userID); err != nil {
		return err
	}
	// Become a trip viewer directly (don't rely on the plan_passengers trigger,
	// which wouldn't fire for a trip with no plans).
	if _, err := tx.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'viewer')
		 ON CONFLICT DO NOTHING`, tripID, userID); err != nil {
		return err
	}
	// Materialise onto the plans they may see; ON CONFLICT keeps a pre-existing
	// (possibly manual) row as-is.
	if _, err := tx.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id, via_trip)
		 SELECT id, $2, true FROM plans
		  WHERE trip_id = $1 AND plan_visible_to_member(id, $2)
		 ON CONFLICT (plan_id, user_id) DO NOTHING`, tripID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveTripPassenger removes a trip-level passenger: it drops the
// trip_passengers row, only the plan_passengers rows it materialised
// (via_trip = true — manual per-plan assignments are preserved), and — once
// nothing else ties the user to the trip — their auto-created viewer
// membership. Owners/editors keep their membership. Returns ErrNotFound when
// the user wasn't a trip passenger.
func (s *Store) RemoveTripPassenger(ctx context.Context, tripID, userID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := lockTripTx(ctx, tx, tripID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`DELETE FROM trip_passengers WHERE trip_id = $1 AND user_id = $2`, tripID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM plan_passengers
		  WHERE user_id = $2 AND via_trip = true
		    AND plan_id IN (SELECT id FROM plans WHERE trip_id = $1)`, tripID, userID); err != nil {
		return err
	}
	// Drop the membership the passenger add created, but only when the user is
	// just a viewer with nothing else keeping them on the trip (no remaining
	// plan_passengers — e.g. a manual per-plan assignment). Owners/editors are
	// untouched.
	if _, err := tx.Exec(ctx,
		`DELETE FROM trip_members
		  WHERE trip_id = $1 AND user_id = $2 AND role = 'viewer'
		    AND NOT EXISTS (
		      SELECT 1 FROM plan_passengers pp
		      JOIN plans pl ON pl.id = pp.plan_id
		      WHERE pl.trip_id = $1 AND pp.user_id = $2)`, tripID, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TripPassengers returns the user ids of the trip's trip-level passengers.
func (s *Store) TripPassengers(ctx context.Context, tripID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id FROM trip_passengers WHERE trip_id = $1 ORDER BY user_id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
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

// CanViewTrip reports whether the viewer may open the trip-detail read. The
// owner always can. Otherwise the viewer must be an accepted friend of the
// owner AND hold any grant on the trip: a trip_members row, a trip-level
// share_all_friends_role, or a plan-scoped grant (plan ownership, passenger,
// plan-level share_all_friends, or an only_visible_to membership). This mirrors
// the friend-gated, any-grant "tile" rule used by ListTrips / sees_trip.
func (s *Store) CanViewTrip(ctx context.Context, tripID, viewerID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM trips t
			WHERE t.id = $1
			  AND (
			       t.created_by = $2
			    OR (
			         EXISTS (SELECT 1 FROM friendships f
			                 WHERE f.status = 'accepted'
			                   AND f.user_low = LEAST(t.created_by, $2)
			                   AND f.user_high = GREATEST(t.created_by, $2))
			         AND (
			              EXISTS (SELECT 1 FROM trip_members tm
			                      WHERE tm.trip_id = t.id AND tm.user_id = $2)
			           OR t.share_all_friends_role IS NOT NULL
			           OR EXISTS (
			                SELECT 1 FROM plans pl
			                WHERE pl.trip_id = t.id
			                  AND (
			                       pl.created_by = $2
			                    OR pl.share_all_friends
			                    OR EXISTS (SELECT 1 FROM plan_passengers pp
			                               WHERE pp.plan_id = pl.id AND pp.user_id = $2)
			                    OR EXISTS (SELECT 1 FROM plan_visibility pv
			                               JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
			                               WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
			                                 AND m.user_id = $2)
			                     )
			              )
			         )
			       )
			  )
		)`, tripID, viewerID).Scan(&ok)
	return ok, err
}

// VisibleTripUserIDs returns every user who can currently see trip tripID under
// the friend-gated tile rule (the same rule as CanViewTrip / ListTrips). Used
// to scope the trip.updated SSE event so all-friends and plan-scoped viewers —
// who have no trip_members row — still get live updates.
func (s *Store) VisibleTripUserIDs(ctx context.Context, tripID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id FROM users u
		JOIN trips t ON t.id = $1
		WHERE
		     u.id = t.created_by
		  OR (
		       EXISTS (SELECT 1 FROM friendships f
		               WHERE f.status = 'accepted'
		                 AND f.user_low = LEAST(t.created_by, u.id)
		                 AND f.user_high = GREATEST(t.created_by, u.id))
		       AND (
		            EXISTS (SELECT 1 FROM trip_members tm WHERE tm.trip_id = t.id AND tm.user_id = u.id)
		         OR t.share_all_friends_role IS NOT NULL
		         OR EXISTS (
		              SELECT 1 FROM plans pl
		              WHERE pl.trip_id = t.id
		                AND (
		                     pl.created_by = u.id
		                  OR pl.share_all_friends
		                  OR EXISTS (SELECT 1 FROM plan_passengers pp WHERE pp.plan_id = pl.id AND pp.user_id = u.id)
		                  OR EXISTS (SELECT 1 FROM plan_visibility pv
		                             JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                             WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to' AND m.user_id = u.id)
		                   )
		            )
		          )
		     )`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
