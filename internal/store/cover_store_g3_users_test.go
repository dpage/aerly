package store

import (
	"testing"
)

// TestG3UsersByIDs covers UsersByIDs: the empty-input fast path and a populated
// lookup keyed by id (missing ids simply absent).
func TestG3UsersByIDs(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	if m, err := s.UsersByIDs(ctx, nil); err != nil || len(m) != 0 {
		t.Fatalf("UsersByIDs(nil) = %v, %v; want empty, nil", m, err)
	}
	a := mkUser(t, s)
	b := mkUser(t, s)
	const missing = int64(0) // no user has id 0
	m, err := s.UsersByIDs(ctx, []int64{a, b, missing})
	if err != nil {
		t.Fatalf("UsersByIDs: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("UsersByIDs = %d users, want 2", len(m))
	}
	if m[a] == nil || m[b] == nil {
		t.Fatalf("UsersByIDs missing an expected user: %+v", m)
	}
	if _, ok := m[missing]; ok {
		t.Errorf("UsersByIDs included a missing id")
	}
}

// TestG3LinkLoginEmptyProvider covers the up-front validation guard.
func TestG3LinkLoginEmptyProvider(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	if _, _, err := s.LinkLogin(ctx, OAuthProfile{ProviderUserID: "x"}, false); err == nil {
		t.Error("LinkLogin with empty provider: want error")
	}
	if _, _, err := s.LinkLogin(ctx, OAuthProfile{Provider: "github"}, false); err == nil {
		t.Error("LinkLogin with empty provider_user_id: want error")
	}
}

// TestG3LinkLoginUsernameCollisionRetry exercises insertNewUserTx's
// SAVEPOINT-based suffix retry loop: a first user takes "shared", then a second
// open-signup with the same provider-supplied username must fall through to
// "shared2".
func TestG3LinkLoginUsernameCollisionRetry(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	// First user grabs the bare username (bootstrap → superuser, but the
	// username allocation is what matters here).
	u1, _, err := s.LinkLogin(ctx, OAuthProfile{
		Provider: "github", ProviderUserID: "g3-coll-1", Username: "shared", Name: "Test User One",
	}, false)
	if err != nil {
		t.Fatalf("LinkLogin first: %v", err)
	}
	if u1.Username != "shared" {
		t.Fatalf("first username = %q, want shared", u1.Username)
	}
	// Second user, different identity, same desired username → suffix retry.
	u2, outcome, err := s.LinkLogin(ctx, OAuthProfile{
		Provider: "github", ProviderUserID: "g3-coll-2", Username: "shared", Name: "Test User Two",
	}, false)
	if err != nil {
		t.Fatalf("LinkLogin second: %v", err)
	}
	if u2.Username != "shared2" {
		t.Fatalf("second username = %q, want shared2 (suffix retry)", u2.Username)
	}
	if outcome != LinkOutcomeOpenSignup {
		t.Errorf("second outcome = %v, want OpenSignup", outcome)
	}
}

// TestG3LinkLoginEmptyUsernameFallsBackToUser covers insertNewUserTx's
// base == "" → "user" fallback (no username, no email local-part).
func TestG3LinkLoginEmptyUsernameFallsBackToUser(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u, _, err := s.LinkLogin(ctx, OAuthProfile{
		Provider: "google", ProviderUserID: "g3-noname-1", Name: "Test User",
	}, false)
	if err != nil {
		t.Fatalf("LinkLogin: %v", err)
	}
	if u.Username != "user" {
		t.Fatalf("username = %q, want user", u.Username)
	}
}

// TestG3LinkLoginEmailOwnedByAnother covers the !claimed && email != ""
// warning branch. A returning user (matched by existing identity, Step 1) signs
// in and the provider now asserts an email that is already verified to a
// DIFFERENT account. The identity match short-circuits Step 2, so the sign-in
// resolves to the returning user, and upsertEmailTx finds the address owned by
// someone else: claimed == false with a non-empty email, firing the warning.
func TestG3LinkLoginEmailOwnedByAnother(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	// Account A owns the shared address as a verified email.
	owner, _, err := s.LinkLogin(ctx, OAuthProfile{
		Provider: "github", ProviderUserID: "g3-own-1", Username: "owner3",
		Name: "Test Owner", Email: "shared3@example.com",
	}, false)
	if err != nil {
		t.Fatalf("LinkLogin owner: %v", err)
	}

	// Account B is established first with its own identity and no shared email.
	b, _, err := s.LinkLogin(ctx, OAuthProfile{
		Provider: "github", ProviderUserID: "g3-b-1", Username: "userb3", Name: "Test B",
	}, false)
	if err != nil {
		t.Fatalf("LinkLogin b setup: %v", err)
	}

	// B signs in again (Step 1 identity match) but now the provider asserts A's
	// verified email. It must NOT be re-pointed to B.
	got, outcome, err := s.LinkLogin(ctx, OAuthProfile{
		Provider: "github", ProviderUserID: "g3-b-1", Username: "userb3",
		Name: "Test B", Email: "shared3@example.com",
	}, false)
	if err != nil {
		t.Fatalf("LinkLogin b repeat: %v", err)
	}
	if got.ID != b.ID {
		t.Fatalf("resolved user = %d, want b %d", got.ID, b.ID)
	}
	if outcome != LinkOutcomeExisting {
		t.Fatalf("outcome = %v, want Existing", outcome)
	}
	// The shared email stays with the original owner.
	if o, err := s.UserByVerifiedEmail(ctx, "shared3@example.com"); err != nil || o.ID != owner.ID {
		t.Fatalf("shared email owner = %v (%v), want %d", o, err, owner.ID)
	}
}
