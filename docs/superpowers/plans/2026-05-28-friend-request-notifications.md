# Friend-Request Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface incoming friend requests in two new ways — an avatar
badge driven by `/api/notifications` + a `notifications.updated` SSE
event, and a one-click "Accept" button in the request email backed by
an HMAC-signed token redeemed at `POST /api/friends/accept-token`.

**Architecture:** A small HMAC token module under `internal/auth`
mints/verifies friendship-accept tokens signed with `Config.SessionKey`.
A new `/api/notifications` endpoint returns a typed open-shape DTO
(today: `friend_requests_pending`). Every friendship state change
(invite-known-user / accept / accept-token / remove) publishes a
recompute-and-push to the affected user via the existing SSE `Hub` with
`VisibleTo` targeting. The SPA wires the new SSE event into a
`notifications` slice of the Zustand store, renders a MUI `Badge` on
the avatar `IconButton`, and runs a one-shot `App.tsx` effect that
reads `?friend_accept=<token>` from the URL post-auth.

**Tech Stack:** Go 1.26 (`net/http`, `crypto/hmac`, `pgx/v5`), React 18
+ TypeScript + MUI (`Badge`, `Snackbar`), Zustand, Vitest, Go's
standard `testing`.

**Spec:** `docs/superpowers/specs/2026-05-28-friend-request-notifications-design.md`

---

## File layout

Create:

- `internal/auth/accept_token.go` — `MintFriendAcceptToken`, `VerifyFriendAcceptToken`, sentinel errors.
- `internal/auth/accept_token_test.go` — round-trip, malformed, expired, tampered, wrong-key.
- `internal/handlers/notifications.go` — `GET /api/notifications` handler + `publishNotifications` helper.
- `internal/handlers/notifications_test.go` — endpoint + publish behaviour.
- `internal/handlers/friend_emails_test.go` — snapshot the new Accept button in plain + HTML bodies.

Modify:

- `internal/api/dto.go` — `NotificationsDTO` type.
- `internal/store/friends.go` — `CountIncomingFriendRequests` method.
- `internal/store/friends_test.go` — tests for the count.
- `internal/handlers/handlers.go` — register `/api/notifications`, `/api/friends/accept-token`.
- `internal/handlers/friends.go` — `acceptFriendToken` handler; call `publishNotifications` from `inviteFriendByUserID`, `acceptFriend`, `removeFriend`; thread token mint into `sendFriendRequestNotification`.
- `internal/handlers/friend_emails.go` — `friendRequestInput.Token`; render Accept button + plain URL.
- `internal/handlers/friends_test.go` — extend SSE assertions and add accept-token cases.
- `web/src/api/types.ts` — `Notifications`, `AcceptFriendTokenResult`.
- `web/src/api/client.ts` — `getNotifications`, `acceptFriendToken`.
- `web/src/state/store.ts` — `notifications`, `notice`, `applyNotificationsUpdate`, `refreshNotifications`, `setNotice`; `init()` fetches notifications.
- `web/src/sse.ts` — `onNotifications` handler + event listener.
- `web/src/sse.test.ts` — coverage for the new event.
- `web/src/App.tsx` — accept-token bootstrap effect, success Snackbar.
- `web/src/App.test.tsx` — token bootstrap cases.
- `web/src/components/AppShell.tsx` — `Badge` wrap + chip beside "Friends…".
- `web/src/components/AppShell.test.tsx` — badge / chip render cases.
- `.env.example` — one-line note about the 7-day token + Accept button.
- `README.md` — same note in the friend-invite paragraph.

No DB migration is needed: the count is derived from the existing
`friendships` table.

---

## Task 1: Mint and verify friend-accept tokens

**Files:**
- Create: `internal/auth/accept_token.go`
- Test: `internal/auth/accept_token_test.go`

The token encoding is `base64url(payload) + "." + base64url(hmac-sha256(payload))`
where `payload = "<recipientID>.<inviterID>.<expiryUnixSeconds>"` (all
decimal). Signing uses HMAC-SHA256 with the supplied key. Verification
re-derives the HMAC over the *raw* payload bytes (not a re-canonicalised
form), then `constant_time` compares.

- [ ] **Step 1: Write the failing test file.**

Create `internal/auth/accept_token_test.go`:

```go
package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var tokenKey = []byte("accept-token-test-key-thirty-two-bytes!")

func TestMintVerifyRoundTrip(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	tok := MintFriendAcceptToken(tokenKey, 42, 99, exp)
	if tok == "" {
		t.Fatal("MintFriendAcceptToken returned empty string")
	}
	r, i, err := VerifyFriendAcceptToken(tokenKey, tok)
	if err != nil {
		t.Fatalf("VerifyFriendAcceptToken: %v", err)
	}
	if r != 42 || i != 99 {
		t.Errorf("recipient/inviter = %d/%d, want 42/99", r, i)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	tok := MintFriendAcceptToken(tokenKey, 1, 2, time.Now().Add(-time.Second))
	_, _, err := VerifyFriendAcceptToken(tokenKey, tok)
	if !errors.Is(err, ErrExpiredAcceptToken) {
		t.Errorf("err = %v, want ErrExpiredAcceptToken", err)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	tok := MintFriendAcceptToken(tokenKey, 1, 2, time.Now().Add(time.Hour))
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("token shape = %q", tok)
	}
	// Flip the recipient id in the payload by re-base64'ing a new value
	// with the original signature; verification must reject.
	bad := encodePayload(99, 2, time.Now().Add(time.Hour).Unix()) + "." + parts[1]
	_, _, err := VerifyFriendAcceptToken(tokenKey, bad)
	if !errors.Is(err, ErrMalformedAcceptToken) {
		t.Errorf("err = %v, want ErrMalformedAcceptToken", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	tok := MintFriendAcceptToken(tokenKey, 1, 2, time.Now().Add(time.Hour))
	_, _, err := VerifyFriendAcceptToken([]byte("different-key-thirty-two-bytes!!"), tok)
	if !errors.Is(err, ErrMalformedAcceptToken) {
		t.Errorf("err = %v, want ErrMalformedAcceptToken", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	cases := []string{
		"",
		"not-base64",
		"only-one-segment",
		"a.b.c", // too many segments
	}
	for _, c := range cases {
		if _, _, err := VerifyFriendAcceptToken(tokenKey, c); err == nil {
			t.Errorf("verify(%q) returned nil, want error", c)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

```bash
go test ./internal/auth/ -run TestMintVerify -v
```

Expected: build error — `MintFriendAcceptToken` undefined.

- [ ] **Step 3: Implement `accept_token.go`.**

Create `internal/auth/accept_token.go`:

```go
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrMalformedAcceptToken signals the token was missing, mis-encoded,
// truncated, or had a mismatched signature.
var ErrMalformedAcceptToken = errors.New("malformed friend-accept token")

// ErrExpiredAcceptToken signals the token verified but its expiry is in
// the past. Handler converts this to HTTP 410 so the caller can surface
// a "ask the sender to resend" message.
var ErrExpiredAcceptToken = errors.New("expired friend-accept token")

