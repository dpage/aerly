# Friend Invite — No-Enumeration Friend List Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the friend-list enumeration leak — outgoing pending invitations should appear in `/api/friends` looking identical whether or not the typed email maps to a registered Aerly user.

**Architecture:** Add a nullable `invited_email` column to `friendships`, populated whenever an invite-by-email creates a pending row. The list endpoint unions friendship rows (with `invited_email` for outgoing pending) and `pending_friend_invites` rows (for invites to unknown emails). The wire DTO carries `email` instead of `friend_id` for outgoing pending rows. A new `DELETE /api/friends/outgoing` endpoint accepts an email so the inviter can cancel without ever learning the target's user_id.

**Tech Stack:** Go 1.24, pgx/v5, embedded SQL migrations, React 18 + Material UI, Vitest + React Testing Library.

**Spec:** `docs/superpowers/specs/2026-05-28-friend-invite-no-enumeration-design.md`

---

## File Map

**Create:**
- `migrations/0009_friend_invited_email.up.sql` — add column, backfill, CHECK constraint.
- `migrations/0009_friend_invited_email.down.sql` — drop constraint and column.

**Modify (backend):**
- `internal/store/friends.go` — `Friendship` struct gains `InvitedEmail`; `RequestFriendship` signature gains `invitedEmail`; `ListFriendships` selects the column; add `ListOutgoingPendingInvites`; add `CancelOutgoingInvite`; `AcceptFriendship` cleans up `pending_friend_invites` defensively.
- `internal/store/friends_test.go` — extend existing tests, add new ones.
- `internal/api/dto.go` — `FriendshipDTO.FriendID` → `omitempty`; add `Email`; add `OutgoingInviteToFriendshipDTO`.
- `internal/api/dto_test.go` — assert outgoing-pending DTO shape.
- `internal/handlers/friends.go` — thread invited email through `inviteFriend`/`inviteFriendByUserID`; `listFriends` merges both sources; new `cancelOutgoingInvite`.
- `internal/handlers/friends_test.go` — new no-enumeration tests.
- `internal/handlers/handlers.go` — register `DELETE /api/friends/outgoing`.

**Modify (frontend):**
- `web/src/api/types.ts` — `Friendship.friend_id?` optional, add `email?`.
- `web/src/api/client.ts` — add `cancelOutgoingInvite(email)`.
- `web/src/components/FriendsDialog.tsx` — branch row rendering on outgoing-pending; wire cancel.
- `web/src/components/FriendsDialog.test.tsx` — new render and cancel tests.

---

## Task 1: Migration — add `invited_email` column with backfill

**Files:**
- Create: `migrations/0009_friend_invited_email.up.sql`
- Create: `migrations/0009_friend_invited_email.down.sql`
- Test: `migrations/migrations_test.go` (existing, no edits — verifies pair count)

- [ ] **Step 1.1: Write the up migration**

Create `migrations/0009_friend_invited_email.up.sql`:

```sql
-- Outgoing pending friendships in the list endpoint must not confirm whether
-- the typed email belongs to a registered user. Storing the inviter-typed
-- email on the row lets the list endpoint render the row by email rather
-- than by friend_id (name/gravatar leaks via the users index).
--
-- New pending rows always get invited_email set by inviteFriendByUserID.
-- For legacy pending rows, backfill from the recipient's oldest verified
-- email — what the inviter would have seen rendered today. If a pending
-- row's recipient has no verified email (shouldn't happen, since the invite
-- path required one at creation), drop the row outright; the inviter can
-- re-invite.

ALTER TABLE friendships
    ADD COLUMN invited_email TEXT;

UPDATE friendships f
SET invited_email = (
    SELECT ue.address
    FROM user_emails ue
    WHERE ue.user_id = CASE
        WHEN f.requested_by = f.user_low THEN f.user_high
        ELSE f.user_low
    END
      AND ue.verified
    ORDER BY ue.verified_at ASC NULLS LAST, ue.created_at ASC
    LIMIT 1
)
WHERE f.status = 'pending'
  AND f.invited_email IS NULL;

DELETE FROM friendships
WHERE status = 'pending' AND invited_email IS NULL;

ALTER TABLE friendships
    ADD CONSTRAINT friendships_pending_has_invited_email
    CHECK (status <> 'pending' OR invited_email IS NOT NULL);
```

- [ ] **Step 1.2: Write the down migration**

Create `migrations/0009_friend_invited_email.down.sql`:

```sql
ALTER TABLE friendships
    DROP CONSTRAINT IF EXISTS friendships_pending_has_invited_email;

ALTER TABLE friendships
    DROP COLUMN IF EXISTS invited_email;
```

- [ ] **Step 1.3: Run the migrations test**

Run: `go test ./migrations/...`
Expected: PASS. (The test only checks file presence and that ups match downs in count.)

- [ ] **Step 1.4: Commit**

```bash
git add migrations/0009_friend_invited_email.up.sql migrations/0009_friend_invited_email.down.sql
git commit -m "migrate: add invited_email to friendships with backfill"
```

---

## Task 2: Store — thread `InvitedEmail` through `Friendship` and `RequestFriendship`

**Files:**
- Modify: `internal/store/friends.go`
- Modify: `internal/store/friends_test.go`

- [ ] **Step 2.1: Write the failing test for storage round-trip**

Append to `internal/store/friends_test.go`:

