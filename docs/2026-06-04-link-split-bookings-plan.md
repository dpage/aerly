# Link / split multi-part bookings — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to
> implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Let users link separate flight/train plans into one multi-part booking
and split a leg out of a multi-part booking, plus auto-group same-PNR legs at
capture time.

**Architecture:** Two transactional `*store.Store` methods (`LinkPlans`,
`SplitPlanPart`) drive two new endpoints (`POST /api/plans/{id}/link`,
`POST /api/plan-parts/{id}/split`); a pure post-pass in `planops.Propose` merges
same-`confirmation_ref` flight/train proposals; the web timeline gains a link
selection mode and `PlanEditDialog` gains a per-leg split button.

**Tech Stack:** Go 1.26 + pgx/v5 (backend), React 18 + TS + Zustand + MUI +
Vitest (frontend), `make build|test|lint|fmt`.

Spec: `docs/2026-06-04-link-split-bookings-design.md`.

---

## File structure

- `internal/store/plans.go` — add `ErrNotSplittable`, `LinkPlans`, `SplitPlanPart`.
- `internal/store/plans_test.go` — store tests.
- `internal/handlers/handlers_plans.go` — add `linkPlans`, `splitPlanPart` handlers + `linkReq`.
- `internal/handlers/handlers.go` — register two routes.
- `internal/handlers/handlers_plans_test.go` — handler tests.
- `internal/planops/propose.go` — `groupByConfirmationRef` post-pass.
- `internal/planops/propose_test.go` — grouping test.
- `internal/emailingest/extract.go` — sharpen `plansSystemPrompt`.
- `web/src/api/types.ts` — `LinkPlansInput`.
- `web/src/api/client.ts` — `linkPlans`, `splitPlanPart`.
- `web/src/state/plansSlice.ts` — `linkPlans`, `splitPlanPart` actions.
- `web/src/components/PlanEditDialog.tsx` — per-leg "Split out" button.
- `web/src/pages/TripTimeline.tsx` — link selection mode.
- Matching `*.test.ts(x)` updates.

---

## Task 1: Store `LinkPlans`

**Files:** Modify `internal/store/plans.go`; Test `internal/store/plans_test.go`.

- [ ] **Step 1: Add the sentinel error** near the other store errors (e.g. top of
  `plans.go` after imports):

```go
// ErrNotSplittable is returned when a split is asked of a plan that has only
// one live part (nothing to separate) or whose type isn't link/split-eligible.
var ErrNotSplittable = errors.New("plan is not splittable")

// linkableType reports whether a plan type may hold multi-leg bookings that can
// be linked/split (flights and trains). Other types are single-venue.
func linkableType(t string) bool { return t == "flight" || t == "train" }
```

- [ ] **Step 2: Write the failing test** `TestLinkPlans` in `plans_test.go`.
  Create a trip, two flight plans each with one part + flight_details, then:

```go
func TestLinkPlans(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tripID := mustCreateTrip(t, s, "T")
	a := mustCreateFlightPlan(t, s, tripID, "AY1", time.Now())
	b := mustCreateFlightPlan(t, s, tripID, "AY2", time.Now().Add(2*time.Hour))

	if err := s.LinkPlans(ctx, a.ID, []int64{b.ID}); err != nil {
		t.Fatalf("LinkPlans: %v", err)
	}
	parts, _ := s.PartsByPlan(ctx, a.ID)
	if len(parts) != 2 {
		t.Fatalf("want 2 parts on primary, got %d", len(parts))
	}
	if parts[0].Seq != 0 || parts[1].Seq != 1 {
		t.Fatalf("parts not re-sequenced: %d,%d", parts[0].Seq, parts[1].Seq)
	}
	if _, err := s.PlanByID(ctx, b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absorbed plan should be deleted, got %v", err)
	}
}
```

  Use existing test helpers; check `plans_test.go` / `helpers_test.go` for the
  real helper names (`newTestStore`, trip/plan creation). If a flight-plan helper
  doesn't exist, add a small local one using `s.CreatePlan` with a
  `CreatePlanPartPayload{Flight: &FlightDetail{Ident: ..., ScheduledOut: ...,
  ScheduledIn: ...}}`.