// encodePayload builds the ASCII payload that goes between the two
// base64url segments. Exported (lowercase, package-local) only so the
// test can construct tampered tokens that share the original signature.
func encodePayload(recipientID, inviterID, expiryUnix int64) string {
	raw := fmt.Sprintf("%d.%d.%d", recipientID, inviterID, expiryUnix)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func MintFriendAcceptToken(key []byte, recipientID, inviterID int64, expiry time.Time) string {
	payload := encodePayload(recipientID, inviterID, expiry.Unix())
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

func VerifyFriendAcceptToken(key []byte, token string) (int64, int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, ErrMalformedAcceptToken
	}
	payload, sig := parts[0], parts[1]

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, want) {
		return 0, 0, ErrMalformedAcceptToken
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return 0, 0, ErrMalformedAcceptToken
	}
	fields := strings.Split(string(raw), ".")
	if len(fields) != 3 {
		return 0, 0, ErrMalformedAcceptToken
	}
	recipientID, err1 := strconv.ParseInt(fields[0], 10, 64)
	inviterID, err2 := strconv.ParseInt(fields[1], 10, 64)
	expiryUnix, err3 := strconv.ParseInt(fields[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, ErrMalformedAcceptToken
	}
	if time.Now().Unix() > expiryUnix {
		return 0, 0, ErrExpiredAcceptToken
	}
	return recipientID, inviterID, nil
}
```

- [ ] **Step 4: Run the test to verify it passes.**

```bash
go test ./internal/auth/ -run TestMintVerify -v
go test ./internal/auth/ -run TestVerify -v
```

Expected: all five tests PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/auth/accept_token.go internal/auth/accept_token_test.go
git commit -m "auth: add HMAC-signed friend-accept tokens"
```

---

## Task 2: Count incoming friend requests in the store

**Files:**
- Modify: `internal/store/friends.go` (append to end)
- Test: `internal/store/friends_test.go` (append)

- [ ] **Step 1: Write the failing tests.**

Append to `internal/store/friends_test.go`:

```go
func TestCountIncomingFriendRequestsEmpty(t *testing.T) {
	s := newStore(t)
	me := mkUser(t, s)
	n, err := s.CountIncomingFriendRequests(ctx, me)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
}

func TestCountIncomingFriendRequestsIgnoresOutgoingAndAccepted(t *testing.T) {
	s := newStore(t)
	me := mkUser(t, s)
	otherA := mkUser(t, s)
	otherB := mkUser(t, s)

	// Outgoing pending: me requested otherA.
	if _, err := s.RequestFriendship(ctx, me, otherA); err != nil {
		t.Fatalf("outgoing: %v", err)
	}
	// Accepted: cross-direction with otherB.
	if _, err := s.RequestFriendship(ctx, me, otherB); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, otherB, me); err != nil {
		t.Fatalf("accept: %v", err)
	}

	n, err := s.CountIncomingFriendRequests(ctx, me)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0 (outgoing+accepted only)", n)
	}
}

func TestCountIncomingFriendRequestsMultiple(t *testing.T) {
	s := newStore(t)
	me := mkUser(t, s)
	inviter1 := mkUser(t, s)
	inviter2 := mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, inviter1, me); err != nil {
		t.Fatalf("seed1: %v", err)
	}
	if _, err := s.RequestFriendship(ctx, inviter2, me); err != nil {
		t.Fatalf("seed2: %v", err)
	}

	n, err := s.CountIncomingFriendRequests(ctx, me)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
}
```

The test additions only use `testing` and the existing helpers; no new
imports are needed in `internal/store/friends_test.go`.

- [ ] **Step 2: Run the test to verify it fails.**

```bash
go test ./internal/store/ -run TestCountIncomingFriendRequests -v
```

Expected: build error — `CountIncomingFriendRequests` undefined.

- [ ] **Step 3: Implement the store method.**

Append to `internal/store/friends.go`:

```go
// CountIncomingFriendRequests returns the number of friendships in
// 'pending' state where viewerID is the recipient (not the requester).
// Used by /api/notifications and the SSE notifications.updated payload.
func (s *Store) CountIncomingFriendRequests(ctx context.Context, viewerID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM friendships
		WHERE status = 'pending'
		  AND requested_by <> $1
		  AND $1 IN (user_low, user_high)`,
		viewerID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
```

- [ ] **Step 4: Run the test to verify it passes.**

```bash
go test ./internal/store/ -run TestCountIncomingFriendRequests -v
```

Expected: all three tests PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/store/friends.go internal/store/friends_test.go
git commit -m "store: add CountIncomingFriendRequests for the notifications badge"
```

---

## Task 3: NotificationsDTO

**Files:**
- Modify: `internal/api/dto.go` (append a new struct)

This is a tiny supporting change — no test on its own; the handler test
in Task 4 covers its serialisation.

- [ ] **Step 1: Add the DTO.**

Append to `internal/api/dto.go`:

```go
// NotificationsDTO is the body of GET /api/notifications and the
// payload of notifications.updated SSE events. It is intentionally an
// open-shape struct: new notification kinds get added as new fields
// with omitempty, so older clients ignoring them keep working.
type NotificationsDTO struct {
	FriendRequestsPending int `json:"friend_requests_pending"`
}
```

- [ ] **Step 2: Verify the package still builds.**

```bash
go build ./internal/api/
```

Expected: exit 0, no output.

- [ ] **Step 3: Commit.**

```bash
git add internal/api/dto.go
git commit -m "api: add NotificationsDTO"
```

---

## Task 4: `GET /api/notifications` endpoint + `publishNotifications` helper

**Files:**
- Create: `internal/handlers/notifications.go`
- Modify: `internal/handlers/handlers.go` (register route)
- Test: `internal/handlers/notifications_test.go`

- [ ] **Step 1: Write the failing test.**

Create `internal/handlers/notifications_test.go`:

```go
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/sse"
)

func TestGetNotificationsRequiresAuth(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, "GET", "/api/notifications", nil, 0)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestGetNotificationsReportsCount(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "me", false)
	a := e.user(t, "a", false)
	b := e.user(t, "b", false)
	if _, err := e.store.RequestFriendship(context.Background(), a, me); err != nil {
		t.Fatalf("seed a→me: %v", err)
	}
	if _, err := e.store.RequestFriendship(context.Background(), b, me); err != nil {
		t.Fatalf("seed b→me: %v", err)
	}

	w := e.req(t, "GET", "/api/notifications", nil, me)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[api.NotificationsDTO](t, w)
	if got.FriendRequestsPending != 2 {
		t.Errorf("count = %d, want 2", got.FriendRequestsPending)
	}
}

func TestPublishNotificationsPushesToUser(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "me", false)
	inviter := e.user(t, "inv", false)

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: me})
	defer unsub()

	if _, err := e.store.RequestFriendship(context.Background(), inviter, me); err != nil {
		t.Fatalf("seed: %v", err)
	}
	e.api.publishNotifications(context.Background(), me)

	events := drainEvents(ch)
	if len(events) != 1 || events[0].Type != "notifications.updated" {
		t.Fatalf("events = %+v", events)
	}
	var got api.NotificationsDTO
	if err := json.Unmarshal(events[0].Data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.FriendRequestsPending != 1 {
		t.Errorf("payload count = %d, want 1", got.FriendRequestsPending)
	}
}

func TestPublishNotificationsScopesToTargetUser(t *testing.T) {
	e := setup(t, nil, nil)
	me := e.user(t, "me", false)
	other := e.user(t, "other", false)

	myCh, myUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: me})
	defer myUnsub()
	otherCh, otherUnsub := e.hub.Subscribe(sse.Subscription{ViewerID: other})
	defer otherUnsub()

	e.api.publishNotifications(context.Background(), me)

	if got := drainEvents(myCh); len(got) != 1 {
		t.Errorf("my events = %d, want 1", len(got))
	}
	if got := drainEvents(otherCh); len(got) != 0 {
		t.Errorf("other events = %d, want 0 (publish should be scoped)", len(got))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

```bash
go test ./internal/handlers/ -run TestGetNotifications -v
go test ./internal/handlers/ -run TestPublishNotifications -v
```

Expected: build errors — handler / helper undefined.

- [ ] **Step 3: Create the handler file.**

Create `internal/handlers/notifications.go`:

```go
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/sse"
)