```go
func TestRequestFriendshipStoresInvitedEmail(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s), mkUser(t, s)
	f, err := s.RequestFriendship(ctx, a, b, "Typed@Example.com")
	if err != nil {
		t.Fatalf("RequestFriendship: %v", err)
	}
	if f.InvitedEmail != "Typed@Example.com" {
		t.Errorf("InvitedEmail = %q, want %q", f.InvitedEmail, "Typed@Example.com")
	}
	got, err := s.FriendshipBetween(ctx, a, b)
	if err != nil {
		t.Fatalf("FriendshipBetween: %v", err)
	}
	if got.InvitedEmail != "Typed@Example.com" {
		t.Errorf("re-read InvitedEmail = %q, want %q", got.InvitedEmail, "Typed@Example.com")
	}
}
```

- [ ] **Step 2.2: Run the test to verify it fails**

Run: `go test ./internal/store -run TestRequestFriendshipStoresInvitedEmail`
Expected: FAIL — `RequestFriendship` does not accept an `invitedEmail` argument; `Friendship.InvitedEmail` does not exist.

- [ ] **Step 2.3: Update the `Friendship` struct and `scanFriendship`**

In `internal/store/friends.go`, change the struct and the scanner:

```go
type Friendship struct {
	UserLow      int64
	UserHigh     int64
	Status       string // "pending" | "accepted"
	RequestedBy  int64
	RequestedAt  time.Time
	AcceptedAt   *time.Time
	InvitedEmail string // empty for non-pending rows; the email the inviter typed
}

func scanFriendship(row pgx.Row) (*Friendship, error) {
	var f Friendship
	var invited *string
	if err := row.Scan(&f.UserLow, &f.UserHigh, &f.Status,
		&f.RequestedBy, &f.RequestedAt, &f.AcceptedAt, &invited); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if invited != nil {
		f.InvitedEmail = *invited
	}
	return &f, nil
}
```

- [ ] **Step 2.4: Update every SELECT to include `invited_email`**

Replace the three SELECT column lists in `internal/store/friends.go`:

- `FriendshipBetween` query:

```sql
SELECT user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email
FROM friendships WHERE user_low = $1 AND user_high = $2
```

- `ListFriendships` query:

```sql
SELECT user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email
FROM friendships
WHERE $1 IN (user_low, user_high)
ORDER BY status DESC,
         COALESCE(accepted_at, requested_at) DESC
```

- `RequestFriendship` first `INSERT ... RETURNING` query and the `FOR UPDATE` re-read and the cross-direction `UPDATE ... RETURNING` query — each `RETURNING` and `SELECT` adds `invited_email` at the end.

- [ ] **Step 2.5: Extend `RequestFriendship` to accept `invitedEmail`**

In `internal/store/friends.go`. No empty-string check here — the CHECK constraint on the table enforces non-NULL for pending rows, and we trust internal callers to pass the address they were given (which `inviteFriend` already validates via `mail.ParseAddress`).

```go
func (s *Store) RequestFriendship(ctx context.Context, requesterID, recipientID int64, invitedEmail string) (*Friendship, error) {
	if requesterID == recipientID {
		return nil, errors.New("cannot friend yourself")
	}
	low, high := pairOrder(requesterID, recipientID)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	inserted, err := scanFriendship(tx.QueryRow(ctx, `
		INSERT INTO friendships (user_low, user_high, status, requested_by, invited_email)
		VALUES ($1, $2, 'pending', $3, $4)
		ON CONFLICT (user_low, user_high) DO NOTHING
		RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email`,
		low, high, requesterID, invitedEmail))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return inserted, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	existing, err := scanFriendship(tx.QueryRow(ctx, `
		SELECT user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email
		FROM friendships WHERE user_low = $1 AND user_high = $2
		FOR UPDATE`,
		low, high))
	if err != nil {
		return nil, err
	}
	if existing.Status == "accepted" || existing.RequestedBy == requesterID {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return existing, nil
	}
	upd, err := scanFriendship(tx.QueryRow(ctx, `
		UPDATE friendships
		SET status = 'accepted', accepted_at = NOW()
		WHERE user_low = $1 AND user_high = $2
		RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email`,
		low, high))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return upd, nil
}
```

Also update the `AcceptFriendship` RETURNING clause:

```sql
RETURNING user_low, user_high, status, requested_by, requested_at, accepted_at, invited_email
```

And the `consumePendingInvitesTx` INSERT — it creates rows directly in `accepted` state, so `invited_email` stays NULL, which the CHECK constraint allows (`status <> 'pending'`).

- [ ] **Step 2.6: Update existing call sites**

`grep -rn "RequestFriendship(" internal/` lists the call sites that need the new third argument:

- `internal/store/friends_test.go` — call sites in `TestRequestFriendshipFreshPending`, `TestRequestFriendshipCrossDirectionAccepts`, `TestRequestFriendshipNoopOnDuplicate`, `TestRequestFriendshipRejectsSelf` (note: passes `a, a` and expects an error; pick any email like `"x@y.com"`), `TestAcceptFriendshipRequiresOtherParty`, `TestRemoveFriendshipDeletesPendingOrAccepted`, `TestListFriendshipsOrientedAroundViewer` (three calls).
- `internal/store/flights_test.go:635` — one call inside the friends-graph-related flight visibility test.
- `internal/handlers/friends.go` — the production call at the bottom of `inviteFriendByUserID`. Change that function's signature to take `invitedEmail string` and thread it through:

```go
func (a *API) inviteFriendByUserID(ctx context.Context, me, target *store.User, invitedEmail, message string) {
	friendship, err := a.Store.RequestFriendship(ctx, me.ID, target.ID, invitedEmail)
	if err != nil {
		slog.Error("friend invite: request failed", "err", err, "from", me.ID, "to", target.ID)
		return
	}
	if friendship.Status != "pending" || friendship.RequestedBy != me.ID {
		return
	}
	a.sendFriendRequestNotification(ctx, me, target, message)
}
```

