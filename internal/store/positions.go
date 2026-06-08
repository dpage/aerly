package store

import (
	"context"
	"errors"
	"time"

	"github.com/dpage/aerly/internal/geo"
	"github.com/jackc/pgx/v5"
)

// Positions re-key (spec §3.1, §7; plan §5 1C).
//
// positions now key on plan_part_id rather than flight_id (the 0010 migration
// added the column and dropped the flights FK; 0011 relaxes the legacy NOT NULL
// flight_id). These helpers are the part-keyed home of what used to live in
// flights.go: the parameter named flightID throughout is now a plan_part_id,
// and Position.FlightID likewise carries the plan_part_id. The method names and
// signatures are unchanged so the poller and the providers' RealPositionFetcher
// / LatestPositionFetcher interfaces re-key by table, not by API — a mechanical
// flight_id → plan_part_id swap with no behavioural change.
//
// The provider/poller code passes Flight.ID (now a plan_part_id) into these,
// and reads Position.FlightID back as the same plan_part_id.

const positionColumns = `plan_part_id, ts, lat, lon, altitude_ft, groundspeed_kt, heading_deg, is_estimated`

func scanPosition(row pgx.Row) (*Position, error) {
	var p Position
	if err := row.Scan(&p.FlightID, &p.Ts, &p.Lat, &p.Lon,
		&p.AltitudeFt, &p.GroundspeedKt, &p.HeadingDeg, &p.IsEstimated); err != nil {
		return nil, err
	}
	return &p, nil
}

// InsertPartPosition appends a position sample for a flight part.
// Position.FlightID is the plan_part_id.
func (s *Store) InsertPartPosition(ctx context.Context, p Position) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO positions (plan_part_id, ts, lat, lon, altitude_ft, groundspeed_kt, heading_deg, is_estimated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.FlightID, p.Ts, p.Lat, p.Lon, p.AltitudeFt, p.GroundspeedKt, p.HeadingDeg, p.IsEstimated)
	return err
}