func (a *API) getNotifications(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	dto, err := a.buildNotificationsDTO(r.Context(), me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// buildNotificationsDTO fans out to every count query the dashboard
// surfaces. Today there is one source (incoming friend requests); new
// kinds add a query + DTO field here.
func (a *API) buildNotificationsDTO(ctx context.Context, userID int64) (api.NotificationsDTO, error) {
	n, err := a.Store.CountIncomingFriendRequests(ctx, userID)
	if err != nil {
		return api.NotificationsDTO{}, err
	}
	return api.NotificationsDTO{FriendRequestsPending: n}, nil
}

// publishNotifications recomputes userID's notification counts and
// pushes them on the SSE hub, restricted to userID. Errors are logged
// but never surface to the HTTP caller — the SPA's bootstrap fetch is
// the safety net for any dropped publish.
func (a *API) publishNotifications(ctx context.Context, userID int64) {
	dto, err := a.buildNotificationsDTO(ctx, userID)
	if err != nil {
		slog.Error("publishNotifications: build dto", "err", err, "user", userID)
		return
	}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("publishNotifications: marshal", "err", err, "user", userID)
		return
	}
	a.Hub.Publish(sse.Event{
		Type:      "notifications.updated",
		Data:      payload,
		VisibleTo: []int64{userID},
	})
}
```

- [ ] **Step 4: Register the route.**

In `internal/handlers/handlers.go`, inside `(*API).Register`, add (next
to the other `/api/friends` lines):

```go
	mux.Handle("GET /api/notifications", req(http.HandlerFunc(a.getNotifications)))
```

- [ ] **Step 5: Run the test to verify it passes.**

```bash
go test ./internal/handlers/ -run TestGetNotifications -v
go test ./internal/handlers/ -run TestPublishNotifications -v
```

Expected: all four tests PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/handlers/notifications.go internal/handlers/handlers.go internal/handlers/notifications_test.go
git commit -m "handlers: add /api/notifications + publishNotifications helper"
```

---

## Task 5: Publish `notifications.updated` from invite/accept/remove

**Files:**
- Modify: `internal/handlers/friends.go`
- Test: `internal/handlers/friends_test.go` (extend existing or add new)

- [ ] **Step 1: Write the failing tests.**

Append to `internal/handlers/friends_test.go`:

```go
func TestInviteKnownUserPublishesNotifications(t *testing.T) {
	e := setup(t, nil, nil)
	inviter := e.user(t, "alice", false)
	target := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, target, "bob@example.com")

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: target})
	defer unsub()

	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, inviter); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %d %s", w.Code, w.Body.String())
	}

	var sawNotif bool
	for _, ev := range drainEvents(ch) {
		if ev.Type == "notifications.updated" {
			sawNotif = true
		}
	}
	if !sawNotif {
		t.Error("recipient did not see a notifications.updated event after invite")
	}
}

func TestAcceptPublishesNotificationsToAccepter(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %s", w.Body.String())
	}

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: bob})
	defer unsub()

	path := "/api/friends/" + strconv.FormatInt(alice, 10) + "/accept"
	if w := e.req(t, "POST", path, nil, bob); w.Code != http.StatusOK {
		t.Fatalf("accept: %s", w.Body.String())
	}
	var got bool
	for _, ev := range drainEvents(ch) {
		if ev.Type == "notifications.updated" {
			got = true
		}
	}
	if !got {
		t.Error("accepter did not see a notifications.updated event")
	}
}

func TestRemovePendingPublishesNotifications(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	seedVerifiedEmail(t, e, bob, "bob@example.com")
	if w := e.req(t, "POST", "/api/friends/invite",
		map[string]any{"email": "bob@example.com"}, alice); w.Code != http.StatusAccepted {
		t.Fatalf("invite: %s", w.Body.String())
	}

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: bob})
	defer unsub()

	// Alice cancels the outgoing pending request.
	path := "/api/friends/" + strconv.FormatInt(bob, 10)
	if w := e.req(t, "DELETE", path, nil, alice); w.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	var got bool
	for _, ev := range drainEvents(ch) {
		if ev.Type == "notifications.updated" {
			got = true
		}
	}
	if !got {
		t.Error("recipient did not see a notifications.updated event after cancel")
	}
}
```

Add the `sse` import to `internal/handlers/friends_test.go` if it's not
already present:

```go
"github.com/dpage/aerly/internal/sse"
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/handlers/ -run "TestInviteKnownUserPublishesNotifications|TestAcceptPublishesNotificationsToAccepter|TestRemovePendingPublishesNotifications" -v
```

Expected: tests fail (no event seen) — the handlers don't publish yet.

- [ ] **Step 3: Wire publishes into the three handlers.**

In `internal/handlers/friends.go`, edit `inviteFriendByUserID` so the
end of the function reads:

```go
	a.sendFriendRequestNotification(ctx, me, target, message)
	a.publishNotifications(ctx, target.ID)
}
```

Edit `acceptFriend` so the success branch reads:

```go
	me := auth.UserFrom(r.Context())
	f, err := a.Store.AcceptFriendship(r.Context(), me.ID, otherID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishNotifications(r.Context(), me.ID)
	writeJSON(w, http.StatusOK, api.ToFriendshipDTO(f, me.ID))
}
```

Edit `removeFriend` so the success branch reads:

```go
	me := auth.UserFrom(r.Context())
	if err := a.Store.RemoveFriendship(r.Context(), me.ID, otherID); err != nil {
		handleStoreErr(w, err)
		return
	}
	// We don't know whether the removed row was an incoming pending,
	// outgoing pending, or an accepted edge. Publishing to both sides is
	// cheap; the count is authoritative on each end after the delete.
	a.publishNotifications(r.Context(), me.ID)
	a.publishNotifications(r.Context(), otherID)
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/handlers/ -run "TestInviteKnownUserPublishesNotifications|TestAcceptPublishesNotificationsToAccepter|TestRemovePendingPublishesNotifications" -v
```

Expected: all three PASS.

- [ ] **Step 5: Confirm nothing else regressed.**

```bash
go test ./internal/handlers/...
```

Expected: all green.

- [ ] **Step 6: Commit.**

```bash
git add internal/handlers/friends.go internal/handlers/friends_test.go
git commit -m "handlers: publish notifications.updated on invite/accept/remove"
```

---

## Task 6: `POST /api/friends/accept-token` endpoint

**Files:**
- Modify: `internal/handlers/friends.go` (new handler)
- Modify: `internal/handlers/handlers.go` (register route)
- Test: `internal/handlers/friends_test.go` (extend)

- [ ] **Step 1: Write the failing tests.**

Append to `internal/handlers/friends_test.go`:

```go
func mintTestToken(t *testing.T, recipient, inviter int64, ttl time.Duration) string {
	t.Helper()
	return auth.MintFriendAcceptToken(sessKey, recipient, inviter, time.Now().Add(ttl))
}

func TestAcceptFriendTokenHappyPath(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	if _, err := e.store.RequestFriendship(context.Background(), alice, bob); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok := mintTestToken(t, bob, alice, time.Hour)

	ch, unsub := e.hub.Subscribe(sse.Subscription{ViewerID: bob})
	defer unsub()

	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, bob)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Friendship *api.FriendshipDTO `json:"friendship"`
		Already    bool               `json:"already"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Already {
		t.Error("already should be false on first accept")
	}
	if resp.Friendship == nil || resp.Friendship.Status != "accepted" {
		t.Errorf("friendship dto = %+v", resp.Friendship)
	}
	var sawNotif bool
	for _, ev := range drainEvents(ch) {
		if ev.Type == "notifications.updated" {
			sawNotif = true
		}
	}
	if !sawNotif {
		t.Error("expected notifications.updated event after token accept")
	}
}

