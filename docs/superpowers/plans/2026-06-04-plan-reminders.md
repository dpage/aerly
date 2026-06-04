# Upcoming-plan email reminders — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users opt in (per-trip, overridable per-plan, with configurable lead time) for email + in-app reminders ahead of each upcoming plan-part, without touching the existing flight-status-change alerts.

**Architecture:** Three new tables (`trip_reminder_optin`, `plan_reminder_optin`, `plan_reminder_sent`). A new restart-safe pass in the existing poller (`remindUpcoming`) queries due `(plan_part, user)` reminders each tick, filters by plan visibility, and dispatches email (new `mailer.BuildPlanReminderEmail`) + an in-app `flight_alerts` row with `kind="reminder"`. Per-viewer opt-in state is surfaced on `TripDTO`/`PlanDTO`; four endpoints mutate it; the React app gets a trip-level switch, a per-plan override control, and an inbox nav branch.

**Tech Stack:** Go 1.26, pgx/v5, `net/http`; React 18 + TS + MUI + Zustand; Postgres migrations embedded via `//go:embed`.

Spec: `docs/superpowers/specs/2026-06-04-plan-reminders-design.md`.

---

### Task 1: Migration 0023 — reminder tables

**Files:**
- Create: `migrations/0023_plan_reminders.up.sql`
- Create: `migrations/0023_plan_reminders.down.sql`
- Test: `migrations/migrations_test.go` (existing table-count style test; add an assertion)

- [ ] **Step 1: Write the up migration**

```sql
-- migrations/0023_plan_reminders.up.sql
-- Upcoming-plan reminders (issue #11). Separate from the flight-status-change
-- alert tables (alert_prefs / plan_alert_optin): these drive scheduled "your
-- plan starts soon" emails + in-app notices, fired per plan_part by the poller.

-- Trip-level opt-in: presence of a row = opted in for the whole trip.
CREATE TABLE trip_reminder_optin (
    trip_id    BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lead_hours INT NOT NULL DEFAULT 24 CHECK (lead_hours > 0),
    PRIMARY KEY (trip_id, user_id)
);

-- Per-plan override. enabled = TRUE → opt in; enabled = FALSE → explicit
-- opt-out (beats a trip-level opt-in). Absence of a row = inherit the trip.
CREATE TABLE plan_reminder_optin (
    plan_id    BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL,
    lead_hours INT NOT NULL DEFAULT 24 CHECK (lead_hours > 0),
    PRIMARY KEY (plan_id, user_id)
);

-- Dedupe: one reminder per part per user, ever.
CREATE TABLE plan_reminder_sent (
    plan_part_id BIGINT NOT NULL REFERENCES plan_parts(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_part_id, user_id)
);
```

- [ ] **Step 2: Write the down migration**

```sql
-- migrations/0023_plan_reminders.down.sql
DROP TABLE IF EXISTS plan_reminder_sent;
DROP TABLE IF EXISTS plan_reminder_optin;
DROP TABLE IF EXISTS trip_reminder_optin;
```

- [ ] **Step 3: Run the migration test to verify up/down apply cleanly**

Run: `go test ./migrations/ -run TestMigrations -v`
Expected: PASS (the generic apply-all-up-then-down test exercises 0023).

- [ ] **Step 4: Commit**

```bash
git add migrations/0023_plan_reminders.up.sql migrations/0023_plan_reminders.down.sql
git commit -m "feat(db): reminder opt-in + sent tables (#11)"
```

---

### Task 2: Store — opt-in CRUD + resolution

**Files:**
- Create: `internal/store/reminders.go`
- Test: `internal/store/reminders_test.go`

- [ ] **Step 1: Write `reminders.go` opt-in CRUD**

```go
// internal/store/reminders.go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// TripReminder is a user's trip-level reminder opt-in. OptedIn=false means no
// row exists (the default).
type TripReminder struct {
	OptedIn   bool
	LeadHours int
}

// TripReminderFor returns the viewer's trip-level reminder opt-in, defaulting
// to not-opted-in (lead 24) when no row exists.
func (s *Store) TripReminderFor(ctx context.Context, tripID, userID int64) (TripReminder, error) {
	tr := TripReminder{OptedIn: false, LeadHours: 24}
	err := s.pool.QueryRow(ctx,
		`SELECT lead_hours FROM trip_reminder_optin WHERE trip_id = $1 AND user_id = $2`,
		tripID, userID).Scan(&tr.LeadHours)
	if errors.Is(err, pgx.ErrNoRows) {
		return tr, nil
	}
	if err != nil {
		return TripReminder{}, err
	}
	tr.OptedIn = true
	return tr, nil
}

// SetTripReminder upserts the trip-level opt-in (opt in / change lead).
func (s *Store) SetTripReminder(ctx context.Context, tripID, userID int64, leadHours int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO trip_reminder_optin (trip_id, user_id, lead_hours)
		VALUES ($1, $2, $3)
		ON CONFLICT (trip_id, user_id) DO UPDATE SET lead_hours = EXCLUDED.lead_hours`,
		tripID, userID, leadHours)
	return err
}

// RemoveTripReminder opts the user out at the trip level. No-op when absent.
func (s *Store) RemoveTripReminder(ctx context.Context, tripID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM trip_reminder_optin WHERE trip_id = $1 AND user_id = $2`, tripID, userID)
	return err
}

// PlanReminder is a user's per-plan reminder override. Override is "inherit"
// (no row), "on" (enabled), or "off" (explicit opt-out).
type PlanReminder struct {
	Override  string // inherit|on|off
	LeadHours int
}

// PlanReminderFor returns the viewer's per-plan override, defaulting to
// "inherit" (lead 24) when no row exists.
func (s *Store) PlanReminderFor(ctx context.Context, planID, userID int64) (PlanReminder, error) {
	pr := PlanReminder{Override: "inherit", LeadHours: 24}
	var enabled bool
	err := s.pool.QueryRow(ctx,
		`SELECT enabled, lead_hours FROM plan_reminder_optin WHERE plan_id = $1 AND user_id = $2`,
		planID, userID).Scan(&enabled, &pr.LeadHours)
	if errors.Is(err, pgx.ErrNoRows) {
		return pr, nil
	}
	if err != nil {
		return PlanReminder{}, err
	}
	if enabled {
		pr.Override = "on"
	} else {
		pr.Override = "off"
	}
	return pr, nil
}

// SetPlanReminder upserts a per-plan override.
func (s *Store) SetPlanReminder(ctx context.Context, planID, userID int64, enabled bool, leadHours int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plan_reminder_optin (plan_id, user_id, enabled, lead_hours)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (plan_id, user_id) DO UPDATE
		SET enabled = EXCLUDED.enabled, lead_hours = EXCLUDED.lead_hours`,
		planID, userID, enabled, leadHours)
	return err
}

