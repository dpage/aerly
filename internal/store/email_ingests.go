package store

import (
	"context"
	"time"
)

// EmailIngestPayload is the input for InsertEmailIngest.
type EmailIngestPayload struct {
	MessageID     *string
	FromAddress   string
	Subject       string
	DKIMPass      bool
	SPFPass       bool
	UserID        *int64
	Status        string
	FlightsAdded  int
	FlightsFailed int
	Error         string
}

// InsertEmailIngest records the outcome of processing one inbound email.
// Returns the new row's id.
func (s *Store) InsertEmailIngest(ctx context.Context, in EmailIngestPayload) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO email_ingests
			(message_id, from_address, subject, dkim_pass, spf_pass, user_id, status,
			 flights_added, flights_failed, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		in.MessageID, in.FromAddress, in.Subject, in.DKIMPass, in.SPFPass, in.UserID,
		in.Status, in.FlightsAdded, in.FlightsFailed, in.Error,
	).Scan(&id)
	return id, err
}

// CountEmailIngestsSince returns how many inbound messages have been recorded
// for a user since the given instant. It drives the per-user ingestion rate
// limit: every audit row (accepted or not) counts, so a sender can't sidestep
// the cap by sending mail that fails extraction. Backed by the
// (user_id, received_at DESC) index.
func (s *Store) CountEmailIngestsSince(ctx context.Context, userID int64, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM email_ingests
		WHERE user_id = $1 AND received_at >= $2`,
		userID, since,
	).Scan(&n)
	return n, err
}
