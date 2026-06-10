package store

import (
	"context"
	"strings"
)

// likeEscape escapes the LIKE metacharacters (backslash, %, _) in a literal
// string so it can be used as a prefix in `col LIKE escaped || '%' ESCAPE '\'`
// without the user's % or _ being treated as wildcards.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func likeEscape(s string) string { return likeEscaper.Replace(s) }

// Tag is one trip_tags row: a normalized matching key plus the display label
// as first typed. Tags group trips but never grant visibility.
type Tag struct {
	TripID       int64
	LabelNorm    string
	LabelDisplay string
}

// normalizeTag lowercases and trims a tag label for the matching key.
func normalizeTag(label string) string {
	return strings.ToLower(strings.TrimSpace(label))
}

// TagsByTrip returns the display labels set on a trip, ordered by the
// normalized key for stable output.
func (s *Store) TagsByTrip(ctx context.Context, tripID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT label_display FROM trip_tags WHERE trip_id = $1 ORDER BY label_norm`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		out = append(out, label)
	}
	return out, rows.Err()
}

// SetTripTags replaces the trip's tag set with the given display labels
// (normalizing each for the matching key). Blank labels are dropped and
// duplicates (by normalized key) collapse to the first-seen display form.
func (s *Store) SetTripTags(ctx context.Context, tripID int64, labels []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM trip_tags WHERE trip_id = $1`, tripID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, label := range labels {
		norm := normalizeTag(label)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		if _, err := tx.Exec(ctx,
			`INSERT INTO trip_tags (trip_id, label_norm, label_display) VALUES ($1, $2, $3)`,
			tripID, norm, strings.TrimSpace(label)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SuggestTags autocompletes over tags on trips the viewer can see, matching the
// normalized prefix q. Returns distinct display labels (visibility-gated: only
// tags on trips the viewer owns or is a member of). An empty q returns no
// suggestions.
func (s *Store) SuggestTags(ctx context.Context, viewerID int64, q string) ([]string, error) {
	norm := normalizeTag(q)
	if norm == "" {
		return nil, nil
	}
	// Escape LIKE metacharacters so a query containing % or _ is matched
	// literally as a prefix rather than as a wildcard pattern.
	likePrefix := likeEscape(norm)
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (tt.label_norm) tt.label_display
		FROM trip_tags tt
		JOIN trips t ON t.id = tt.trip_id
		WHERE tt.label_norm LIKE $2 || '%' ESCAPE '\'
		  AND (t.created_by = $1
		    OR EXISTS (SELECT 1 FROM trip_members tm
		               WHERE tm.trip_id = t.id AND tm.user_id = $1))
		ORDER BY tt.label_norm
		LIMIT 20`, viewerID, likePrefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		out = append(out, label)
	}
	return out, rows.Err()
}