- [ ] **Step 3: Run it, expect FAIL** (`LinkPlans` undefined):
  `go test ./internal/store/ -run TestLinkPlans`

- [ ] **Step 4: Implement `LinkPlans`:**

```go
// LinkPlans folds the absorbed plans' parts into the primary plan, making one
// multi-part booking. All plans must be in the same trip and share one
// link/split-eligible type (flight|train). The primary's title, confirmation_ref,
// notes, passengers and visibility win; the absorbed plans are deleted. Live
// tracking (positions) and per-type details travel with each part (they key on
// plan_part_id); flight_alerts.plan_id is repointed so deleting the absorbed
// plans leaves no dangling reference.
func (s *Store) LinkPlans(ctx context.Context, primaryID int64, absorbIDs []int64) error {
	if len(absorbIDs) == 0 {
		return errors.New("no plans to link")
	}
	for _, id := range absorbIDs {
		if id == primaryID {
			return errors.New("cannot link a plan to itself")
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	primary, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM plans WHERE id = $1 FOR UPDATE`, primaryID))
	if err != nil {
		return err
	}
	if !linkableType(primary.Type) {
		return fmt.Errorf("plan type %q cannot be linked", primary.Type)
	}
	// Validate every absorbed plan: exists, same trip, same type.
	rows, err := tx.Query(ctx, `SELECT id, trip_id, type FROM plans WHERE id = ANY($1) FOR UPDATE`, absorbIDs)
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	for rows.Next() {
		var id, tripID int64
		var typ string
		if err := rows.Scan(&id, &tripID, &typ); err != nil {
			rows.Close()
			return err
		}
		if tripID != primary.TripID {
			rows.Close()
			return fmt.Errorf("plan %d is not in the same trip", id)
		}
		if typ != primary.Type {
			rows.Close()
			return fmt.Errorf("plan %d has type %q, not %q", id, typ, primary.Type)
		}
		seen[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range absorbIDs {
		if !seen[id] {
			return ErrNotFound
		}
	}
	// Repoint alerts first (they reference plan_id, no FK), then re-parent parts.
	if _, err := tx.Exec(ctx, `UPDATE flight_alerts SET plan_id = $1 WHERE plan_id = ANY($2)`, primaryID, absorbIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_parts SET plan_id = $1, updated_at = NOW() WHERE plan_id = ANY($2)`, primaryID, absorbIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM plans WHERE id = ANY($1)`, absorbIDs); err != nil {
		return err
	}
	if err := resequencePartsTx(ctx, tx, primaryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE plans SET updated_at = NOW() WHERE id = $1`, primaryID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// resequencePartsTx renumbers a plan's live (non-dismissed) parts seq=0..n by
// start time so a freshly merged/split plan reads in chronological order.
// Dismissed parts keep their rows but are pushed after live ones.
func resequencePartsTx(ctx context.Context, tx pgx.Tx, planID int64) error {
	_, err := tx.Exec(ctx, `
		WITH ordered AS (
			SELECT id, ROW_NUMBER() OVER (
				ORDER BY (dismissed_at IS NOT NULL), starts_at, id) - 1 AS rn
			FROM plan_parts WHERE plan_id = $1)
		UPDATE plan_parts p SET seq = o.rn
		FROM ordered o WHERE p.id = o.id AND p.seq <> o.rn`, planID)
	return err
}
```

  Ensure `fmt` is imported in `plans.go` (add if missing).

- [ ] **Step 5: Run it, expect PASS:** `go test ./internal/store/ -run TestLinkPlans`

- [ ] **Step 6: Add rejection tests** `TestLinkPlansRejects` (cross-trip,
  cross-type, self, empty, missing id → respective errors), run, expect PASS.

- [ ] **Step 7: Commit:** `git add -A && git commit -m "feat(store): LinkPlans folds plans into one multi-part booking"`

---

## Task 2: Store `SplitPlanPart`

**Files:** Modify `internal/store/plans.go`; Test `internal/store/plans_test.go`.

- [ ] **Step 1: Write the failing test** `TestSplitPlanPart`: build a 2-part flight
  plan (use `LinkPlans` from Task 1, or `CreatePlan` with two parts), set
  visibility `only_visible_to` a user + a passenger, then split the second part:

```go
func TestSplitPlanPart(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// ... build a 2-part flight plan `p` in trip `tripID`, viewer `u` ...
	if err := s.SetPlanVisibility(ctx, p.ID, "only_visible_to", []int64{u}); err != nil {
		t.Fatal(err)
	}
	parts, _ := s.PartsByPlan(ctx, p.ID)
	newID, parentID, err := s.SplitPlanPart(ctx, parts[1].ID)
	if err != nil {
		t.Fatalf("SplitPlanPart: %v", err)
	}
	if parentID != p.ID {
		t.Fatalf("parent = %d, want %d", parentID, p.ID)
	}
	np, _ := s.PartsByPlan(ctx, newID)
	if len(np) != 1 {
		t.Fatalf("new plan should have 1 part, got %d", len(np))
	}
	vis, _ := s.PlanVisibilityFor(ctx, newID)
	if vis == nil || vis.Mode != "only_visible_to" || len(vis.UserIDs) != 1 || vis.UserIDs[0] != u {
		t.Fatalf("visibility not copied to split plan: %+v", vis)
	}
}
```

- [ ] **Step 2: Run it, expect FAIL** (`SplitPlanPart` undefined).

- [ ] **Step 3: Implement `SplitPlanPart`:**

```go
// SplitPlanPart moves one part out of its plan into a brand-new plan in the same
// trip, so a mis-grouped booking can be separated. The new plan copies the
// parent's type, source, title, confirmation_ref and notes, and — crucially —
// its passengers and visibility, so the split leg keeps the exact same audience
// (a split must never widen privacy). Returns the new and parent plan ids.
// Returns ErrNotSplittable if the plan has one or zero live parts or its type is
// not link/split-eligible.
func (s *Store) SplitPlanPart(ctx context.Context, partID int64) (newPlanID, parentPlanID int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	var parent Plan
	err = tx.QueryRow(ctx, `
		SELECT `+strings.ReplaceAll(planColumns, "id,", "pl.id,")+`
		FROM plan_parts part JOIN plans pl ON pl.id = part.plan_id
		WHERE part.id = $1 FOR UPDATE OF pl`, partID).Scan(
		&parent.ID, &parent.TripID, &parent.Type, &parent.Title, &parent.ConfirmationRef,
		&parent.Notes, &parent.Source, &parent.CreatedBy, &parent.CreatedAt, &parent.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	if !linkableType(parent.Type) {
		return 0, 0, ErrNotSplittable
	}
	var liveCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM plan_parts WHERE plan_id = $1 AND dismissed_at IS NULL`, parent.ID).Scan(&liveCount); err != nil {
		return 0, 0, err
	}
	if liveCount <= 1 {
		return 0, 0, ErrNotSplittable
	}
	// New plan: same identity fields as parent (a copy; user edits afterward).
	if err := tx.QueryRow(ctx, `
		INSERT INTO plans (trip_id, type, title, confirmation_ref, notes, source, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		parent.TripID, parent.Type, parent.Title, parent.ConfirmationRef, parent.Notes,
		parent.Source, parent.CreatedBy).Scan(&newPlanID); err != nil {
		return 0, 0, err
	}
	// Copy visibility (parent → new) BEFORE passengers so the audience matches.
	if _, err := tx.Exec(ctx, `INSERT INTO plan_visibility (plan_id, mode) SELECT $1, mode FROM plan_visibility WHERE plan_id = $2`, newPlanID, parent.ID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO plan_visibility_members (plan_id, user_id) SELECT $1, user_id FROM plan_visibility_members WHERE plan_id = $2`, newPlanID, parent.ID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO plan_passengers (plan_id, user_id, via_trip, added_at) SELECT $1, user_id, via_trip, added_at FROM plan_passengers WHERE plan_id = $2`, newPlanID, parent.ID); err != nil {
		return 0, 0, err
	}
	// Move the part + its alerts to the new plan.
	if _, err := tx.Exec(ctx, `UPDATE flight_alerts SET plan_id = $1 WHERE plan_part_id = $2`, newPlanID, partID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE plan_parts SET plan_id = $1, updated_at = NOW() WHERE id = $2`, newPlanID, partID); err != nil {
		return 0, 0, err
	}
	if err := resequencePartsTx(ctx, tx, parent.ID); err != nil {
		return 0, 0, err
	}
	if err := resequencePartsTx(ctx, tx, newPlanID); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return newPlanID, parent.ID, nil
}
```

  Note: the `strings.ReplaceAll(planColumns,...)` trick is fragile; prefer writing
  the explicit column list `pl.id, pl.trip_id, pl.type, pl.title,
  pl.confirmation_ref, pl.notes, pl.source, pl.created_by, pl.created_at,
  pl.updated_at` inline. Use the explicit list in the real implementation.

- [ ] **Step 4: Run it, expect PASS.**

- [ ] **Step 5: Add `TestSplitPlanPartRejectsSinglePart`** (single-part plan →
  `ErrNotSplittable`) and a passengers-copied assertion; run, expect PASS.

- [ ] **Step 6: Commit:** `git commit -am "feat(store): SplitPlanPart separates a leg into its own plan"`

---

## Task 3: HTTP endpoints

**Files:** Modify `internal/handlers/handlers_plans.go`, `handlers.go`; Test
`internal/handlers/handlers_plans_test.go`.

- [ ] **Step 1: Register routes** in `handlers.go` after the move route (line ~121):

```go
mux.Handle("POST /api/plans/{id}/link", req(http.HandlerFunc(a.linkPlans)))
mux.Handle("POST /api/plan-parts/{id}/split", req(http.HandlerFunc(a.splitPlanPart)))
```

- [ ] **Step 2: Add the request type** next to `moveReq`:

```go
type linkReq struct {
	PlanIDs []int64 `json:"plan_ids"`
}
```

- [ ] **Step 3: Add handlers** in `handlers_plans.go`:

```go
// linkPlans folds the plans named in the body into the path plan (the primary),
// making one multi-part booking. Editor rights on the plan's trip are required;
// the store validates that every absorbed plan is in that same trip.
func (a *API) linkPlans(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in linkReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(in.PlanIDs) == 0 {
		writeError(w, http.StatusBadRequest, "plan_ids required")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	// Editor on every absorbed plan too (defense in depth; store re-checks trip).
	for _, pid := range in.PlanIDs {
		if err := a.requirePlanEdit(r.Context(), pid, me, w); err != nil {
			return
		}
	}
	// Capture trip ids of absorbed plans before they vanish, to notify deletion.
	absorbed := map[int64]int64{} // planID -> tripID
	for _, pid := range in.PlanIDs {
		if p, err := a.Store.PlanByID(r.Context(), pid); err == nil {
			absorbed[pid] = p.TripID
		}
	}
	if err := a.Store.LinkPlans(r.Context(), id, in.PlanIDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	for pid, tripID := range absorbed {
		a.publishPlanDeleted(r.Context(), tripID, pid)
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}

// splitPlanPart separates one leg of a multi-part booking into its own plan.
func (a *API) splitPlanPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePartEdit(r.Context(), id, me, w); err != nil {
		return
	}
	newID, parentID, err := a.Store.SplitPlanPart(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotSplittable) {
			writeError(w, http.StatusBadRequest, "plan has only one part")
			return
		}
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), newID, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, parentID)
	a.publishPlanUpdated(r.Context(), dto.TripID, newID)
	writeJSON(w, http.StatusCreated, dto)
}
```

  Confirm `errors` and `store` are imported in `handlers_plans.go` (they are).

- [ ] **Step 4: Write handler tests** in `handlers_plans_test.go`: a successful
  link (200, parts folded), a non-editor link (403), a split (201, new plan with
  one part), a split of a single-part plan (400). Follow the existing
  `handlers_plans_test.go` harness (look at the move/update tests for the request
  helper + auth context setup).

- [ ] **Step 5: Run:** `go test ./internal/handlers/ -run 'Link|Split'` → PASS.

- [ ] **Step 6: Commit:** `git commit -am "feat(api): link/split endpoints for multi-part bookings"`

---

## Task 4: Capture-time auto-grouping + LLM prompt

**Files:** Modify `internal/planops/propose.go`, `internal/emailingest/extract.go`;
Test `internal/planops/propose_test.go`.

- [ ] **Step 1: Write the failing test** `TestProposeGroupsByConfirmationRef` in
  `propose_test.go`: feed a fake extractor two flight plans sharing
  `confirmation_ref="ABC123"` (different idents/times), assert `Propose` returns
  one plan with two parts ordered by `StartsAt`; and that two flights with empty
  refs stay separate. Use the existing fake-extractor pattern in that test file.

- [ ] **Step 2: Run it, expect FAIL.**

- [ ] **Step 3: Implement** the post-pass in `propose.go`, called from `Propose`
  right before `applyTransferTimes(out)`:

```go
out = groupByConfirmationRef(out)

// groupByConfirmationRef folds flight/train proposals that share a non-empty,
// case-insensitive confirmation_ref into a single multi-part proposal, parts
// ordered by start. The LLM and importers sometimes split one PNR across
// several plans; this re-groups them so the user confirms one booking. Plans
// with an empty ref, or of a non-linkable type, are passed through untouched and
// in their original order.
func groupByConfirmationRef(plans []ProposedPlan) []ProposedPlan {
	type group struct{ idx int }
	byKey := map[string]int{} // key -> index in out
	out := make([]ProposedPlan, 0, len(plans))
	for _, p := range plans {
		key := strings.ToUpper(strings.TrimSpace(p.ConfirmationRef))
		if key == "" || (p.Type != "flight" && p.Type != "train") {
			out = append(out, p)
			continue
		}
		gk := p.Type + "\x00" + key
		if i, ok := byKey[gk]; ok {
			out[i].Parts = append(out[i].Parts, p.Parts...)
			if p.Confidence < out[i].Confidence {
				out[i].Confidence = p.Confidence
			}
			// A merged plan is no longer a single-flight rebooking candidate.
			out[i].SupersedesPartID = nil
			continue
		}
		byKey[gk] = len(out)
		out = append(out, p)
	}
	for i := range out {
		if len(out[i].Parts) > 1 {
			sort.SliceStable(out[i].Parts, func(a, b int) bool {
				return out[i].Parts[a].StartsAt.Before(out[i].Parts[b].StartsAt)
			})
		}
	}
	return out
}
```

  Add `"sort"` to `propose.go` imports.

- [ ] **Step 4: Run it, expect PASS.**

- [ ] **Step 5: Sharpen the LLM prompt** in `extract.go` `plansSystemPrompt` —
  tighten the existing "One booking = one plan" sentence to:

  > One booking = one plan; a round-trip, multi-city, or connecting itinerary
  > booked under a single confirmation reference (PNR) is ONE plan with several
  > parts — one part per leg, in travel order. Only emit separate plans when the
  > legs were genuinely booked separately (different confirmation references).

- [ ] **Step 6: Run the package tests:** `go test ./internal/planops/ ./internal/emailingest/` → PASS (adjust any prompt-snapshot assertion in `extract_test.go`).

- [ ] **Step 7: Commit:** `git commit -am "feat(planops): group same-PNR legs into one booking; sharpen extract prompt"`

---

## Task 5: Web API client + store actions

**Files:** Modify `web/src/api/types.ts`, `web/src/api/client.ts`,
`web/src/state/plansSlice.ts` (+ tests).

- [ ] **Step 1: Add the input type** in `types.ts`:

```ts
export interface LinkPlansInput {
  plan_ids: number[];
}
```

- [ ] **Step 2: Add client calls** in `client.ts` after `movePlan`:

```ts
  linkPlans: (planId: number, input: LinkPlansInput) =>
    request<Plan>('POST', `/api/plans/${planId}/link`, input),
  splitPlanPart: (partId: number) =>
    request<Plan>('POST', `/api/plan-parts/${partId}/split`),
```

  Import `LinkPlansInput` in `client.ts`.

- [ ] **Step 3: Add store actions** in `plansSlice.ts` (interface + impl), mirroring `movePlan`:

```ts
  linkPlans: (primaryId: number, planIds: number[]) => Promise<void>;
  splitPlanPart: (partId: number) => Promise<void>;
```
```ts
  async linkPlans(primaryId, planIds) {
    await api.linkPlans(primaryId, { plan_ids: planIds });
    await reloadCurrent(get);
  },
  async splitPlanPart(partId) {
    await api.splitPlanPart(partId);
    await reloadCurrent(get);
  },
```

- [ ] **Step 4: Update `plansSlice.test.ts`** to cover the two new actions (assert
  client called + reload), following the existing `movePlan` test.

- [ ] **Step 5: Run:** `cd web && npx vitest run src/state/plansSlice.test.ts src/api/client.test.ts` → PASS.

- [ ] **Step 6: Commit:** `git commit -am "feat(web): link/split api client + store actions"`

---

## Task 6: Split button in PlanEditDialog

**Files:** Modify `web/src/components/PlanEditDialog.tsx` (+ test).

- [ ] **Step 1: Read** `PlanEditDialog.tsx` to see how `editableParts` are
  rendered (the per-part block around line 213) and how actions like move are
  wired, plus how the dialog closes/refreshes.

- [ ] **Step 2: Write the failing test** in `PlanEditDialog.test.tsx`: render with
  a 2-part flight plan, assert a "Split out" button is present on each leg and
  clicking it calls a mocked `splitPlanPart` with the part id; render with a
  1-part plan and assert no "Split out" button.

- [ ] **Step 3: Implement.** Pull `splitPlanPart` from the store
  (`const splitPlanPart = useStore((s) => s.splitPlanPart)`). In the per-part
  block, when `editableParts.length > 1` and the plan type is `flight`/`train`,
  render a small `Button`/`IconButton` "Split out" that calls
  `await splitPlanPart(part.id)` then closes the dialog (the leg moves to a new
  plan; the timeline reload reflects it). Guard against splitting while saving.

- [ ] **Step 4: Run the test, expect PASS.**

- [ ] **Step 5: Commit:** `git commit -am "feat(web): split a leg out of a booking from the edit dialog"`

---

## Task 7: Link selection mode on the timeline

**Files:** Modify `web/src/pages/TripTimeline.tsx` (+ test).

- [ ] **Step 1: Read** `TripTimeline.tsx` — `multiPartPlanIds` (line ~65),
  `PartCard` props (line ~168), and how plans/parts and the edit affordances are
  rendered, plus where trip-level action buttons live.

- [ ] **Step 2: Write the failing test** in `TripTimeline.test.tsx`: enter link
  mode, select two flight plans, confirm, assert `linkPlans` called with
  `(primaryId, [otherId])`; assert selecting a flight and a hotel keeps confirm
  disabled (mixed/ineligible types).

- [ ] **Step 3: Implement.** Add local state `linkMode: boolean` and `selected:
  Set<number>` (plan ids). A "Link bookings" toggle button (shown to editors)
  enters the mode; in link mode each eligible plan (type flight/train) shows a
  checkbox/toggle; ineligible plans are non-selectable. A "Link N" confirm button
  is enabled only when ≥2 plans of the *same* type are selected; it calls
  `linkPlans(primary, rest)` where `primary` is the earliest-starting selected
  plan, then exits link mode. Derive editor rights the same way the existing
  edit/delete affordances do.

- [ ] **Step 4: Run the test, expect PASS.**

- [ ] **Step 5: Commit:** `git commit -am "feat(web): link bookings via timeline selection"`

---

## Task 8: Full verification

- [ ] **Step 1:** `make fmt`
- [ ] **Step 2:** `make lint`
- [ ] **Step 3:** `make build`
- [ ] **Step 4:** `make test` (Go + web). Fix any failures before proceeding.
- [ ] **Step 5: Commit** any fmt/lint fixups.

---

## Self-review notes

- **Spec coverage:** Link (Task 1,3,7) ✓; Split (Task 2,3,6) ✓; auto-group +
  prompt (Task 4) ✓; privacy copy on split (Task 2) ✓; alerts repoint (Task 1,2)
  ✓; flight+train scope via `linkableType` ✓; same-trip/same-type guards ✓.
- **Type consistency:** `LinkPlans(primaryID, absorbIDs)`, `SplitPlanPart(partID)
  → (newPlanID, parentPlanID, err)`, `ErrNotSplittable`, `resequencePartsTx`,
  `groupByConfirmationRef`, web `linkPlans(primaryId, planIds)` /
  `splitPlanPart(partId)` are used consistently across tasks.
- **No schema migration** — confirmed by the design's model facts.