Update the one caller in `inviteFriend` to pass `addr`:

```go
default:
	if target.ID != me.ID {
		a.inviteFriendByUserID(r.Context(), me, target, addr, in.Message)
	}
```

For the test call sites, pass any non-empty string (e.g. `"test@example.com"`); the email value is irrelevant to those tests, only the signature must compile.

(Task 6 will add the test that asserts the actual typed address is what lands in the DB.)

- [ ] **Step 2.7: Run the targeted test**

Run: `go test ./internal/store -run TestRequestFriendshipStoresInvitedEmail`
Expected: PASS.

- [ ] **Step 2.8: Run the full store + handlers test suites**

Run: `go test ./internal/store/... ./internal/handlers/...`
Expected: PASS for everything (existing tests now go through the extended signature).

- [ ] **Step 2.9: Commit**

```bash
git add internal/store/friends.go internal/store/friends_test.go internal/store/flights_test.go internal/handlers/friends.go
git commit -m "store: thread invited_email through Friendship and RequestFriendship"
```

---

## Task 3: Store — `ListOutgoingPendingInvites`

**Files:**
- Modify: `internal/store/friends.go`
- Modify: `internal/store/friends_test.go`

- [ ] **Step 3.1: Write the failing test**

Append to `internal/store/friends_test.go`:

```go
func TestListOutgoingPendingInvites(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "first@example.com", "hi"); err != nil {
		t.Fatalf("seed first: %v", err)
	}
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "Second@Example.COM", ""); err != nil {
		t.Fatalf("seed second: %v", err)
	}
	other := mkUser(t, s)
	if _, err := s.UpsertPendingFriendInvite(ctx, other, "other@example.com", ""); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	got, err := s.ListOutgoingPendingInvites(ctx, inviter)
	if err != nil {
		t.Fatalf("ListOutgoingPendingInvites: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Order is requested_at DESC; both seeds happened essentially together,
	// so we don't assert order here, just contents.
	emails := map[string]bool{}
	for _, p := range got {
		emails[p.EmailLower] = true
	}
	if !emails["first@example.com"] || !emails["second@example.com"] {
		t.Errorf("unexpected emails: %+v", emails)
	}
}
```

- [ ] **Step 3.2: Run to verify it fails**

Run: `go test ./internal/store -run TestListOutgoingPendingInvites`
Expected: FAIL — `ListOutgoingPendingInvites` doesn't exist.

- [ ] **Step 3.3: Implement `ListOutgoingPendingInvites`**

In `internal/store/friends.go`, after `ListFriendships`:

```go
// ListOutgoingPendingInvites returns the pending_friend_invites rows
// authored by inviterID — the email-only outgoing invites whose targets
// haven't (yet) verified the address. The list endpoint unions these with
// friendship rows so an outgoing pending invite looks identical regardless
// of whether the target is already a registered user.
func (s *Store) ListOutgoingPendingInvites(ctx context.Context, inviterID int64) ([]*PendingFriendInvite, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT email_lower, inviter_id, message, created_at
		FROM pending_friend_invites
		WHERE inviter_id = $1
		ORDER BY created_at DESC`,
		inviterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PendingFriendInvite
	for rows.Next() {
		var p PendingFriendInvite
		if err := rows.Scan(&p.EmailLower, &p.InviterID, &p.Message, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3.4: Run the test**

Run: `go test ./internal/store -run TestListOutgoingPendingInvites`
Expected: PASS.

- [ ] **Step 3.5: Commit**

```bash
git add internal/store/friends.go internal/store/friends_test.go
git commit -m "store: add ListOutgoingPendingInvites for friend-list union"
```

---

## Task 4: Store — `CancelOutgoingInvite` (by email)

**Files:**
- Modify: `internal/store/friends.go`
- Modify: `internal/store/friends_test.go`

- [ ] **Step 4.1: Write the failing tests**

Append to `internal/store/friends_test.go`:

```go
func TestCancelOutgoingInviteKnownTarget(t *testing.T) {
	s := newStore(t)
	inviter, target := mkUser(t, s), mkUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, target, "target@example.com"); err != nil {
		t.Fatalf("seed verified email: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, inviter, target, "Target@Example.com"); err != nil {
		t.Fatalf("seed friendship: %v", err)
	}

	if err := s.CancelOutgoingInvite(ctx, inviter, "Target@Example.com"); err != nil {
		t.Fatalf("CancelOutgoingInvite: %v", err)
	}
	if _, err := s.FriendshipBetween(ctx, inviter, target); !errors.Is(err, ErrNotFound) {
		t.Errorf("friendship should be gone, got %v", err)
	}
}

func TestCancelOutgoingInviteUnknownTarget(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	if _, err := s.UpsertPendingFriendInvite(ctx, inviter, "stranger@example.com", "hi"); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	if err := s.CancelOutgoingInvite(ctx, inviter, "STRANGER@example.com"); err != nil {
		t.Fatalf("CancelOutgoingInvite: %v", err)
	}
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_friend_invites WHERE inviter_id = $1`,
		inviter).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("pending invite not deleted: %d remain", n)
	}
}