// RemovePlanReminder clears a per-plan override (revert to inheriting the
// trip). No-op when absent.
func (s *Store) RemovePlanReminder(ctx context.Context, planID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM plan_reminder_optin WHERE plan_id = $1 AND user_id = $2`, planID, userID)
	return err
}

// MarkReminderSent records that a reminder for (part, user) has been dispatched
// so it never fires again. Idempotent.
func (s *Store) MarkReminderSent(ctx context.Context, partID, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plan_reminder_sent (plan_part_id, user_id) VALUES ($1, $2)
		ON CONFLICT (plan_part_id, user_id) DO NOTHING`, partID, userID)
	return err
}

// DueReminder is one resolved reminder the poller should dispatch.
type DueReminder struct {
	PlanPartID int64
	PlanID     int64
	TripID     int64
	UserID     int64
	LeadHours  int
	StartsAt   time.Time
	StartTZ    string
	StartLabel string
	PlanType   string
	PlanTitle  string
	Ident      string // flight ident when the part is a flight, else ""
	Email      string // newest verified address, "" when none
}
```

- [ ] **Step 2: Write the opt-in CRUD tests**

```go
// internal/store/reminders_test.go
package store

import (
	"testing"
	"time"
)

func TestTripReminder_UpsertAndRemove(t *testing.T) {
	s, ctx := newTestStore(t)
	uid := mustUser(t, s, "alice")
	tid := mustTrip(t, s, uid, "Trip")

	got, err := s.TripReminderFor(ctx, tid, uid)
	if err != nil {
		t.Fatalf("TripReminderFor: %v", err)
	}
	if got.OptedIn {
		t.Fatalf("default OptedIn = true, want false")
	}
	if err := s.SetTripReminder(ctx, tid, uid, 12); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}
	got, _ = s.TripReminderFor(ctx, tid, uid)
	if !got.OptedIn || got.LeadHours != 12 {
		t.Fatalf("after set = %+v, want {true 12}", got)
	}
	if err := s.RemoveTripReminder(ctx, tid, uid); err != nil {
		t.Fatalf("RemoveTripReminder: %v", err)
	}
	got, _ = s.TripReminderFor(ctx, tid, uid)
	if got.OptedIn {
		t.Fatalf("after remove OptedIn = true, want false")
	}
}

func TestPlanReminder_OverrideStates(t *testing.T) {
	s, ctx := newTestStore(t)
	uid := mustUser(t, s, "alice")
	tid := mustTrip(t, s, uid, "Trip")
	pid := mustPlan(t, s, tid, uid, "flight")

	got, _ := s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "inherit" {
		t.Fatalf("default override = %q, want inherit", got.Override)
	}
	if err := s.SetPlanReminder(ctx, pid, uid, false, 24); err != nil {
		t.Fatalf("SetPlanReminder off: %v", err)
	}
	got, _ = s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "off" {
		t.Fatalf("override = %q, want off", got.Override)
	}
	if err := s.SetPlanReminder(ctx, pid, uid, true, 6); err != nil {
		t.Fatalf("SetPlanReminder on: %v", err)
	}
	got, _ = s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "on" || got.LeadHours != 6 {
		t.Fatalf("override = %+v, want {on 6}", got)
	}
	if err := s.RemovePlanReminder(ctx, pid, uid); err != nil {
		t.Fatalf("RemovePlanReminder: %v", err)
	}
	got, _ = s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "inherit" {
		t.Fatalf("after remove override = %q, want inherit", got.Override)
	}
}

