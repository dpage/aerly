package store

import (
	"context"
	"fmt"
	"time"
)

// FloatingEnd is one part end whose zone is not yet known and whose stored
// instant is local wall-clock digits (source 'floating').
type FloatingEnd struct {
	PartID, TripID int64
	End            string // "start" or "end"
	At             time.Time
	PlanType       string
	IATA           string
	Lat, Lon       *float64
}

// FloatingEnds returns every non-dismissed part end with source 'floating'
// (an end only when it has a time). The coordinates are the end's own; an end
// missing either coordinate falls back to the start's as ResolvePartTZ does,
// except that an end with a destination airport code does not, since the
// start's coordinates would be the origin's and the code names the right zone.
func (s *Store) FloatingEnds(ctx context.Context) ([]FloatingEnd, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT part.id, pl.trip_id, 'start', part.starts_at, pl.type,
		       COALESCE(fd.origin_iata, ''), part.start_lat, part.start_lon
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		LEFT JOIN flight_details fd ON fd.plan_part_id = part.id
		WHERE part.dismissed_at IS NULL AND part.start_tz_source = 'floating'
		UNION ALL
		SELECT part.id, pl.trip_id, 'end', part.ends_at, pl.type,
		       COALESCE(fd.dest_iata, ''),
		       CASE WHEN part.end_lat IS NOT NULL AND part.end_lon IS NOT NULL THEN part.end_lat
		            WHEN COALESCE(fd.dest_iata, '') <> '' THEN NULL
		            ELSE part.start_lat END,
		       CASE WHEN part.end_lat IS NOT NULL AND part.end_lon IS NOT NULL THEN part.end_lon
		            WHEN COALESCE(fd.dest_iata, '') <> '' THEN NULL
		            ELSE part.start_lon END
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		LEFT JOIN flight_details fd ON fd.plan_part_id = part.id
		WHERE part.dismissed_at IS NULL AND part.end_tz_source = 'floating'
		  AND part.ends_at IS NOT NULL
		ORDER BY 1, 3`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FloatingEnd
	for rows.Next() {
		var e FloatingEnd
		if err := rows.Scan(&e.PartID, &e.TripID, &e.End, &e.At, &e.PlanType,
			&e.IATA, &e.Lat, &e.Lon); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ZonedEnd is a part end in a trip that already has a zone: the raw material
// for borrowing a neighbour's or the trip's zone.
type ZonedEnd struct {
	At    time.Time
	TZ    string
	Dwell time.Duration
}

// TripZonedEnds returns every non-dismissed end in the trip with a zone. Dwell
// is the part's duration for a start end (0 when it has no end) and 0 for an
// end end, so a zone is weighted by the time spent in it.
func (s *Store) TripZonedEnds(ctx context.Context, tripID int64) ([]ZonedEnd, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT part.starts_at, part.start_tz,
		       COALESCE(part.ends_at - part.starts_at, interval '0')
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE pl.trip_id = $1 AND part.dismissed_at IS NULL AND part.start_tz <> ''
		UNION ALL
		SELECT part.ends_at, part.end_tz, interval '0'
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE pl.trip_id = $1 AND part.dismissed_at IS NULL AND part.end_tz <> ''
		  AND part.ends_at IS NOT NULL`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ZonedEnd
	for rows.Next() {
		var z ZonedEnd
		if err := rows.Scan(&z.At, &z.TZ, &z.Dwell); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// ZoneFix gives one floating part end its zone, source and re-anchored instant.
type ZoneFix struct {
	PartID int64
	End    string // "start" or "end"
	TZ     string
	Source string
	At     time.Time
	Old    time.Time // the end's instant when the fix was planned
}

// ApplyZoneFixes writes the fixes in one transaction. The update only matches
// an end that is still floating and still holds the instant it was planned
// from, so a re-run, or an edit that landed since the fixes were planned (a
// changed zone or a changed time), is left alone. A fix that fails validation aborts all.
// It returns the number of rows actually written.
func (s *Store) ApplyZoneFixes(ctx context.Context, fixes []ZoneFix) (int, error) {
	type resolved struct {
		sql string
		f   ZoneFix
	}
	var work []resolved
	for _, f := range fixes {
		var q string
		switch f.End {
		case "start":
			q = `UPDATE plan_parts SET start_tz = $1, start_tz_source = $2, starts_at = $3, updated_at = NOW()
			     WHERE id = $4 AND start_tz_source = 'floating' AND starts_at = $5`
		case "end":
			q = `UPDATE plan_parts SET end_tz = $1, end_tz_source = $2, ends_at = $3, updated_at = NOW()
			     WHERE id = $4 AND end_tz_source = 'floating' AND ends_at = $5`
		default:
			return 0, fmt.Errorf("zone fix for part %d: unknown end %q", f.PartID, f.End)
		}
		src, err := ResolveSource(f.TZ, f.Source)
		if err != nil {
			return 0, fmt.Errorf("zone fix for part %d %s: %w", f.PartID, f.End, err)
		}
		f.Source = src
		work = append(work, resolved{q, f})
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	written := 0
	for _, w := range work {
		tag, err := tx.Exec(ctx, w.sql, w.f.TZ, w.f.Source, w.f.At, w.f.PartID, w.f.Old)
		if err != nil {
			return 0, err
		}
		written += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return written, nil
}