func TestAcceptFriendTokenAlreadyAccepted(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	// No pending row exists — token-accept should report already=true.
	tok := mintTestToken(t, bob, alice, time.Hour)
	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, bob)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"already":true`) {
		t.Errorf("body should report already=true: %s", w.Body.String())
	}
}

func TestAcceptFriendTokenWrongRecipient(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	mallory := e.user(t, "mallory", false)
	if _, err := e.store.RequestFriendship(context.Background(), alice, bob); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok := mintTestToken(t, bob, alice, time.Hour)
	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, mallory)
	if w.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestAcceptFriendTokenExpired(t *testing.T) {
	e := setup(t, nil, nil)
	alice := e.user(t, "alice", false)
	bob := e.user(t, "bob", false)
	tok := auth.MintFriendAcceptToken(sessKey, bob, alice, time.Now().Add(-time.Second))
	w := e.req(t, "POST", "/api/friends/accept-token",
		map[string]any{"token": tok}, bob)
	if w.Code != http.StatusGone {
		t.Errorf("code = %d, want 410; body=%s", w.Code, w.Body.String())
	}
}

func TestAcceptFriendTokenMalformed(t *testing.T) {
	e := setup(t, nil, nil)
	bob := e.user(t, "bob", false)
	for _, body := range []map[string]any{
		{},
		{"token": ""},
		{"token": "garbage"},
		{"token": "still.garbage"},
	} {
		w := e.req(t, "POST", "/api/friends/accept-token", body, bob)
		if w.Code != http.StatusBadRequest {
			t.Errorf("token=%v -> code=%d want 400 (body=%s)", body, w.Code, w.Body.String())
		}
	}
}
```

Make sure `internal/handlers/friends_test.go` imports `time`, `strings`,
`encoding/json`, and `github.com/dpage/aerly/internal/auth` (add any
that are missing).

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/handlers/ -run TestAcceptFriendToken -v
```

Expected: 404 on the new route — handler not yet registered.

- [ ] **Step 3: Implement the handler.**

Append to `internal/handlers/friends.go`:

```go
type acceptFriendTokenReq struct {
	Token string `json:"token"`
}

type acceptFriendTokenResp struct {
	Friendship *api.FriendshipDTO `json:"friendship,omitempty"`
	Already    bool               `json:"already,omitempty"`
}

func (a *API) acceptFriendToken(w http.ResponseWriter, r *http.Request) {
	var in acceptFriendTokenReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body")
		return
	}
	if strings.TrimSpace(in.Token) == "" {
		writeError(w, http.StatusBadRequest, "token required")
		return
	}
	recipientID, inviterID, err := auth.VerifyFriendAcceptToken(a.Config.SessionKey, in.Token)
	switch {
	case errors.Is(err, auth.ErrExpiredAcceptToken):
		writeError(w, http.StatusGone, "invitation link expired — ask the sender to resend")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid invitation link")
		return
	}
	me := auth.UserFrom(r.Context())
	if me.ID != recipientID {
		writeError(w, http.StatusForbidden, "this invitation isn't for your account")
		return
	}

	f, err := a.Store.AcceptFriendship(r.Context(), me.ID, inviterID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No pending row to accept. The recipient may already be friends
		// with the inviter, or the inviter cancelled the request. Surface
		// as a quiet "already" so the SPA shows an informational toast.
		writeJSON(w, http.StatusOK, acceptFriendTokenResp{Already: true})
		return
	case err != nil:
		handleStoreErr(w, err)
		return
	}
	a.publishNotifications(r.Context(), me.ID)
	dto := api.ToFriendshipDTO(f, me.ID)
	writeJSON(w, http.StatusOK, acceptFriendTokenResp{Friendship: &dto})
}
```

Make sure the file's imports include `errors`, `strings`, and (already
present) `auth`, `api`, `store`.

- [ ] **Step 4: Register the route.**

In `internal/handlers/handlers.go`, inside `Register`, alongside the
other `/api/friends` lines:

```go
	mux.Handle("POST /api/friends/accept-token", req(http.HandlerFunc(a.acceptFriendToken)))
```

- [ ] **Step 5: Run the tests to verify they pass.**

```bash
go test ./internal/handlers/ -run TestAcceptFriendToken -v
```

Expected: all five PASS.

- [ ] **Step 6: Confirm the full handlers suite still passes.**

```bash
go test ./internal/handlers/...
```

Expected: all green.

- [ ] **Step 7: Commit.**

```bash
git add internal/handlers/friends.go internal/handlers/handlers.go internal/handlers/friends_test.go
git commit -m "handlers: add POST /api/friends/accept-token"
```

---

## Task 7: Accept button in the friend-request email

**Files:**
- Modify: `internal/handlers/friend_emails.go`
- Modify: `internal/handlers/friends.go` (`sendFriendRequestNotification`)
- Test: `internal/handlers/friend_emails_test.go` (new)

- [ ] **Step 1: Write the failing test.**

Create `internal/handlers/friend_emails_test.go`:

```go
package handlers