var _ = time.Now // keep time import for the DueReminders test added next
```

> NOTE on helpers: reuse the package's existing test helpers. Check `internal/store/*_test.go` for the real names (e.g. `newTestStore`, and how users/trips/plans/parts are created). If `mustUser`/`mustTrip`/`mustPlan`/`mustPart` don't exist, add thin local helpers in `reminders_test.go` that insert via the existing store methods (`CreateTrip`, `CreatePlan`, …) — match the signatures already used in `alerts_test.go`.

- [ ] **Step 3: Run the CRUD tests**

Run: `go test ./internal/store/ -run 'TestTripReminder_UpsertAndRemove|TestPlanReminder_OverrideStates' -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/store/reminders.go internal/store/reminders_test.go
git commit -m "feat(store): reminder opt-in CRUD + resolution types (#11)"
```

---

### Task 3: Store — `DueReminders` query

**Files:**
- Modify: `internal/store/reminders.go` (add `DueReminders`)
- Test: `internal/store/reminders_test.go`

- [ ] **Step 1: Add `DueReminders`**

```go
// DueReminders returns every (plan_part, user) reminder that is ready to fire
// at `now`: the user is effectively opted in (per-plan override beats the
// trip), the part is active and still upcoming, now is within the lead window,
// and no reminder was sent for that pair yet. Visibility is NOT applied here —
// the caller filters each candidate through VisiblePlanUserIDs so we reuse the
// tested §4 predicate rather than duplicate it.
func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]DueReminder, error) {
	rows, err := s.pool.Query(ctx, `
		WITH pairs AS (
			-- trip-level opt-in expands to every plan in the trip
			SELECT DISTINCT pl.id AS plan_id, pl.trip_id, tro.user_id
			FROM trip_reminder_optin tro
			JOIN plans pl ON pl.trip_id = tro.trip_id
			UNION
			-- per-plan override (enabled resolved below)
			SELECT DISTINCT pro.plan_id, pl.trip_id, pro.user_id
			FROM plan_reminder_optin pro
			JOIN plans pl ON pl.id = pro.plan_id
		),
		resolved AS (
			SELECT p.plan_id, p.trip_id, p.user_id,
			       COALESCE(pro.enabled, TRUE) AS enabled,
			       COALESCE(pro.lead_hours, tro.lead_hours) AS lead_hours
			FROM pairs p
			LEFT JOIN plan_reminder_optin pro
			       ON pro.plan_id = p.plan_id AND pro.user_id = p.user_id
			LEFT JOIN trip_reminder_optin tro
			       ON tro.trip_id = p.trip_id AND tro.user_id = p.user_id
		)
		SELECT pp.id, r.plan_id, r.trip_id, r.user_id, r.lead_hours,
		       pp.starts_at, pp.start_tz, pp.start_label, pl.type, pl.title,
		       COALESCE(fd.ident, ''),
		       COALESCE((
		           SELECT e.address FROM user_emails e
		           WHERE e.user_id = r.user_id AND e.verified = TRUE
		           ORDER BY e.verified_at DESC NULLS LAST, e.id DESC
		           LIMIT 1
		       ), '')
		FROM resolved r
		JOIN plans pl ON pl.id = r.plan_id
		JOIN plan_parts pp ON pp.plan_id = r.plan_id
		LEFT JOIN flight_details fd ON fd.plan_part_id = pp.id
		WHERE r.enabled
		  AND pp.status <> 'cancelled'
		  AND pp.dismissed_at IS NULL
		  AND pp.starts_at > $1
		  AND $1 >= pp.starts_at - make_interval(hours => r.lead_hours)
		  AND NOT EXISTS (
		      SELECT 1 FROM plan_reminder_sent prs
		      WHERE prs.plan_part_id = pp.id AND prs.user_id = r.user_id
		  )
		ORDER BY pp.starts_at, r.user_id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueReminder
	for rows.Next() {
		var d DueReminder
		if err := rows.Scan(&d.PlanPartID, &d.PlanID, &d.TripID, &d.UserID, &d.LeadHours,
			&d.StartsAt, &d.StartTZ, &d.StartLabel, &d.PlanType, &d.PlanTitle,
			&d.Ident, &d.Email); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Write `DueReminders` resolution tests**

Cover: (a) trip opt-in fires for a part inside the lead window; (b) a part outside the window / already started does not fire; (c) a plan `off` override suppresses a trip opt-in; (d) a plan `on` override fires with no trip opt-in and uses its lead; (e) a `plan_reminder_sent` row suppresses; (f) cancelled/dismissed parts excluded.

```go
func TestDueReminders_Resolution(t *testing.T) {
	s, ctx := newTestStore(t)
	uid := mustUser(t, s, "alice")
	tid := mustTrip(t, s, uid, "Trip")
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	// Plan A: part starts in 10h. Trip opt-in lead 24h → inside window → fires.
	planA := mustPlan(t, s, tid, uid, "flight")
	partA := mustPart(t, s, planA, now.Add(10*time.Hour))
	// Plan B: part starts in 40h. Trip lead 24h → outside window → no fire.
	planB := mustPlan(t, s, tid, uid, "hotel")
	partB := mustPart(t, s, planB, now.Add(40*time.Hour))

	if err := s.SetTripReminder(ctx, tid, uid, 24); err != nil {
		t.Fatal(err)
	}
	due, err := s.DueReminders(ctx, now)
	if err != nil {
		t.Fatalf("DueReminders: %v", err)
	}
	if !hasReminder(due, partA, uid) {
		t.Errorf("partA (10h, lead 24) not due")
	}
	if hasReminder(due, partB, uid) {
		t.Errorf("partB (40h, lead 24) due, want not due")
	}

	// Plan A off-override suppresses despite the trip opt-in.
	if err := s.SetPlanReminder(ctx, planA, uid, false, 24); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueReminders(ctx, now)
	if hasReminder(due, partA, uid) {
		t.Errorf("partA with off-override still due")
	}

	// Plan B on-override lead 48h pulls it into the window.
	if err := s.SetPlanReminder(ctx, planB, uid, true, 48); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueReminders(ctx, now)
	if !hasReminder(due, partB, uid) {
		t.Errorf("partB with on-override lead 48 not due")
	}

	// Once marked sent, partB stops firing.
	if err := s.MarkReminderSent(ctx, partB, uid); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueReminders(ctx, now)
	if hasReminder(due, partB, uid) {
		t.Errorf("partB still due after MarkReminderSent")
	}
}

func hasReminder(due []DueReminder, partID, userID int64) bool {
	for _, d := range due {
		if d.PlanPartID == partID && d.UserID == userID {
			return true
		}
	}
	return false
}
```

> NOTE: `mustPart(t, s, planID, startsAt)` should insert a `plan_parts` row (seq 0, status 'planned', a non-empty `starts_at`) via the existing part-creation store method. Confirm the real signature in the store (e.g. `CreatePlanPart` / `AddPlanPart`) and adapt; flights also need a `flight_details` row if `mustPlan` type is "flight" — match how `alerts_test.go` builds flight parts.

- [ ] **Step 3: Run the test**

Run: `go test ./internal/store/ -run TestDueReminders_Resolution -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/store/reminders.go internal/store/reminders_test.go
git commit -m "feat(store): DueReminders scheduler query (#11)"
```

---

### Task 4: Mailer — plan reminder email

**Files:**
- Create: `internal/mailer/plan_reminder.go`
- Test: `internal/mailer/plan_reminder_test.go`

- [ ] **Step 1: Write `plan_reminder.go`**

```go
// internal/mailer/plan_reminder.go
package mailer

import (
	"fmt"
	"strings"
	"time"
)

// PlanReminderInput is the data needed to render an upcoming-plan reminder
// (issue #11). Label is the human name for the plan (flight ident, plan title,
// or a type fallback); StartsAt/StartTZ give the local start time; TripID
// targets the "Open Aerly" link at the trip timeline.
type PlanReminderInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	TripID    int64
	Label     string
	StartsAt  time.Time
	StartTZ   string // IANA; falls back to UTC when empty/invalid
}

// PlanReminderLabel derives a concise label from a plan's type/title/ident.
// Flights lead with their ident; otherwise the title wins, then a type word.
func PlanReminderLabel(planType, title, ident string) string {
	if planType == "flight" && ident != "" {
		return "flight " + ident
	}
	if strings.TrimSpace(title) != "" {
		return title
	}
	switch planType {
	case "flight":
		return "your flight"
	case "train":
		return "your train"
	case "hotel":
		return "your hotel check-in"
	case "ground":
		return "your transfer"
	case "dining":
		return "your reservation"
	case "excursion":
		return "your excursion"
	default:
		return "your plan"
	}
}

// PlanReminderSubject returns the Subject line, e.g. "Upcoming: flight BA123".
func PlanReminderSubject(label string) string {
	return "Upcoming: " + label
}

// localTime renders StartsAt in the part's IANA zone, falling back to UTC.
func localTime(t time.Time, tz string) string {
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return t.In(loc).Format("Mon 2 Jan, 15:04 MST")
}

// BuildPlanReminderEmail renders the complete RFC822 reminder message.
func BuildPlanReminderEmail(in PlanReminderInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	subject := PlanReminderSubject(in.Label)
	when := localTime(in.StartsAt, in.StartTZ)
	lead := fmt.Sprintf("%s starts %s", capitalise(in.Label), when)
	link := fmt.Sprintf("%s/trips/%d", site, in.TripID)

	plain := fmt.Sprintf(
		"%s.\r\n\r\nOpen Aerly to see the details: %s\r\n\r\n— Aerly\r\n",
		lead, link)

	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 16px;font-size:15px;">%s.</p>`+
			`<p style="margin:0;"><a href="%s" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open Aerly</a></p>`,
		HTMLEscape(lead), HTMLEscape(link), BrandColor)

	return AssembleRFC822(in.FromAddr, in.ToAddr, subject,
		plain, HTMLShell(subject, htmlBody, in.PublicURL))
}

// capitalise upper-cases the first rune of s (for the lead line). ASCII-only is
// fine for our labels.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
```

- [ ] **Step 2: Write the mailer test**

```go
// internal/mailer/plan_reminder_test.go
package mailer

import (
	"strings"
	"testing"
	"time"
)

func TestPlanReminderLabel(t *testing.T) {
	cases := []struct{ typ, title, ident, want string }{
		{"flight", "", "BA123", "flight BA123"},
		{"hotel", "Hilton Vienna", "", "Hilton Vienna"},
		{"dining", "", "", "your reservation"},
	}
	for _, c := range cases {
		if got := PlanReminderLabel(c.typ, c.title, c.ident); got != c.want {
			t.Errorf("PlanReminderLabel(%q,%q,%q) = %q, want %q", c.typ, c.title, c.ident, got, c.want)
		}
	}
}

func TestBuildPlanReminderEmail(t *testing.T) {
	msg := BuildPlanReminderEmail(PlanReminderInput{
		FromAddr:  "aerly@example.com",
		ToAddr:    "alice@example.com",
		PublicURL: "https://aerly.test",
		TripID:    7,
		Label:     "flight BA123",
		StartsAt:  time.Date(2026, 6, 5, 9, 30, 0, 0, time.UTC),
		StartTZ:   "UTC",
	})
	for _, want := range []string{
		"Subject: Upcoming: flight BA123",
		"Flight BA123 starts",
		"https://aerly.test/trips/7",
		"To: alice@example.com",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("email missing %q\n---\n%s", want, msg)
		}
	}
}
```

- [ ] **Step 3: Run + commit**

Run: `go test ./internal/mailer/ -run 'TestPlanReminderLabel|TestBuildPlanReminderEmail' -v` → PASS

```bash
git add internal/mailer/plan_reminder.go internal/mailer/plan_reminder_test.go
git commit -m "feat(mailer): upcoming-plan reminder email (#11)"
```

---

### Task 5: Poller — `remindUpcoming` pass

**Files:**
- Create: `internal/poller/reminders.go`
- Modify: `internal/poller/poller.go` (call `p.remindUpcoming(ctx, now)` at the end of `tick`)
- Test: `internal/poller/reminders_test.go`

- [ ] **Step 1: Write `reminders.go`**

```go
// internal/poller/reminders.go
package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/mailer"
	"github.com/dpage/aerly/internal/store"
)

// remindUpcoming dispatches upcoming-plan reminders (issue #11). It is a
// restart-safe DB-driven pass: each tick it asks the store for due
// (part, user) reminders, filters them by plan visibility, then sends email +
// an in-app alert and marks the pair sent. It is wholly independent of the
// flight-status-change alert path (maybeAlert) and of alert_prefs.
func (p *Poller) remindUpcoming(ctx context.Context, now time.Time) {
	due, err := p.Store.DueReminders(ctx, now)
	if err != nil {
		slog.Error("reminder: list due", "err", err)
		return
	}
	// Cache the visible-user set per plan so N parts of one plan cost one query.
	visCache := map[int64]map[int64]bool{}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		vis, ok := visCache[d.PlanID]
		if !ok {
			ids, verr := p.Store.VisiblePlanUserIDs(ctx, d.PlanID)
			if verr != nil {
				slog.Error("reminder: visibility", "plan", d.PlanID, "err", verr)
				continue
			}
			vis = make(map[int64]bool, len(ids))
			for _, id := range ids {
				vis[id] = true
			}
			visCache[d.PlanID] = vis
		}
		if !vis[d.UserID] {
			continue // trip-level opt-in must not leak a hidden plan
		}
		guard("poller.remind", d.PlanPartID, func() { p.dispatchReminder(ctx, d) })
	}
}

// dispatchReminder sends the email + in-app reminder for one due pair, then
// marks it sent. MarkReminderSent runs last so a crash mid-send re-sends rather
// than silently dropping (mirrors the dedupe-sig ordering in alerts.go).
func (p *Poller) dispatchReminder(ctx context.Context, d store.DueReminder) {
	label := mailer.PlanReminderLabel(d.PlanType, d.PlanTitle, d.Ident)

	// In-app: always (reuses the flight_alerts inbox with kind="reminder").
	p.publishReminder(d, label)

	// Email: only when mail is configured and the user has a verified address.
	if p.MailFromAddress != "" && d.Email != "" {
		send := p.SendAlertEmail
		if send == nil {
			send = mailer.Send
		}
		msg := mailer.BuildPlanReminderEmail(mailer.PlanReminderInput{
			FromAddr:  p.MailFromAddress,
			ToAddr:    d.Email,
			PublicURL: p.PublicURL,
			TripID:    d.TripID,
			Label:     label,
			StartsAt:  d.StartsAt,
			StartTZ:   d.StartTZ,
		})
		if err := send(ctx, p.SendmailPath, p.MailFromAddress, msg); err != nil {
			slog.Error("reminder: send email", "to", d.Email, "part", d.PlanPartID, "err", err)
		}
	}

	if err := p.Store.MarkReminderSent(ctx, d.PlanPartID, d.UserID); err != nil {
		slog.Error("reminder: mark sent", "part", d.PlanPartID, "user", d.UserID, "err", err)
	}
}

// publishReminder persists an in-app alert row (kind="reminder") and pushes the
// user-private alert.created SSE event, reusing the same shape as a flight
// alert so the inbox renders it with no client change beyond nav branching.
func (p *Poller) publishReminder(d store.DueReminder, label string) {
	msg := mailer.PlanReminderSubject(label)
	stored, err := p.Store.InsertFlightAlert(context.Background(), store.FlightAlert{
		UserID:     d.UserID,
		PlanPartID: d.PlanPartID,
		PlanID:     d.PlanID,
		TripID:     d.TripID,
		Ident:      label, // not a flight ident for non-flights; carries the label
		Kind:       "reminder",
		Status:     d.PlanType,
		Message:    msg,
	})
	if err != nil {
		slog.Error("reminder: persist inbox row", "user", d.UserID, "err", err)
		return
	}
	dto := api.NotificationsDTO{Alert: ptrFlightAlertDTO(api.ToFlightAlertDTO(stored))}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("reminder: marshal", "err", err)
		return
	}
	p.Hub.Publish(sseAlertEvent(d.UserID, payload))
}
```

- [ ] **Step 2: Wire the pass into `tick`**

In `internal/poller/poller.go`, at the very end of `func (p *Poller) tick(ctx context.Context)` (after the metadata-pass loop), add:

```go
	// Upcoming-plan reminders (issue #11) — independent of the status-change
	// alert path above.
	p.remindUpcoming(ctx, now)
```

- [ ] **Step 3: Write the poller test**

```go
// internal/poller/reminders_test.go
package poller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func TestRemindUpcoming_EmailAndInApp(t *testing.T) {
	s, ctx := newPollerTestStore(t) // reuse the alerts_test.go store helper
	owner := mustUser(t, s, "alice")
	mustVerifiedEmail(t, s, owner, "alice@example.com")
	tid := mustTrip(t, s, owner, "Trip")
	plan := mustPlan(t, s, tid, owner, "hotel")
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	part := mustPart(t, s, plan, now.Add(2*time.Hour))
	if err := s.SetTripReminder(ctx, tid, owner, 24); err != nil {
		t.Fatal(err)
	}

	var sent []string
	p := &Poller{
		Store:           s,
		Hub:             newTestHub(t),
		MailFromAddress: "aerly@example.com",
		PublicURL:       "https://aerly.test",
		SendAlertEmail: func(_ context.Context, _, _, msg string) error {
			sent = append(sent, msg)
			return nil
		},
	}

	p.remindUpcoming(ctx, now)

	if len(sent) != 1 || !strings.Contains(sent[0], "Upcoming:") {
		t.Fatalf("want 1 reminder email, got %d: %v", len(sent), sent)
	}
	alerts, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 1 || alerts[0].Kind != "reminder" {
		t.Fatalf("want 1 in-app reminder, got %+v", alerts)
	}

	// Second tick must not re-send (marked sent).
	sent = nil
	p.remindUpcoming(ctx, now)
	if len(sent) != 0 {
		t.Fatalf("re-sent on second tick: %v", sent)
	}
}
```

> NOTE: align helper names with `internal/poller/alerts_test.go` (it already builds a store, a hub, users with verified emails, trips, flight parts). Reuse those exact helpers; only add `mustPart`/`mustPlan` for non-flight types if missing.

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/poller/ -run TestRemindUpcoming_EmailAndInApp -v` → PASS
Run: `go test ./internal/poller/...` → PASS (no regression in existing alert tests)

```bash
git add internal/poller/reminders.go internal/poller/poller.go internal/poller/reminders_test.go
git commit -m "feat(poller): upcoming-plan reminder pass (#11)"
```

---

### Task 6: API — DTO fields + endpoints

**Files:**
- Modify: `internal/api/dto.go` (add fields to `TripDTO` and `PlanDTO`)
- Modify: `internal/handlers/handlers_trips.go` (`tripDTO` populates reminder fields)
- Modify: `internal/handlers/handlers_plans.go` (`planDTO` populates reminder fields)
- Create: `internal/handlers/handlers_reminders.go` (four handlers)
- Modify: `internal/handlers/handlers.go` (register four routes)
- Test: `internal/handlers/handlers_reminders_test.go`

- [ ] **Step 1: Add DTO fields**

In `internal/api/dto.go`, add to `TripDTO` (after `UpdatedAt` block, before close):

```go
	// ReminderOptedIn / ReminderLeadHours are the requesting viewer's
	// trip-level upcoming-plan reminder opt-in (issue #11), per-viewer.
	ReminderOptedIn   bool `json:"reminder_opted_in"`
	ReminderLeadHours int  `json:"reminder_lead_hours"`
```

Add to `PlanDTO` (next to `AlertOptedIn`):

```go
	// ReminderOverride is the viewer's per-plan reminder override (issue #11):
	// "inherit" (use trip), "on", or "off". ReminderLeadHours is the override's
	// lead when "on" (else the default 24).
	ReminderOverride  string `json:"reminder_override"`
	ReminderLeadHours int    `json:"reminder_lead_hours"`
```

- [ ] **Step 2: Populate in `tripDTO`**

In `internal/handlers/handlers_trips.go`, inside `tripDTO`, before `return dto, nil`:

```go
	tr, err := a.Store.TripReminderFor(r.Context(), t.ID, viewerID)
	if err != nil {
		return api.TripDTO{}, err
	}
	dto.ReminderOptedIn = tr.OptedIn
	dto.ReminderLeadHours = tr.LeadHours
```

- [ ] **Step 3: Populate in `planDTO`**

In `internal/handlers/handlers_plans.go`, where `optedIn` is computed (around line 802), add alongside it and set on the returned struct:

```go
	reminder := store.PlanReminder{Override: "inherit", LeadHours: 24}
	if viewerID != 0 {
		reminder, err = a.Store.PlanReminderFor(ctx, planID, viewerID)
		if err != nil {
			return api.PlanDTO{}, err
		}
	}
```

and in the `return api.PlanDTO{...}` literal add:

```go
		ReminderOverride:  reminder.Override,
		ReminderLeadHours: reminder.LeadHours,
```

- [ ] **Step 4: Write the four handlers**

```go
// internal/handlers/handlers_reminders.go
package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/auth"
)

// clampLead bounds a lead-hours value to a sane range (1h..1yr). Defaults to 24
// when non-positive.
func clampLead(h int) int {
	if h <= 0 {
		return 24
	}
	if h > 8760 {
		return 8760
	}
	return h
}

type reminderInput struct {
	LeadHours int  `json:"lead_hours"`
	Enabled   bool `json:"enabled"`
}

func (a *API) setTripReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid trip id")
		return
	}
	ok, err := a.Store.CanViewTrip(r.Context(), tripID, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var in reminderInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := a.Store.SetTripReminder(r.Context(), tripID, me.ID, clampLead(in.LeadHours)); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteTripReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid trip id")
		return
	}
	if err := a.Store.RemoveTripReminder(r.Context(), tripID, me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setPlanReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	planID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	ok, err := a.Store.CanViewPlan(r.Context(), planID, me.ID, me.IsSuperuser)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var in reminderInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := a.Store.SetPlanReminder(r.Context(), planID, me.ID, in.Enabled, clampLead(in.LeadHours)); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deletePlanReminder(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	planID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid plan id")
		return
	}
	if err := a.Store.RemovePlanReminder(r.Context(), planID, me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Register routes**

In `internal/handlers/handlers.go`, near the plan-alert-optin routes (lines 156–157), add:

```go
	mux.Handle("PUT /api/trips/{id}/reminder", req(http.HandlerFunc(a.setTripReminder)))
	mux.Handle("DELETE /api/trips/{id}/reminder", req(http.HandlerFunc(a.deleteTripReminder)))
	mux.Handle("PUT /api/plans/{id}/reminder", req(http.HandlerFunc(a.setPlanReminder)))
	mux.Handle("DELETE /api/plans/{id}/reminder", req(http.HandlerFunc(a.deletePlanReminder)))
```

- [ ] **Step 6: Write handler tests**

```go
// internal/handlers/handlers_reminders_test.go
package handlers

import (
	"net/http"
	"testing"
)

func TestTripReminder_RoundTrip(t *testing.T) {
	e := newTestEnv(t)
	owner := e.mustUser("alice")
	tid := e.mustTrip(owner, "Trip")

	// Default: not opted in.
	if optedIn, _ := tripReminderState(t, e, tid, owner); optedIn {
		t.Fatal("default reminder_opted_in = true, want false")
	}
	// PUT opts in with a lead.
	e.do(t, owner, http.MethodPut, "/api/trips/"+itoa(tid)+"/reminder",
		`{"lead_hours":12}`, http.StatusNoContent)
	optedIn, lead := tripReminderState(t, e, tid, owner)
	if !optedIn || lead != 12 {
		t.Fatalf("after PUT = (%v,%d), want (true,12)", optedIn, lead)
	}
	// DELETE opts out.
	e.do(t, owner, http.MethodDelete, "/api/trips/"+itoa(tid)+"/reminder", "", http.StatusNoContent)
	if optedIn, _ := tripReminderState(t, e, tid, owner); optedIn {
		t.Fatal("after DELETE reminder_opted_in = true, want false")
	}
}

func TestPlanReminder_Override(t *testing.T) {
	e := newTestEnv(t)
	owner := e.mustUser("alice")
	tid := e.mustTrip(owner, "Trip")
	pid := e.mustPlan(tid, owner, "flight")

	e.do(t, owner, http.MethodPut, "/api/plans/"+itoa(pid)+"/reminder",
		`{"enabled":false,"lead_hours":24}`, http.StatusNoContent)
	if ov := planReminderOverride(t, e, tid, pid, owner); ov != "off" {
		t.Fatalf("override = %q, want off", ov)
	}
}

func TestSetTripReminder_NonMemberHidden(t *testing.T) {
	e := newTestEnv(t)
	owner := e.mustUser("alice")
	stranger := e.mustUser("bob")
	tid := e.mustTrip(owner, "Trip")
	e.do(t, stranger, http.MethodPut, "/api/trips/"+itoa(tid)+"/reminder",
		`{"lead_hours":24}`, http.StatusNotFound)
}
```

> NOTE: mirror the helper names/usage in `followups_test.go` / `handlers_alerts_test.go` (`newTestEnv`, `e.do`, reading trip detail JSON). `tripReminderState` / `planReminderOverride` parse the `GET /api/trips/{id}` body for `reminder_opted_in`/`reminder_lead_hours` and the plan's `reminder_override` — copy the JSON-walk in `planAlertOptedIn`.

- [ ] **Step 7: Run + commit**

Run: `go test ./internal/api/... ./internal/handlers/... -run 'Reminder' -v` → PASS
Run: `go test ./internal/...` → PASS

```bash
git add internal/api/dto.go internal/handlers/handlers_trips.go internal/handlers/handlers_plans.go internal/handlers/handlers_reminders.go internal/handlers/handlers.go internal/handlers/handlers_reminders_test.go
git commit -m "feat(api): reminder opt-in DTO fields + endpoints (#11)"
```

---

### Task 7: Web — types, client, store slice

**Files:**
- Modify: `web/src/api/types.ts` (Trip + Plan fields)
- Modify: `web/src/api/client.ts` (four methods)
- Modify: `web/src/state/alertsSlice.ts` (four actions)
- Test: `web/src/state/alertsSlice.test.ts`

- [ ] **Step 1: Add types**

In `web/src/api/types.ts`, add to `Trip`:

```ts
  /** Viewer's trip-level upcoming-plan reminder opt-in (#11). */
  reminder_opted_in: boolean;
  reminder_lead_hours: number;
