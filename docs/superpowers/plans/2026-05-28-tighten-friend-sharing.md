# Tighten flight sharing to friends — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make flights private by default, gate the broadcast toggle and per-user passenger/share lists on accepted friendships, and invert the FlightList filter so users opt in to seeing friends' flights instead of opting out.

**Architecture:** Tighten the SQL visibility predicate (`ListVisibleFlights`, `CanView`, `VisibleUserIDs`) so `is_public` only widens to creator's accepted friends and the standalone friend-of-creator branch is removed. Add a `Store.AreAcceptedFriends` helper and call it from the passenger/share-mutation handlers. Lift friendships into the Zustand store so a `useFriendUsers()` selector can filter the FlightDialog autocompletes. Rename `showMineOnly` → `showFriends` in the store with a one-shot localStorage migration, and invert the FlightList toggle UI.

**Tech Stack:** Go 1.24+ / `pgx/v5` / `net/http` (backend); React / TypeScript / Vite / MUI / Zustand / Vitest (frontend). Tests run via `make test` (Go + web).

**Spec:** `docs/superpowers/specs/2026-05-28-tighten-friend-sharing-design.md`

**Branch:** `worktree-tighten-friend-sharing` (already created via `EnterWorktree`).

---

## File map

**Backend:**
- Modify `internal/store/friends.go` — add `AreAcceptedFriends(ctx, a, b int64) (bool, error)`.
- Modify `internal/store/flights.go` — tighten `ListVisibleFlights`, `CanView`, `VisibleUserIDs`.
- Modify `internal/store/flights_test.go` — add visibility-rule cases.
- Modify `internal/store/friends_test.go` — add `AreAcceptedFriends` tests.
- Modify `internal/handlers/flights.go` — friendship check inside `addPassenger`, `addShare`, and the `createFlight` passenger/share loops.
- Modify `internal/handlers/handlers_test.go` — friend-required cases for the three handlers + the superuser-uses-creator's-friend-graph case.

**Frontend:**
- Modify `web/src/state/store.ts` — `friendships` slice + `refreshFriendships` action; rename `showMineOnly` → `showFriends` with localStorage migration.
- Create `web/src/state/friendUsers.ts` — `useFriendUsers()` selector.
- Modify `web/src/state/store.test.ts` — tests for the renamed flag, migration, and friendship slice.
- Create `web/src/state/friendUsers.test.ts` — tests for the new selector.
- Modify `web/src/state/visibleFlights.ts` — invert filter, rename consumer.
- Modify `web/src/state/visibleFlights.test.ts` — invert assertions.
- Modify `web/src/components/FlightList.tsx` — toggle label/tooltip; rename binding.
- Modify `web/src/components/FlightList.test.tsx` — relabelled toggle + inverted assertions.
- Modify `web/src/components/FlightDialog.tsx` — relabel switch / helper, restrict autocomplete options.
- Modify `web/src/components/FlightDialog.test.tsx` — relabel + friends-only autocomplete + legacy-chip preservation.
- Modify `web/src/components/FriendsDialog.tsx` — read friendships from store, dispatch actions through store.
- Modify `web/src/components/FriendsDialog.test.tsx` — adjust mocks to the store-backed flow.

---

## Test commands (used throughout)

| Scope | Command (from worktree root) |
|---|---|
| One Go test | `go test ./internal/store/ -run TestName -v` |
| One Go package | `go test ./internal/store/...` or `./internal/handlers/...` |
| One web test file | `cd web && npm run test -- src/state/store.test.ts` |
| One web test by name | `cd web && npm run test -- -t "test name fragment"` |
| Full suite | `make test` |
| Lint + typecheck | `make lint && make typecheck-web` |

The Go test suite stands up a temporary Postgres database via `internal/testsupport`; tests that need users/flights/friendships use the existing helpers (`testsupport.InsertUser`, `mkUser`, `mkFlight`, `s.RequestFriendship`, `s.AcceptFriendship`).

---

## Task 1: Add `Store.AreAcceptedFriends`

**Files:**
- Modify: `internal/store/friends.go`
- Modify: `internal/store/friends_test.go`

- [ ] **Step 1: Write the failing test.** Append at the end of `internal/store/friends_test.go`:

```go
func TestAreAcceptedFriends(t *testing.T) {
	s := newStore(t)
	a := testsupport.InsertUser(t, s.pool, fmt.Sprintf("aaf-a%d", loginSeq.Add(1)), false, true)
	b := testsupport.InsertUser(t, s.pool, fmt.Sprintf("aaf-b%d", loginSeq.Add(1)), false, true)
	c := testsupport.InsertUser(t, s.pool, fmt.Sprintf("aaf-c%d", loginSeq.Add(1)), false, true)

	// No row at all → false.
	ok, err := s.AreAcceptedFriends(ctx, a, b)
	if err != nil || ok {
		t.Fatalf("no row: ok=%v err=%v want false,nil", ok, err)
	}

	// Pending request → false.
	if _, err := s.RequestFriendship(ctx, a, b); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	ok, err = s.AreAcceptedFriends(ctx, a, b)
	if err != nil || ok {
		t.Fatalf("pending: ok=%v err=%v want false,nil", ok, err)
	}

	// Accept → true, and order of arguments doesn't matter.
	if _, err := s.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	ok, err = s.AreAcceptedFriends(ctx, a, b)
	if err != nil || !ok {
		t.Fatalf("accepted (a,b): ok=%v err=%v want true,nil", ok, err)
	}
	ok, err = s.AreAcceptedFriends(ctx, b, a)
	if err != nil || !ok {
		t.Fatalf("accepted (b,a): ok=%v err=%v want true,nil", ok, err)
	}

	// Unrelated user → false.
	ok, err = s.AreAcceptedFriends(ctx, a, c)
	if err != nil || ok {
		t.Fatalf("unrelated: ok=%v err=%v want false,nil", ok, err)
	}

	// Self → false (cheap guard; mirrors FriendshipBetween).
	ok, err = s.AreAcceptedFriends(ctx, a, a)
	if err != nil || ok {
		t.Fatalf("self: ok=%v err=%v want false,nil", ok, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

```
go test ./internal/store/ -run TestAreAcceptedFriends -v
```

Expected: FAIL — `s.AreAcceptedFriends undefined`.

- [ ] **Step 3: Implement.** Append to `internal/store/friends.go` (right after `FriendshipBetween`):

```go
// AreAcceptedFriends reports whether a and b have an accepted friendship.
// Argument order doesn't matter; the query normalises to the canonical pair.
// Returns false (no error) when a == b — callers may legitimately ask about
// the creator/passenger being the same user.
func (s *Store) AreAcceptedFriends(ctx context.Context, a, b int64) (bool, error) {
	if a == b {
		return false, nil
	}
	low, high := pairOrder(a, b)
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM friendships
			WHERE user_low = $1 AND user_high = $2 AND status = 'accepted')`,
		low, high).Scan(&ok)
	return ok, err
}
```

- [ ] **Step 4: Run the test to verify it passes.**

```
go test ./internal/store/ -run TestAreAcceptedFriends -v
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```
git add internal/store/friends.go internal/store/friends_test.go
git commit -m "feat(store): add AreAcceptedFriends helper for friendship checks"
```

---

## Task 2: Tighten `ListVisibleFlights` and `CanView`

The standalone friend-of-creator branch goes away, and `is_public` only widens to creator's accepted friends.

**Files:**
- Modify: `internal/store/flights.go:498-583`
- Modify: `internal/store/flights_test.go`

- [ ] **Step 1: Write the failing tests.** Append to `internal/store/flights_test.go`:

```go
func TestListVisibleFlights_FriendGatedVisibility(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	pending := mkUser(t, s)

	if _, err := s.RequestFriendship(ctx, creator, friend); err != nil {
		t.Fatalf("RequestFriendship friend: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, friend, creator); err != nil {
		t.Fatalf("AcceptFriendship friend: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, creator, pending); err != nil {
		t.Fatalf("RequestFriendship pending: %v", err)
	}

	private, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PR1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight private: %v", err)
	}

	public, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PU1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: true,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight public: %v", err)
	}

	contains := func(list []*Flight, id int64) bool {
		for _, f := range list {
			if f.ID == id {
				return true
			}
		}
		return false
	}
	mustList := func(viewer int64) []*Flight {
		t.Helper()
		got, err := s.ListVisibleFlights(ctx, viewer, false, true)
		if err != nil {
			t.Fatalf("ListVisibleFlights: %v", err)
		}
		return got
	}

	// Creator sees both.
	got := mustList(creator)
	if !contains(got, private.ID) || !contains(got, public.ID) {
		t.Errorf("creator: got %v, want both flights", got)
	}

	// Friend sees ONLY the public one (the change — used to see both via
	// standalone friend-of-creator branch).
	got = mustList(friend)
	if contains(got, private.ID) {
		t.Errorf("friend should not see private flight")
	}
	if !contains(got, public.ID) {
		t.Errorf("friend should see public flight")
	}

	// Stranger sees neither (the change — used to see public via is_public).
	got = mustList(stranger)
	if contains(got, private.ID) || contains(got, public.ID) {
		t.Errorf("stranger should see nothing, got %v", got)
	}

	// Pending friend sees nothing — pending is not enough.
	got = mustList(pending)
	if contains(got, private.ID) || contains(got, public.ID) {
		t.Errorf("pending should see nothing, got %v", got)
	}
}

