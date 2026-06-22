package store

import (
	"testing"
)

// These tests drive the mid-transaction "if err != nil { return err }" branches
// in users.go (LinkLogin / insertNewUserTx / findUserForLogin) that a healthy
// database never reaches. The store pool is a concrete *pgxpool.Pool with no
// mock seam, so each branch is exercised by a real fault against the isolated,
// throwaway test database (dropped on cleanup), mirroring the established
// pattern in cover_store_g2_faults_test.go and errors_test.go.

// TestG3bFindUserForLoginEmailScanError covers the Step-2 (verified-email
// match) Scan-error branch in findUserForLogin. We seed a user with a verified
// email but NO identity row for the incoming (provider, provider_user_id), so
// Step 1 returns ErrNoRows and execution falls through to the email match.
// Renaming the users.session_version column then makes the RETURNING/SELECT
// column list still resolve at parse time on the cached side but breaks the
// scan into &u.SessionVersion... in practice dropping the column makes the
// SELECT fail. To get a genuine *scan* error (not a query error) we instead
// drop the user_identities table AFTER seeding so Step 1 errors with a real DB
// error rather than ErrNoRows, covering the non-ErrNoRows return in Step 1.
func TestG3bFindUserForLoginStep1DBError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	// Drop user_identities so the Step 1 JOIN query fails with a real DB error
	// (not ErrNoRows), hitting `if !errors.Is(err, pgx.ErrNoRows) { return ... }`.
	if _, err := s.pool.Exec(ctx, `ALTER TABLE user_identities RENAME TO user_identities_g3b`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "1", Username: "x"}, true); err == nil {
		t.Error("LinkLogin should fail when user_identities is gone (Step 1 query)")
	}
}

// TestG3bFindUserForLoginEmailDBError covers the Step-2 (verified-email match)
// non-ErrNoRows return. Step 1 must first return ErrNoRows cleanly (so
// user_identities stays intact and empty), then the email lookup must hit a
// real DB error. We drop user_emails so the Step 2 JOIN fails.
func TestG3bFindUserForLoginEmailDBError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	if _, err := s.pool.Exec(ctx, `ALTER TABLE user_emails RENAME TO user_emails_g3b`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// Provider/ProviderUserID with no identity row -> Step 1 ErrNoRows; Email set
	// so Step 2 runs and fails on the missing user_emails table.
	if _, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "2", Email: "g3b@example.com"}, true); err == nil {
		t.Error("LinkLogin should fail when user_emails is gone (Step 2 query)")
	}
}

// TestG3bFindUserForLoginInviteeDBError covers the Step-3 (invitee-by-username)
// non-ErrNoRows return. Step 1 and Step 2 must complete cleanly (no identity,
// no email match), then the username lookup must hit a real DB error. We seed a
// user with a verified email of a different address (so Step 2 finds nothing),
// keep user_identities/user_emails intact, but break the Step 3 query by
// renaming the users column the NOT EXISTS subquery references is impractical;
// instead we drop a column used only by Step 3's scan. The simplest genuine DB
// error here is achieved by making the Step 3 query reference a now-missing
// object. Renaming user_identities would also break Step 1, so we cannot. We
// therefore force the error via the lower(username) functional path: rename the
// users table's "username" column so the WHERE lower(username) clause fails
// only after Steps 1 and 2 (which key on identities/emails) have run.
//
// Step 1 (identities join) and Step 2 (emails join) both still SELECT
// users.username via the column list, so renaming username breaks them too.
// That still lands on a non-ErrNoRows DB error in Step 1, which is already
// covered above; the dedicated Step 3 branch requires Steps 1 and 2 to pass.
// Reaching Step 3's error in isolation is not feasible without a column the
// earlier steps don't touch, so this case is intentionally omitted.

// TestG3bInsertNewUserInsertError covers the non-23505 INSERT-error return at
// the bottom of insertNewUserTx's loop. To reach insertNewUserTx, all three
// lookup steps in findUserForLogin must run cleanly and miss, which needs the
// users / user_identities / user_emails tables and the userColumns projection
// intact (so we cannot rename any selected column). Instead we add a CHECK
// constraint that the lookups never evaluate but every INSERT violates, so the
// SAVEPOINT succeeds, the INSERT ... RETURNING fails with a CHECK violation
// (error code 23514, not the 23505 the savepoint-retry handles), and the loop
// returns that error.
func TestG3bInsertNewUserInsertError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	if _, err := s.pool.Exec(ctx,
		`ALTER TABLE users ADD CONSTRAINT users_g3b_block CHECK (false) NOT VALID`); err != nil {
		t.Fatalf("add constraint: %v", err)
	}
	// Unique provider/email/username so all three lookup steps miss cleanly,
	// then insertNewUserTx's INSERT violates the always-false CHECK (23514),
	// returning from the loop's final `return nil, err`.
	if _, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "3", Username: "fresh", Email: "fresh@example.com"}, true); err == nil {
		t.Error("LinkLogin should fail when the users CHECK constraint rejects the insert")
	}
}

// TestG3bLinkLoginExistingIdentityUpdateError covers the Step-1 path's
// `UPDATE user_identities SET last_used_at = NOW()` exec-error branch in
// LinkLogin. We seed an existing user+identity via a successful LinkLogin, then
// rename the user_identities.last_used_at column so the repeat sign-in's
// last_used_at bump fails AFTER the identity match and the users UPDATE have
// both succeeded.
func TestG3bLinkLoginExistingIdentityUpdateError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	// First sign-in: creates the user + identity row.
	if _, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "50", Username: "repeat"}, true); err != nil {
		t.Fatalf("seed sign-in: %v", err)
	}
	// Break the last_used_at bump used only on the existing-identity path.
	if _, err := s.pool.Exec(ctx,
		`ALTER TABLE user_identities RENAME COLUMN last_used_at TO last_used_at_g3b`); err != nil {
		t.Fatalf("rename column: %v", err)
	}
	// Repeat sign-in: Step 1 matches the identity, the users UPDATE succeeds,
	// then the user_identities last_used_at UPDATE fails on the renamed column.
	if _, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "50", Username: "repeat"}, false); err == nil {
		t.Error("LinkLogin should fail when user_identities.last_used_at is gone (existing-identity bump)")
	}
}