```

Add to `Plan`:

```ts
  /** Viewer's per-plan reminder override (#11): "inherit" | "on" | "off". */
  reminder_override: 'inherit' | 'on' | 'off';
  reminder_lead_hours: number;
```

- [ ] **Step 2: Add client methods**

In `web/src/api/client.ts`, in the `api` object near `optOutPlanAlerts`:

```ts
  setTripReminder: (tripId: number, leadHours: number) =>
    request<void>('PUT', `/api/trips/${tripId}/reminder`, { lead_hours: leadHours }),
  clearTripReminder: (tripId: number) =>
    request<void>('DELETE', `/api/trips/${tripId}/reminder`),
  setPlanReminder: (planId: number, enabled: boolean, leadHours: number) =>
    request<void>('PUT', `/api/plans/${planId}/reminder`, { enabled, lead_hours: leadHours }),
  clearPlanReminder: (planId: number) =>
    request<void>('DELETE', `/api/plans/${planId}/reminder`),
```

- [ ] **Step 3: Add store actions**

In `web/src/state/alertsSlice.ts`, add to the `AlertsSlice` interface:

```ts
  setTripReminder: (tripId: number, leadHours: number) => Promise<void>;
  clearTripReminder: (tripId: number) => Promise<void>;
  setPlanReminder: (planId: number, enabled: boolean, leadHours: number) => Promise<void>;
  clearPlanReminder: (planId: number) => Promise<void>;