func TestCanView_FriendGatedVisibility(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, creator, friend); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, friend, creator); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}

	private, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PR2", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}
	public, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "PU2", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: true,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}

	cases := []struct {
		name    string
		fid     int64
		viewer  int64
		wantOK  bool
	}{
		{"creator/private", private.ID, creator, true},
		{"creator/public", public.ID, creator, true},
		{"friend/private", private.ID, friend, false},
		{"friend/public", public.ID, friend, true},
		{"stranger/private", private.ID, stranger, false},
		{"stranger/public", public.ID, stranger, false},
	}
	for _, tc := range cases {
		ok, err := s.CanView(ctx, tc.fid, tc.viewer, false)
		if err != nil {
			t.Errorf("%s: err=%v", tc.name, err)
		}
		if ok != tc.wantOK {
			t.Errorf("%s: ok=%v want %v", tc.name, ok, tc.wantOK)
		}
	}
}

func TestListVisibleFlights_ExplicitShareSurvivesNonFriend(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	nonFriend := mkUser(t, s)

	f, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "EX1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight: %v", err)
	}
	if err := s.AddShare(ctx, f.ID, nonFriend); err != nil {
		t.Fatalf("AddShare: %v", err)
	}

	got, err := s.ListVisibleFlights(ctx, nonFriend, false, true)
	if err != nil {
		t.Fatalf("ListVisibleFlights: %v", err)
	}
	found := false
	for _, x := range got {
		if x.ID == f.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("non-friend with explicit share should still see flight")
	}
}
```

- [ ] **Step 2: Run the new tests to confirm they fail.**

```
go test ./internal/store/ -run "TestListVisibleFlights_FriendGatedVisibility|TestCanView_FriendGatedVisibility|TestListVisibleFlights_ExplicitShareSurvivesNonFriend" -v
```

Expected: at least the `friend/private` and `stranger/public` cases fail with the current rule.

- [ ] **Step 3: Tighten `ListVisibleFlights`.** In `internal/store/flights.go`, replace the visibility predicate inside `ListVisibleFlights` (currently lines 514-525):

```go
	if !showAllForSuperuser {
		conds = append(conds, `(created_by = $1
		   OR EXISTS (SELECT 1 FROM flight_passengers
		              WHERE flight_id = flights.id AND user_id = $1)
		   OR EXISTS (SELECT 1 FROM flight_shares
		              WHERE flight_id = flights.id AND user_id = $1)
		   OR (is_public = TRUE
		       AND EXISTS (SELECT 1 FROM friendships f
		                   WHERE f.status = 'accepted'
		                     AND $1 IN (f.user_low, f.user_high)
		                     AND flights.created_by IN (f.user_low, f.user_high))))`)
		args = append(args, viewerID)
	}
```

Also update the doc comment above `ListVisibleFlights` to read:

```
// ListVisibleFlights returns flights the viewer is allowed to see.
// Visibility rule: created_by=viewer OR passenger OR share-list OR
// (is_public AND friend-of-creator with accepted friendship) OR
// (showAllForSuperuser AND caller is superuser). The superuser-show-all
// branch is gated by the caller — pass true only when the request
// actually originated from a superuser session that opted in.
```

- [ ] **Step 4: Tighten `CanView`.** In the same file, replace the predicate inside `CanView` (currently lines 567-581):

```go
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM flights
			WHERE id = $1
			  AND (created_by = $2
			       OR EXISTS (SELECT 1 FROM flight_passengers
			                  WHERE flight_id = $1 AND user_id = $2)
			       OR EXISTS (SELECT 1 FROM flight_shares
			                  WHERE flight_id = $1 AND user_id = $2)
			       OR (is_public = TRUE
			           AND EXISTS (SELECT 1 FROM friendships f
			                       WHERE f.status = 'accepted'
			                         AND $2 IN (f.user_low, f.user_high)
			                         AND flights.created_by IN (f.user_low, f.user_high)))))`,
		flightID, viewerID).Scan(&ok)
	return ok, err
```

- [ ] **Step 5: Run the new tests to verify they pass.**

```
go test ./internal/store/ -run "TestListVisibleFlights_FriendGatedVisibility|TestCanView_FriendGatedVisibility|TestListVisibleFlights_ExplicitShareSurvivesNonFriend" -v
```

Expected: PASS.

- [ ] **Step 6: Run the full store test suite to confirm nothing else broke.**

```
go test ./internal/store/...
```

Expected: PASS. If any existing visibility-related test fails, inspect — pre-existing tests may have relied on the now-removed standalone friend branch and need updating (the migration-seeded everyone-friends data only matters in handler integration tests, not store unit tests that build their own graph).

- [ ] **Step 7: Commit.**

```
git add internal/store/flights.go internal/store/flights_test.go
git commit -m "feat(store): gate flight visibility on accepted friendship + is_public"
```

---

## Task 3: Tighten `VisibleUserIDs`

The SSE fan-out helper must match the new read rule, otherwise events leak to bystanders who can no longer load the flight.

**Files:**
- Modify: `internal/store/flights.go:461-496`
- Modify: `internal/store/flights_test.go`

- [ ] **Step 1: Write the failing test.** Append to `internal/store/flights_test.go`:

```go
func TestVisibleUserIDs_FriendGated(t *testing.T) {
	s := newStore(t)
	now := time.Now()

	creator := mkUser(t, s)
	friend := mkUser(t, s)
	stranger := mkUser(t, s)
	pendingUser := mkUser(t, s)
	passenger := mkUser(t, s)
	sharedUser := mkUser(t, s)

	if _, err := s.RequestFriendship(ctx, creator, friend); err != nil {
		t.Fatalf("req friend: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, friend, creator); err != nil {
		t.Fatalf("accept friend: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, creator, pendingUser); err != nil {
		t.Fatalf("req pending: %v", err)
	}

	// Private flight: only creator + explicit passenger + explicit share.
	priv, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "VU1", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: false,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight private: %v", err)
	}
	if err := s.AddPassenger(ctx, priv.ID, passenger); err != nil {
		t.Fatalf("AddPassenger: %v", err)
	}
	if err := s.AddShare(ctx, priv.ID, sharedUser); err != nil {
		t.Fatalf("AddShare: %v", err)
	}

	uids, err := s.VisibleUserIDs(ctx, priv.ID)
	if err != nil {
		t.Fatalf("VisibleUserIDs: %v", err)
	}
	want := map[int64]bool{creator: true, passenger: true, sharedUser: true}
	got := map[int64]bool{}
	for _, u := range uids {
		got[u] = true
	}
	for u := range want {
		if !got[u] {
			t.Errorf("missing %d in viewers", u)
		}
	}
	if got[friend] || got[stranger] || got[pendingUser] {
		t.Errorf("non-passenger/non-share viewers leaked: friend=%v stranger=%v pending=%v",
			got[friend], got[stranger], got[pendingUser])
	}

	// Public flight: creator's accepted friends join; strangers and pending do not.
	pub, err := s.CreateFlight(ctx, CreateFlightPayload{
		Ident: "VU2", ScheduledOut: now, ScheduledIn: now.Add(time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK", IsPublic: true,
	}, creator)
	if err != nil {
		t.Fatalf("CreateFlight public: %v", err)
	}
	uids, err = s.VisibleUserIDs(ctx, pub.ID)
	if err != nil {
		t.Fatalf("VisibleUserIDs public: %v", err)
	}
	got = map[int64]bool{}
	for _, u := range uids {
		got[u] = true
	}
	if !got[creator] || !got[friend] {
		t.Errorf("public flight should include creator and friend; got %v", got)
	}
	if got[stranger] || got[pendingUser] {
		t.Errorf("public flight should not include stranger/pending; got %v", got)
	}
}
```