import (
	"strings"
	"testing"
)

func TestBuildFriendRequestEmailEmbedsAcceptToken(t *testing.T) {
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr:     "noreply@aerly.test",
		ToAddr:       "bob@example.com",
		PublicURL:    "https://aerly.test",
		InviterName:  "Alice",
		InviterLogin: "alice",
		Message:      "",
		Token:        "the-token-bytes",
	})

	// Plain-text body should include a clearly-labelled Accept URL.
	if !strings.Contains(msg, "https://aerly.test/?friend_accept=the-token-bytes") {
		t.Errorf("missing Accept URL in body:\n%s", msg)
	}
	// HTML body should include an Accept button anchor.
	if !strings.Contains(msg, `href="https://aerly.test/?friend_accept=the-token-bytes"`) {
		t.Error("missing Accept button anchor")
	}
	if !strings.Contains(strings.ToLower(msg), ">accept<") {
		t.Error("missing visible Accept label on the button")
	}
}

func TestBuildFriendRequestEmailKeepsReviewLinkAlongsideAccept(t *testing.T) {
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr: "n@a", ToAddr: "b@b", PublicURL: "https://aerly.test",
		InviterLogin: "alice", Token: "tok",
	})
	if !strings.Contains(msg, "/friends") {
		t.Error("Review URL (/friends) should still be present")
	}
	if !strings.Contains(msg, "/?friend_accept=tok") {
		t.Error("Accept URL should be present alongside Review")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/handlers/ -run TestBuildFriendRequestEmail -v
```

Expected: build error — `friendRequestInput` has no `Token` field.

- [ ] **Step 3: Add `Token` and render both CTAs.**

In `internal/handlers/friend_emails.go`:

Add to `friendRequestInput`:

```go
type friendRequestInput struct {
	FromAddr     string
	ToAddr       string
	PublicURL    string
	InviterName  string
	InviterLogin string
	Message      string
	Token        string
}
```

Replace the body of `buildFriendRequestEmail` with:

```go
func buildFriendRequestEmail(in friendRequestInput) string {
	link := strings.TrimRight(in.PublicURL, "/")
	inviter := inviterLabel(in.InviterName, in.InviterLogin)
	acceptURL := link + "/?friend_accept=" + in.Token
	reviewURL := link + "/friends"

	plain := fmt.Sprintf(
		"%s wants to add you as a friend on Aerly. Once you accept they'll be able to see your flights and you'll see theirs.\r\n\r\n"+
			"Accept this request with one click:\r\n  %s\r\n\r\n"+
			"Or review the request at:\r\n  %s\r\n",
		inviter, acceptURL, reviewURL)
	if msg := strings.TrimSpace(in.Message); msg != "" {
		plain += "\r\nMessage from " + inviter + ":\r\n  " + msg + "\r\n"
	}
	plain += "\r\n— Aerly\r\n"

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 12px;font-size:15px;"><strong>%s</strong> wants to add you as a friend on Aerly.</p>`+
			`<p style="margin:0 0 16px;font-size:15px;">Once you accept they'll be able to see your flights and you'll see theirs.</p>`+
			`<p style="margin:0;">`+
			`<a href="%s" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;margin-right:8px;">Accept</a>`+
			`<a href="%s" style="display:inline-block;padding:10px 18px;border-radius:6px;border:1px solid %s;color:%s;font-weight:600;text-decoration:none;">Review the request</a>`+
			`</p>`,
		mailer.HTMLEscape(inviter),
		mailer.HTMLEscape(acceptURL), mailer.BrandColor,
		mailer.HTMLEscape(reviewURL), mailer.BrandColor, mailer.BrandColor)
	if msg := strings.TrimSpace(in.Message); msg != "" {
		htmlBody += fmt.Sprintf(
			`<p style="margin:18px 0 6px;font-size:13px;color:#666;">Message from %s:</p>`+
				`<blockquote style="margin:0;padding:10px 14px;border-left:3px solid #eaeaea;color:#333;font-size:14px;">%s</blockquote>`,
			mailer.HTMLEscape(inviter), mailer.HTMLEscape(msg))
	}

	subject := inviter + " sent you a friend request on Aerly"
	return assembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain, mailer.HTMLShell(subject, htmlBody, in.PublicURL))
}
```

- [ ] **Step 4: Mint the token in the sender.**

In `internal/handlers/friends.go`, near the top of the file (after the
existing constants), add:

```go
// friendAcceptTokenTTL bounds how long the Accept button in the friend
// request email remains clickable. The underlying pending friendship
// row stays around longer — only the email link goes dead — so a
// recipient can still accept in-app.
const friendAcceptTokenTTL = 7 * 24 * time.Hour
```

Update `sendFriendRequestNotification` to mint and pass the token:

```go
func (a *API) sendFriendRequestNotification(ctx context.Context, inviter, recipient *store.User, message string) {
	if a.Config == nil || a.Config.MailFromAddress == "" {
		slog.Warn("friend invite: MAIL_FROM_ADDRESS unset, skipping notification email",
			"from", inviter.ID, "to", recipient.ID)
		return
	}
	addrs, err := a.Store.EmailsByUser(ctx, recipient.ID)
	if err != nil {
		slog.Error("friend invite: load recipient emails failed", "err", err, "to", recipient.ID)
		return
	}
	to := ""
	for _, e := range addrs {
		if e.Verified {
			to = e.Address
			break
		}
	}
	if to == "" {
		return
	}
	token := auth.MintFriendAcceptToken(
		a.Config.SessionKey,
		recipient.ID, inviter.ID,
		time.Now().Add(friendAcceptTokenTTL),
	)
	msg := buildFriendRequestEmail(friendRequestInput{
		FromAddr:     a.Config.MailFromAddress,
		ToAddr:       to,
		PublicURL:    a.Config.PublicURL,
		InviterName:  inviter.Name,
		InviterLogin: inviter.Username,
		Message:      message,
		Token:        token,
	})
	sendCtx, cancel := context.WithTimeout(ctx, friendEmailSendTimeout)
	defer cancel()
	if err := mailer.Send(sendCtx, a.Config.SendmailPath, a.Config.MailFromAddress, msg); err != nil {
		slog.Error("friend invite: send notification failed", "err", err)
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass.**

```bash
go test ./internal/handlers/ -run TestBuildFriendRequestEmail -v
go test ./internal/handlers/...
```

Expected: both new tests PASS, plus the full handlers suite stays green.

- [ ] **Step 6: Commit.**

```bash
git add internal/handlers/friend_emails.go internal/handlers/friend_emails_test.go internal/handlers/friends.go
git commit -m "handlers: add Accept button + signed token to friend-request email"
```

---

## Task 8: Frontend types and API client methods

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts` (extend)

- [ ] **Step 1: Write the failing test.**

Append to `web/src/api/client.test.ts`:

```ts
describe('notifications', () => {
  it('GET /api/notifications returns the typed body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ friend_requests_pending: 3 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const n = await api.getNotifications();
    expect(n.friend_requests_pending).toBe(3);
    expect(fetchSpy).toHaveBeenCalledWith('/api/notifications', expect.objectContaining({ method: 'GET' }));
  });

  it('POST /api/friends/accept-token sends the token in the body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ already: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const r = await api.acceptFriendToken('abc.def');
    expect(r.already).toBe(true);
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/friends/accept-token',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ token: 'abc.def' }),
      }),
    );
  });
});
```

Look at the existing `web/src/api/client.test.ts` for the `vi.spyOn` /
`globalThis.fetch` pattern before pasting — match the imports already
present at the top of the file (don't double-import).

- [ ] **Step 2: Run the test to verify it fails.**

```bash
cd web && npx vitest run src/api/client.test.ts
```

Expected: TypeScript errors — `getNotifications` / `acceptFriendToken`
not on `api`.

- [ ] **Step 3: Add the types.**

Append to `web/src/api/types.ts`:

```ts
export interface Notifications {
  /** Count of friendship rows where the viewer is the recipient and
   *  status is still 'pending'. */
  friend_requests_pending: number;
}

