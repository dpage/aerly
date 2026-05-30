package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// canceled returns an already-cancelled context so pool queries fail fast,
// exercising the DB-error return branches.
func canceled() context.Context {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	return c
}

// TestPositionQueryErrorPaths exercises the DB-error branches of the
// (part-keyed) position helpers that survived the Wave 3 flight cut-over.
func TestPositionQueryErrorPaths(t *testing.T) {
	s := newStore(t)
	cc := canceled()

	if _, err := s.LatestRealPosition(cc, 1); err == nil {
		t.Error("LatestRealPosition should error on cancelled ctx")
	}
	if _, err := s.LatestPartPositions(cc, []int64{1}); err == nil {
		t.Error("LatestPartPositions should error on cancelled ctx")
	}
	if _, err := s.PartTracks(cc, []int64{1}, 10); err == nil {
		t.Error("PartTracks should error on cancelled ctx")
	}
	if _, err := s.PositionsForFlight(cc, 1, 10); err == nil {
		t.Error("PositionsForFlight should error on cancelled ctx")
	}
	if err := s.InsertPartPosition(cc, Position{FlightID: 1, Ts: time.Now()}); err == nil {
		t.Error("InsertPartPosition should error on cancelled ctx")
	}
}

func TestUserQueryErrorPaths(t *testing.T) {
	s := newStore(t)
	cc := canceled()

	if _, err := s.UserByID(cc, 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID cancelled should be non-NotFound error, got %v", err)
	}
	if _, err := s.UserByIdentity(cc, "github", "1"); err == nil {
		t.Error("UserByIdentity should error on cancelled ctx")
	}
	if _, err := s.UserByUsername(cc, "x"); err == nil {
		t.Error("UserByUsername should error on cancelled ctx")
	}
	if _, err := s.ListUsers(cc); err == nil {
		t.Error("ListUsers should error on cancelled ctx")
	}
	if _, err := s.InviteUser(cc, InvitePayload{Username: "x"}); err == nil {
		t.Error("InviteUser should error on cancelled ctx")
	}
	if _, err := s.UpdateUser(cc, 1, UpdateUserPayload{}); err == nil {
		t.Error("UpdateUser should error on cancelled ctx")
	}
	if err := s.DeleteUser(cc, 1); err == nil {
		t.Error("DeleteUser should error on cancelled ctx")
	}
	if _, err := s.CountUsers(cc); err == nil {
		t.Error("CountUsers should error on cancelled ctx")
	}
	if _, _, err := s.LinkLogin(cc,
		OAuthProfile{Provider: "github", ProviderUserID: "1", Username: "x"}, true); err == nil {
		t.Error("LinkLogin should error on cancelled ctx (tx.Begin)")
	}
}

// TestLinkLoginFirstQueryErrors covers the error path on the initial identity
// lookup — a non-ErrNoRows database failure. We force it by dropping the
// users / user_identities tables after Begin would otherwise succeed.
func TestLinkLoginFirstQueryErrors(t *testing.T) {
	s := newStore(t)
	if _, err := s.pool.Exec(ctx, `DROP TABLE positions, user_emails, user_identities, users CASCADE`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	_, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "1", Username: "x"}, true)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("expected a real DB error, got %v", err)
	}
}

// TestLinkLoginSuffixesUsernameOnConflict covers the username-allocation
// fallback in the new-user INSERT branch: a second sign-in whose
// provider-supplied username collides with an existing linked user gets a
// numeric suffix so the open-signup flow doesn't 500.
func TestLinkLoginSuffixesUsernameOnConflict(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "100", Username: "dup"}, true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	u, _, err := s.LinkLogin(ctx,
		OAuthProfile{Provider: "github", ProviderUserID: "200", Username: "dup"}, false)
	if err != nil {
		t.Fatalf("conflicting username should be suffixed, got %v", err)
	}
	// Pin the specific "suffix the provider username" contract: the new
	// row's username must derive from "dup" (a HasPrefix-then-different
	// pair). A bare != "dup" check would also pass if allocation drifted
	// to e.g. the email local-part or the "user" fallback.
	if !strings.HasPrefix(u.Username, "dup") || u.Username == "dup" {
		t.Errorf("expected a 'dup' + numeric suffix, got %q", u.Username)
	}
}