```

and the implementations (each reloads the current trip so the DTO fields refresh):

```ts
  async setTripReminder(tripId, leadHours) {
    await api.setTripReminder(tripId, leadHours);
    await reloadCurrent(get);
  },
  async clearTripReminder(tripId) {
    await api.clearTripReminder(tripId);
    await reloadCurrent(get);
  },
  async setPlanReminder(planId, enabled, leadHours) {
    await api.setPlanReminder(planId, enabled, leadHours);
    await reloadCurrent(get);
  },
  async clearPlanReminder(planId) {
    await api.clearPlanReminder(planId);
    await reloadCurrent(get);
  },
```

- [ ] **Step 4: Test the slice actions (mock `api`)**

Add to `web/src/state/alertsSlice.test.ts` a test asserting `setTripReminder` calls `api.setTripReminder` with the right args and triggers a reload — mirror the existing `optInPlanAlerts` test in that file.

- [ ] **Step 5: Run + commit**

Run: `cd web && npm run test -- alertsSlice` → PASS; `npm run build` (tsc) → no type errors

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/state/alertsSlice.ts web/src/state/alertsSlice.test.ts
git commit -m "feat(web): reminder api client + store actions (#11)"
```

---

### Task 8: Web — trip-level reminder control

**Files:**
- Create: `web/src/components/TripReminderToggle.tsx`
- Modify: `web/src/pages/TripDetail.tsx` (render it)
- Test: `web/src/components/TripReminderToggle.test.tsx`

