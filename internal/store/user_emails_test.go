package store

import (
	"errors"
	"strings"
	"testing"
)

func TestUpsertVerifiedEmail_Insert(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})

	if err := s.UpsertVerifiedEmail(ctx, u.ID, "Alice@Example.com"); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	got, err := s.UserByVerifiedEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("UserByVerifiedEmail: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("user_id = %d, want %d", got.ID, u.ID)
	}
}

func TestUpsertVerifiedEmail_TrimsWhitespace(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "  alice@example.com  "); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	if _, err := s.UserByVerifiedEmail(ctx, "alice@example.com"); err != nil {
		t.Errorf("trimmed lookup failed: %v", err)
	}
}

func TestUpsertVerifiedEmail_EmptyRejected(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "   "); err == nil {
		t.Error("expected error for empty address")
	}
}

func TestUpsertVerifiedEmail_IdempotentSameUser(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})

	for i := 0; i < 3; i++ {
		if err := s.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	emails, err := s.EmailsByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("EmailsByUser: %v", err)
	}
	if len(emails) != 1 {
		t.Errorf("len(emails) = %d, want 1", len(emails))
	}
	if !emails[0].Verified {
		t.Error("expected row to be verified")
	}
}

func TestUpsertVerifiedEmail_OtherUserRejected(t *testing.T) {
	s := newStore(t)
	u1, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	u2, _ := s.InviteUser(ctx, InvitePayload{Username: "bob"})

	if err := s.UpsertVerifiedEmail(ctx, u1.ID, "shared@example.com"); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := s.UpsertVerifiedEmail(ctx, u2.ID, "shared@example.com")
	if err == nil {
		t.Fatal("expected error when another user owns the address, got nil")
	}
	if !strings.Contains(err.Error(), "address already") {
		t.Errorf("error = %v, want one mentioning 'address already'", err)
	}
}