func TestCancelOutgoingInviteNoMatchIsNoop(t *testing.T) {
	s := newStore(t)
	inviter := mkUser(t, s)
	// No row exists. Must return nil (the handler relies on the no-op being
	// indistinguishable from a real cancellation).
	if err := s.CancelOutgoingInvite(ctx, inviter, "nobody@example.com"); err != nil {
		t.Errorf("no-op cancel returned %v", err)
	}
}
```

- [ ] **Step 4.2: Run to verify it fails**

Run: `go test ./internal/store -run TestCancelOutgoingInvite`
Expected: FAIL — `CancelOutgoingInvite` doesn't exist.

- [ ] **Step 4.3: Implement `CancelOutgoingInvite`**

Append to `internal/store/friends.go`:

```go
// CancelOutgoingInvite removes whatever outgoing pending invite inviterID
// has open for the given email — across both pending_friend_invites (for
// unknown targets) and friendships (for known targets where the inviter
// is requested_by). Returns nil even when nothing matched, so the handler
// can serve an identical 204 regardless of whether the target email
// belongs to a registered user.
func (s *Store) CancelOutgoingInvite(ctx context.Context, inviterID int64, email string) error {
	addr := strings.ToLower(strings.TrimSpace(email))
	if addr == "" {
		return errors.New("email required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_friend_invites
		 WHERE inviter_id = $1 AND email_lower = $2`,
		inviterID, addr); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM friendships
		 WHERE status = 'pending'
		   AND requested_by = $1
		   AND lower(invited_email) = $2`,
		inviterID, addr); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
```

- [ ] **Step 4.4: Run the cancel tests**

Run: `go test ./internal/store -run TestCancelOutgoingInvite -v`
Expected: PASS for all three.

- [ ] **Step 4.5: Commit**

```bash
git add internal/store/friends.go internal/store/friends_test.go
git commit -m "store: add CancelOutgoingInvite (by email, no-op friendly)"
```

---

## Task 5: API DTO — shape change for outgoing pending

**Files:**
- Modify: `internal/api/dto.go`
- Modify: `internal/api/dto_test.go`

- [ ] **Step 5.1: Inspect the current `dto_test.go`**

Run: `grep -n FriendshipDTO internal/api/dto_test.go`
Note the existing test names — we extend rather than replace.

- [ ] **Step 5.2: Write the failing test for the new shape**

Append to `internal/api/dto_test.go`:

The file is `package api` (internal — see `head -1 internal/api/dto_test.go`), so call symbols directly without prefix:

```go
func TestToFriendshipDTOOutgoingPendingOmitsFriendID(t *testing.T) {
	f := &store.Friendship{
		UserLow: 1, UserHigh: 2, Status: "pending",
		RequestedBy: 1, InvitedEmail: "Typed@Example.com",
		RequestedAt: time.Now(),
	}
	dto := ToFriendshipDTO(f, 1)
	if dto.Direction != "outgoing" {
		t.Errorf("Direction = %q, want outgoing", dto.Direction)
	}
	if dto.FriendID != 0 {
		t.Errorf("FriendID = %d, want 0 (omitted)", dto.FriendID)
	}
	if dto.Email != "Typed@Example.com" {
		t.Errorf("Email = %q, want %q", dto.Email, "Typed@Example.com")
	}
}

func TestToFriendshipDTOIncomingPendingKeepsFriendID(t *testing.T) {
	f := &store.Friendship{
		UserLow: 1, UserHigh: 2, Status: "pending",
		RequestedBy: 2, InvitedEmail: "Typed@Example.com",
		RequestedAt: time.Now(),
	}
	dto := ToFriendshipDTO(f, 1)
	if dto.Direction != "incoming" {
		t.Errorf("Direction = %q, want incoming", dto.Direction)
	}
	if dto.FriendID != 2 {
		t.Errorf("FriendID = %d, want 2", dto.FriendID)
	}
	if dto.Email != "" {
		t.Errorf("Email = %q, want empty for incoming", dto.Email)
	}
}

func TestPendingInviteToFriendshipDTO(t *testing.T) {
	p := &store.PendingFriendInvite{
		EmailLower: "stranger@example.com",
		InviterID:  1,
		Message:    "hi",
		CreatedAt:  time.Now(),
	}
	dto := OutgoingInviteToFriendshipDTO(p)
	if dto.FriendID != 0 || dto.Status != "pending" || dto.Direction != "outgoing" ||
		dto.Email != "stranger@example.com" {
		t.Errorf("unexpected DTO: %+v", dto)
	}
}
```

- [ ] **Step 5.3: Run to verify it fails**

Run: `go test ./internal/api -run "TestToFriendshipDTOOutgoingPending|TestPendingInviteToFriendshipDTO"`
Expected: FAIL — `FriendshipDTO.Email` doesn't exist; `OutgoingInviteToFriendshipDTO` doesn't exist.

- [ ] **Step 5.4: Update `FriendshipDTO` and `ToFriendshipDTO`**

In `internal/api/dto.go`:

```go
type FriendshipDTO struct {
	// FriendID is the *other* user in the pair. Omitted (zero on the wire)
	// for outgoing pending invites — those expose only the typed email so
	// the inviter never learns whether the target is a registered user.
	FriendID int64 `json:"friend_id,omitempty"`
	// Email is set only for outgoing pending invites and carries the
	// inviter-typed address. Omitted otherwise.
	Email       string     `json:"email,omitempty"`
	Status      string     `json:"status"` // "pending" | "accepted"
	Direction   string     `json:"direction,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
}

func ToFriendshipDTO(f *store.Friendship, viewerID int64) FriendshipDTO {
	dto := FriendshipDTO{
		Status:      f.Status,
		RequestedAt: f.RequestedAt,
		AcceptedAt:  f.AcceptedAt,
	}
	if f.Status == "pending" {
		if f.RequestedBy == viewerID {
			dto.Direction = "outgoing"
			// No FriendID on the wire: it would let the inviter look up
			// the target in /api/users.
			dto.Email = f.InvitedEmail
			return dto
		}
		dto.Direction = "incoming"
	}
	dto.FriendID = f.FriendID(viewerID)
	return dto
}

// OutgoingInviteToFriendshipDTO renders a pending_friend_invites row as
// an outgoing-pending FriendshipDTO. Used by the list handler to union
// email-only invites (target not yet registered) with friendship rows.
func OutgoingInviteToFriendshipDTO(p *store.PendingFriendInvite) FriendshipDTO {
	return FriendshipDTO{
		Email:       p.EmailLower,
		Status:      "pending",
		Direction:   "outgoing",
		RequestedAt: p.CreatedAt,
	}
}
```

- [ ] **Step 5.5: Run the DTO tests**

Run: `go test ./internal/api -v`
Expected: PASS, including the new tests.

- [ ] **Step 5.6: Commit**

```bash
git add internal/api/dto.go internal/api/dto_test.go
git commit -m "api: FriendshipDTO carries email for outgoing pending"
```

---

## Task 6: Handler — thread typed email through `inviteFriend`

**Files:**
- Modify: `internal/handlers/friends.go`
- Modify: `internal/handlers/friends_test.go`

- [ ] **Step 6.1: Write the failing test**

Append to `internal/handlers/friends_test.go`:

```go
func TestInviteFriendStoresTypedEmailOnFriendshipRow(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")

	w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "Bob@Example.com"}, inviter)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202", w.Code)
	}
	f, err := e.store.FriendshipBetween(context.Background(), inviter, target)
	if err != nil {
		t.Fatalf("FriendshipBetween: %v", err)
	}
	if f.InvitedEmail != "Bob@Example.com" {
		t.Errorf("InvitedEmail = %q, want %q", f.InvitedEmail, "Bob@Example.com")
	}
}
```

- [ ] **Step 6.2: Run the test**

Run: `go test ./internal/handlers -run TestInviteFriendStoresTypedEmailOnFriendshipRow`
Expected: PASS — Task 2 already wired the typed address through `inviteFriendByUserID` → `RequestFriendship`. This test locks in the behavior so future refactors can't silently drop it.

- [ ] **Step 6.3: Run the existing identical-response test**

Run: `go test ./internal/handlers -run "TestInviteFriendStoresTypedEmailOnFriendshipRow|TestInviteFriendResponseIdenticalForKnownAndUnknown"`
Expected: PASS for both.

- [ ] **Step 6.4: Run the full handlers suite**

Run: `go test ./internal/handlers/...`
Expected: PASS.

- [ ] **Step 6.5: Commit**

```bash
git add internal/handlers/friends_test.go
git commit -m "test: assert inviter-typed email lands on outgoing pending friendship row"
```

---

## Task 7: Handler — list endpoint unions both sources

**Files:**
- Modify: `internal/handlers/friends.go`
- Modify: `internal/handlers/friends_test.go`

- [ ] **Step 7.1: Write the failing tests**

Append to `internal/handlers/friends_test.go`:

```go
func TestListFriendsOutgoingPendingHidesIdentity(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	known := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, known, "bob@example.com")

	// Known target: friendship row with invited_email.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "Bob@Example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatalf("known invite code = %d", w.Code)
	}
	// Unknown target: pending_friend_invites row only.
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatalf("unknown invite code = %d", w.Code)
	}

	w := e.req(t, "GET", "/api/friends", nil, inviter)
	if w.Code != http.StatusOK {
		t.Fatalf("list code = %d", w.Code)
	}
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	emails := map[string]bool{}
	for _, r := range rows {
		if r.Direction != "outgoing" || r.Status != "pending" {
			t.Errorf("row not outgoing-pending: %+v", r)
		}
		if r.FriendID != 0 {
			t.Errorf("row leaks FriendID=%d for outgoing pending: %+v", r.FriendID, r)
		}
		if r.Email == "" {
			t.Errorf("row missing Email: %+v", r)
		}
		emails[strings.ToLower(r.Email)] = true
	}
	if !emails["bob@example.com"] || !emails["ghost@example.com"] {
		t.Errorf("emails = %+v, want both bob and ghost", emails)
	}
}

func TestListFriendsOutgoingPendingShapeIdentical(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	known := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, known, "bob@example.com")

	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("known invite failed")
	}
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("unknown invite failed")
	}
	w := e.req(t, "GET", "/api/friends", nil, inviter)
	rows := decodeBody[[]api.FriendshipDTO](t, w)
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
	// The presence/absence of every field except Email and RequestedAt
	// must match across the two rows.
	for _, r := range rows {
		if r.FriendID != 0 || r.AcceptedAt != nil {
			t.Errorf("row carries leaky field: %+v", r)
		}
		if r.Status != "pending" || r.Direction != "outgoing" {
			t.Errorf("row shape differs: %+v", r)
		}
	}
}
```

Add `"strings"` to the import block of `internal/handlers/friends_test.go` — it's not yet imported (`github.com/dpage/aerly/internal/api` and `store` already are).

- [ ] **Step 7.2: Run to verify the tests fail**

Run: `go test ./internal/handlers -run "TestListFriendsOutgoingPending"`
Expected: FAIL — the current `listFriends` only returns rows from `friendships`, so the `ghost@example.com` invite is missing; and outgoing-pending DTOs still carry `FriendID`.

- [ ] **Step 7.3: Update `listFriends` to union both sources**

In `internal/handlers/friends.go`:

```go
func (a *API) listFriends(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	rows, err := a.Store.ListFriendships(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	pending, err := a.Store.ListOutgoingPendingInvites(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FriendshipDTO, 0, len(rows)+len(pending))
	for _, f := range rows {
		out = append(out, api.ToFriendshipDTO(f, me.ID))
	}
	for _, p := range pending {
		out = append(out, api.OutgoingInviteToFriendshipDTO(p))
	}
	writeJSON(w, http.StatusOK, out)
}
```

(The two sources are disjoint by design — see the spec's "List path" section — so no dedup is needed. Sort order is "pending block first by recency" from friendships plus "by created_at DESC" within the appended pending block; the frontend doesn't require global sort.)

- [ ] **Step 7.4: Run the targeted tests**

Run: `go test ./internal/handlers -run "TestListFriendsOutgoingPending" -v`
Expected: PASS for both.

- [ ] **Step 7.5: Run the full handlers suite**

Run: `go test ./internal/handlers/...`
Expected: PASS — including `TestInviteFriendResponseIdenticalForKnownAndUnknown` which we did not modify.

- [ ] **Step 7.6: Commit**

```bash
git add internal/handlers/friends.go internal/handlers/friends_test.go
git commit -m "handlers: union outgoing pending invites into /api/friends"
```

---

## Task 8: Handler — `DELETE /api/friends/outgoing`

**Files:**
- Modify: `internal/handlers/friends.go`
- Modify: `internal/handlers/handlers.go`
- Modify: `internal/handlers/friends_test.go`

- [ ] **Step 8.1: Write the failing tests**

Append to `internal/handlers/friends_test.go`:

```go
func TestCancelOutgoingInviteKnownTarget(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("seed invite failed")
	}

	w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "bob@example.com"}, inviter)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if _, err := e.store.FriendshipBetween(context.Background(), inviter, target); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("friendship still present: %v", err)
	}
}

func TestCancelOutgoingInviteUnknownTarget(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "ghost@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatal("seed invite failed")
	}

	w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "ghost@example.com"}, inviter)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", w.Code)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_friend_invites WHERE inviter_id = $1`,
		inviter).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("invite not deleted: %d remain", n)
	}
}

