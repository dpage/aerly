package store

import (
	"testing"
	"time"
)

func TestInsertEmailIngest_Minimum(t *testing.T) {
	s := newStore(t)

	id, err := s.InsertEmailIngest(ctx, EmailIngestPayload{
		FromAddress: "devrim@example.com",
		Status:      "no_user",
		DKIMPass:    true,
	})
	if err != nil {
		t.Fatalf("InsertEmailIngest: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}
}

func TestInsertEmailIngest_FullFields(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})

	msgID := "<abc@example.com>"
	id, err := s.InsertEmailIngest(ctx, EmailIngestPayload{
		MessageID:     &msgID,
		FromAddress:   "alice@example.com",
		Subject:       "Your booking",
		DKIMPass:      true,
		SPFPass:       true,
		UserID:        &u.ID,
		Status:        "accepted",
		FlightsAdded:  2,
		FlightsFailed: 1,
		Error:         "",
	})
	if err != nil {
		t.Fatalf("InsertEmailIngest: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	// Verify the row landed with the expected user link.
	var gotUser *int64
	var gotSPF bool
	var gotAdded, gotFailed int
	if err := s.pool.QueryRow(ctx,
		`SELECT user_id, spf_pass, flights_added, flights_failed FROM email_ingests WHERE id = $1`, id,
	).Scan(&gotUser, &gotSPF, &gotAdded, &gotFailed); err != nil {
		t.Fatalf("verify row: %v", err)
	}
	if gotUser == nil || *gotUser != u.ID {
		t.Errorf("user_id = %v, want %d", gotUser, u.ID)
	}
	if !gotSPF {
		t.Error("spf_pass = false, want true")
	}
	if gotAdded != 2 || gotFailed != 1 {
		t.Errorf("added/failed = %d/%d, want 2/1", gotAdded, gotFailed)
	}
}

func TestCountEmailIngestsSince(t *testing.T) {
	s := newStore(t)
	alice, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	bob, _ := s.InviteUser(ctx, InvitePayload{Username: "bob"})

	// Three rows for alice, one for bob, plus one with no user (rejection row).
	for i := 0; i < 3; i++ {
		if _, err := s.InsertEmailIngest(ctx, EmailIngestPayload{
			FromAddress: "alice@example.com", Status: "accepted", UserID: &alice.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.InsertEmailIngest(ctx, EmailIngestPayload{
		FromAddress: "bob@example.com", Status: "accepted", UserID: &bob.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertEmailIngest(ctx, EmailIngestPayload{
		FromAddress: "nobody@example.com", Status: "no_user",
	}); err != nil {
		t.Fatal(err)
	}

	since := time.Now().Add(-24 * time.Hour)
	n, err := s.CountEmailIngestsSince(ctx, alice.ID, since)
	if err != nil {
		t.Fatalf("CountEmailIngestsSince: %v", err)
	}
	if n != 3 {
		t.Errorf("alice count = %d, want 3", n)
	}

	// A window that starts in the future excludes everything just inserted.
	n, err = s.CountEmailIngestsSince(ctx, alice.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CountEmailIngestsSince: %v", err)
	}
	if n != 0 {
		t.Errorf("future-window count = %d, want 0", n)
	}
}