export interface AcceptFriendTokenResult {
  /** Populated when the token resolved to a freshly-accepted row. */
  friendship?: Friendship;
  /** True when the pending row was already gone (already accepted,
   *  cancelled by the inviter, etc.). Mutually exclusive with
   *  `friendship`. */
  already?: boolean;
}
```

- [ ] **Step 4: Add the API methods.**

In `web/src/api/client.ts`, extend the import to add `Notifications` and
`AcceptFriendTokenResult`, then add to the `api` object (near the other
`/api/friends` entries):

```ts
  getNotifications: () => request<Notifications>('GET', '/api/notifications'),
  acceptFriendToken: (token: string) =>
    request<AcceptFriendTokenResult>('POST', '/api/friends/accept-token', { token }),
```

- [ ] **Step 5: Run the test to verify it passes.**

```bash
cd web && npx vitest run src/api/client.test.ts
```

Expected: full file PASS.

- [ ] **Step 6: Commit.**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/api/client.test.ts
git commit -m "web: api client for notifications + accept-friend-token"
```

---

## Task 9: Zustand state for notifications + notice

**Files:**
- Modify: `web/src/state/store.ts`

This step is wiring without its own test file — Task 11 covers the
badge UI which exercises this state, and Task 12 exercises the notice
toast. We do not add a dedicated store test because the existing
project doesn't have store-only tests (everything is exercised through
the components).

- [ ] **Step 1: Extend `AppState`.**

In `web/src/state/store.ts`, extend the `import type` block:

```ts
import type {
  Capabilities,
  CreateFlightInput,
  Flight,
  InviteUserInput,
  Notifications,
  UpdateFlightInput,
  UpdateUserInput,
  User,
} from '../api/types';
```

Inside the `AppState` interface, add (next to `error`):

```ts
  notifications: Notifications;
  notice: { message: string; severity: 'success' | 'info' } | null;
```

Add the new method signatures next to `setError`:

```ts
  refreshNotifications: () => Promise<void>;
  applyNotificationsUpdate: (n: Notifications) => void;
  setNotice: (n: { message: string; severity: 'success' | 'info' } | null) => void;
```

- [ ] **Step 2: Seed initial state.**

Inside the `create<AppState>(…)` literal, add fields next to `error`:

```ts
  notifications: { friend_requests_pending: 0 },
  notice: null,
```

- [ ] **Step 3: Fetch notifications inside `init()`.**

Replace the body of `init()` with:

```ts
  async init() {
    try {
      const [me, capabilities] = await Promise.all([api.getMe(), api.getConfig()]);
      set({ me, capabilities, auth: 'authenticated' });
      await Promise.all([get().refreshAll(), get().refreshNotifications()]);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        set({ me: null, auth: 'anonymous' });
      } else {
        set({ error: errorMessage(err), auth: 'anonymous' });
      }
    }
  },
```

- [ ] **Step 4: Add the new method bodies.**

Insert near `setError`:

```ts
  async refreshNotifications() {
    try {
      const n = await api.getNotifications();
      set({ notifications: n });
    } catch {
      // Non-fatal: stale badge is fine; SSE / next reload will recover.
    }
  },

  applyNotificationsUpdate(n) {
    set({ notifications: n });
  },

  setNotice(n) {
    set({ notice: n });
  },
```

Also clear `notice` and reset `notifications` on logout. In the
existing `async logout()` body, replace the `set({ … })` literal so it
includes:

```ts
      notifications: { friend_requests_pending: 0 },
      notice: null,
```

- [ ] **Step 5: Type-check.**

```bash
cd web && npx tsc --noEmit
```

Expected: exit 0.

- [ ] **Step 6: Run the full web test suite (nothing should regress).**

```bash
cd web && npm test -- --run
```

Expected: all green.

- [ ] **Step 7: Commit.**

```bash
git add web/src/state/store.ts
git commit -m "web: notifications + notice slice in the Zustand store"
```

---

## Task 10: SSE handler for `notifications.updated`

**Files:**
- Modify: `web/src/sse.ts`
- Modify: `web/src/sse.test.ts`

- [ ] **Step 1: Write the failing test.**

Append to `web/src/sse.test.ts`:

```ts
describe('notifications.updated events', () => {
  it('parses payload and forwards to onNotifications', () => {
    const onNotifications = vi.fn();
    const onFlight = vi.fn();
    const onDelete = vi.fn();
    const teardown = connectSSE({ onFlight, onDelete, onNotifications });

    const es = getMockEventSource();
    es.dispatch('notifications.updated', JSON.stringify({ friend_requests_pending: 4 }));
    expect(onNotifications).toHaveBeenCalledWith({ friend_requests_pending: 4 });

    teardown();
  });

  it('logs and ignores a malformed notifications payload', () => {
    const onNotifications = vi.fn();
    const teardown = connectSSE({
      onFlight: vi.fn(), onDelete: vi.fn(), onNotifications,
    });
    const es = getMockEventSource();
    es.dispatch('notifications.updated', '{not-json}');
    expect(onNotifications).not.toHaveBeenCalled();
    teardown();
  });
});
```

Look at the existing test file (`web/src/sse.test.ts`) for the
`getMockEventSource` helper and the EventSource mock setup before
adding — name the helper match whatever's already in scope.

- [ ] **Step 2: Run the test to verify it fails.**

```bash
cd web && npx vitest run src/sse.test.ts
```

Expected: TS errors — `onNotifications` not on `SSEHandlers`.

- [ ] **Step 3: Extend the handler.**

In `web/src/sse.ts`, update the interfaces and add the listener:

```ts
import type { Flight, Notifications } from './api/types';

export interface SSEHandlers {
  onFlight: (flight: Flight) => void;
  onDelete: (id: number) => void;
  onNotifications: (n: Notifications) => void;
}
```