func TestCancelOutgoingInviteNoMatch204(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "nobody@example.com"}, inviter)
	if w.Code != http.StatusNoContent {
		t.Errorf("no-match cancel = %d, want 204", w.Code)
	}
}

func TestCancelOutgoingInviteBadInput(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	if w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": ""}, inviter); w.Code != http.StatusBadRequest {
		t.Errorf("empty email = %d, want 400", w.Code)
	}
	if w := e.req(t, "DELETE", "/api/friends/outgoing",
		map[string]any{"email": "not-an-email"}, inviter); w.Code != http.StatusBadRequest {
		t.Errorf("bad email = %d, want 400", w.Code)
	}
}
```

Add `"errors"` to the import block of `internal/handlers/friends_test.go` — it's not imported yet (verified with `head -15`). `store` is already imported.

- [ ] **Step 8.2: Run to verify they fail**

Run: `go test ./internal/handlers -run "TestCancelOutgoingInvite"`
Expected: FAIL — route doesn't exist; expect 404 from the mux.

- [ ] **Step 8.3: Register the new route**

In `internal/handlers/handlers.go`, in the friends route block:

```go
mux.Handle("GET /api/friends", req(http.HandlerFunc(a.listFriends)))
mux.Handle("POST /api/friends/invite", req(http.HandlerFunc(a.inviteFriend)))
mux.Handle("DELETE /api/friends/outgoing", req(http.HandlerFunc(a.cancelOutgoingInvite)))
mux.Handle("POST /api/friends/{userId}/accept", req(http.HandlerFunc(a.acceptFriend)))
mux.Handle("DELETE /api/friends/{userId}", req(http.HandlerFunc(a.removeFriend)))
```

(Order matters for Go 1.22+ ServeMux pattern precedence: more specific patterns win, but listing `outgoing` before `{userId}` is defensive and clearer.)

- [ ] **Step 8.4: Implement the handler**

Append to `internal/handlers/friends.go`:

```go
type cancelOutgoingReq struct {
	Email string `json:"email"`
}