- [ ] **Step 1: Write the component**

```tsx
// web/src/components/TripReminderToggle.tsx
import { useEffect, useState } from 'react';
import { Box, FormControlLabel, Stack, Switch, TextField } from '@mui/material';

import { useStore } from '../state/store';
import type { Trip } from '../api/types';

interface Props {
  trip: Trip;
}

/** Trip-level "Email me reminders" opt-in (#11). On = a row in
 * trip_reminder_optin; the number field sets the lead time in hours. */
export default function TripReminderToggle({ trip }: Props) {
  const setTripReminder = useStore((s) => s.setTripReminder);
  const clearTripReminder = useStore((s) => s.clearTripReminder);
  const setError = useStore((s) => s.setError);

  const [on, setOn] = useState(trip.reminder_opted_in);
  const [lead, setLead] = useState(String(trip.reminder_lead_hours || 24));
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setOn(trip.reminder_opted_in);
    setLead(String(trip.reminder_lead_hours || 24));
  }, [trip.reminder_opted_in, trip.reminder_lead_hours]);

  const apply = async (next: boolean, leadHours: number) => {
    setBusy(true);
    setOn(next);
    try {
      if (next) await setTripReminder(trip.id, leadHours);
      else await clearTripReminder(trip.id);
    } catch (err) {
      setOn(!next);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const leadNum = () => {
    const n = Number.parseInt(lead, 10);
    return Number.isFinite(n) && n > 0 ? n : 24;
  };

  return (
    <Stack direction="row" spacing={2} alignItems="center">
      <FormControlLabel
        control={
          <Switch
            checked={on}
            disabled={busy}
            onChange={(e) => void apply(e.target.checked, leadNum())}
            inputProps={{ 'aria-label': 'Email me reminders for this trip' }}
          />
        }
        label="Email me reminders"
      />
      {on && (
        <Box>
          <TextField
            label="Hours before"
            type="number"
            size="small"
            value={lead}
            disabled={busy}
            onChange={(e) => setLead(e.target.value)}
            onBlur={() => void apply(true, leadNum())}
            slotProps={{ htmlInput: { min: 1, 'aria-label': 'Reminder lead time in hours' } }}
            sx={{ width: 130 }}
          />
        </Box>
      )}
    </Stack>
  );
}
```