- [ ] **Step 2: Run the test.**

```
go test ./internal/store/ -run TestVisibleUserIDs_FriendGated -v
```

Expected: FAIL — currently the helper unions all friends regardless of `is_public`.

- [ ] **Step 3: Update `VisibleUserIDs`.** Replace the function in `internal/store/flights.go` (currently lines 461-496) with:

```go
// VisibleUserIDs returns the union of {creator, passengers, share-list,
// + creator's accepted friends IFF the flight is public} for a single flight
// — the exact set of user IDs that can see the flight through any
// non-superuser-override path. Used by publishers to populate the VisibleTo
// set on SSE events before broadcasting.
//
// The shape matches ListVisibleFlights/CanView: friends only join when the
// flight is public. Callers that want the public-broadcast path explicitly
// (Hub.publishPublic) should consult Flight.IsPublic separately.
func (s *Store) VisibleUserIDs(ctx context.Context, flightID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT created_by FROM flights WHERE id = $1 AND created_by IS NOT NULL
		UNION
		SELECT user_id FROM flight_passengers WHERE flight_id = $1
		UNION
		SELECT user_id FROM flight_shares     WHERE flight_id = $1
		UNION
		SELECT CASE WHEN f.user_low = flights.created_by
		            THEN f.user_high ELSE f.user_low END
		FROM friendships f, flights
		WHERE flights.id = $1
		  AND flights.is_public = TRUE
		  AND f.status = 'accepted'
		  AND flights.created_by IN (f.user_low, f.user_high)`, flightID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run the test to confirm it passes.**

```
go test ./internal/store/ -run TestVisibleUserIDs_FriendGated -v
```

Expected: PASS.

- [ ] **Step 5: Run the full store + handler suite.**

```
go test ./internal/store/... ./internal/handlers/...
```

Expected: PASS. If any handler-integration test depending on SSE delivery to a "friend" who is now not a passenger/share/public-friend fails, that is the regression we'd want — adjust the test to set up the appropriate friendship + is_public combination.

- [ ] **Step 6: Commit.**

```
git add internal/store/flights.go internal/store/flights_test.go
git commit -m "feat(store): scope VisibleUserIDs friends-join to is_public flights"
```

---

## Task 4: Enforce friendship on `addPassenger` and `addShare` handlers

**Files:**
- Modify: `internal/handlers/flights.go` (functions `addPassenger`, `addShare`)
- Modify: `internal/handlers/handlers_test.go`

- [ ] **Step 1: Write the failing tests.** Append to `internal/handlers/handlers_test.go`. Use the existing test scaffolding (`newTestEnv`, etc.) — search the file for an existing `addPassenger` test to match style. Adapt the names/signatures to match (an existing test in the file uses `e.store.AddPassenger`; the integration tests typically use the HTTP-level helper). The two new cases:

```go
func TestAddPassengerRequiresFriendship(t *testing.T) {
	e := newTestEnv(t)
	defer e.close()

	creator := e.signedInUser(t, "creator-aps")
	stranger := testsupport.InsertUser(t, e.store.pool, "stranger-aps", false, true)

	f := mustCreateFlight(t, e, creator, "AP1")

	// Stranger is not a friend → 400.
	body := bytes.NewBufferString(fmt.Sprintf(`{"user_id":%d}`, stranger))
	w := e.request(t, http.MethodPost, fmt.Sprintf("/api/flights/%d/passengers", f.ID), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("addPassenger non-friend = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not a friend") {
		t.Errorf("error body = %q, expected 'not a friend' message", w.Body.String())
	}

	// Befriend → succeeds.
	friend := stranger
	if _, err := e.store.RequestFriendship(ctx, creator.ID, friend); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if _, err := e.store.AcceptFriendship(ctx, friend, creator.ID); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	body = bytes.NewBufferString(fmt.Sprintf(`{"user_id":%d}`, friend))
	w = e.request(t, http.MethodPost, fmt.Sprintf("/api/flights/%d/passengers", f.ID), body)
	if w.Code != http.StatusNoContent {
		t.Fatalf("addPassenger friend = %d, want 204", w.Code)
	}

	// Creator adding self is always allowed (skip the friendship check for self).
	body = bytes.NewBufferString(fmt.Sprintf(`{"user_id":%d}`, creator.ID))
	w = e.request(t, http.MethodPost, fmt.Sprintf("/api/flights/%d/passengers", f.ID), body)
	if w.Code != http.StatusNoContent {
		t.Fatalf("addPassenger self = %d, want 204", w.Code)
	}
}

func TestAddShareRequiresFriendship(t *testing.T) {
	e := newTestEnv(t)
	defer e.close()

	creator := e.signedInUser(t, "creator-ash")
	stranger := testsupport.InsertUser(t, e.store.pool, "stranger-ash", false, true)

	f := mustCreateFlight(t, e, creator, "AS1")

	body := bytes.NewBufferString(fmt.Sprintf(`{"user_id":%d}`, stranger))
	w := e.request(t, http.MethodPost, fmt.Sprintf("/api/flights/%d/shares", f.ID), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("addShare non-friend = %d, want 400", w.Code)
	}

	if _, err := e.store.RequestFriendship(ctx, creator.ID, stranger); err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if _, err := e.store.AcceptFriendship(ctx, stranger, creator.ID); err != nil {
		t.Fatalf("AcceptFriendship: %v", err)
	}
	body = bytes.NewBufferString(fmt.Sprintf(`{"user_id":%d}`, stranger))
	w = e.request(t, http.MethodPost, fmt.Sprintf("/api/flights/%d/shares", f.ID), body)
	if w.Code != http.StatusNoContent {
		t.Fatalf("addShare friend = %d, want 204", w.Code)
	}
}
```

If the existing test file does not have helpers named `newTestEnv`, `e.signedInUser`, `e.request`, `mustCreateFlight`, adapt the test bodies to use whatever pattern is in the file (look at the existing `TestAddPassenger*` and `TestCreateFlight*` cases nearby). The semantics must be: send a request as `creator`, assert 400 for non-friend, befriend, assert 204 for friend.

- [ ] **Step 2: Run the failing tests.**

```
go test ./internal/handlers/ -run "TestAddPassengerRequiresFriendship|TestAddShareRequiresFriendship" -v
```

Expected: FAIL (currently no friendship check; non-friend additions succeed with 204).

- [ ] **Step 3: Implement the check in `addPassenger`.** In `internal/handlers/flights.go`, modify `addPassenger` (currently lines 221-246):

