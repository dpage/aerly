# Aerly

[![CI](https://github.com/dpage/aerly/actions/workflows/ci.yml/badge.svg)](https://github.com/dpage/aerly/actions/workflows/ci.yml)

Single-binary Go + React app that tracks your friends' flights on a live world map.
Built for the small ritual of "who's already in the air to PostgreSQL Conference Europe?"

- **Backend**: Go 1.26, `net/http`, `pgx/v5`, GitHub OAuth, Server-Sent Events.
- **Frontend**: Vite + React 18 + TypeScript + MUI + Zustand + MapLibre GL.
- **Data sources** (see [APIs.md](APIs.md) for full comparison and alternatives):
  - [OpenSky Network](https://opensky-network.org/) for live ADS-B positions (free for non-commercial use; rate-limited).
  - [AeroDataBox](https://rapidapi.com/aedbx-aedbx/api/aerodatabox/) on RapidAPI for schedule + airport + airframe lookups (cheap pay-per-call).
  - An in-memory **stub** that interpolates positions along a great-circle when nothing is configured — useful for demos with no external dependencies.
  - A **dead-reckoner** wraps whichever tracker is in use, extrapolating from the last real fix toward the destination when ADS-B coverage drops out (oceanic gaps, etc.). Estimated positions are flagged so the UI renders them with reduced opacity and a dashed outline.
- **Deploy**: one statically-linked binary with the SPA embedded via `//go:embed`,
  fronted by nginx + Let's Encrypt.

## Quickstart (local development)

Prerequisites: Go ≥ 1.26, Node ≥ 20, a running PostgreSQL with a database for the app.

```bash
# 1. Create a Postgres database.
createdb aerly

# 2. Configure environment.
cp .env.example .env
# Edit .env — fill in DATABASE_URL, GITHUB_CLIENT_ID/SECRET, SESSION_KEY.
# Leave OPENSKY_USERNAME / AERODATABOX_RAPIDAPI_KEY blank for the stub backends.

# 3. Register a GitHub OAuth app at https://github.com/settings/developers
#    Homepage URL:           http://localhost:8080
#    Authorization callback: http://localhost:8080/auth/github/callback

# 4. Build everything and run.
make build
make run
# Browse to http://localhost:8080
```

`make dev` runs the Go server on `:8080` and the Vite dev server on `:5173` with a proxy for `/api`, `/auth`, `/healthz`, for frontend hot-reload.

The first GitHub user to sign in is automatically marked a superuser. They can then invite others by GitHub login from the **Manage users** dialog in the top bar.

The friend-request email includes a one-click "Accept" button signed
with a 7-day token; an avatar badge surfaces pending requests
in-app even when email is not configured.

## Configuration

All configuration is via environment variables (see `.env.example`).

| Variable                   | Required | Default                       | Notes                                                                                  |
|----------------------------|----------|-------------------------------|----------------------------------------------------------------------------------------|
| `LISTEN_ADDR`              |          | `:8080`                       |                                                                                        |
| `PUBLIC_URL`               |          | `http://localhost:8080`       | Used for the OAuth callback URL.                                                       |
| `DATABASE_URL`             | yes      |                               | Standard libpq URL.                                                                    |
| `GITHUB_CLIENT_ID`         | yes¹     |                               | From the GitHub OAuth app.                                                             |
| `GITHUB_CLIENT_SECRET`     | yes¹     |                               | From the GitHub OAuth app.                                                             |
| `GOOGLE_CLIENT_ID`         | yes¹     |                               | From the Google OAuth app. GitHub and Google are independent providers — configure either or both. |
| `GOOGLE_CLIENT_SECRET`     | yes¹     |                               | From the Google OAuth app.                                                             |
| `SESSION_KEY`              | yes      |                               | ≥ 32 random chars. `openssl rand -base64 48`.                                          |
| `MAIL_FROM_ADDRESS`        |          |                               | Envelope/From for outbound mail (friend invites, account-link notices). When unset, those emails are skipped. |
| `MAIL_SENDMAIL_PATH`       |          | `/usr/sbin/sendmail`          | Path to a sendmail-compatible binary used to send mail.                                |
| `POLL_INTERVAL`            |          | `60s`                         | How often the poller refreshes active flights. Non-Enroute flights are throttled to 5×. |
| `OPENSKY_USERNAME`         |          |                               | OpenSky account for HTTP Basic Auth. Unlocks higher rate limits than anonymous.        |
| `OPENSKY_PASSWORD`         |          |                               |                                                                                        |
| `OPENSKY_ENABLED`          |          | `0`                           | Set to `1` to use OpenSky anonymously (heavily rate-limited).                          |
| `AERODATABOX_RAPIDAPI_KEY` |          |                               | When set, the Add Flight dialog drops to its minimal "ident + date" form.              |
| `DEV_AUTH_BYPASS`          |          | `0`                           | Local-only: `1` enables `/auth/dev-login?login=…` to skip OAuth. Refuses non-localhost.|

¹ At least one OAuth provider (GitHub or Google) must be fully configured, unless `DEV_AUTH_BYPASS=1`. Each provider needs both its ID and secret, or neither.

Database migrations are applied automatically on every startup from the embedded `migrations/` directory.

## Email-forwarded flight ingest (optional)

When enabled, users can add flights by forwarding the confirmation email
from their airline or travel agent to a configured address. The server
extracts the flight(s) with an LLM and creates them automatically. v1
matches the forwarder by the email GitHub reports as their **primary,
verified** address on sign-in.

### How it works

1. Postfix on the host delivers `flights@<your host>` to a Maildir owned
   by the service account.
2. The server watches the Maildir; for each new message it:
   - Looks up the sender by `From:` against the user's verified email
     addresses (the one fetched from GitHub OAuth at sign-in).
   - Requires a DKIM pass on the sender's domain (configurable).
   - Sends the body and HTML to the configured LLM with a strict JSON
     schema asking for `{ident, date}` per leg plus the schedule
     details (origin/dest IATA, departure/arrival local date+time) when
     the email contains them. Any attached PDFs are passed as native
     document blocks (provider must support documents — see "PDF
     attachments" below).
   - Resolves each leg via the existing flight resolver and creates the
     flight with the forwarder as the sole passenger. If the resolver
     reports the flight isn't in the airline's schedule yet *and* the
     LLM extracted enough schedule detail from the email itself, the
     flight is added from the email's own data and the reply asks the
     user to double-check the times in the app.
3. The server replies with a summary of what was added or skipped. If the
   sender isn't recognised or DKIM fails, the message is moved to a
   `.failed/` Maildir subdirectory and **no reply is sent** (we don't
   want to confirm to a potential spoofer that the address is live).

### Host setup

Postfix:

```text
# /etc/aliases (or your virtual map equivalent)
flights: aerly
# Then:
newaliases
```

The `aerly` local user must own a Maildir at the configured
path. opendkim (or equivalent) must be in postfix's `smtpd_milters`
chain and must stamp `Authentication-Results:` headers on inbound mail,
otherwise `EMAIL_INGEST_REQUIRE_DKIM=1` will reject every message.

### Configuration

| Variable | Default | Notes |
|---|---|---|
| `EMAIL_INGEST_ENABLED` | `0` | Master switch. |
| `EMAIL_INGEST_MAILDIR` | (required if enabled) | Maildir path. |
| `EMAIL_INGEST_ADDRESS` | (required if enabled) | Address users forward to. |
| `EMAIL_INGEST_POLL_INTERVAL` | `30s` | Maildir scan cadence. |
| `EMAIL_INGEST_REQUIRE_DKIM` | `1` | Require DKIM pass for the sender's domain. |
| `EMAIL_INGEST_MAX_BODY_BYTES` | `1048576` | Truncation guard before LLM call. |
| `EMAIL_INGEST_SENDMAIL` | `/usr/sbin/sendmail` | Path used for reply mail. |
| `LLM_PROVIDER` | `anthropic` | One of `anthropic` / `openai` / `gemini` / `ollama`. |
| `LLM_MODEL` | `claude-haiku-4-5` | Model ID. |
| `LLM_API_KEY` | (required unless `ollama`) | Provider API key. |

`EMAIL_INGEST_ENABLED=1` requires `AERODATABOX_RAPIDAPI_KEY` (the
resolver is what turns `{ident, date}` into a full flight record).

### PDF attachments

When an email has PDF attachments, they're passed natively to the LLM as
document content blocks (no Go-side text extraction), so image-only
ticket PDFs work as well as text-layer ones.

This currently requires `LLM_PROVIDER` to be one that supports document
blocks: `anthropic` or `gemini`. If you set `LLM_PROVIDER=openai` or
`ollama`, the provider rejects the document blocks and `RealLLM.Complete`
silently retries text-only — the visible body still gets parsed, but
attached PDFs are skipped.

### Limitations

- The sender's address is matched against any **verified** email on
  the user's account. The GitHub primary is added on sign-in; users can
  add and verify additional addresses (click-through link, 24h expiry)
  from the avatar menu's **Email addresses** dialog. Both the menu
  entry and the underlying `/api/me/emails/*` routes are gated on
  `EMAIL_INGEST_ENABLED=1` because they share the sendmail pipe.
- The `.failed/` directory accumulates poisonous messages; the operator
  decides when to inspect and delete.

## Tracker and resolver modes

The tracker decides where the poller gets a position for each flight; the resolver fills in the rest of a flight's metadata at creation time.

- **Tracker — stub (default)**: synthesises a position from the schedule and the embedded IATA table. No external calls. Good enough for "the plane should be roughly here" demos.
- **Tracker — OpenSky**: real ADS-B fixes keyed on the airframe's `icao24` (lowercase Mode-S hex). Requires the user (or the resolver) to record an `icao24` against the flight. Falls back to the dead-reckoner whenever OpenSky has no fresh fix.
- **Resolver — none**: the Add Flight dialog shows the full manual form (every field).
- **Resolver — AeroDataBox**: one RapidAPI call per `POST /api/flights/resolve {ident, date}` returns the full schedule, both airports with coordinates, and the `icao24`. The Add Flight dialog becomes "ident + departure date" and everything else is filled in for you.

Mix and match as you like — e.g. AeroDataBox to autofill + stub for positions during development, then OpenSky once you want real tracking.

## Data sources and limitations

> For a full comparison of current and alternative APIs — including unified APIs that handle
> both scheduling and positioning — see [APIs.md](APIs.md).

### OpenSky Network — live position tracking

[OpenSky](https://opensky-network.org/) is a crowdsourced network of volunteer-operated,
ground-based ADS-B receivers. Aircraft broadcast their position, altitude, and velocity via
an ADS-B OUT transponder; any receiver within radio range picks up the signal and uploads it
to OpenSky's servers.

**Limitations:**

- **Ground receiver coverage required.** If there is no receiver within roughly 200–400 km
  (line-of-sight) of the aircraft, no position is available. This means:
  - Oceanic routes (North Atlantic, Pacific, polar) will typically have no live fix for hours
    at a time.
  - Remote or sparsely populated continental areas may have gaps too.
  - Low-altitude departures and arrivals (below ~2 000 ft AGL) may disappear momentarily
    while still in a receiver's shadow.
- **ADS-B OUT required.** Older aircraft using only Mode-C or Mode-S transponders do not
  broadcast position data and will not appear at all. ADS-B equipage is now mandatory in
  most controlled airspace, but some charter, cargo, and older regional aircraft may be
  exempt.
- **Rate limits.** Anonymous access is heavily rate-limited by OpenSky. An authenticated
  account (`OPENSKY_USERNAME` / `OPENSKY_PASSWORD`) raises the limit but is still a shared,
  free service — Aerly backs off automatically on `429` responses.
- **Dead-reckoner fallback.** When a live fix is unavailable (coverage gap, rate-limited
  response, or aircraft not ADS-B-equipped), the dead-reckoner extrapolates from the last
  known fix toward the destination along a great-circle. Estimated positions are flagged and
  rendered with reduced opacity and a dashed outline in the UI.

### AeroDataBox via RapidAPI — flight schedule and metadata

[AeroDataBox](https://rapidapi.com/aedbx-aedbx/api/aerodatabox/) provides scheduled
departure/arrival times, origin/destination airports with coordinates, aircraft type, and
the ICAO24 (Mode-S hex) address needed to track a flight on OpenSky.

**Limitations:**

- **Schedule availability.** Flights are often not in AeroDataBox's database until a day or
  two before departure (sometimes less). Resolving a flight booked far in advance may return
  no results; try again closer to the travel date.
- **Pay-per-call.** Each `/api/flights/resolve` call consumes one RapidAPI credit.
  Aerly caches successful responses for 24 hours to avoid duplicate charges.
- **ICAO24 accuracy.** The aircraft assigned to a flight can change (equipment swap, wet
  lease, last-minute sub). AeroDataBox reports the currently scheduled airframe, which may
  differ from the one that actually operates. If OpenSky's live positions seem wrong, the
  ICAO24 field can be corrected manually in the flight edit form.

## Architecture

```
cmd/server/          Entrypoint: config, DB pool, migrations, HTTP server, poller goroutine.
internal/
├── config/          Env-var parsing and validation.
├── db/              pgx pool + embedded-SQL migrator.
├── store/           Typed pgx queries for trips, plans, parts, users, positions.
├── api/             Shared JSON DTOs (used by both handlers and poller).
├── auth/            GitHub + Google OAuth, HMAC-signed session cookies, middleware.
├── airports/        Embedded IATA → (lat, lon) table.
├── geo/             Great-circle helpers (slerp, bearing, haversine).
├── geocode/         Nominatim geocoding + timezone anchoring for plan venues.
├── geotz/           IANA timezone lookup from coordinates (tzf).
├── providers/       External flight-data integrations: Tracker (Stub, OpenSky)
│                    + Resolver (AeroDataBox) + DeadReckoner wrapper.
├── tripitics/       TripIt .ics parsing into plans/parts.
├── emailingest/     Forwarded-email Maildir drain + LLM extraction + auto-reply.
├── planops/         Plan propose/commit pipeline incl. rebooking supersession.
├── mailer/          Outbound multipart email assembly (alerts, invites, replies).
├── notify/          Visibility-scoped trip/plan SSE event publishing.
├── poller/          Background goroutine: refresh active flights, persist, broadcast.
├── sse/             Server-Sent Events broadcast hub.
└── handlers/        JSON API endpoints and SPA fallback handler.

migrations/          0001_init.up.sql, .down.sql, etc. Embedded into the Go binary.
web/                 Vite + React SPA. After `npm run build`, web/dist is embedded too.
deploy/              Example systemd unit and nginx config for the Hetzner host.
```

## Deployment (Hetzner / single VM)

The Go binary embeds the SPA and runs the poller in the same process, so deployment is a single file plus a systemd unit.

```bash
# On the dev machine:
GOOS=linux GOARCH=amd64 make build
scp bin/aerly  user@host:/opt/aerly/aerly
scp deploy/aerly.service user@host:/etc/systemd/system/aerly.service
# Create /etc/aerly.env with the env vars from .env.example.

# On the host:
systemctl daemon-reload
systemctl enable --now aerly
```

Then drop `deploy/nginx.conf.example` into `/etc/nginx/sites-available/`, adjust the hostname, symlink into `sites-enabled`, and reload nginx. The SSE endpoint needs `proxy_buffering off` — that block is already in the example.

## Project status

Pre-release. Tracker and resolver paths are working end-to-end with OpenSky and AeroDataBox. The codebase is well covered by tests: a Go suite (`make test-go`, ~75% statement coverage with a Postgres-backed integration layer) and a Vitest suite for the SPA (`make test-web`) gated at 90% per-file coverage (`make cover-web`).

## Roadmap / follow-up work

**Open**
- **Flight data: migrate to FlightAware AeroAPI.** Evaluate replacing AeroDataBox with AeroAPI for richer, fresher gate/terminal and status data. AeroAPI is metered per-query (cost-sensitive at poll cadence — lean on `last_resolved_at` throttling) and coverage varies, so spike against real routes first. Slots in behind the existing `providers.Resolver` interface as a sibling implementation; AeroDataBox stays the default until then.

**Shipped (trip-planning follow-ups)**
- Gate-change alerts — gate/terminal parsed from AeroDataBox onto `flight_details`, always-alert branch with dedupe in the poller.
- Binary / PDF upload — multipart transport into the LLM extractor.
- Single-flight track on the tracker — focused view returns the flown track.
- Backend SSE for trip/plan edits — `trip.updated`/`plan.updated`/`plan.deleted` emitted on mutations so shared timelines update live.
- Per-plan alert opt-in exposed as `alert_opted_in` on `PlanDTO`.
- Per-resource calendar token granularity — each trip/plan feed independently revocable.
- iCal DST — VTIMEZONE blocks now carry DAYLIGHT/STANDARD `RRULE` transitions.
- Alerts inbox — `alert.created` SSE events drive an in-app inbox and toast, with a combined alert/friend avatar badge and a live unread count.
- Passenger trips — trips you're a passenger on (not just own) appear under **My trips**, badged to tell them apart from trips you own.
- Trip-level passengers — add a friend as a **Passenger** in the Share-trip dialog and they become a passenger on every plan (existing and future): they see all non-hidden plans, appear in each plan's passenger list and alerts, and the trip lands under their My trips.
- Upcoming-plan reminders — opt in at the **trip** level (with a configurable lead time in hours) to be reminded before every plan you can see; override per plan to change the lead time or to opt a single plan in or out. Reminders arrive by email and in the in-app inbox, fired per plan-part by the poller. They're independent of the gate/delay/cancellation alerts, which always fire when the API reports a change.

## Licence

PostgreSQL License — see [LICENSE](LICENSE).

## Author

Dave Page