Inside the `open()` function, after the `flight.deleted` listener:

```ts
    es.addEventListener('notifications.updated', (ev) => {
      try {
        const n = JSON.parse((ev as MessageEvent).data) as Notifications;
        handlers.onNotifications(n);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
```

- [ ] **Step 4: Run the test to verify it passes.**

```bash
cd web && npx vitest run src/sse.test.ts
```

Expected: full file PASS.

- [ ] **Step 5: Commit.**

```bash
git add web/src/sse.ts web/src/sse.test.ts
git commit -m "web: SSE handler for notifications.updated"
```

---

## Task 11: Badge on the avatar + chip beside "Friends…"

**Files:**
- Modify: `web/src/components/AppShell.tsx`
- Modify: `web/src/components/AppShell.test.tsx`

- [ ] **Step 1: Write the failing tests.**

Find the `h.state` literal at the top of `AppShell.test.tsx` and extend
it to include the new state. Add to the existing setup:

```ts
const h = vi.hoisted(() => ({
  state: {
    me: null as User | null,
    logout: vi.fn(),
    capabilities: { resolver_available: false, poll_interval_sec: 60, email_ingest_enabled: false },
    notifications: { friend_requests_pending: 0 },
  },
}));
```

Add new test cases:

```ts
describe('AppShell notifications badge', () => {
  beforeEach(() => {
    h.state.me = { id: 1, username: 'me', name: 'Me', avatar_url: '', is_superuser: false, is_active: true, has_logged_in: true };
    h.state.notifications = { friend_requests_pending: 0 };
  });

  it('hides the badge when no pending requests', () => {
    render(<AppShell />);
    const avatarButton = screen.getByRole('button', { name: /account menu/i });
    // MUI Badge marks the bubble invisible via class on the badge span.
    const bubble = avatarButton.parentElement?.querySelector('.MuiBadge-invisible');
    expect(bubble).toBeTruthy();
  });

  it('shows the count when there is a pending request', () => {
    h.state.notifications = { friend_requests_pending: 2 };
    render(<AppShell />);
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('renders a count chip next to the Friends menu item', async () => {
    h.state.notifications = { friend_requests_pending: 3 };
    render(<AppShell />);
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    const friendsItem = screen.getByRole('menuitem', { name: /friends/i });
    expect(friendsItem.textContent ?? '').toMatch(/3/);
  });
});
```

(`AppShell` is imported via the existing test setup; mirror the file's
import block at the top.)

- [ ] **Step 2: Run the test to verify it fails.**

```bash
cd web && npx vitest run src/components/AppShell.test.tsx
```

Expected: tests fail — badge not present.

- [ ] **Step 3: Add the badge and chip to `AppShell.tsx`.**

In `web/src/components/AppShell.tsx`:

Extend the imports:

```tsx
import {
  AppBar,
  Avatar,
  Badge,
  Box,
  Button,
  Chip,
  Divider,
  IconButton,
  ListItemIcon,
  Menu,
  MenuItem,
  Toolbar,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
```

Add a selector near `const me = …`:

```tsx
const pendingRequests = useStore((s) => s.notifications.friend_requests_pending);
```

Wrap the existing Tooltip+IconButton with a `Badge` (replace the block
that today is `<Tooltip title="Account menu">…</Tooltip>` with):

```tsx
<Badge
  badgeContent={pendingRequests}
  color="error"
  overlap="circular"
  invisible={pendingRequests === 0}
  anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
>
  <Tooltip title="Account menu">
    <IconButton
      size="small"
      onClick={(e) => setMenuAnchor(e.currentTarget)}
      aria-label="Account menu"
    >
      <Avatar src={me?.avatar_url} sx={{ width: 28, height: 28 }}>
        {me?.username.charAt(0).toUpperCase()}
      </Avatar>
    </IconButton>
  </Tooltip>
</Badge>
```

In the existing "Friends…" `MenuItem`, replace its single text child
with a flex row that puts the chip at the right:

```tsx
<MenuItem
  onClick={() => {
    closeMenu();
    setFriendsOpen(true);
  }}
>
  <ListItemIcon>
    <PeopleIcon fontSize="small" />
  </ListItemIcon>
  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexGrow: 1 }}>
    <Box>Friends…</Box>
    {pendingRequests > 0 && (
      <Chip
        label={pendingRequests}
        size="small"
        color="error"
        sx={{ ml: 'auto' }}
      />
    )}
  </Box>
</MenuItem>
```

- [ ] **Step 4: Run the test to verify it passes.**

```bash
cd web && npx vitest run src/components/AppShell.test.tsx
```

Expected: full file PASS.

- [ ] **Step 5: Run the full web suite.**

```bash
cd web && npm test -- --run
```

Expected: all green.

- [ ] **Step 6: Commit.**

```bash
git add web/src/components/AppShell.tsx web/src/components/AppShell.test.tsx
git commit -m "web: avatar badge + Friends chip showing pending request count"
```

---

## Task 12: Accept-token bootstrap effect + success Snackbar

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/App.test.tsx`

- [ ] **Step 1: Write the failing tests.**

Extend the `h.state` block in `web/src/App.test.tsx` with the new state
slots and a fake `acceptFriendToken`:

```ts
const h = vi.hoisted(() => {
  return {
    connectSSE: vi.fn((_handlers: {
      onFlight: (f: unknown) => void;
      onDelete: (id: number) => void;
      onNotifications: (n: unknown) => void;
    }) => vi.fn()),
    api: {
      acceptFriendToken: vi.fn(),
    },
    state: {
      auth: 'loading' as 'loading' | 'anonymous' | 'authenticated',
      error: null as string | null,
      notice: null as { message: string; severity: 'success' | 'info' } | null,
      init: vi.fn(),
      setError: vi.fn(),
      setNotice: vi.fn(),
      refreshNotifications: vi.fn(),
      applyFlightUpdate: vi.fn(),
      applyFlightDelete: vi.fn(),
      applyNotificationsUpdate: vi.fn(),
      users: [] as Array<{ id: number; name: string }>,
    },
  };
});
const connectSSE = h.connectSSE;
const state = h.state;