// SmoothEstimatedTrack re-lays the dead-reckoned (is_estimated) positions that
// precede a freshly-landed real fix onto a smooth great-circle, removing the
// dog-leg that appears when ADS-B coverage returns: the dead-reckoner always
// extrapolates toward the destination, so when a real fix arrives off that line
// the flown track kinks sharply from the last estimate to the true position. A
// smoothed path is far more likely to reflect where the aircraft actually flew.
//
// The smoothing anchor is whichever solid reference point is geographically
// closer to the new fix: the flight's origin (at its departure time) or the
// last real fix before this one. Every estimated sample timestamped strictly
// between the anchor and the fix is moved onto the great-circle from the anchor
// to the fix, parameterised by its share of the elapsed time, and its heading
// re-pointed along that path. Real fixes are never moved and the samples stay
// flagged is_estimated. fix must be the real position that just landed
// (is_estimated == false); a no-op otherwise.
func (s *Store) SmoothEstimatedTrack(ctx context.Context, f *Flight, fix Position) error {
	if fix.IsEstimated {
		return nil
	}

	// Candidate anchor 1: the last real fix before this one (the start of the
	// coverage gap the dead-reckoner filled).
	var aLat, aLon float64
	var aTs time.Time
	haveAnchor := false
	if prev, err := s.lastRealPositionBefore(ctx, fix.FlightID, fix.Ts); err != nil {
		return err
	} else if prev != nil {
		aLat, aLon, aTs = prev.Lat, prev.Lon, prev.Ts
		haveAnchor = true
	}

	// Candidate anchor 2: the origin, at the flight's departure time. Wins when
	// it's closer to the new fix than the last real fix — or when there's no
	// real fix yet (the track so far is schedule-based dead reckoning from the
	// origin, per DeadReckoner step 3).
	if f.OriginLat != nil && f.OriginLon != nil {
		dep := f.ScheduledOut
		if f.ActualOut != nil {
			dep = *f.ActualOut
		} else if f.EstimatedOut != nil {
			dep = *f.EstimatedOut
		}
		closer := !haveAnchor ||
			geo.HaversineNM(fix.Lat, fix.Lon, *f.OriginLat, *f.OriginLon) <
				geo.HaversineNM(fix.Lat, fix.Lon, aLat, aLon)
		if closer {
			aLat, aLon, aTs = *f.OriginLat, *f.OriginLon, dep
			haveAnchor = true
		}
	}

	// No solid anchor, or no time span between it and the fix to interpolate
	// over — nothing to smooth.
	if !haveAnchor || !aTs.Before(fix.Ts) {
		return nil
	}
	span := fix.Ts.Sub(aTs).Seconds()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, ts FROM positions
		WHERE plan_part_id = $1 AND is_estimated = TRUE AND ts > $2 AND ts < $3
		ORDER BY ts`, fix.FlightID, aTs, fix.Ts)
	if err != nil {
		return err
	}
	type est struct {
		id int64
		ts time.Time
	}
	var ests []est
	for rows.Next() {
		var e est
		if err := rows.Scan(&e.id, &e.ts); err != nil {
			rows.Close()
			return err
		}
		ests = append(ests, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, e := range ests {
		frac := e.ts.Sub(aTs).Seconds() / span
		lat, lon := geo.Slerp(aLat, aLon, fix.Lat, fix.Lon, frac)
		hdg := int16(geo.Bearing(lat, lon, fix.Lat, fix.Lon))
		if _, err := tx.Exec(ctx, `
			UPDATE positions SET lat = $2, lon = $3, heading_deg = $4 WHERE id = $1`,
			e.id, lat, lon, hdg); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// lastRealPositionBefore returns the most recent NON-estimated position for a
// flight part strictly older than ts, or (nil, nil) when there isn't one. It
// backs SmoothEstimatedTrack's "last real fix" anchor (the new fix is already
// persisted, so plain LatestRealPosition would return the fix itself).
func (s *Store) lastRealPositionBefore(ctx context.Context, partID int64, ts time.Time) (*Position, error) {
	p, err := scanPosition(s.pool.QueryRow(ctx, `
		SELECT `+positionColumns+`
		FROM positions
		WHERE plan_part_id = $1 AND is_estimated = FALSE AND ts < $2
		ORDER BY ts DESC
		LIMIT 1`, partID, ts))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // genuine "no earlier real fix"
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// LatestRealPosition returns the most recent NON-estimated (ADS-B / airline)
// position for a flight part, or (nil, nil) if there isn't one. The
// dead-reckoner uses it as its extrapolation anchor. partID is a plan_part_id.
func (s *Store) LatestRealPosition(ctx context.Context, partID int64) (*Position, error) {
	p, err := scanPosition(s.pool.QueryRow(ctx, `
		SELECT `+positionColumns+`
		FROM positions
		WHERE plan_part_id = $1 AND is_estimated = FALSE
		ORDER BY ts DESC
		LIMIT 1`, partID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // genuine "no data yet"
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// LatestPosition returns the most recent position for a single flight part,
// real or dead-reckoned. Returns (nil, nil) when there are none. partID is a
// plan_part_id.
func (s *Store) LatestPosition(ctx context.Context, partID int64) (*Position, error) {
	p, err := scanPosition(s.pool.QueryRow(ctx, `
		SELECT `+positionColumns+`
		FROM positions
		WHERE plan_part_id = $1
		ORDER BY ts DESC
		LIMIT 1`, partID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // genuine "no data yet"
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// LatestPartPositions returns the latest position per flight part for the given
// plan_part_ids. The map is keyed by plan_part_id.
func (s *Store) LatestPartPositions(ctx context.Context, partIDs []int64) (map[int64]*Position, error) {
	out := map[int64]*Position{}
	if len(partIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (plan_part_id)
			`+positionColumns+`
		FROM positions
		WHERE plan_part_id = ANY($1)
		ORDER BY plan_part_id, ts DESC`, partIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		out[p.FlightID] = p
	}
	return out, rows.Err()
}

// PartTracks returns up to perPart of the most recent positions per part
// (oldest first within each entry) for the given plan_part_ids — used to draw
// the flown-so-far polyline. The map is keyed by plan_part_id.
func (s *Store) PartTracks(ctx context.Context, partIDs []int64, perPart int) (map[int64][]*Position, error) {
	out := map[int64][]*Position{}
	if len(partIDs) == 0 {
		return out, nil
	}
	if perPart <= 0 {
		perPart = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+positionColumns+`
		FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY plan_part_id ORDER BY ts DESC) AS rn
			FROM positions
			WHERE plan_part_id = ANY($1)
		) ranked
		WHERE rn <= $2
		ORDER BY plan_part_id, ts ASC`, partIDs, perPart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		out[p.FlightID] = append(out[p.FlightID], p)
	}
	return out, rows.Err()
}

// PositionsForFlight returns all positions for a single flight part, newest
// first. partID is a plan_part_id.
func (s *Store) PositionsForFlight(ctx context.Context, partID int64, limit int) ([]*Position, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+positionColumns+`
		FROM positions
		WHERE plan_part_id = $1
		ORDER BY ts DESC
		LIMIT $2`, partID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Position
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
