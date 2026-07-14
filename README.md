# Aerly

[![CI](https://github.com/dpage/aerly/actions/workflows/ci.yml/badge.svg)](https://github.com/dpage/aerly/actions/workflows/ci.yml)

Single-binary Go + React app for planning shared trips and tracking your friends' flights on a
live world map. It gathers a trip's flights, hotels, trains, taxis, meals and excursions onto one
shared timeline and map, with live flight tracking, nearby-places discovery, calendar/PDF export,
and push notifications. Built for the small ritual of "who's already in the air to PostgreSQL
Conference Europe?"

- **Backend**: Go 1.26, `net/http`, `pgx/v5`, GitHub + Google OAuth, Server-Sent Events.
- **Frontend**: Vite + React 18 + TypeScript + MUI + Zustand + MapLibre GL, packaged as an
  installable PWA.
- **Data sources** (see [APIs.md](APIs.md) for full detail, comparison, and alternatives):
  - [OpenSky Network](https://opensky-network.org/) for live ADS-B positions (free for non-commercial use; rate-limited).
  - [AeroDataBox](https://rapidapi.com/aedbx-aedbx/api/aerodatabox/) on RapidAPI for schedule + airport + airframe lookups (cheap pay-per-call).
  - [Geoapify Places](https://www.geoapify.com/places-api/) for the Explore nearby-points-of-interest feature (OpenStreetMap-derived; free tier).
  - [OpenStreetMap Nominatim](https://nominatim.org/) for geocoding plan venues from their addresses (keyless).
  - An **LLM** (Anthropic / OpenAI / Gemini / Ollama) that extracts bookings from pasted text, uploaded PDFs, and forwarded emails.
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

Configuration is via environment variables (see `.env.example`), optionally
backed by a YAML config file.

### Config file

Pass `--config /path/to/aerly.yaml` to the server to read settings from a
YAML file. Its keys are the same environment-variable names listed below, so
the file is a drop-in alternative to setting them in the environment:

```yaml
# /etc/aerly/aerly.yaml
LISTEN_ADDR: ":8080"
DATABASE_URL: "postgres://aerly@localhost/aerly"
SESSION_KEY: "…at least 32 random chars…"
GITHUB_CLIENT_ID: "…"
GITHUB_CLIENT_SECRET: "…"
DEV_AUTH_BYPASS: false   # booleans map to the 0/1 the loader expects
POLL_INTERVAL: 60s
```

Environment variables (and a `.env`) **override** file values, so the file
supplies defaults that a deployment can still tweak via the environment.

Because the file typically holds secrets (`DATABASE_URL`, OAuth client
secrets, `SESSION_KEY`), it **must** have `0400` permissions
(`chmod 400 aerly.yaml`) — read-only by its owner. The server refuses to
start if the file is any more permissive.

### Environment variables

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
| `MAIL_FROM_ADDRESS`        |          |                               | Envelope/From for outbound mail (friend invites, account-link notices, admin quota alerts). When unset, those emails are skipped. |
| `MAIL_SENDMAIL_PATH`       |          | `/usr/sbin/sendmail`          | Path to a sendmail-compatible binary used to send mail.                                |
| `POLL_INTERVAL`            |          | `60s`                         | How often the poller refreshes active flights. Non-Enroute flights are throttled to 5×. |
| `OPENSKY_USERNAME`         |          |                               | OpenSky account for HTTP Basic Auth. Unlocks higher rate limits than anonymous.        |
| `OPENSKY_PASSWORD`         |          |                               |                                                                                        |
| `OPENSKY_ENABLED`          |          | `0`                           | Set to `1` to use OpenSky anonymously (heavily rate-limited).                          |
| `AERODATABOX_RAPIDAPI_KEY` |          |                               | When set, the Add Flight dialog drops to its minimal "ident + date" form. See **Tracker and resolver modes**. |
| `GEOAPIFY_API_KEY`         |          |                               | Enables the Explore nearby-points-of-interest feature. Blank = off (the Explore tab, the "Explore nearby" button, and the preference to hide Explore are all withdrawn). Free tier at [Geoapify](https://myprojects.geoapify.com/). |
| `DEV_AUTH_BYPASS`          |          | `0`                           | Local-only: `1` enables `/auth/dev-login?login=…` to skip OAuth. Refuses non-localhost.|
| `ATTACHMENTS_STORE`        |          |                               | Enables per-plan file attachments. Blank = off. Set to an absolute filesystem path, or an `s3://bucket[/prefix]` URL. See **Plan attachments** below. |

¹ At least one OAuth provider (GitHub or Google) must be fully configured, unless `DEV_AUTH_BYPASS=1`. Each provider needs both its ID and secret, or neither.

Four further optional features carry their own configuration, each documented in its own section below: **AI booking extraction** (`LLM_*`), **Web push notifications** (`WEBPUSH_VAPID_*`), **email-forwarded flight ingest** (`EMAIL_INGEST_*`), and **plan attachments** (`ATTACHMENTS_*`).

Database migrations are applied automatically on every startup from the embedded `migrations/` directory.

## AI booking extraction (optional)

The **New plan** dialog can capture a booking from unstructured input: paste a confirmation email or itinerary as text, or upload a booking PDF, and an LLM extracts the plan (flight, hotel, train, taxi, meal or excursion) for you to review and confirm. The same extractor powers the forwarded-email ingest below.

It's enabled by configuring an LLM. With none configured, the paste/upload propose endpoints (`POST /api/trips/{id}/ingest`) return `503` and the New plan dialog falls back to manual entry only; everything else works.

| Variable       | Default            | Notes                                                                              |
|----------------|--------------------|------------------------------------------------------------------------------------|
| `LLM_PROVIDER` | `anthropic`        | One of `anthropic` / `openai` / `gemini` / `ollama`.                               |
| `LLM_MODEL`    | `claude-haiku-4-5` | Model ID for the chosen provider.                                                  |
| `LLM_API_KEY`  | (required unless `ollama`) | Provider API key. The keyless local `ollama` provider needs no key.        |

PDF uploads (and forwarded PDF attachments) are passed to the model as native document blocks, which only `anthropic` and `gemini` support; with `openai` or `ollama` the extractor silently retries text-only, so the visible text is still parsed but attached PDFs are skipped. See [APIs.md](APIs.md) for provider notes.

## Web push notifications (optional)

Aerly can push flight-change alerts and trip-share notices to a user's device even when the app is closed, via the standard [Web Push](https://developer.mozilla.org/en-US/docs/Web/API/Push_API) / VAPID protocol — no Apple/Google developer account, app store, or certificates. Users opt in per-device from **Preferences → Push**.

Generate a VAPID key pair once and keep the private half in your secret store:

```bash
npx web-push generate-vapid-keys
```

Leave both keys blank to disable push end-to-end: the API reports it disabled, the sender no-ops, and the UI hides the enable-push toggle.

| Variable                    | Default      | Notes                                                                                 |
|-----------------------------|--------------|---------------------------------------------------------------------------------------|
| `WEBPUSH_VAPID_PUBLIC_KEY`  |              | Handed to browsers so they can subscribe. Set together with the private key.           |
| `WEBPUSH_VAPID_PRIVATE_KEY` |              | Signs each push; a **secret**. Set together with the public key.                       |
| `WEBPUSH_VAPID_SUBJECT`     | `PUBLIC_URL` | VAPID subject: a `mailto:` or `https:` URL identifying this deployment.                 |

On iOS, Web Push only works once Aerly is installed to the Home Screen (see **Install as an app** below).

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
   - Requires a DKIM pass aligned with the sender's domain (configurable).
     DKIM is the sole sender-authentication check: it survives forwarding
     because the signing domain aligns with the `From:` header.
   - Enforces a per-user rolling-24h ingestion cap (default 50) before
     the LLM runs, so a compromised account or prompt-injection can't
     drive unbounded LLM spend or fan plans into the database.
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
   sender isn't recognised, DKIM fails, or the sender is over their
   rate limit, the message is moved to a `.failed/` Maildir subdirectory
   and **no reply is sent** (we don't want to confirm to a potential
   spoofer that the address is live, or amplify a flood with bounces).

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
chain and must stamp `Authentication-Results:` headers (with a `dkim=`
result) on inbound mail, otherwise `EMAIL_INGEST_REQUIRE_DKIM=1` will
reject every message. The boundary MTA must also strip any inbound
`Authentication-Results:` headers and re-stamp its own, so a sender
can't forge a pass (this is what `EMAIL_INGEST_DKIM_AUTHSERV_ID` keys
trust on).

### Configuration

| Variable | Default | Notes |
|---|---|---|
| `EMAIL_INGEST_ENABLED` | `0` | Master switch. |
| `EMAIL_INGEST_MAILDIR` | (required if enabled) | Maildir path. |
| `EMAIL_INGEST_ADDRESS` | (required if enabled) | Address users forward to. |
| `EMAIL_INGEST_POLL_INTERVAL` | `30s` | Maildir scan cadence. |
| `EMAIL_INGEST_REQUIRE_DKIM` | `1` | Require a DKIM pass aligned with the sender's domain. |
| `EMAIL_INGEST_DKIM_AUTHSERV_ID` | (required if DKIM on) | The authserv-id your boundary MTA stamps on `Authentication-Results`. Only headers bearing it are trusted. |
| `EMAIL_INGEST_RATE_LIMIT_PER_DAY` | `50` | Max messages per verified user in a rolling 24h. `0` disables. |
| `EMAIL_INGEST_MAX_BODY_BYTES` | `1048576` (1 MiB) | Truncation guard before the LLM call. |
| `EMAIL_INGEST_MAX_ATTACHMENTS` | `5` | Max PDF attachments per message forwarded to the LLM. |
| `EMAIL_INGEST_MAX_ATTACH_BYTES` | `10485760` (10 MiB) | Max total attachment bytes per message. |
| `EMAIL_INGEST_SENDMAIL` | (`MAIL_SENDMAIL_PATH`) | Path used for reply mail; defaults to `MAIL_SENDMAIL_PATH`. |

The LLM extractor is configured separately (see **AI booking extraction** above). `EMAIL_INGEST_ENABLED=1` additionally requires both a configured LLM (`LLM_API_KEY`, or `LLM_PROVIDER=ollama`) and `AERODATABOX_RAPIDAPI_KEY` (the resolver is what turns `{ident, date}` into a full flight record); the server refuses to start otherwise.

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

## Plan attachments (optional)

Users can attach files — a PDF ticket, a booking confirmation, a scanned
voucher — to any plan (booking). The feature is gated entirely by server
configuration: with `ATTACHMENTS_STORE` unset the upload/download endpoints
report disabled and the UI hides the affordance.

The file bytes live **out-of-band** from the database in a configured object
store; the database holds only metadata (filename, size, content type) plus the
opaque storage key. Set `ATTACHMENTS_STORE` to one of:

- an **absolute filesystem path** (e.g. `/var/lib/aerly/attachments`) — blobs are
  written under it in a two-level sharded directory layout (`ab/cd/<uuid>`) so a
  single directory never has to hold millions of entries; or
- an **`s3://bucket[/prefix]` URL** — blobs become objects in an S3 (or any
  S3-compatible) bucket. The MinIO client is used under the hood, so AWS S3,
  MinIO, Cloudflare R2, etc. all work.

Access mirrors the plan's own authorization: anyone who can **edit** the plan's
trip can upload or remove attachments; anyone who can **view** the plan can
download them. Deleting an attachment (or its plan) sweeps the blob.

| Variable | Default | Notes |
|---|---|---|
| `ATTACHMENTS_STORE` | (off) | Absolute path or `s3://bucket[/prefix]`. Blank disables the feature. |
| `ATTACHMENTS_MAX_BYTES` | `26214400` (25 MiB) | Per-file upload cap. |
| `ATTACHMENTS_S3_ACCESS_KEY` | (required for s3) | S3 access key id. |
| `ATTACHMENTS_S3_SECRET_KEY` | (required for s3) | S3 secret access key. |
| `ATTACHMENTS_S3_REGION` | `us-east-1` | Bucket region. |
| `ATTACHMENTS_S3_ENDPOINT` | (AWS default) | Override host for S3-compatible stores (e.g. MinIO). No scheme. |
| `ATTACHMENTS_S3_USE_SSL` | `1` | `0` to talk plain HTTP to the endpoint. |

## Tracker and resolver modes

The tracker decides where the poller gets a position for each flight; the resolver fills in the rest of a flight's metadata at creation time.

- **Tracker — stub (default)**: synthesises a position from the schedule and the embedded IATA table. No external calls. Good enough for "the plane should be roughly here" demos.
- **Tracker — OpenSky**: real ADS-B fixes keyed on the airframe's `icao24` (lowercase Mode-S hex). Requires the user (or the resolver) to record an `icao24` against the flight. Falls back to the dead-reckoner whenever OpenSky has no fresh fix.
- **Resolver — none**: the Add Flight dialog shows the full manual form (every field).
- **Resolver — AeroDataBox**: one RapidAPI call per `POST /api/flights/resolve {ident, date}` returns the full schedule, both airports with coordinates, and the `icao24`. The Add Flight dialog becomes "ident + departure date" and everything else is filled in for you.

Mix and match as you like — e.g. AeroDataBox to autofill + stub for positions during development, then OpenSky once you want real tracking.

### Admin quota alerts

When an upstream data provider rejects a request because we've hit its rate
limit or quota (HTTP `429`) — OpenSky position polling, or AeroDataBox flight
lookups — Aerly emails the **admins** so they can raise the plan tier or widen
`POLL_INTERVAL`. "Admins" are superusers with a verified email address; the
alert is sent to each of them.

The alert fires at the source (the provider) rather than the poller, because
the dead-reckoner deliberately hides a tracker error to fall back to an
extrapolated position. It is self-throttled to at most one email per provider
per hour, so a sustained throttle doesn't bury the inbox. It is a no-op until
`MAIL_FROM_ADDRESS` is configured (like Aerly's other outbound-mail flows), and
needs no extra configuration.

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

### Geoapify Places — nearby points of interest

[Geoapify Places](https://www.geoapify.com/places-api/) backs the Explore feature: given a
place (the trip destination, geocoded via Nominatim) or a hotel's pinned coordinates, it returns
nearby sights, museums, landmarks, places of worship, parks, and food, which you can add to the
trip as excursions. It's a keyed service over an OpenStreetMap-derived dataset (ODbL, attributed
in the UI), with results cached for 7 days.

**Limitations:**

- **Region-dependent coverage.** POI density and metadata are excellent in UK and European city
  centres and thinner elsewhere.
- **Sparse descriptions.** OSM POIs often lack a description; Aerly shows the OSM `description`
  tag as a blurb where present and otherwise links out to Wikidata, Wikipedia, and a website.
- **Requires a key.** With `GEOAPIFY_API_KEY` unset there is no POI resolver, so Explore is
  withdrawn app-wide (an earlier keyless Overpass fallback was removed for reliability — see
  [APIs.md](APIs.md)).

### Geocoding and booking extraction

Two more upstreams fill in the rest of a trip:

- **[Nominatim](https://nominatim.org/)** (OpenStreetMap, keyless) geocodes a plan's address to
  coordinates so hotels, restaurants and transfers plot on the map, and anchors each venue to its
  IANA timezone. Aerly sends a descriptive `User-Agent` per OSM policy and rate-limits itself.
- **An LLM** (see **AI booking extraction**) parses pasted text, uploaded PDFs and forwarded
  emails into structured plans. See [APIs.md](APIs.md) for the provider trade-offs.

## Install as an app (PWA)

Aerly is a Progressive Web App, so it installs to the home screen on both iOS
and Android with no app store — the same bundle the browser serves, embedded in
the Go binary.

- **Install.** Aerly shows an in-app install affordance: on Android/desktop
  Chromium it captures the browser's install event and offers an **Install**
  button; on iOS (which has no install API) it shows a one-time hint to use
  Safari's **Share → "Add to Home Screen"**. The browser's own install UI
  (address-bar icon / menu) works too. It then launches full-screen like a
  native app.
- **Offline (view).** A service worker (generated by
  [`vite-plugin-pwa`](https://vite-pwa-org.netlify.app/) with Workbox) precaches
  the app shell and runtime-caches your itinerary data (`GET /api/*`, network-first)
  plus recently viewed map tiles. Open the app on a plane with no Wi-Fi and your
  trips, timelines, and last-seen maps are still there. You need to have loaded
  it online at least once first, and creating/editing still needs a connection.
  When the network returns the SSE stream reconnects and the caches refresh on
  their own — an "offline" banner shows while you're disconnected. On logout the
  account-scoped API cache is cleared, so a shared device won't reveal the
  previous user's trips offline.
- **Auto-update.** Just like in the browser: when a new build is deployed the
  service worker fetches it in the background and the existing "A new version of
  Aerly is available — Refresh" prompt activates it. This keeps working when
  installed to the home screen, and stays within Apple/Google rules because
  Aerly is plain web content (and would remain compliant under guideline 4.7 if
  later wrapped for the stores).

The live SSE stream (`/api/events`) and the `/auth` and `/healthz` endpoints are
deliberately excluded from caching. The server serves `sw.js` with `no-cache` so
new builds are picked up promptly (`internal/handlers/spa.go`).

App-store listings (a Capacitor wrapper for the App Store, a Trusted Web
Activity for the Play Store) can be layered on top of this PWA later; the PWA is
the shared foundation for both.

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
├── maps/            Extracts coordinates from Google Maps URLs (short-link expansion, host-allowlisted).
├── providers/       External integrations: Tracker (Stub, OpenSky) + Resolver
│                    (AeroDataBox) + DeadReckoner wrapper + POI resolver (Geoapify).
├── importics/       TripIt & Kayak .ics parsing into plans/parts.
├── feeds/           SSRF-guarded fetch + refresh of trip iCal feed subscriptions.
├── emailingest/     Forwarded-email Maildir drain + LLM extraction + auto-reply.
├── planops/         Plan propose/commit pipeline incl. rebooking supersession.
├── mailer/          Outbound multipart email assembly (alerts, invites, replies).
├── push/            Web Push (VAPID) delivery to subscribed devices.
├── attachments/     Out-of-band blob store for plan attachments (filesystem or S3).
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

The Go integration tests create and drop a fresh database per test via a maintenance connection. Point that at your Postgres with `TEST_DATABASE_URL` (default `postgres://aerly:aerly@127.0.0.1:5432/postgres?sslmode=disable`); when Postgres is unreachable those tests **skip** so `go test ./...` stays green, unless you set `AERLY_REQUIRE_DB=1` (as CI does) to make them fail instead.

## Roadmap / follow-up work

**Open**
- **Flight data: migrate to FlightAware AeroAPI.** Evaluate replacing AeroDataBox with AeroAPI for richer, fresher gate/terminal and status data. AeroAPI is metered per-query (cost-sensitive at poll cadence — lean on `last_resolved_at` throttling) and coverage varies, so spike against real routes first. Slots in behind the existing `providers.Resolver` interface as a sibling implementation; AeroDataBox stays the default until then.

**Shipped (trip-planning follow-ups)**
- Explore nearby — the trip **Explore** tab and a hotel-tile **Explore nearby** button surface nearby sights, museums, landmarks, places of worship, parks and food from Geoapify (OSM-derived, cached), addable to the trip as excursions and shown on an in-app mini-map. Gated on `GEOAPIFY_API_KEY`; hideable per-user in **Preferences → Features**.
- Web push notifications — flight-change alerts and trip shares pushed to a device via the Web Push / VAPID protocol, opt-in per-device in **Preferences → Push**. Gated on `WEBPUSH_VAPID_*`.
- AI booking capture — paste text or upload a PDF in the **New plan** dialog and an LLM extracts the booking; the same extractor also backs the forwarded-email ingest.
- External calendar feeds — subscribe a trip to one or more iCal feed URLs (e.g. a conference's published schedule) from the **Edit trip** dialog. The server refreshes them periodically (SSRF-guarded, conditional GETs) and their events render as read-only "external plan" tiles, interleaved into the timeline behind a per-viewer **Show external plans** toggle (off by default, stored in `localStorage`). Sharing is inherited wholesale from the trip — no per-event sharing — and the events are kept out of the map and `.ics` export but included in the trip PDF when the toggle is on.
- Plan attachments — upload files (PDF tickets, confirmations) to a plan, stored out-of-band in a configured filesystem path or S3 bucket and gated by `ATTACHMENTS_STORE`.
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