func (a *API) cancelOutgoingInvite(w http.ResponseWriter, r *http.Request) {
	var in cancelOutgoingReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	addr := strings.TrimSpace(in.Email)
	if addr == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.Store.CancelOutgoingInvite(r.Context(), me.ID, parsed.Address); err != nil {
		// Don't surface store errors that would leak which path matched —
		// log internally, return 204 either way.
		slog.Error("cancel outgoing invite failed", "err", err, "by", me.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 8.5: Run the cancel tests**

Run: `go test ./internal/handlers -run "TestCancelOutgoingInvite" -v`
Expected: PASS for all four.

- [ ] **Step 8.6: Run the full handlers suite**

Run: `go test ./internal/handlers/...`
Expected: PASS.

- [ ] **Step 8.7: Commit**

```bash
git add internal/handlers/friends.go internal/handlers/handlers.go internal/handlers/friends_test.go
git commit -m "handlers: add DELETE /api/friends/outgoing (cancel by email)"
```

---

## Task 9: Frontend — types and client

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`

- [ ] **Step 9.1: Update `Friendship` type**

In `web/src/api/types.ts`, change the `Friendship` interface:

```ts
export interface Friendship {
  /** The other user in the pair. Absent (omitted on the wire) for outgoing
   *  pending invites — the inviter must not learn whether the target email
   *  belongs to a registered Aerly user. Present otherwise. */
  friend_id?: number;
  /** Inviter-typed email. Present only for outgoing pending invites. */
  email?: string;
  status: FriendshipStatus;
  direction?: FriendshipDirection;
  requested_at: string;
  accepted_at?: string;
}
```

- [ ] **Step 9.2: Add the cancel client method**

In `web/src/api/client.ts`, in the `api` object (next to the other friend methods):

```ts
  cancelOutgoingInvite: (email: string) =>
    request<void>('DELETE', '/api/friends/outgoing', { email }).then(() => undefined),
```

- [ ] **Step 9.3: Run the typecheck**

Run: `cd web && npm run typecheck` (or `npx tsc --noEmit` if there's no script; check `package.json`).
Expected: PASS — no new type errors. Any consumer that read `friendship.friend_id` unconditionally will now require a guard; the only such site is `FriendsDialog.tsx`, addressed in Task 10.

If typecheck flags `FriendsDialog.tsx`, leave the errors uncommitted; Task 10 resolves them.

- [ ] **Step 9.4: Commit (with the FriendsDialog still pre-update)**

Skip this commit — fold the types + client + dialog changes into a single commit at the end of Task 10. Track this step as "no commit yet".

---

## Task 10: Frontend — render outgoing pending row + cancel wire

**Files:**
- Modify: `web/src/components/FriendsDialog.tsx`
- Modify: `web/src/components/FriendsDialog.test.tsx`

- [ ] **Step 10.1: Write the failing tests**

In `web/src/components/FriendsDialog.test.tsx`, first extend the mocked api with the new method. Update the `h = vi.hoisted` block:

```ts
const h = vi.hoisted(() => ({
  api: {
    listFriends: vi.fn(),
    inviteFriend: vi.fn(),
    acceptFriend: vi.fn(),
    removeFriend: vi.fn(),
    cancelOutgoingInvite: vi.fn(),
  },
  setError: vi.fn(),
  users: [] as User[],
}));
```

Then add tests inside the existing `describe('FriendsDialog', ...)` block:

```ts
it('renders outgoing pending rows by email, never the target user identity', async () => {
  h.api.listFriends.mockResolvedValue([
    { email: 'ghost@example.com', status: 'pending', direction: 'outgoing',
      requested_at: new Date().toISOString() },
    { email: 'bob@example.com', status: 'pending', direction: 'outgoing',
      requested_at: new Date().toISOString() },
  ]);
  // The user "Bob" IS in the local user index (e.g., the inviter happens to
  // be friends with another Bob already), but we must NOT render his name
  // on the outgoing pending row.
  h.users = [user({ id: 2, username: 'bob', name: 'Bob' })];

  render(<FriendsDialog open onClose={() => {}} />);
  await screen.findByText('bob@example.com');
  expect(screen.getByText('ghost@example.com')).toBeInTheDocument();
  expect(screen.queryByText('Bob')).not.toBeInTheDocument();
  // Two "invite sent" chips, one per row.
  expect(screen.getAllByText(/invite sent/i)).toHaveLength(2);
});

it('cancels an outgoing pending invite by calling cancelOutgoingInvite(email)', async () => {
  h.api.listFriends.mockResolvedValueOnce([
    { email: 'ghost@example.com', status: 'pending', direction: 'outgoing',
      requested_at: new Date().toISOString() },
  ]);
  h.api.cancelOutgoingInvite.mockResolvedValueOnce(undefined);
  h.api.listFriends.mockResolvedValueOnce([]); // refetch after cancel
  // window.confirm — auto-accept for this test.
  const origConfirm = window.confirm;
  window.confirm = () => true;

  render(<FriendsDialog open onClose={() => {}} />);
  await screen.findByText('ghost@example.com');
  const cancelBtn = screen.getByRole('button', { name: /cancel|remove/i });
  await userEvent.click(cancelBtn);

  await waitFor(() =>
    expect(h.api.cancelOutgoingInvite).toHaveBeenCalledWith('ghost@example.com'),
  );

  window.confirm = origConfirm;
});
```

- [ ] **Step 10.2: Run to verify they fail**

Run: `cd web && npm run test -- FriendsDialog`
Expected: FAIL — current dialog renders by `friend_id`/user index, and there's no `cancelOutgoingInvite` wire.

- [ ] **Step 10.3: Branch the row rendering**

In `web/src/components/FriendsDialog.tsx`, replace the table-row loop body. Add a stable key helper and the branch:

```tsx
const rowKey = (f: Friendship) =>
  f.direction === 'outgoing' && f.status === 'pending'
    ? `outgoing:${f.email}`
    : `friend:${f.friend_id}`;

// ... inside the table body:
{friends.map((f) => {
  if (f.direction === 'outgoing' && f.status === 'pending') {
    const email = f.email ?? '';
    return (
      <TableRow key={rowKey(f)} hover>
        <TableCell>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Avatar sx={{ width: 24, height: 24 }}>
              {email.charAt(0).toUpperCase() || '?'}
            </Avatar>
            <span>{email}</span>
          </Box>
        </TableCell>
        <TableCell align="center">
          <Chip label="invite sent" size="small" color="warning" variant="outlined" />
        </TableCell>
        <TableCell align="right">
          <Tooltip title="Cancel">
            <IconButton
              size="small"
              aria-label={`Cancel invite to ${email}`}
              onClick={() => void handleCancelOutgoing(email)}
            >
              <DeleteOutlineIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        </TableCell>
      </TableRow>
    );
  }

  // existing rendering for accepted + incoming pending, using userIndex.get(f.friend_id)
  const friendId = f.friend_id!;
  const label = friendLabel(friendId);
  const u = userIndex.get(friendId);
  return (
    <TableRow key={rowKey(f)} hover>
      {/* ... existing TableCells unchanged ... */}
    </TableRow>
  );
})}
```

Add the cancel handler near `handleRemove`:

```tsx
const handleCancelOutgoing = async (email: string) => {
  if (!window.confirm(`Cancel the invite to ${email}?`)) return;
  try {
    await api.cancelOutgoingInvite(email);
    setFriends((rows) =>
      rows.filter(
        (r) => !(r.direction === 'outgoing' && r.status === 'pending' && r.email === email),
      ),
    );
  } catch (err) {
    reportError(err);
  }
};
```

- [ ] **Step 10.4: Run the targeted tests**

Run: `cd web && npm run test -- FriendsDialog`
Expected: PASS for the two new tests AND all existing tests in the file. (The existing "lists current friends with their status" test seeds an outgoing-pending row with `friend_id: 4, direction: 'outgoing'` — note it has `friend_id` set but NO `email`, which doesn't match the new branch shape. Update that test's seed row to use `email: 'dan@example.com'` instead and drop `friend_id`, then assert `dan@example.com` is in the DOM rather than `Dan`.)

- [ ] **Step 10.5: Update the pre-existing test that seeds outgoing pending**

In the first `it(...)` block ("lists current friends with their status when opened"):

```ts
h.api.listFriends.mockResolvedValue([
  friend({ friend_id: 2, status: 'accepted' }),
  friend({ friend_id: 3, status: 'pending', direction: 'incoming' }),
  { email: 'dan@example.com', status: 'pending', direction: 'outgoing',
    requested_at: new Date().toISOString() },
]);
```

Replace the `expect(screen.getByText('Dan'))` assertion with `expect(screen.getByText('dan@example.com'))`. Keep the other assertions.

Similarly update the second `it(...)` block ("sends an invite with the optional message"): the listFriends mock returns

```ts
h.api.listFriends.mockResolvedValueOnce([
  { email: 'bob@example.com', status: 'pending', direction: 'outgoing',
    requested_at: new Date().toISOString() },
]);
```

The assertion `expect(await screen.findByText(/invite sent/i))` already works.

- [ ] **Step 10.6: Run the whole frontend test suite**

Run: `cd web && npm run test`
Expected: PASS.

- [ ] **Step 10.7: Run the typecheck**

Run: `cd web && npm run typecheck` (or whatever the project uses).
Expected: PASS.

- [ ] **Step 10.8: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/components/FriendsDialog.tsx web/src/components/FriendsDialog.test.tsx
git commit -m "feat: render outgoing pending invites by email, hide target identity"
```

---

## Task 11: End-to-end verification

**Files:** none (manual + full-suite run)

- [ ] **Step 11.1: Run the full backend suite**

Run: `make test-go`
Expected: PASS.

- [ ] **Step 11.2: Run the full frontend suite**

Run: `make test-web`
Expected: PASS.

- [ ] **Step 11.3: Manual verification (dev server)**

Run: `make dev`

In the browser, log in as user A. Open the Friends dialog.

1. Invite a known user's verified email (use another seeded test user). Confirm:
   - The pending row appears with the **typed email**, generic letter avatar, "invite sent" chip.
   - No name from `/api/users` appears for this row.
   - Open DevTools → Network → `/api/friends` response. Confirm the outgoing pending row has `email` set and **no** `friend_id`.

2. Invite an email that no user owns. Confirm:
   - A row appears identical in shape to (1).
   - DevTools response shape matches.

3. Click the trash/cancel icon on each row. Confirm:
   - The confirm dialog shows the email.
   - On confirm, the row disappears.
   - DevTools shows `DELETE /api/friends/outgoing` with `{"email": "..."}` and a 204 response.

4. Have user B (the known target) log in in another window. Confirm they see the incoming pending request with A's name and avatar (their view is unchanged).

5. Accept from B's side. Confirm both users now see the row as "accepted" with full identity (this is the consented post-acceptance state).

- [ ] **Step 11.4: Note any deviations**

If manual verification surfaces a UI rough edge or a flow gap, file a follow-up task. The plan does not require fixing UI polish beyond the privacy-correct rendering.

---

## Self-Review (run after writing tasks, before handing off)

Spec coverage check:

| Spec section | Implemented in |
|---|---|
| Storage: `invited_email` column | Task 1 |
| Storage: backfill | Task 1 |
| Storage: CHECK constraint | Task 1 |
| Invite path threads typed email | Task 6 |
| List path unions both sources | Task 7 |
| DTO shape (`friend_id` optional, `email` set for outgoing pending) | Task 5 |
| Cancel path (`DELETE /api/friends/outgoing`) | Task 8 |
| Accept path cleanup of `pending_friend_invites` | (defensive; not strictly needed under Approach A — see below) |
| Rendering changes in `FriendsDialog` | Task 10 |
| Tests: backend no-enumeration | Tasks 6, 7, 8 |
| Tests: frontend hides identity, calls cancel by email | Task 10 |

**Note on the "accept path cleanup":** under Approach A, the cleanup path described in the spec (delete matching `pending_friend_invites` when a friendship is accepted) is a no-op in normal flow: invites to verified emails never go through `pending_friend_invites`. The plan intentionally omits the defensive cleanup to keep the diff focused. If a follow-up surfaces drift between the two tables, add the cleanup as a separate task.

Type / signature consistency: `RequestFriendship(ctx, requesterID, recipientID int64, invitedEmail string)`, `ListOutgoingPendingInvites(ctx, inviterID int64)`, `CancelOutgoingInvite(ctx, inviterID int64, email string)`, `OutgoingInviteToFriendshipDTO(p *store.PendingFriendInvite)` — used consistently across all tasks.
