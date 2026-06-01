# Handoff: end-to-end browser testing of the trip-planning redesign

**Date:** 2026-05-30
**For:** a fresh Claude session on a desktop with Chrome + Playwright
**Repo state:** everything below is on `main` (and branch
`claude/trip-planning-core-redesign-bcUYe`), commit `e3960a4` or later.

---

## 0. TL;DR for the new session

The trip-planning redesign (Aerly → trips-first planner) and all its follow-ups
except the AeroAPI migration are **implemented, merged to `main`, and gated**
(Go build/vet + full DB-backed suite over migrations 0010–0015; ~352 web tests;
0 lint errors). What has **not** been done: driving the **rendered** app in a
real browser. The cloud sandbox had no browser, so UI verification so far is the
component-test suite + an API-level curl smoke test only.

**Your job:** run the app locally and drive it in a real browser with Playwright
— including the **WebKit** engine to validate the Safari outlined-input label
fix — capture screenshots, and report anything broken.

Read `docs/prd/`, `docs/spec/`, and `docs/plan/` (the 2026-05-28/29 files) for
product + design context, and the README "Roadmap / follow-up work" section.

---

## 1. What to verify (the gap)

Everything is green at the test/API level. The unverified surface is **real
browser rendering + interaction**:

1. The app loads, the trip list renders, routing works (react-router).
2. The core journeys actually work clicking through, not just via curl.
3. **Safari rendering of outlined inputs** — see §5. This is the one thing that
   is genuinely unverified anywhere and is the reason this handoff exists.

---

## 2. Launch the app locally

Prereqs: Go 1.26+, Node, and a local PostgreSQL.

```bash
# 1. Postgres: a role + database the app can use. Example (adjust to taste):
#    createuser aerly --pwprompt   # password: aerly  (or set your own)
#    createdb -O aerly aerly
#    The app runs all migrations (0001–0015) on startup.

# 2. .env in the repo root (NOT committed; .env is gitignored):
cat > .env <<'EOF'
LISTEN_ADDR=:8080
PUBLIC_URL=http://localhost:8080
DATABASE_URL=postgres://aerly:aerly@127.0.0.1:5432/aerly?sslmode=disable
SESSION_KEY=dev-smoke-test-session-key-0123456789abcdef
DEV_AUTH_BYPASS=1
POLL_INTERVAL=30s
EOF

# 3. Build the SPA (embedded by the Go server) + the binary, then run:
make build        # build-web (vite) + build-go → bin/aerly
make run          # serves the SPA + API on http://localhost:8080
# (or `make dev` for vite hot-reload on a separate port proxying the API)
```

The single-origin built server on `:8080` is the simplest target for Playwright.

**Optional keys** (most flows work without them):
- `AERODATABOX_RAPIDAPI_KEY` — enables flight ident+date *lookup*
  (`/api/flights/resolve`) and gate/terminal data. Without it, lookup returns
  `501`; you can still add a flight by typing its fields manually.
- `LLM_PROVIDER` / `LLM_MODEL` / `LLM_API_KEY` — enables paste/upload **ingest**
  extraction. Without them, ingest returns `503` (graceful). Needed to e2e the
  paste/upload → confirm flow.
- `OPENSKY_*` — live positions; otherwise a stub tracker (no live tracks).

## 3. Auth in dev (no GitHub OAuth)

`DEV_AUTH_BYPASS=1` exposes `GET /auth/dev-login?login=<name>` which fabricates a
session and sets the cookie (first-ever user becomes superuser). In Playwright,
navigate to it before driving the SPA:

```js
await page.goto('http://localhost:8080/auth/dev-login?login=alice'); // sets cookie, redirects to /
// add a second user for sharing tests: ?login=bob
```

There's also `GET /auth/dev-info` (the login page probes it to show the dev form).

## 4. Core journeys to drive (and screenshot)

1. **Trip list (home `/`)** — empty state, then "New trip" → create → it appears
   grouped under Upcoming/Happening now/Past.
2. **Add plans** (`Add to trip` dialog, four tabs):
   - **Manual** — a hotel (check-in/out dates) and a flight. Flight uses the
     ident+date lookup if a resolver key is set; otherwise fill fields manually.
   - **Paste / Upload** — needs an LLM key; verify the confirm step (proposed
     plans, low-confidence flags, supersede-vs-add) then commit.
3. **Timeline** (`/trips/:id`) — day-grouped, local-time headers; a multi-night
   hotel renders as a **band**; parts of one booking are **visually linked**;
   superseded/cancelled parts greyed.
4. **Map tab** (`/trips/:id/map`) — MapLibre plots geocoded parts.
5. **Tracker** (`/tracker`) — convergence map + −/+ day window sliders + tag
   selector; deep-link a flight via `/tracker?part=<id>` for the single-flight
   focus (it now fetches the track; empty under the stub tracker).
6. **Sharing** — the trip "Share" dialog: add a member as editor/viewer; the
   per-plan "Who can see this?" control (everyone / hidden-from / only-visible-to);
   passenger picker. Verify with the second dev user (`bob`) what they can/can't see.
7. **Calendar** — "Subscribe" (trip) and the account-menu "Subscribe to
   calendar…" (personal): issue/copy/regenerate tokens; fetch a `.ics` URL.
