package store

import (
	"errors"
	"testing"
	"time"
)

func TestG2CalendarTokenScopesAndValidation(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	u := mkUser(t, s)
	trip := mkTrip(t, s, u)

	// Invalid scope on both entrypoints.
	if _, err := s.CalendarToken(ctx, u, "bogus", 0); err == nil {
		t.Error("CalendarToken invalid scope should error")
	}
	if _, err := s.RegenerateCalendarToken(ctx, u, "bogus", 0); err == nil {
		t.Error("RegenerateCalendarToken invalid scope should error")
	}
	// trip/plan scope requires a resource id.
	if _, err := s.CalendarToken(ctx, u, "trip", 0); err == nil {
		t.Error("CalendarToken trip scope without resource should error")
	}
	if _, err := s.RegenerateCalendarToken(ctx, u, "plan", 0); err == nil {
		t.Error("RegenerateCalendarToken plan scope without resource should error")
	}

	// A trip-scoped token issues, resolves, regenerates, lists, and revokes.
	ct, err := s.CalendarToken(ctx, u, "trip", trip)
	if err != nil {
		t.Fatalf("CalendarToken trip: %v", err)
	}
	if ct.Scope != "trip" || ct.ResourceID != trip {
		t.Errorf("token = %+v, want trip/%d", ct, trip)
	}

	info, err := s.CalendarTokenByValue(ctx, ct.Token)
	if err != nil {
		t.Fatalf("CalendarTokenByValue: %v", err)
	}
	if info.UserID != u || info.Scope != "trip" || info.ResourceID != trip {
		t.Errorf("info = %+v", info)
	}
	if _, err := s.CalendarTokenByValue(ctx, "no-such-token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("CalendarTokenByValue unknown = %v, want ErrNotFound", err)
	}

	// Regenerate replaces the token value for that resource.
	regen, err := s.RegenerateCalendarToken(ctx, u, "trip", trip)
	if err != nil {
		t.Fatalf("RegenerateCalendarToken: %v", err)
	}
	if regen.Token == ct.Token {
		t.Error("regenerated token should differ")
	}
	if _, err := s.CalendarTokenByValue(ctx, ct.Token); !errors.Is(err, ErrNotFound) {
		t.Error("old token should stop resolving after regeneration")
	}

	// Also issue a "me" token, then list both for the user.
	if _, err := s.CalendarToken(ctx, u, "me", 0); err != nil {
		t.Fatalf("CalendarToken me: %v", err)
	}
	list, err := s.ListCalendarTokens(ctx, u)
	if err != nil {
		t.Fatalf("ListCalendarTokens: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListCalendarTokens = %d, want 2", len(list))
	}

	// Revoke: scoped to the owner. Another user cannot revoke by guessing.
	other := mkUser(t, s)
	if err := s.RevokeCalendarToken(ctx, other, regen.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-user revoke = %v, want ErrNotFound", err)
	}
	if err := s.RevokeCalendarToken(ctx, u, regen.Token); err != nil {
		t.Fatalf("RevokeCalendarToken: %v", err)
	}
	if err := s.RevokeCalendarToken(ctx, u, regen.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("double revoke = %v, want ErrNotFound", err)
	}
}

func TestG2CalendarEventsForScopes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	planID := mkTypedPlan(t, s, trip, owner, "flight", "BA123", "ABC123", "window seat")

	start := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	mkPart(t, s, planID, start, &end, "Europe/London", "Europe/Paris", "LHR")

	// "me" feed: owner sees their own trip's part.
	mine, err := s.CalendarEventsForUser(ctx, owner)
	if err != nil {
		t.Fatalf("CalendarEventsForUser: %v", err)
	}
	if len(mine) != 1 || mine[0].Title != "BA123" || mine[0].TripID != trip {
		t.Fatalf("me feed = %+v", mine)
	}

	// "trip" feed scoped to the trip.
	tripEvents, err := s.CalendarEventsForTrip(ctx, owner, trip)
	if err != nil {
		t.Fatalf("CalendarEventsForTrip: %v", err)
	}
	if len(tripEvents) != 1 || tripEvents[0].PlanID != planID {
		t.Fatalf("trip feed = %+v", tripEvents)
	}

	// "plan" feed scoped to the plan.
	planEvents, err := s.CalendarEventsForPlan(ctx, owner, planID)
	if err != nil {
		t.Fatalf("CalendarEventsForPlan: %v", err)
	}
	if len(planEvents) != 1 || planEvents[0].StartLabel != "LHR" {
		t.Fatalf("plan feed = %+v", planEvents)
	}

	// A stranger sees none of it across all three scopes.
	stranger := mkUser(t, s)
	if ev, _ := s.CalendarEventsForUser(ctx, stranger); len(ev) != 0 {
		t.Errorf("stranger me feed = %d, want 0", len(ev))
	}
	if ev, _ := s.CalendarEventsForTrip(ctx, stranger, trip); len(ev) != 0 {
		t.Errorf("stranger trip feed = %d, want 0", len(ev))
	}
	if ev, _ := s.CalendarEventsForPlan(ctx, stranger, planID); len(ev) != 0 {
		t.Errorf("stranger plan feed = %d, want 0", len(ev))
	}
}

func TestG2CalendarErrorPaths(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	cc := canceled()
	if _, err := s.ListCalendarTokens(cc, 1); err == nil {
		t.Error("ListCalendarTokens cancelled should error")
	}
	if _, err := s.CalendarTokenByValue(cc, "x"); err == nil {
		t.Error("CalendarTokenByValue cancelled should error")
	}
	if err := s.RevokeCalendarToken(cc, 1, "x"); err == nil {
		t.Error("RevokeCalendarToken cancelled should error")
	}
	if _, err := s.CalendarToken(cc, 1, "me", 0); err == nil {
		t.Error("CalendarToken cancelled should error")
	}
	if _, err := s.RegenerateCalendarToken(cc, 1, "me", 0); err == nil {
		t.Error("RegenerateCalendarToken cancelled should error")
	}
	if _, err := s.CalendarEventsForUser(cc, 1); err == nil {
		t.Error("CalendarEventsForUser cancelled should error")
	}
	if _, err := s.CalendarEventsForTrip(cc, 1, 1); err == nil {
		t.Error("CalendarEventsForTrip cancelled should error")
	}
	if _, err := s.CalendarEventsForPlan(cc, 1, 1); err == nil {
		t.Error("CalendarEventsForPlan cancelled should error")
	}
}