vi.mock('./sse', () => ({ connectSSE: h.connectSSE }));
vi.mock('./api/client', () => ({ api: h.api, ApiError: class {} }));
```

(The existing file has slightly different mocks — preserve everything
already there, only add the keys above. The exact merge will need a
hand-Edit, not a blind paste.)

Add new tests:

```ts
describe('friend-accept token bootstrap', () => {
  it('does not POST while anonymous; preserves the token in URL', async () => {
    state.auth = 'anonymous';
    window.history.pushState({}, '', '/?friend_accept=tok1');
    render(<App />);
    expect(h.api.acceptFriendToken).not.toHaveBeenCalled();
    expect(window.location.search).toBe('?friend_accept=tok1');
  });

  it('POSTs and shows a success notice when authenticated', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({
      friendship: { friend_id: 9, status: 'accepted', direction: '', requested_at: '' },
    });
    state.auth = 'authenticated';
    state.users = [{ id: 9, name: 'Alice' }];
    window.history.pushState({}, '', '/?friend_accept=tokA');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(h.api.acceptFriendToken).toHaveBeenCalledWith('tokA');
    expect(state.setNotice).toHaveBeenCalledWith({
      message: "You're now friends with Alice.",
      severity: 'success',
    });
    expect(window.location.search).toBe('');
    expect(state.refreshNotifications).toHaveBeenCalled();
  });

  it('shows an info notice when the request was already accepted', async () => {
    h.api.acceptFriendToken.mockResolvedValueOnce({ already: true });
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/?friend_accept=tokB');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setNotice).toHaveBeenCalledWith({
      message: "You're already friends — nothing to accept.",
      severity: 'info',
    });
  });

  it('surfaces a server error via setError, not setNotice', async () => {
    const err = new Error("this invitation isn't for your account");
    h.api.acceptFriendToken.mockRejectedValueOnce(err);
    state.auth = 'authenticated';
    window.history.pushState({}, '', '/?friend_accept=tokC');
    render(<App />);
    await new Promise((r) => setTimeout(r, 0));
    expect(state.setError).toHaveBeenCalledWith("this invitation isn't for your account");
    expect(state.setNotice).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
cd web && npx vitest run src/App.test.tsx
```

Expected: tests fail — effect not yet implemented.

- [ ] **Step 3: Implement the effect.**

In `web/src/App.tsx`:

Extend the imports:

```tsx
import { useEffect, useMemo } from 'react';
import { Alert, Box, CircularProgress, CssBaseline, Snackbar, ThemeProvider } from '@mui/material';
import { api } from './api/client';
```

Pull in the new state and an `users` selector:

```tsx
const setNotice = useStore((s) => s.setNotice);
const notice = useStore((s) => s.notice);
const refreshNotifications = useStore((s) => s.refreshNotifications);
const applyNotificationsUpdate = useStore((s) => s.applyNotificationsUpdate);
const users = useStore((s) => s.users);
```

Extend the existing SSE wiring to include `onNotifications`:

```tsx
useEffect(() => {
  if (auth !== 'authenticated') return;
  return connectSSE(
    {
      onFlight: (f) => applyFlightUpdate(f),
      onDelete: (id) => applyFlightDelete(id),
      onNotifications: (n) => applyNotificationsUpdate(n),
    },
    { showAll },
  );
}, [auth, applyFlightUpdate, applyFlightDelete, applyNotificationsUpdate, showAll]);
```

Add the accept-token effect (after the SSE effect):

```tsx
useEffect(() => {
  if (auth !== 'authenticated') return;
  const params = new URLSearchParams(window.location.search);
  const token = params.get('friend_accept');
  if (!token) return;
  void (async () => {
    try {
      const r = await api.acceptFriendToken(token);
      if (r.already) {
        setNotice({
          message: "You're already friends — nothing to accept.",
          severity: 'info',
        });
      } else {
        const friend = r.friendship
          ? users.find((u) => u.id === r.friendship!.friend_id)
          : undefined;
        const label = friend?.name?.trim() || 'them';
        setNotice({
          message: `You're now friends with ${label}.`,
          severity: 'success',
        });
      }
      void refreshNotifications();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      params.delete('friend_accept');
      const qs = params.toString();
      const url =
        window.location.pathname + (qs ? '?' + qs : '') + window.location.hash;
      window.history.replaceState({}, '', url);
    }
  })();
}, [auth, users, refreshNotifications, setError, setNotice]);
```

Render the success snackbar next to the existing error one:

```tsx
<Snackbar
  open={notice !== null}
  autoHideDuration={6000}
  onClose={() => setNotice(null)}
  anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
>
  {notice ? (
    <Alert severity={notice.severity} variant="filled" onClose={() => setNotice(null)}>
      {notice.message}
    </Alert>
  ) : undefined}
</Snackbar>
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
cd web && npx vitest run src/App.test.tsx
```

Expected: full file PASS.

- [ ] **Step 5: Run the full web suite.**

```bash
cd web && npm test -- --run
```

Expected: all green.

- [ ] **Step 6: Commit.**

```bash
git add web/src/App.tsx web/src/App.test.tsx
git commit -m "web: accept-token bootstrap + success Snackbar"
```

---

## Task 13: README + .env.example note

**Files:**
- Modify: `.env.example`
- Modify: `README.md`

- [ ] **Step 1: Add an `.env.example` note.**

Locate the `MAIL_FROM_ADDRESS` paragraph (`.env.example:31-39`). Append
one paragraph after the existing block that documents the friend-invite
flow specifically:

```
# Friend-request notifications: when MAIL_FROM_ADDRESS is set, Aerly
# also emails the recipient of a friend request with an "Accept"
# button. The button URL embeds a 7-day HMAC-signed token; after the
# 7 days elapse the recipient can still accept in-app from the Friends
# dialog (the avatar badge surfaces incoming requests live).
```

- [ ] **Step 2: Add the same note to the README.**

In `README.md`, find the existing friend-invite or signups paragraph
(near the top, where the Friends dialog is described). Add one
sentence:

```
The friend-request email includes a one-click "Accept" button signed
with a 7-day token; an avatar badge surfaces pending requests
in-app even when email is not configured.
```

- [ ] **Step 3: Commit.**

```bash
git add .env.example README.md
git commit -m "docs: note Accept-button token TTL + in-app badge"
```

---

## Task 14: End-to-end verification

- [ ] **Step 1: Run the full Go test suite.**

```bash
go test ./...
```

Expected: every package `ok`.

- [ ] **Step 2: Run the full web test suite.**

```bash
cd web && npm test -- --run
```

Expected: all green, no new failures.

- [ ] **Step 3: Manual smoke (informational only — describe in the PR description, not via a test).**

The user will validate by hand: with `MAIL_FROM_ADDRESS` configured and
two browser profiles, send a friend request from A → B, and confirm:

1. B's avatar gains a red `1` badge within ~1 s (SSE).
2. The Friends menu item shows a matching chip when the menu is opened.
3. B's email inbox receives the request with an Accept button.
4. Clicking Accept while signed in as B lands them on `/` with a
   success toast "You're now friends with A." and the badge drops to 0.
5. Clicking Accept while signed out lands them on Login; signing in
   then auto-processes the accept and shows the same toast.
6. Re-clicking the same Accept link a second time shows the info toast
   "You're already friends — nothing to accept."

The user will produce a screenshot of the avatar badge as the final
acceptance signal.

- [ ] **Step 4: Final commit / branch state check.**

```bash
git log --oneline origin/main..HEAD
```

Expected: one commit per Task 1–13 (13 commits), each scoped and
self-contained.

---

## Out of scope (do NOT implement)

- Generic notifications inbox / drawer.
- "Mark as read" semantics.
- Push notifications.
- A migration to backfill or resend any historic invites.
- Surfacing flight shares or identity-link emails in the same badge.