8. **Alerts** — account-menu "Alert preferences…" (channels + threshold); the
   per-plan "notify me of changes" toggle.
9. **Live SSE** — open the same trip in two browser contexts (alice + a shared
   viewer); edit a plan in one and confirm it updates live in the other
   (`plan.updated` / `trip.updated`).

Watch the **console + network** tabs for errors, and for any call to the retired
`/api/flights` CRUD (there should be none — only `/api/flights/resolve` survives).

## 5. Safari outlined-input label check (the headline item)

Aerly carries a deliberate workaround for a **Safari**-only bug where the MUI
outlined-input notch renders unreliably (the focused border draws *through* the
shrunk label). The fix is a global theme override in `web/src/theme.ts`
(`MuiOutlinedInput` legend `width:0` + a solid-background painted `MuiInputLabel`).
It has only ever been reasoned about, never seen rendering in Safari in this
project's CI.

**Playwright bundles WebKit — the same engine as Safari.** Use it:

```bash
npx playwright install webkit
```

```js
// playwright with { browserName: 'webkit' }
// For each dialog with inputs (New trip, Add to trip → Manual per type,
// Share, Alert preferences, Calendar subscribe, the tag input):
//  - focus a field so its label shrinks to the top border
//  - screenshot the input
//  - confirm: NO border line drawn through the label text, NO overlapping
//    text, label sits cleanly on the border in BOTH light and dark themes
//    (theme toggle is in the account menu).
```

Compare WebKit screenshots against Chromium for the same inputs. The bug, if the
hack regressed, shows only in WebKit. Pay special attention to any input that
sits on `background.default` rather than a Dialog's `paper` surface (the painted
label background matches `paper`, so a non-paper surface could show a faint seam)
— in practice all the new text inputs live inside Dialogs, but verify the
**tag input on the trip-detail header** specifically.

## 6. Known caveats / not-yet-verified

- **Gate-change alerts** (`flight_details.origin_gate/dest_gate`, poller
  always-alert branch) are unit-tested only — live verification needs a real
  AeroDataBox key returning gate data.
- Flight **lookup** and **ingest extraction** need the optional keys (§2).
- The stub tracker produces no live positions, so tracker maps/tracks render
  empty without OpenSky.
- Unknown `/api/*` paths return the SPA HTML (`200`) rather than a JSON `404`
  — pre-existing catch-all behaviour, not a regression; flag if it bothers you.

## 7. Quick data seed (optional, via API)

If you'd rather seed without clicking (e.g. to get screenshots fast):

```bash
J=/tmp/cj.txt; B=http://localhost:8080
curl -s -c $J "$B/auth/dev-login?login=alice" -o /dev/null
TID=$(curl -s -b $J -X POST "$B/api/trips" -H 'Content-Type: application/json' \
  -d '{"name":"Lisbon, October","destination":"Lisbon","starts_on":"2026-10-20","ends_on":"2026-10-24"}' \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
curl -s -b $J -X POST "$B/api/trips/$TID/plans" -H 'Content-Type: application/json' \
  -d '{"type":"hotel","title":"Hotel Lisboa","parts":[{"type":"hotel","starts_at":"2026-10-20T15:00:00Z","ends_at":"2026-10-24T11:00:00Z","start_label":"Hotel Lisboa","start_tz":"Europe/Lisbon","hotel":{"property_name":"Hotel Lisboa","room_type":"Double"}}]}'
curl -s -b $J -X POST "$B/api/trips/$TID/plans" -H 'Content-Type: application/json' \
  -d '{"type":"flight","title":"BA286","parts":[{"type":"flight","starts_at":"2026-10-20T07:00:00Z","ends_at":"2026-10-20T10:00:00Z","start_label":"LHR","end_label":"LIS","start_tz":"Europe/London","end_tz":"Europe/Lisbon","flight":{"ident":"BA286","scheduled_out":"2026-10-20T07:00:00Z","scheduled_in":"2026-10-20T10:00:00Z","origin_iata":"LHR","dest_iata":"LIS"}}]}'
```

## 8. Repo map (where things live)

- Docs: `docs/prd/2026-05-28-*`, `docs/spec/2026-05-29-*`, `docs/plan/2026-05-29-*`.
- Backend: `internal/store` (trips/plans/parts/tags/calendar/alerts/tracker +
  the `*_details` satellites), `internal/handlers` (per-area `handlers_*.go` +
  `publish.go` for SSE), `internal/planops` (ingest/rebooking/hotel-times),
  `internal/poller` (tracker re-key + alerts), `migrations/0010–0015`.
- Frontend: `web/src/pages` (TripList, TripDetail, TripTimeline, TripMap,
  Tracker), `web/src/components` (AddToTripDialog, TripMembersDialog,
  PlanPrivacyDialog, AlertPrefsDialog, CalendarSubscribeDialog, TagInput,
  TrackerMap, PlanTypeIcon), `web/src/state/*Slice.ts`, `web/src/api/{types,client}.ts`,
  `web/src/theme.ts` (the Safari hack).

## 9. After testing

Report findings; for any real bug, the reproduce-first bug-fix flow is a good
fit. The only open roadmap item is the **FlightAware AeroAPI migration**
(AeroDataBox stays default) — separate from this e2e pass.