- [ ] **Step 2: Render in `TripDetail.tsx`**

Import and place `<TripReminderToggle trip={trip} />` in the trip header/settings area (near where the trip's other per-viewer controls or actions live). Match the surrounding layout.

- [ ] **Step 3: Component test**

```tsx
// web/src/components/TripReminderToggle.test.tsx
// Render with a trip where reminder_opted_in=false; toggle the switch; assert
// setTripReminder(trip.id, 24) is called. Then a trip with reminder_opted_in
// true shows the hours field. Mirror PlanAlertToggle.test.tsx's store-mock setup.
```

- [ ] **Step 4: Run + commit**

Run: `cd web && npm run test -- TripReminderToggle` → PASS

```bash
git add web/src/components/TripReminderToggle.tsx web/src/components/TripReminderToggle.test.tsx web/src/pages/TripDetail.tsx
git commit -m "feat(web): trip-level reminder toggle (#11)"
```

---

### Task 9: Web — per-plan reminder override control

**Files:**
- Create: `web/src/components/PlanReminderOverride.tsx`
- Modify: `web/src/components/PlanEditDialog.tsx` (render it)
- Test: `web/src/components/PlanReminderOverride.test.tsx`

- [ ] **Step 1: Write the component**

```tsx
// web/src/components/PlanReminderOverride.tsx
import { useEffect, useState } from 'react';
import { Box, MenuItem, Stack, TextField } from '@mui/material';

import { useStore } from '../state/store';
import type { Plan } from '../api/types';

interface Props {
  plan: Plan;
}

/** Per-plan reminder override (#11). "Use trip setting" clears the override;
 * "Remind me" / "Don't remind me" write an explicit plan_reminder_optin row. */
export default function PlanReminderOverride({ plan }: Props) {
  const setPlanReminder = useStore((s) => s.setPlanReminder);
  const clearPlanReminder = useStore((s) => s.clearPlanReminder);
  const setError = useStore((s) => s.setError);

  const [mode, setMode] = useState(plan.reminder_override);
  const [lead, setLead] = useState(String(plan.reminder_lead_hours || 24));
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setMode(plan.reminder_override);
    setLead(String(plan.reminder_lead_hours || 24));
  }, [plan.reminder_override, plan.reminder_lead_hours]);

  const leadNum = () => {
    const n = Number.parseInt(lead, 10);
    return Number.isFinite(n) && n > 0 ? n : 24;
  };

  const apply = async (next: 'inherit' | 'on' | 'off', leadHours: number) => {
    setBusy(true);
    setMode(next);
    try {
      if (next === 'inherit') await clearPlanReminder(plan.id);
      else await setPlanReminder(plan.id, next === 'on', leadHours);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Stack direction="row" spacing={2} alignItems="center">
      <TextField
        select
        size="small"
        label="Reminder"
        value={mode}
        disabled={busy}
        onChange={(e) => void apply(e.target.value as 'inherit' | 'on' | 'off', leadNum())}
        sx={{ minWidth: 180 }}
      >
        <MenuItem value="inherit">Use trip setting</MenuItem>
        <MenuItem value="on">Remind me</MenuItem>
        <MenuItem value="off">Don&apos;t remind me</MenuItem>
      </TextField>
      {mode === 'on' && (
        <Box>
          <TextField
            label="Hours before"
            type="number"
            size="small"
            value={lead}
            disabled={busy}
            onChange={(e) => setLead(e.target.value)}
            onBlur={() => void apply('on', leadNum())}
            slotProps={{ htmlInput: { min: 1, 'aria-label': 'Reminder lead time in hours' } }}
            sx={{ width: 130 }}
          />
        </Box>
      )}
    </Stack>
  );
}
```

- [ ] **Step 2: Render in `PlanEditDialog.tsx`**

Place `<PlanReminderOverride plan={plan} />` in the dialog body near the existing `PlanAlertToggle` (or notes/visibility section). Match surrounding spacing.

- [ ] **Step 3: Component test**

```tsx
// web/src/components/PlanReminderOverride.test.tsx
// plan.reminder_override="inherit" → select shows "Use trip setting"; choose
// "Don't remind me" → setPlanReminder(plan.id, false, 24) called. Choosing
// "Use trip setting" from "on" → clearPlanReminder(plan.id) called.
```

- [ ] **Step 4: Run + commit**

Run: `cd web && npm run test -- PlanReminderOverride` → PASS

```bash
git add web/src/components/PlanReminderOverride.tsx web/src/components/PlanReminderOverride.test.tsx web/src/components/PlanEditDialog.tsx
git commit -m "feat(web): per-plan reminder override (#11)"
```

---

### Task 10: Web — inbox relabel + reminder nav branch

**Files:**
- Modify: `web/src/components/Layout.tsx`
- Test: `web/src/components/Layout.test.tsx`

- [ ] **Step 1: Relabel + branch nav**

In `Layout.tsx`, change the inbox section header from `Flight alerts` to `Alerts`. Change the alert `onClick` to branch by kind:

```tsx
                    onClick={() => {
                      closeMenu();
                      if (al.kind === 'reminder') navigate(`/trips/${al.trip_id}`);
                      else navigate(`/tracker?part=${al.plan_part_id}`);
                    }}
```

- [ ] **Step 2: Test the branch**

Add a `Layout.test.tsx` case: an alert with `kind:'reminder'` and `trip_id:5` navigates to `/trips/5` on click; a non-reminder alert still navigates to `/tracker?part=…`. Mirror the existing alert-inbox test setup in that file.

- [ ] **Step 3: Run + commit**

Run: `cd web && npm run test -- Layout` → PASS

```bash
git add web/src/components/Layout.tsx web/src/components/Layout.test.tsx
git commit -m "feat(web): reminder inbox label + nav branch (#11)"
```

---

### Task 11: Docs

**Files:**
- Modify: `README.md` (a short note in the alerts/notifications area)

- [ ] **Step 1: Document the feature**

Add a concise paragraph: users can opt in to upcoming-plan reminders at the trip level (with a configurable lead time in hours) and override per plan (including opting a single plan out); reminders are delivered by email and in the in-app inbox, and are independent of gate/delay/cancellation alerts, which always fire.

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: upcoming-plan reminders (#11)"
```

---

## Final verification (before opening the PR)

- [ ] `make build` → succeeds
- [ ] `make test` (or `go test ./...` + `cd web && npm test`) → all green
- [ ] `make lint` → clean
- [ ] `make fmt` → no diff (run `gofmt`/prettier as the project does)
- [ ] Manual sanity: with `DEV_AUTH_BYPASS`, opt in at trip level on a trip with a part ~now+1h, confirm a reminder email is attempted (logs) and an in-app alert appears.

## Self-review notes (coverage vs spec)

- Data model § migration → Task 1. ✔
- Effective opt-in resolution → Task 2 (`*ReminderFor`) + Task 3 (`DueReminders` SQL). ✔
- Scheduler pass + visibility filter + dedupe-after-dispatch → Task 5. ✔
- Email template → Task 4. ✔
- DTO fields + 4 endpoints + authz → Task 6. ✔
- Frontend trip control, plan override, inbox branch → Tasks 7–10. ✔
- Testing across store/poller/mailer/handlers/web → in each task. ✔
- Non-goals (no alert_prefs coupling, no status-change changes) honored: reminder path never reads `alert_prefs`; `maybeAlert` untouched. ✔