```go
func (a *API) addPassenger(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), fid, me, w); err != nil {
		return
	}
	var in userIDReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := a.requireFriendOfCreator(r.Context(), fid, in.UserID, w); err != nil {
		return
	}
	if err := a.Store.AddPassenger(r.Context(), fid, in.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Implement the check in `addShare`.** In the same file, modify `addShare` (currently lines 271-296):

```go
func (a *API) addShare(w http.ResponseWriter, r *http.Request) {
	fid, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireEdit(r.Context(), fid, me, w); err != nil {
		return
	}
	var in userIDReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.UserID == 0 {
		writeError(w, http.StatusBadRequest, "user_id required")
		return
	}
	if err := a.requireFriendOfCreator(r.Context(), fid, in.UserID, w); err != nil {
		return
	}
	if err := a.Store.AddShare(r.Context(), fid, in.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.publishFlightByID(r.Context(), fid)
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Add the `requireFriendOfCreator` helper.** Add to `internal/handlers/flights.go`, near `flightViewers` / `flightIsPublic` (the existing helper block, around line 384):

```go
// requireFriendOfCreator writes a 400 response and returns a non-nil error
// when target is neither the flight's creator nor an accepted friend of the
// creator. Used to gate addPassenger / addShare / createFlight passenger and
// share-list mutations so we never link a flight to a non-friend.
//
// The check is against the *creator*'s friend graph, not the actor's — a
// superuser editing on someone else's behalf still has to respect the
// creator's friendships.
func (a *API) requireFriendOfCreator(ctx context.Context, flightID, target int64, w http.ResponseWriter) error {
	f, err := a.Store.FlightByID(ctx, flightID)
	if err != nil {
		handleStoreErr(w, err)
		return err
	}
	if target == f.CreatedBy {
		return nil
	}
	ok, err := a.Store.AreAcceptedFriends(ctx, f.CreatedBy, target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return err
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "target is not a friend of the flight creator")
		return errNotFriend
	}
	return nil
}

var errNotFriend = errors.New("not a friend of creator")
```

If `errors` isn't already imported in `internal/handlers/flights.go`, add it to the import block. `FlightByID` is already used elsewhere in this file.

- [ ] **Step 6: Run the tests to verify they pass.**

```
go test ./internal/handlers/ -run "TestAddPassengerRequiresFriendship|TestAddShareRequiresFriendship" -v
```

Expected: PASS.

- [ ] **Step 7: Run the full handler suite.**

```
go test ./internal/handlers/...
```

Expected: PASS. Pre-existing tests that add a non-friend passenger/share may need a friendship setup added before the call — fix them in this commit by inserting the appropriate `RequestFriendship` + `AcceptFriendship` calls.

- [ ] **Step 8: Commit.**

```
git add internal/handlers/flights.go internal/handlers/handlers_test.go
git commit -m "feat(api): reject non-friend passenger/share additions"
```

---

## Task 5: Enforce friendship on `createFlight` passenger/share loops

`createFlight` accepts `passenger_ids` and `shared_user_ids` in the create payload. Pre-validate all of them (no per-row rollback needed) before any DB writes.

**Files:**
- Modify: `internal/handlers/flights.go` (function `createFlight`)
- Modify: `internal/handlers/handlers_test.go`

- [ ] **Step 1: Write the failing test.** Append:

```go
func TestCreateFlightRejectsNonFriendPassengerAndShare(t *testing.T) {
	e := newTestEnv(t)
	defer e.close()

	creator := e.signedInUser(t, "creator-cf")
	stranger := testsupport.InsertUser(t, e.store.pool, "stranger-cf", false, true)

	body := fmt.Sprintf(`{
		"ident":"NF1",
		"scheduled_out":"2030-01-01T00:00:00Z",
		"scheduled_in":"2030-01-01T05:00:00Z",
		"origin_iata":"LHR","dest_iata":"JFK",
		"passenger_ids":[%d]
	}`, stranger)
	w := e.request(t, http.MethodPost, "/api/flights", bytes.NewBufferString(body))
	if w.Code != http.StatusBadRequest {
		t.Errorf("createFlight non-friend passenger = %d, want 400", w.Code)
	}

	body = fmt.Sprintf(`{
		"ident":"NF2",
		"scheduled_out":"2030-01-01T00:00:00Z",
		"scheduled_in":"2030-01-01T05:00:00Z",
		"origin_iata":"LHR","dest_iata":"JFK",
		"shared_user_ids":[%d]
	}`, stranger)
	w = e.request(t, http.MethodPost, "/api/flights", bytes.NewBufferString(body))
	if w.Code != http.StatusBadRequest {
		t.Errorf("createFlight non-friend share = %d, want 400", w.Code)
	}

	// Verify no flight rows were created (pre-validate, not partial-commit).
	var n int
	if err := e.store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM flights WHERE created_by = $1`, creator.ID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected no flights created; got %d", n)
	}
}
```

- [ ] **Step 2: Run the test.**

```
go test ./internal/handlers/ -run TestCreateFlightRejectsNonFriendPassengerAndShare -v
```

Expected: FAIL — current behaviour creates the flight, then 400s on the passenger/share add, leaving an orphan flight row.

- [ ] **Step 3: Pre-validate in `createFlight`.** In `internal/handlers/flights.go`, modify `createFlight` (currently lines 118-157). Add the validation block immediately after the body has been decoded and before `a.Store.CreateFlight`:

```go
func (a *API) createFlight(w http.ResponseWriter, r *http.Request) {
	var in createFlightReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	me := auth.UserFrom(r.Context())
	for _, uid := range in.PassengerIDs {
		if uid == me.ID {
			continue
		}
		ok, err := a.Store.AreAcceptedFriends(r.Context(), me.ID, uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "passenger is not a friend")
			return
		}
	}
	for _, uid := range in.SharedUserIDs {
		if uid == me.ID {
			continue
		}
		ok, err := a.Store.AreAcceptedFriends(r.Context(), me.ID, uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "share target is not a friend")
			return
		}
	}
	f, err := a.Store.CreateFlight(r.Context(), store.CreateFlightPayload{
		Ident:        in.Ident,
		ScheduledOut: in.ScheduledOut,
		ScheduledIn:  in.ScheduledIn,
		OriginIATA:   in.OriginIATA,
		DestIATA:     in.DestIATA,
		ICAO24:       in.ICAO24,
		Notes:        in.Notes,
		IsPublic:     in.IsPublic,
	}, me.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f = a.backfillCoordsIfNeeded(r.Context(), f)
	for _, uid := range in.PassengerIDs {
		if err := a.Store.AddPassenger(r.Context(), f.ID, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, uid := range in.SharedUserIDs {
		if err := a.Store.AddShare(r.Context(), f.ID, uid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	passengers, _ := a.Store.PassengersByFlight(r.Context(), []int64{f.ID})
	shares, _ := a.Store.SharedUserIDsByFlight(r.Context(), []int64{f.ID})
	dto := api.ToFlightDTO(f, passengers[f.ID], shares[f.ID], nil, nil)
	a.publishFlightDTO(r.Context(), dto)
	writeJSON(w, http.StatusCreated, dto)
}
```

The pre-validation uses `me.ID` (the actor) as the creator, since in `createFlight` the actor IS the new flight's creator — a superuser cannot use this endpoint to create a flight under someone else's name.

- [ ] **Step 4: Run the test to verify it passes.**

```
go test ./internal/handlers/ -run TestCreateFlightRejectsNonFriendPassengerAndShare -v
```

Expected: PASS.

- [ ] **Step 5: Run the full handler test suite.**

```
go test ./internal/handlers/...
```

Expected: PASS. Existing `TestCreateFlight*` tests that include a passenger may now need a friendship setup; add it in this commit if so.

- [ ] **Step 6: Commit.**

```
git add internal/handlers/flights.go internal/handlers/handlers_test.go
git commit -m "feat(api): pre-validate friendship in createFlight passenger/share lists"
```

---

## Task 6: Add `friendships` slice + `refreshFriendships` to the store

Lift friendships into the Zustand store so a `useFriendUsers()` selector can be used by FlightDialog and FriendsDialog without each component fetching independently.

**Files:**
- Modify: `web/src/state/store.ts`
- Modify: `web/src/state/store.test.ts`

- [ ] **Step 1: Write the failing test.** Append to `web/src/state/store.test.ts`:

```ts
describe('friendships slice', () => {
  it('starts empty and refreshFriendships loads from the API', async () => {
    const fixtures = [
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 },
      { user_low: 1, user_high: 3, friend_id: 3, status: 'pending', requested_by: 1, direction: 'outgoing' },
    ];
    vi.mocked(api.listFriends).mockResolvedValueOnce(fixtures as never);

    expect(useStore.getState().friendships).toEqual([]);
    await useStore.getState().refreshFriendships();
    expect(useStore.getState().friendships).toEqual(fixtures);
  });

  it('refreshAll() also refreshes friendships', async () => {
    vi.mocked(api.listFlights).mockResolvedValue([]);
    vi.mocked(api.listUsers).mockResolvedValue([]);
    vi.mocked(api.listFriends).mockResolvedValue([
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 } as never,
    ]);

    await useStore.getState().refreshAll();
    expect(api.listFriends).toHaveBeenCalled();
    expect(useStore.getState().friendships).toHaveLength(1);
  });
});
```

Make sure the file's existing `vi.mock('../api/client', ...)` block mocks `listFriends`. If not, extend it (look at the top of the file — it should already mock `listFlights`, `listUsers`, etc.).

- [ ] **Step 2: Run the test.**

```
cd web && npm run test -- src/state/store.test.ts -t "friendships slice"
```

Expected: FAIL — `friendships` and `refreshFriendships` don't exist yet.

- [ ] **Step 3: Add the slice to the store.** In `web/src/state/store.ts`:

  Add `Friendship` to the type imports:
  ```ts
  import type {
    Capabilities,
    CreateFlightInput,
    Flight,
    Friendship,
    InviteUserInput,
    UpdateFlightInput,
    UpdateUserInput,
    User,
  } from '../api/types';
  ```

  Extend `AppState`:
  ```ts
    friendships: Friendship[];
    refreshFriendships: () => Promise<void>;
  ```
  (Insert `friendships` next to `users`, and `refreshFriendships` next to `refreshUsers`.)

  Add initial state and action implementation. In the `create` body, after `users: []`, add:
  ```ts
    friendships: [],
  ```

  After the `refreshUsers` action, add:
  ```ts
    async refreshFriendships() {
      try {
        const friendships = await api.listFriends();
        set({ friendships });
      } catch (err) {
        set({ error: errorMessage(err) });
      }
    },
  ```

  Update `refreshAll`:
  ```ts
    async refreshAll() {
      await Promise.all([
        get().refreshFlights(),
        get().refreshUsers(),
        get().refreshFriendships(),
      ]);
    },
  ```

- [ ] **Step 4: Run the test to verify it passes.**

```
cd web && npm run test -- src/state/store.test.ts -t "friendships slice"
```

Expected: PASS.

- [ ] **Step 5: Run the full store test file.**

```
cd web && npm run test -- src/state/store.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit.**

```
git add web/src/state/store.ts web/src/state/store.test.ts
git commit -m "feat(web): lift friendships into the Zustand store"
```

---

## Task 7: Migrate FriendsDialog to consume the store-backed friendships

Replace the dialog's local `useState<Friendship[]>` with `useStore((s) => s.friendships)` and call `refreshFriendships()` after mutations. Keeps a single source of truth.

**Files:**
- Modify: `web/src/components/FriendsDialog.tsx`
- Modify: `web/src/components/FriendsDialog.test.tsx`

- [ ] **Step 1: Read the existing FriendsDialog test file to see the existing mock pattern.**

```
cd web && head -50 src/components/FriendsDialog.test.tsx
```

This file mocks `../api/client` at the top and asserts on `h.api.listFriends`, etc. We will keep those mocks; what changes is that `FriendsDialog` now goes through `useStore.refreshFriendships()` instead of calling `api.listFriends` directly. The store action is implemented in terms of `api.listFriends`, so the mock surface is unchanged — but we need to ensure the dialog reads from the store.

- [ ] **Step 2: Update `FriendsDialog.tsx`.** Replace the local state block:

  Remove:
  ```ts
  const [friends, setFriends] = useState<Friendship[]>([]);
  ```

  Add (near the top of the component, with the other `useStore` lines):
  ```ts
  const friends = useStore((s) => s.friendships);
  const refreshFriendships = useStore((s) => s.refreshFriendships);
  ```

  Replace the initial-load effect:
  ```ts
  useEffect(() => {
    if (!open) return;
    void refreshFriendships();
    setInviteFeedback(null);
  }, [open, refreshFriendships]);
  ```

  In `handleInvite`, replace:
  ```ts
  const updated = await api.listFriends();
  setFriends(updated);
  ```
  with:
  ```ts
  await refreshFriendships();
  ```

  In `handleAccept`, replace:
  ```ts
  const updated = await api.acceptFriend(other);
  setFriends((rows) => rows.map((r) => (r.friend_id === other ? updated : r)));
  ```
  with:
  ```ts
  await api.acceptFriend(other);
  await refreshFriendships();
  ```

  In `handleRemove` (search for it in the file), replace the local-state update similarly:
  ```ts
  await api.removeFriend(other);
  await refreshFriendships();
  ```

  Remove the now-unused `useState` import if it's no longer referenced; remove the `Friendship` type import if no longer used. Keep `User` and the others.

- [ ] **Step 3: Update tests.** The existing FriendsDialog tests render the dialog and inspect the API mock; if any test relied on `setFriends` being called with a specific arg (it shouldn't), update to expect `api.listFriends` to be called again after the mutation. Look for assertions like `expect(h.api.listFriends).toHaveBeenCalledTimes(...)` — keep them; the counts may change because the store action now drives the second load instead of inline code. Run the suite and adjust counts as needed.

- [ ] **Step 4: Run the FriendsDialog tests.**

```
cd web && npm run test -- src/components/FriendsDialog.test.tsx
```

Expected: PASS. If a `toHaveBeenCalledTimes` assertion fails by 1, adjust to the new count.

- [ ] **Step 5: Run lint to catch unused imports.**

```
cd web && npm run lint
```

Expected: clean.

- [ ] **Step 6: Commit.**

```
git add web/src/components/FriendsDialog.tsx web/src/components/FriendsDialog.test.tsx
git commit -m "refactor(web): read FriendsDialog state from the store"
```

---

## Task 8: Add `useFriendUsers()` selector

A hook that returns the subset of `users` for whom an `accepted` friendship with the viewer exists.

**Files:**
- Create: `web/src/state/friendUsers.ts`
- Create: `web/src/state/friendUsers.test.ts`

- [ ] **Step 1: Write the failing test.** Create `web/src/state/friendUsers.test.ts`:

```ts
import { describe, expect, it, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';

import { useStore } from './store';
import { useFriendUsers } from './friendUsers';
import type { Friendship, User } from '../api/types';

function user(id: number, name = `user${id}`): User {
  return {
    id,
    username: name,
    email: `${name}@example.com`,
    is_superuser: false,
    is_active: true,
    avatar_url: '',
  } as User;
}

function friendship(viewer: number, other: number, status: Friendship['status']): Friendship {
  const [low, high] = viewer < other ? [viewer, other] : [other, viewer];
  return {
    user_low: low,
    user_high: high,
    friend_id: other,
    status,
    requested_by: viewer,
  } as Friendship;
}

describe('useFriendUsers', () => {
  beforeEach(() => {
    useStore.setState({
      me: user(1) as never,
      users: [user(1), user(2), user(3), user(4)],
      friendships: [
        friendship(1, 2, 'accepted'),
        friendship(1, 3, 'pending'),
      ],
    });
  });

  it('returns only users with an accepted friendship to me', () => {
    const { result } = renderHook(() => useFriendUsers());
    expect(result.current.map((u) => u.id)).toEqual([2]);
  });

  it('returns an empty list when me is null', () => {
    useStore.setState({ me: null });
    const { result } = renderHook(() => useFriendUsers());
    expect(result.current).toEqual([]);
  });

  it('excludes me from the friend list even if a stray self-loop exists in users', () => {
    useStore.setState({
      friendships: [
        friendship(1, 2, 'accepted'),
        friendship(1, 4, 'accepted'),
      ],
    });
    const { result } = renderHook(() => useFriendUsers());
    expect(result.current.map((u) => u.id).sort()).toEqual([2, 4]);
  });
});
```

- [ ] **Step 2: Run the test.**

```
cd web && npm run test -- src/state/friendUsers.test.ts
```

Expected: FAIL — file doesn't exist.

- [ ] **Step 3: Implement.** Create `web/src/state/friendUsers.ts`:

```ts
import { useMemo } from 'react';

import { useStore } from './store';
import type { User } from '../api/types';

/**
 * Returns the subset of loaded users that the signed-in viewer has an
 * accepted friendship with. Used by share/passenger pickers to limit
 * options to friends. Returns [] while `me` is unknown.
 */
export function useFriendUsers(): User[] {
  const meId = useStore((s) => s.me?.id);
  const users = useStore((s) => s.users);
  const friendships = useStore((s) => s.friendships);

  return useMemo(() => {
    if (meId == null) return [];
    const friendIds = new Set<number>();
    for (const f of friendships) {
      if (f.status !== 'accepted') continue;
      const other = f.user_low === meId ? f.user_high : f.user_low;
      if (other !== meId) friendIds.add(other);
    }
    return users.filter((u) => friendIds.has(u.id));
  }, [meId, users, friendships]);
}
```

- [ ] **Step 4: Run the test to verify it passes.**

```
cd web && npm run test -- src/state/friendUsers.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```
git add web/src/state/friendUsers.ts web/src/state/friendUsers.test.ts
git commit -m "feat(web): add useFriendUsers selector"
```

---

## Task 9: Rename `showMineOnly` → `showFriends` with localStorage migration

Inverted semantics: new default is OFF (= only mine). Migration: legacy `'0'` (was "Only my flights OFF" = show everyone) → new ON; legacy absent or `'1'` (was "Only my flights ON" = mine) → new OFF.

**Files:**
- Modify: `web/src/state/store.ts`
- Modify: `web/src/state/store.test.ts`

- [ ] **Step 1: Write the failing tests.** In `web/src/state/store.test.ts`, locate the existing `showMineOnly` tests (around lines 394-460). Replace them with these — and **delete the old `showMineOnly`-named tests** in the same edit:

```ts
describe('showFriends state and persistence', () => {
  const LEGACY_KEY = 'ft.show_mine_only';
  const NEW_KEY = 'ft.show_friends';

  beforeEach(() => {
    window.localStorage.removeItem(LEGACY_KEY);
    window.localStorage.removeItem(NEW_KEY);
  });

  it('defaults to false (only-mine view) for a fresh user', () => {
    // Re-import the module to pick up the fresh localStorage.
    vi.resetModules();
    return import('./store').then(({ useStore: fresh }) => {
      expect(fresh.getState().showFriends).toBe(false);
    });
  });

  it('migrates legacy showMineOnly=ON (default/absent) to showFriends=false', () => {
    window.localStorage.removeItem(LEGACY_KEY);
    vi.resetModules();
    return import('./store').then(({ useStore: fresh }) => {
      expect(fresh.getState().showFriends).toBe(false);
    });
  });

  it("migrates legacy showMineOnly='0' (off) to showFriends=true and clears the legacy key", () => {
    window.localStorage.setItem(LEGACY_KEY, '0');
    vi.resetModules();
    return import('./store').then(({ useStore: fresh }) => {
      expect(fresh.getState().showFriends).toBe(true);
      expect(window.localStorage.getItem(LEGACY_KEY)).toBeNull();
    });
  });

  it("does not migrate when the new key is already present", () => {
    window.localStorage.setItem(NEW_KEY, '1');
    window.localStorage.setItem(LEGACY_KEY, '0');
    vi.resetModules();
    return import('./store').then(({ useStore: fresh }) => {
      expect(fresh.getState().showFriends).toBe(true);
      // Legacy key is left alone if migration didn't run.
      expect(window.localStorage.getItem(LEGACY_KEY)).toBe('0');
    });
  });

  it('setShowFriends(true) persists "1" and turns the toggle on', () => {
    useStore.getState().setShowFriends(true);
    expect(useStore.getState().showFriends).toBe(true);
    expect(window.localStorage.getItem(NEW_KEY)).toBe('1');
  });

  it('setShowFriends(false) removes the key', () => {
    window.localStorage.setItem(NEW_KEY, '1');
    useStore.setState({ showFriends: true });
    useStore.getState().setShowFriends(false);
    expect(useStore.getState().showFriends).toBe(false);
    expect(window.localStorage.getItem(NEW_KEY)).toBeNull();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail.**

```
cd web && npm run test -- src/state/store.test.ts -t "showFriends"
```

Expected: FAIL — `showFriends` and `setShowFriends` don't exist; `loadShowMineOnly` still in use.

- [ ] **Step 3: Update the store.** In `web/src/state/store.ts`:

  Replace the key constant block:
  ```ts
  const SHOW_ALL_KEY = 'ft.show_all';
  const SHOW_OLD_KEY = 'ft.show_old';
  const SHOW_FRIENDS_KEY = 'ft.show_friends';
  const LEGACY_SHOW_MINE_ONLY_KEY = 'ft.show_mine_only';
  ```

  Replace the `AppState` lines:
  ```ts
  // was: showMineOnly: boolean;
  /** When true, the flight list also includes flights from other users that
   * those users have shared with the viewer (via the per-user share list,
   * via "Share with all friends", or as a passenger). When false (the
   * default), the list is restricted to flights the viewer created or is
   * a passenger on. Persisted to localStorage with default-OFF semantics
   * — absence means OFF, '1' means ON. */
  showFriends: boolean;
  ```

  And in the action set:
  ```ts
  // was: setShowMineOnly: (v: boolean) => void;
  setShowFriends: (v: boolean) => void;
  ```

  Replace the load/persist helpers (delete `loadShowMineOnly` / `persistShowMineOnly`, add):
  ```ts
  // Default-OFF semantics with a one-shot migration from the legacy
  // `ft.show_mine_only` key. Legacy '0' (Only my flights OFF — used to mean
  // "show all loaded flights") becomes new ON (showFriends = true). Any
  // other legacy state (absent or '1' / unexpected value) defaults to OFF,
  // which matches the new default. The legacy key is removed once the
  // migration runs so subsequent loads use the new key only.
  function loadShowFriends(): boolean {
    try {
      const existing = window.localStorage.getItem(SHOW_FRIENDS_KEY);
      if (existing !== null) {
        return existing === '1';
      }
      const legacy = window.localStorage.getItem(LEGACY_SHOW_MINE_ONLY_KEY);
      if (legacy === null) return false;
      const migrated = legacy === '0';
      if (migrated) {
        window.localStorage.setItem(SHOW_FRIENDS_KEY, '1');
      }
      window.localStorage.removeItem(LEGACY_SHOW_MINE_ONLY_KEY);
      return migrated;
    } catch {
      return false;
    }
  }

  function persistShowFriends(v: boolean): void {
    try {
      if (v) window.localStorage.setItem(SHOW_FRIENDS_KEY, '1');
      else window.localStorage.removeItem(SHOW_FRIENDS_KEY);
    } catch {
      // ignore — best effort
    }
  }
  ```

  Update the initial-state block:
  ```ts
  // was: showMineOnly: loadShowMineOnly(),
  showFriends: loadShowFriends(),
  ```

  Replace the action:
  ```ts
  // was: setShowMineOnly(v) { ... }
  setShowFriends(v) {
    persistShowFriends(v);
    set({ showFriends: v });
  },
  ```

  (The exact placement should mirror the existing `setShowMineOnly` definition — copy that structure, just change the names and the persist call.)

- [ ] **Step 4: Run the tests to verify they pass.**

```
cd web && npm run test -- src/state/store.test.ts -t "showFriends"
```

Expected: PASS.

- [ ] **Step 5: Run the full store test file to make sure nothing else broke.**

```
cd web && npm run test -- src/state/store.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit.**

```
git add web/src/state/store.ts web/src/state/store.test.ts
git commit -m "feat(web): rename showMineOnly to showFriends with default-OFF semantics"
```

---

## Task 10: Invert `useVisibleFlights` filter

The hook now consults `showFriends`. When `showFriends === false`, restrict to creator/passenger; when `true`, pass through.

**Files:**
- Modify: `web/src/state/visibleFlights.ts`
- Modify: `web/src/state/visibleFlights.test.ts`

- [ ] **Step 1: Update the tests first.** In `web/src/state/visibleFlights.test.ts`, find the section near lines 124-200 that has `showMineOnly` tests, and replace those `describe`/`it` blocks. The intent is preserved — just renamed and inverted:

  Replace the `it('filters to flights where me is a passenger when showMineOnly is on', ...)` block and the surrounding two (`it('returns the full list when showMineOnly is off, ...')` and the two `showMineOnly: true` follow-ups) with:

```ts
  it('filters to flights where me is creator or passenger when showFriends is off', () => {
    const result = useVisibleFlightsAgainst(
      [createMine(), createOthers(), createWhereImPassenger(), createSharedToMe()],
      {
        ...initial,
        showFriends: false,
        me: { id: ME, username: 'me' } as User,
      },
    );
    expect(result.map((f) => f.id).sort()).toEqual(
      [createMine().id, createWhereImPassenger().id].sort(),
    );
  });

  it('returns the full loaded list when showFriends is on', () => {
    const all = [createMine(), createOthers(), createWhereImPassenger(), createSharedToMe()];
    const result = useVisibleFlightsAgainst(all, {
      ...initial,
      showFriends: true,
      me: { id: ME, username: 'me' } as User,
    });
    expect(result.map((f) => f.id).sort()).toEqual(all.map((f) => f.id).sort());
  });

  it('does not apply the showFriends filter while me is null', () => {
    const all = [createMine(), createOthers()];
    const result = useVisibleFlightsAgainst(all, {
      ...initial,
      showFriends: false,
      me: null,
    });
    expect(result).toHaveLength(all.length);
  });
```

  The existing test file references local helpers `createMine`, `createOthers`, `createWhereImPassenger` and uses `showMineOnly: ...` field names. Use the existing helpers, and if a `createSharedToMe` helper doesn't exist, inline a flight where the viewer is in `shared_user_ids` only (not creator, not passenger). The key is that with `showFriends: false`, that flight should be filtered OUT (matching the design decision that explicit shares only appear when toggle is ON).

  If the existing tests use a wrapper like `useVisibleFlightsAgainst(flights, state)` to set up the store before calling the hook, reuse it. Otherwise, look at how the existing tests do it and follow the same pattern — match the file's style.

  **Important:** also rename any state-setup lines that say `showMineOnly: ...` elsewhere in the test file to `showFriends: ...` (with the inverted value if needed: `showMineOnly: false` was "show everything" which is now `showFriends: true`; `showMineOnly: true` was "only mine" which is now `showFriends: false`). Search the whole file.

- [ ] **Step 2: Run the tests to verify they fail.**

```
cd web && npm run test -- src/state/visibleFlights.test.ts
```

Expected: FAIL — `showMineOnly` no longer exists in store and the new logic isn't wired yet.

- [ ] **Step 3: Update the hook.** Replace `web/src/state/visibleFlights.ts` with:

```ts
import { useEffect, useMemo, useState } from 'react';

import type { Flight } from '../api/types';
import { useStore } from './store';

const OLD_THRESHOLD_MS = 24 * 60 * 60 * 1000;

/** How often `useVisibleFlights` re-evaluates the age filter so flights age
 * out without needing a server event. Exported for tests. */
export const OLD_TICK_MS = 60 * 1000;

/** Pure: returns true when the flight's effective arrival (actual_in,
 * else estimated_in, else scheduled_in) is more than 24h before `nowMs`.
 * Invalid timestamps fall through as not-old — matches the server's
 * COALESCE >= NOW() - 24h predicate at the boundary. */
export function isOld(f: Flight, nowMs: number): boolean {
  const arrIso = f.actual_in ?? f.estimated_in ?? f.scheduled_in;
  const arrMs = Date.parse(arrIso);
  if (Number.isNaN(arrMs)) return false;
  return arrMs < nowMs - OLD_THRESHOLD_MS;
}

/** Returns the subset of loaded flights that should be visible right now,
 * honouring the user's `showOld` and `showFriends` toggles and ageing
 * flights out as time passes (refreshed every OLD_TICK_MS). When
 * `showFriends` is OFF the list is restricted to flights the viewer
 * created or is a passenger on. */
export function useVisibleFlights(): Flight[] {
  const flights = useStore((s) => s.flights);
  const showOld = useStore((s) => s.showOld);
  const showFriends = useStore((s) => s.showFriends);
  const meId = useStore((s) => s.me?.id);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (showOld) return;
    const id = window.setInterval(() => setNow(Date.now()), OLD_TICK_MS);
    return () => window.clearInterval(id);
  }, [showOld]);

  return useMemo(() => {
    let out = flights;
    if (!showOld) out = out.filter((f) => !isOld(f, now));
    // Skip the friend filter while "me" is unknown (auth still loading,
    // or tests that don't populate it) — otherwise we'd hide every flight
    // on first paint. "Mine" = the user is a passenger OR the creator.
    if (!showFriends && meId != null) {
      out = out.filter((f) => f.created_by === meId || f.passenger_ids.includes(meId));
    }
    return out;
  }, [flights, showOld, showFriends, meId, now]);
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

```
cd web && npm run test -- src/state/visibleFlights.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```
git add web/src/state/visibleFlights.ts web/src/state/visibleFlights.test.ts
git commit -m "feat(web): invert flight-list filter — showFriends opt-in"
```

---

## Task 11: Update FlightList toggle label, tooltip, and binding

**Files:**
- Modify: `web/src/components/FlightList.tsx`
- Modify: `web/src/components/FlightList.test.tsx`

- [ ] **Step 1: Update the failing tests first.** In `web/src/components/FlightList.test.tsx`, find the two showMineOnly-related tests (around lines 421-444). Replace them with:

```ts
  it('renders the Show friends flights toggle and reflects the showFriends state', () => {
    state.showFriends = false;
    state.me = { id: 1, username: 'me' } as User;
    render(<FlightList onEditFlight={() => {}} />);
    const toggle = screen.getByLabelText('Show friends flights');
    expect(toggle).not.toBeChecked();
  });

  it('clicking the Show friends flights toggle calls setShowFriends(true) when it is off', async () => {
    state.showFriends = false;
    render(<FlightList onEditFlight={() => {}} />);
    await userEvent.click(screen.getByLabelText('Show friends flights'));
    expect(state.setShowFriends).toHaveBeenCalledWith(true);
  });

  it('clicking the Show friends flights toggle calls setShowFriends(false) when it is on', async () => {
    state.showFriends = true;
    render(<FlightList onEditFlight={() => {}} />);
    await userEvent.click(screen.getByLabelText('Show friends flights'));
    expect(state.setShowFriends).toHaveBeenCalledWith(false);
  });
```

  Also search the test file for any other `showMineOnly` / `setShowMineOnly` references (e.g. in the default `state` object) and rename to `showFriends` / `setShowFriends`. Update the default value: `showMineOnly: false` becomes `showFriends: false` (same effective view).

- [ ] **Step 2: Run the tests to verify they fail.**

```
cd web && npm run test -- src/components/FlightList.test.tsx
```

Expected: FAIL — old labels still in the component.

- [ ] **Step 3: Update the FlightList component.** In `web/src/components/FlightList.tsx`:

  Replace:
  ```ts
  const showMineOnly = useStore((s) => s.showMineOnly);
  const setShowMineOnly = useStore((s) => s.setShowMineOnly);
  ```
  with:
  ```ts
  const showFriends = useStore((s) => s.showFriends);
  const setShowFriends = useStore((s) => s.setShowFriends);
  ```

  Replace the tooltip + toggle block (currently lines 59-74):
  ```tsx
        <Tooltip title="Also show flights from friends that they shared with you.">
          <FormControlLabel
            control={
              <Switch
                checked={showFriends}
                onChange={(e) => setShowFriends(e.target.checked)}
                size="small"
              />
            }
            label={
              <Typography variant="caption" color="text.secondary">
                Show friends flights
              </Typography>
            }
          />
        </Tooltip>
  ```

- [ ] **Step 4: Run the tests to verify they pass.**

```
cd web && npm run test -- src/components/FlightList.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```
git add web/src/components/FlightList.tsx web/src/components/FlightList.test.tsx
git commit -m "feat(web): rename FlightList toggle to Show friends flights"
```

---

## Task 12: Relabel FlightDialog public toggle to "Share with all friends"

**Files:**
- Modify: `web/src/components/FlightDialog.tsx` (function `VisibilityBlock`, ~lines 562-637)
- Modify: `web/src/components/FlightDialog.test.tsx`

- [ ] **Step 1: Update the tests.** In `web/src/components/FlightDialog.test.tsx`, find the three references to `'Share with everyone'` (around lines 182, 463, 473) and replace each with `'Share with all friends'`. Also add a new test for the updated helper text:

```ts
  it('shows the all-friends helper text when the public toggle is on', async () => {
    // ...existing render setup for an editable flight (mirror the surrounding tests)...
    await userEvent.click(screen.getByLabelText('Share with all friends'));
    expect(
      screen.getByText(/shared with all your friends/i),
    ).toBeInTheDocument();
  });
```

  Place the new test next to the existing one that asserts on the helper text (search for `'list is ignored'` or similar).

- [ ] **Step 2: Run the tests to verify they fail.**

```
cd web && npm run test -- src/components/FlightDialog.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Update the component.** In `web/src/components/FlightDialog.tsx`, modify `VisibilityBlock` (lines 575-637):

  Update the comment above (lines 571-574):
  ```ts
  // VisibilityBlock renders the "Share with all friends" toggle + per-user
  // share list, used by both the minimal and full FlightDialog forms. When
  // public, the share list is dimmed and gets a helper note — it's still
  // editable so un-toggling later doesn't lose the curated list.
  ```

  Replace the switch label:
  ```tsx
        label="Share with all friends"
  ```

  Replace the Autocomplete helper text:
  ```tsx
            helperText={
              isPublic
                ? 'Flight is shared with all your friends — this list is ignored until you turn off "Share with all friends".'
                : 'Users listed here can see the flight in addition to its passengers.'
            }
  ```

- [ ] **Step 4: Run the tests to verify they pass.**

```
cd web && npm run test -- src/components/FlightDialog.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```
git add web/src/components/FlightDialog.tsx web/src/components/FlightDialog.test.tsx
git commit -m "feat(web): relabel public toggle to Share with all friends"
```

---

## Task 13: Restrict FlightDialog passenger/share Autocomplete options to friends

Already-attached non-friend users remain as chips; the dropdown only offers accepted friends.

**Files:**
- Modify: `web/src/components/FlightDialog.tsx`
- Modify: `web/src/components/FlightDialog.test.tsx`

- [ ] **Step 1: Write the failing tests.** Append to `web/src/components/FlightDialog.test.tsx`:

```ts
  it('only lists accepted friends as passenger options', async () => {
    // Reuse the file's existing render helper. Set up the store so:
    //   - me has id 1
    //   - users has 1 (me), 2 (friend), 3 (stranger)
    //   - friendships has (1,2) accepted only
    // Then open the dialog in create mode and open the Passengers dropdown.
    state.me = { id: 1, username: 'me' } as User;
    state.users = [
      { id: 1, username: 'me' } as User,
      { id: 2, username: 'friend' } as User,
      { id: 3, username: 'stranger' } as User,
    ];
    state.friendships = [
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 } as never,
    ];
    render(<FlightDialog open editId={null} onClose={() => {}} />);

    // The Passengers field is labelled "Passengers" — adjust to whichever
    // label the file's other tests use.
    const passengersInput = screen.getByLabelText(/passengers/i);
    await userEvent.click(passengersInput);

    expect(screen.getByText('friend')).toBeInTheDocument();
    expect(screen.queryByText('stranger')).not.toBeInTheDocument();
  });

  it('keeps a non-friend chip when editing a flight that already has them as passenger', async () => {
    state.me = { id: 1, username: 'me' } as User;
    state.users = [
      { id: 1, username: 'me' } as User,
      { id: 3, username: 'legacy-non-friend' } as User,
    ];
    state.friendships = [];
    state.flights = [
      {
        id: 42,
        created_by: 1,
        passenger_ids: [3],
        shared_user_ids: [],
        is_public: false,
        // ...whatever other Flight fields the file's helper requires
      } as never,
    ];
    render(<FlightDialog open editId={42} onClose={() => {}} />);

    expect(await screen.findByText('legacy-non-friend')).toBeInTheDocument();
  });
```

  The test scaffolding (`state`, the way `render` is called, the way the file mocks `useStore`) varies — match what the other tests in the file do. If the existing tests use a helper that picks a default `me`, default users, etc., extend it to also accept a `friendships` argument.

- [ ] **Step 2: Run the tests to verify they fail.**

```
cd web && npm run test -- src/components/FlightDialog.test.tsx
```

Expected: FAIL — dropdown still offers strangers.

- [ ] **Step 3: Update the component.** In `web/src/components/FlightDialog.tsx`:

  Add the import (near the other `useStore` import):
  ```ts
  import { useFriendUsers } from '../state/friendUsers';
  ```

  In the `FlightDialog` component body, after `const users = useStore((s) => s.users);`, add:
  ```ts
  const friendUsers = useFriendUsers();
  ```

  Add a helper that merges friend options with whatever is currently selected, so MUI doesn't drop already-attached non-friends from the chips' labels:

  ```ts
  // Autocomplete option lists are limited to friends, but if the flight
  // already has a non-friend in its value (legacy data, or a friend who
  // unfriended after the original share), we still want to render that
  // user's chip with the right label. Merge value into options for that
  // case — non-friends not in value remain hidden.
  function mergeOptions(friends: User[], selected: User[]): User[] {
    const byId = new Map<number, User>();
    for (const u of friends) byId.set(u.id, u);
    for (const u of selected) if (!byId.has(u.id)) byId.set(u.id, u);
    return [...byId.values()];
  }
  ```

  Place the helper at module scope (next to `formatDateOnly` at the bottom of the file, for example).

  In the **minimal form** (around line 338), replace `options={users}` for the passenger Autocomplete with:
  ```ts
              options={mergeOptions(friendUsers, minimal.passengers)}
  ```

  In the **full form** (around line 466), replace `options={users}` for the passenger Autocomplete with:
  ```ts
              options={mergeOptions(friendUsers, form.passengers)}
  ```

  For the `VisibilityBlock` calls (lines 368 and 495), the `users` prop currently receives the full user list. Change those two sites to pass the filtered list — and update `VisibilityBlock` to use the prop as-is (it already does, just rename internally for clarity is optional). The call sites become:

  ```ts
              <VisibilityBlock
                users={mergeOptions(friendUsers, minimal.sharedWith)}
                sharedWith={minimal.sharedWith}
                isPublic={minimal.isPublic}
                disabled={visibilityDisabled}
                onSharedChange={(value) => setMinimal({ ...minimal, sharedWith: value })}
                onPublicChange={(value) => setMinimal({ ...minimal, isPublic: value })}
              />
  ```

  And the corresponding `<VisibilityBlock>` in the full form:

  ```ts
              <VisibilityBlock
                users={mergeOptions(friendUsers, form.sharedWith)}
                sharedWith={form.sharedWith}
                isPublic={form.isPublic}
                disabled={visibilityDisabled}
                onSharedChange={(value) => setForm({ ...form, sharedWith: value })}
                onPublicChange={(value) => setForm({ ...form, isPublic: value })}
              />
  ```

  (The exact prop list should mirror what the file already has; the only change is the `users={...}` value.)

- [ ] **Step 4: Run the tests to verify they pass.**

```
cd web && npm run test -- src/components/FlightDialog.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```
git add web/src/components/FlightDialog.tsx web/src/components/FlightDialog.test.tsx
git commit -m "feat(web): restrict passenger/share autocompletes to friends"
```

---

## Task 14: Full verification — tests, lint, typecheck, manual dev-server smoke

**Files:** none (verification only).

- [ ] **Step 1: Run the full test suite.**

```
make test
```

Expected: PASS (Go + web). If any test fails, fix it. Common breakages from this plan:
- Handler tests that didn't friend the actor+target before adding passenger/share — add the friendship.
- Store tests that asserted visibility for a friend on a private flight — they were testing the now-removed branch; either delete or update with `IsPublic: true`.

- [ ] **Step 2: Run the linter.**

```
make lint
```

Expected: clean.

- [ ] **Step 3: Run the web typechecker.**

```
make typecheck-web
```

Expected: clean. If unused imports remain (e.g. the old `useState`/`Friendship` types in `FriendsDialog.tsx`), remove them.

- [ ] **Step 4: Smoke-test in the dev server.**

```
make dev
```

Then in a browser:

1. Log in.
2. Confirm the FlightList toggle reads **Show friends flights** and is OFF — only flights you created/are a passenger on are visible.
3. Toggle ON — friends' opted-in flights appear.
4. Open a flight as the creator: confirm the switch reads **Share with all friends**. With the switch ON, the helper text reads "Flight is shared with all your friends — ...".
5. Open the Passengers Autocomplete — only friends appear in the dropdown. If a non-friend was already attached to the flight, their chip still shows up in `value`.
6. Try to POST a non-friend in the passenger list via the dev tools network panel — server returns 400.

Note any UI/UX papercuts but don't fix them in this PR unless they are regressions caused by the change.

- [ ] **Step 5: Final commit (only if there are amendments from steps 1-4).** Otherwise skip.

```
git status
# If any fixups were made:
git add -p
git commit -m "fix: address verification feedback for friend-sharing tightening"
```

- [ ] **Step 6: Done.** Hand off to `superpowers:finishing-a-development-branch` to decide whether to open a PR, merge, or leave the branch sitting.

---

## Self-review summary

- **Spec coverage:** every section of the spec maps to one or more tasks (visibility rule → Task 2 + 3; friendship checks → Task 4 + 5; toggle inversion → Task 9 + 10 + 11; relabel → Task 12; autocomplete restriction → Task 13; friends-only selector → Task 8; store lift → Task 6 + 7; existing-data preservation → covered by Task 2's `TestListVisibleFlights_ExplicitShareSurvivesNonFriend` and Task 13's legacy-chip test; tests → distributed across the relevant tasks).
- **Placeholders:** none.
- **Type consistency:** `AreAcceptedFriends(ctx, a, b int64) (bool, error)` is used identically in Tasks 1, 4, 5. `requireFriendOfCreator(ctx, flightID, target int64, w http.ResponseWriter) error` is defined in Task 4 and not used elsewhere. `useFriendUsers(): User[]` is defined in Task 8 and consumed in Task 13. `setShowFriends(v: boolean) => void` and `showFriends: boolean` are defined in Task 9, consumed in Tasks 10 + 11, and tested in Tasks 9 + 11.
