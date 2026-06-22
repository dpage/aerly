package store

import (
	"errors"
	"testing"
	"time"
)

// TestG3VerifyEmailByTokenEmpty covers the empty-token guard that returns
// ErrNotFound without touching the DB.
func TestG3VerifyEmailByTokenEmpty(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	if _, err := s.VerifyEmailByToken(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("VerifyEmailByToken(\"\") = %v, want ErrNotFound", err)
	}
}

// TestG3FlightPartAlertSigNoDetails covers FlightPartAlertSig's ErrNotFound
// branch when the part has no flight_details row.
func TestG3FlightPartAlertSigNoDetails(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan := mkPlan(t, s, trip, owner)
	part := addPlanPart(t, s, plan, time.Now()) // no flight_details satellite

	if _, _, err := s.FlightPartAlertSig(ctx, part); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FlightPartAlertSig with no flight_details = %v, want ErrNotFound", err)
	}
}

// TestG3SuperuserAndUserEmails exercises the scan loops of SuperuserEmails and
// EmailsByUser with real rows (a verified superuser address and a user's email
// list).
func TestG3SuperuserAndUserEmails(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	// A user with a couple of email rows.
	u := mkUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, u, "g3verified@example.com"); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	if _, _, err := s.InsertUnverifiedEmail(ctx, u, "g3pending@example.com"); err != nil {
		t.Fatalf("InsertUnverifiedEmail: %v", err)
	}
	emails, err := s.EmailsByUser(ctx, u)
	if err != nil {
		t.Fatalf("EmailsByUser: %v", err)
	}
	if len(emails) != 2 {
		t.Fatalf("EmailsByUser = %d, want 2", len(emails))
	}

	// Promote a separate user to active superuser and give them a verified
	// address so SuperuserEmails returns a row.
	su := mkUser(t, s)
	tru := true
	if _, err := s.UpdateUser(ctx, su, UpdateUserPayload{IsSuperuser: &tru}); err != nil {
		t.Fatalf("UpdateUser superuser: %v", err)
	}
	if err := s.UpsertVerifiedEmail(ctx, su, "g3admin@example.com"); err != nil {
		t.Fatalf("UpsertVerifiedEmail admin: %v", err)
	}
	addrs, err := s.SuperuserEmails(ctx)
	if err != nil {
		t.Fatalf("SuperuserEmails: %v", err)
	}
	found := false
	for _, a := range addrs {
		if a == "g3admin@example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SuperuserEmails = %v, want it to include g3admin@example.com", addrs)
	}
}