func TestUserByVerifiedEmail_NotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.UserByVerifiedEmail(ctx, "nobody@example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUserByVerifiedEmail_RequiresVerified(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	// Manually insert an unverified row, bypassing UpsertVerifiedEmail.
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO user_emails (user_id, address, verified) VALUES ($1,$2,FALSE)`,
		u.ID, "pending@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserByVerifiedEmail(ctx, "pending@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound (unverified rows must not match)", err)
	}
}

func TestEmailsByUser_Empty(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	got, err := s.EmailsByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("EmailsByUser: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestEmailsByUser_MultipleNewestFirst(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "first@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "second@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err := s.EmailsByUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Newest first by created_at, then id.
	if got[0].Address != "second@example.com" || got[1].Address != "first@example.com" {
		t.Errorf("order = [%s, %s]", got[0].Address, got[1].Address)
	}
}

func TestInsertUnverifiedEmail_Insert(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})

	e, token, err := s.InsertUnverifiedEmail(ctx, u.ID, "Alice@Example.com")
	if err != nil {
		t.Fatalf("InsertUnverifiedEmail: %v", err)
	}
	if e.Verified {
		t.Error("new row should be unverified")
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
	if e.VerifyToken == nil || *e.VerifyToken != token {
		t.Errorf("row.VerifyToken = %v, want %q", e.VerifyToken, token)
	}
	if e.VerifySentAt == nil {
		t.Error("VerifySentAt should be set")
	}
}

func TestInsertUnverifiedEmail_TrimsAndRejectsEmpty(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})

	if _, _, err := s.InsertUnverifiedEmail(ctx, u.ID, "   "); err == nil {
		t.Error("expected error for empty address")
	}
	if _, _, err := s.InsertUnverifiedEmail(ctx, u.ID, "  bob@example.com  "); err != nil {
		t.Fatalf("InsertUnverifiedEmail trim: %v", err)
	}
	got, _ := s.EmailsByUser(ctx, u.ID)
	if len(got) != 1 || got[0].Address != "bob@example.com" {
		t.Errorf("got = %+v, want one row with trimmed address", got)
	}
}

func TestInsertUnverifiedEmail_ConflictReturnsErrAddressTaken(t *testing.T) {
	s := newStore(t)
	u1, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	u2, _ := s.InviteUser(ctx, InvitePayload{Username: "bob"})

	if _, _, err := s.InsertUnverifiedEmail(ctx, u1.ID, "shared@example.com"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, _, err := s.InsertUnverifiedEmail(ctx, u2.ID, "shared@example.com")
	if !errors.Is(err, ErrAddressTaken) {
		t.Errorf("err = %v, want ErrAddressTaken", err)
	}
}

func TestResendVerification_RotatesToken(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	e, oldToken, err := s.InsertUnverifiedEmail(ctx, u.ID, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}

	updated, newToken, err := s.ResendVerification(ctx, u.ID, e.ID)
	if err != nil {
		t.Fatalf("ResendVerification: %v", err)
	}
	if newToken == "" || newToken == oldToken {
		t.Errorf("newToken = %q, oldToken = %q, want a new non-empty value", newToken, oldToken)
	}
	if updated.VerifyToken == nil || *updated.VerifyToken != newToken {
		t.Errorf("row.VerifyToken = %v, want %q", updated.VerifyToken, newToken)
	}
}

func TestResendVerification_WrongUserNotFound(t *testing.T) {
	s := newStore(t)
	u1, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	u2, _ := s.InviteUser(ctx, InvitePayload{Username: "bob"})
	e, _, _ := s.InsertUnverifiedEmail(ctx, u1.ID, "alice@example.com")

	_, _, err := s.ResendVerification(ctx, u2.ID, e.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestResendVerification_AlreadyVerified(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.EmailsByUser(ctx, u.ID)
	_, _, err := s.ResendVerification(ctx, u.ID, rows[0].ID)
	if !errors.Is(err, ErrAlreadyVerified) {
		t.Errorf("err = %v, want ErrAlreadyVerified", err)
	}
}

func TestVerifyEmailByToken_HappyPath(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	_, token, _ := s.InsertUnverifiedEmail(ctx, u.ID, "alice@example.com")

	got, err := s.VerifyEmailByToken(ctx, token)
	if err != nil {
		t.Fatalf("VerifyEmailByToken: %v", err)
	}
	if !got.Verified || got.VerifiedAt == nil {
		t.Errorf("row not marked verified: %+v", got)
	}
	if got.VerifyToken != nil {
		t.Errorf("VerifyToken should be cleared, got %v", got.VerifyToken)
	}
}

func TestVerifyEmailByToken_SecondCallNotFound(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	_, token, _ := s.InsertUnverifiedEmail(ctx, u.ID, "alice@example.com")

	if _, err := s.VerifyEmailByToken(ctx, token); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := s.VerifyEmailByToken(ctx, token)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("second call err = %v, want ErrNotFound", err)
	}
}

func TestVerifyEmailByToken_BadToken(t *testing.T) {
	s := newStore(t)
	_, err := s.VerifyEmailByToken(ctx, "no-such-token")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestVerifyEmailByToken_ExpiredTokenNotFound(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	_, token, _ := s.InsertUnverifiedEmail(ctx, u.ID, "alice@example.com")

	// Backdate verify_sent_at to 25 hours ago to push the row past the TTL.
	if _, err := s.pool.Exec(ctx,
		`UPDATE user_emails SET verify_sent_at = NOW() - INTERVAL '25 hours' WHERE verify_token = $1`,
		token); err != nil {
		t.Fatal(err)
	}
	_, err := s.VerifyEmailByToken(ctx, token)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expired token err = %v, want ErrNotFound", err)
	}
}

func TestDeleteUserEmail_HappyPath(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	e, _, _ := s.InsertUnverifiedEmail(ctx, u.ID, "alice@example.com")

	if err := s.DeleteUserEmail(ctx, u.ID, e.ID); err != nil {
		t.Fatalf("DeleteUserEmail: %v", err)
	}
	got, _ := s.EmailsByUser(ctx, u.ID)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestDeleteUserEmail_WrongUserNotFound(t *testing.T) {
	s := newStore(t)
	u1, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	u2, _ := s.InviteUser(ctx, InvitePayload{Username: "bob"})
	e, _, _ := s.InsertUnverifiedEmail(ctx, u1.ID, "alice@example.com")

	if err := s.DeleteUserEmail(ctx, u2.ID, e.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	// Row is still there for the rightful owner.
	got, _ := s.EmailsByUser(ctx, u1.ID)
	if len(got) != 1 {
		t.Errorf("rightful owner lost their row, got %d", len(got))
	}
}

func TestPrimaryEmail_NoVerifiedReturnsNotFound(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	// An unverified address must not count.
	if _, _, err := s.InsertUnverifiedEmail(ctx, u.ID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrimaryEmail(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The core regression: a user's login email is flagged primary on sign-in, and
// adding a newer secondary address must NOT steal notifications away from it.
func TestPrimaryEmail_PrefersFlaggedPrimaryOverNewerSecondary(t *testing.T) {
	s := newStore(t)
	u, _, err := s.LinkLogin(ctx,
		githubProfile("9001", "alice", "", "", "login@example.com"), true)
	if err != nil {
		t.Fatalf("LinkLogin: %v", err)
	}
	// A later, verified secondary address (newer created_at).
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "secondary@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err := s.PrimaryEmail(ctx, u.ID)
	if err != nil {
		t.Fatalf("PrimaryEmail: %v", err)
	}
	if got != "login@example.com" {
		t.Errorf("PrimaryEmail = %q, want the login address", got)
	}
}

func TestSetPrimaryEmail_MovesPrimary(t *testing.T) {
	s := newStore(t)
	u, _, err := s.LinkLogin(ctx,
		githubProfile("9100", "alice", "", "", "login@example.com"), true)
	if err != nil {
		t.Fatalf("LinkLogin: %v", err)
	}
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "second@example.com"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.EmailsByUser(ctx, u.ID)
	var target int64
	for _, r := range rows {
		if r.Address == "second@example.com" {
			target = r.ID
		}
	}
	if err := s.SetPrimaryEmail(ctx, u.ID, target); err != nil {
		t.Fatalf("SetPrimaryEmail: %v", err)
	}
	// The chosen address is now primary, and it's the one notifications use.
	got, err := s.PrimaryEmail(ctx, u.ID)
	if err != nil {
		t.Fatalf("PrimaryEmail: %v", err)
	}
	if got != "second@example.com" {
		t.Errorf("primary = %q, want second@example.com", got)
	}
	// The old primary was cleared: exactly one row is flagged.
	rows, _ = s.EmailsByUser(ctx, u.ID)
	primaries := 0
	for _, r := range rows {
		if r.IsPrimary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Errorf("primary count = %d, want exactly 1", primaries)
	}
}

// Exercises the mid-call "if err != nil { return err }" branches that a healthy
// database never triggers, by renaming user_emails away so the statements fail
// (mirrors the pattern in cover_store_g2_faults_test.go).
func TestPrimaryEmailAndSetPrimary_DBErrors(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	if _, err := s.pool.Exec(ctx, `ALTER TABLE user_emails RENAME TO user_emails_x`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := s.PrimaryEmail(ctx, u.ID); err == nil {
		t.Error("PrimaryEmail should error when user_emails is gone")
	}
	if err := s.SetPrimaryEmail(ctx, u.ID, 1); err == nil {
		t.Error("SetPrimaryEmail should error when user_emails is gone")
	}
}

func TestSetPrimaryEmail_UnverifiedRejected(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	row, _, err := s.InsertUnverifiedEmail(ctx, u.ID, "pending@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrimaryEmail(ctx, u.ID, row.ID); !errors.Is(err, ErrNotVerified) {
		t.Errorf("err = %v, want ErrNotVerified", err)
	}
}

func TestSetPrimaryEmail_NotOwnedReturnsNotFound(t *testing.T) {
	s := newStore(t)
	u1, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	u2, _ := s.InviteUser(ctx, InvitePayload{Username: "bob"})
	if err := s.UpsertVerifiedEmail(ctx, u2.ID, "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.EmailsByUser(ctx, u2.ID)
	if err := s.SetPrimaryEmail(ctx, u1.ID, rows[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// Legacy rows (pre-migration data that was never flagged primary) fall back to
// the oldest verified address.
func TestPrimaryEmail_FallsBackToOldestWhenNoneFlagged(t *testing.T) {
	s := newStore(t)
	u, _ := s.InviteUser(ctx, InvitePayload{Username: "alice"})
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "oldest@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertVerifiedEmail(ctx, u.ID, "newer@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err := s.PrimaryEmail(ctx, u.ID)
	if err != nil {
		t.Fatalf("PrimaryEmail: %v", err)
	}
	if got != "oldest@example.com" {
		t.Errorf("PrimaryEmail = %q, want oldest verified", got)
	}
}
