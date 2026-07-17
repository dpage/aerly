# API reference

Aerly uses external APIs for flight scheduling/metadata, live positioning, points-of-interest
discovery, address geocoding, and LLM-based booking extraction. This document covers each,
explains their limitations, and compares the alternative unified APIs that handle the flight
concerns from a single integration.

---

## Currently integrated

### OpenSky Network — live position tracking

[OpenSky](https://opensky-network.org/) is a crowdsourced network of volunteer-operated,
ground-based ADS-B receivers. Aircraft broadcast their position, altitude, and velocity via
an ADS-B OUT transponder; any receiver within radio range picks up the signal and uploads it
to OpenSky's servers.

**Cost:** Free (non-commercial use). Authenticated accounts get higher rate limits.

**Limitations:**

- **Ground receiver coverage required.** If there is no receiver within roughly 200–400 km
  (line-of-sight) of the aircraft, no position is available:
  - Oceanic routes (North Atlantic, Pacific, polar) will typically have no live fix for
    hours at a time.
  - Remote or sparsely populated continental areas may also have gaps.
  - Low-altitude departures and arrivals may disappear momentarily while still in a
    receiver's shadow.
- **ADS-B OUT required.** Older aircraft using only Mode-C or Mode-S transponders do not
  broadcast position data and will not appear. ADS-B equipage is now mandatory in most
  controlled airspace, but some charter, cargo, and older regional aircraft may be exempt.
- **Rate limits.** Anonymous access is heavily rate-limited. An authenticated account
  (`OPENSKY_USERNAME` / `OPENSKY_PASSWORD`) raises the limit, but it remains a shared free
  service — Aerly backs off automatically on `429` responses.
- **icao24 required.** OpenSky identifies aircraft by their Mode-S hex address (icao24).
  Aerly gets this from AeroDataBox at flight-creation time; if the scheduled airframe
  changes (equipment swap, wet lease), the position may track the wrong aircraft until the
  flight record is corrected manually.
- **Dead-reckoner fallback.** When a live fix is unavailable, Aerly extrapolates from the
  last known fix toward the destination along a great-circle. Estimated positions are
  rendered with reduced opacity and a dashed outline.

---

### AeroDataBox via RapidAPI — flight schedule and metadata

[AeroDataBox](https://rapidapi.com/aedbx-aedbx/api/aerodatabox/) provides scheduled
departure/arrival times, origin/destination airports with coordinates, aircraft type, and
the icao24 address used by the OpenSky tracker.

**Cost:** Pay-per-call via RapidAPI. Aerly caches successful responses for 24 hours to
avoid duplicate charges.

**Limitations:**

- **Schedule availability.** Flights are often not in AeroDataBox's database until one or
  two days before departure. Resolving a flight booked far in advance may return no results;
  try again closer to the travel date.
- **icao24 accuracy.** The aircraft assigned to a flight can change. AeroDataBox reports the
  currently scheduled airframe, which may differ from the one that actually operates.

---

### Points of interest — Geoapify Places

The trip "Explore" tab and the hotel-tile "Explore nearby" button list sightseeing places
(attractions, museums, historic landmarks, places of worship, parks, and optionally food and drink)
around a location, either the trip destination (geocoded via Geoapify) or a hotel's pinned
coordinates. The resolver implements `providers.POIResolver`, with a 7-day in-memory result cache
in front of it.

**[Geoapify Places](https://www.geoapify.com/places-api/) — used when `GEOAPIFY_API_KEY` is set.**
A keyed, purpose-built POI service whose dataset is OpenStreetMap-derived. It answers categorised,
radius-bounded queries directly and reliably.

- **Cost:** free tier (no card, ~3000 requests/day), which the 7-day cache keeps us well within.
- **Attribution:** the underlying data is OpenStreetMap (ODbL); the Explore panel shows the
  "Data © OpenStreetMap contributors" attribution.
- **No key, no Explore:** with `GEOAPIFY_API_KEY` unset the app has no POI resolver, so `/api/config`
  reports `explore_enabled=false` and the frontend withdraws the Explore tab, the "Explore nearby"
  button and the preference to hide Explore. (An earlier keyless Overpass fallback was removed: the
  public Overpass instances rate-limited and timed out a busy server IP, silently returning "no
  places found" in production, which is exactly why we moved to Geoapify.)

**Limitations (inherited from the OSM data):**

- **Coverage varies by region.** POI density and description quality are excellent in UK and
  European city centres but thin out in less-mapped regions.
- **Sparse metadata.** OSM POIs often lack a description; Aerly shows the OSM `description` tag as a
  blurb where present, and otherwise links out to the map and (where tagged) Wikidata, Wikipedia,
  and a website rather than showing editorial content.

---

### Geoapify Geocoding — address geocoding

[Geoapify Geocoding](https://www.geoapify.com/geocoding-api/) turns a plan's free-text address
into coordinates so hotels, restaurants, transfers and excursions plot on the map, and it supplies
the point Aerly anchors to an IANA timezone (via `tzf`) so local times are correct. It also
geocodes a typed place name in the Explore search box. Aerly moved onto Geoapify from the public
Nominatim instance, whose usage policy capped bulk lookups at 4 requests/minute and offered no
match-confidence signal with which to reject a wrong result (see **Why not Google Maps Platform**
below for the alternative that was also ruled out along the way).

- **Cost:** free tier (no card required), 3,000 credits/day. This reuses the same
  `GEOAPIFY_API_KEY` already required for Places, so there's no separate key to provision.
- **Attribution:** "Powered by Geoapify", alongside the underlying "Data © OpenStreetMap
  contributors" (ODbL) credit; both are shown on the map.
- **How a result is chosen.** Geoapify returns a ranked candidate list, each carrying a
  `rank.confidence` score. Aerly accepts the top candidate only when it clears
  `GEOCODE_MIN_CONFIDENCE` (default `0.5`) *and* leads the runner-up by at least `GEOCODE_MARGIN`
  (default `0.15`). A result that fails either test is ambiguous rather than wrong, so it's handed
  to the configured LLM, which either picks a candidate by index or declines; with no LLM
  configured, or if it declines, nothing is plotted. A missing pin is always preferred to a wrong
  one.

**Limitations:**

- **Coverage varies by region.** The data is OpenStreetMap-derived, so it's excellent in UK and
  European city centres and noticeably thinner elsewhere.
- **The confidence thresholds are uncalibrated guesses.** Nobody has yet run a real address corpus
  through Geoapify to tune `GEOCODE_MIN_CONFIDENCE` or `GEOCODE_MARGIN`; the defaults are a
  starting point, not a measured result.
- **`rank.confidence` alone cannot disambiguate a chain.** Verified against the live API on 17
  July 2026: querying "Premier Inn Manchester" returns five different Premier Inns, every one at
  confidence `1.0`. Confidence tells you the match is good, not that it's the *right* one; the
  margin check is what catches this case, which is exactly why it exists.
- **`rank.match_type` is not a quality signal.** One live result returned confidence `0` alongside
  `match_type: "full_match"`, so Aerly doesn't branch on it at all.
- **No key means no geocoding at all.** With `GEOAPIFY_API_KEY` unset, addresses simply don't
  plot, exactly as the Explore tab withdraws without the same key.

#### Why not Google Maps Platform

Google's Maps Platform was investigated and ruled out, recorded here so nobody re-runs the
investigation the next time someone asks "why don't we just use Google?":

- The Maps Platform Terms §3.2.3(e), together with the Service Specific Terms §6.2 (Geocoding)
  and §14.2 (Places), forbid using Google's geocoding or places content "in conjunction with a
  non-Google map". Aerly renders with MapLibre, which disqualifies Google outright regardless of
  call volume.
- Google's more permissive EEA terms only apply to accounts with an EEA billing address; the UK
  is not in the EEA, so they don't help Aerly.
- Cost was never the blocker: at Aerly's volume, geocoding would be free either way (Google's free
  tier covers 10,000 geocodes/month).
- There is no supported route from a Google feature ID (`ftid`) or CID to coordinates under any
  Google API, so holding a Google key would not have fixed the pasted-share-link case (a Google
  Maps link carrying no coordinates in the URL) that motivated part of this work anyway.
- Google also prohibits scraping its Maps content, which is the second reason the abandoned
  `og:image`-scraping approach for share links was the wrong direction.

---

### LLM providers — booking extraction

Aerly uses a large language model to extract structured bookings from unstructured input: text
pasted into the **New plan** dialog, uploaded booking PDFs, and forwarded confirmation emails. The
model is asked for a strict JSON schema (plan type, places, dates/times, references) which Aerly
validates before proposing anything. The provider and model are configurable
(`LLM_PROVIDER` / `LLM_MODEL` / `LLM_API_KEY`).

**Supported providers:** `anthropic` (default, `claude-haiku-4-5`), `openai`, `gemini`, and
`ollama` (a keyless local runtime).

**Cost:** Pay-per-token at the chosen provider (or free/self-hosted with `ollama`). Aerly favours a
small, cheap model by default; extraction is a bounded, one-shot call per paste/upload/email, and
email ingest is additionally rate-limited per user.

**Limitations:**

- **PDF (document) support.** Uploaded and forwarded PDFs are sent as native document blocks, which
  only `anthropic` and `gemini` support. With `openai` or `ollama` the extractor silently retries
  text-only: the visible body is still parsed, but attached PDFs are skipped.
- **Best-effort extraction.** Output is always validated and surfaced for the user to confirm (for
  pasted/uploaded input) rather than committed blindly; a model miss degrades to "nothing
  extracted", never to bad data written silently.
- **Optional.** With no LLM configured, the paste/upload propose endpoints return `503` and the New
  plan dialog offers manual entry only; email ingest can't be enabled at all.

---

## Alternative unified APIs

These APIs provide both scheduling metadata and live positioning from a single integration,
removing the icao24 dependency and improving oceanic coverage.

### FlightAware AeroAPI

[AeroAPI](https://www.flightaware.com/commercial/aeroapi) is the most established unified
flight data API. A single endpoint (`/flights/{ident}`) returns live position, schedule,
route, aircraft type, and ETA together. Position data is sourced from FAA/Eurocontrol radar
feeds, ground ADS-B networks, and (on commercial tiers) Aireon space-based ADS-B — giving
much better oceanic and polar coverage than OpenSky alone.

**Pricing:**
- Personal (requires operating an ADS-B feeder, non-commercial only): ~$20/month flat
- Commercial: usage-based at approximately **$0.005 per query**, $5/month minimum credit
- High-volume flat subscriptions from ~$200/month

**Estimated cost at Aerly's scale** (5 active flights, 60 s poll interval, ~10 flight-days/month):
~15,000 position queries/month ≈ **$75/month**. Stretching to a 5-minute poll interval
for all flights reduces this to roughly **$15–20/month**, at the cost of less frequent track
updates.

**Upgrade path:** Clear tiered subscriptions to enterprise. Aireon global coverage
(oceanic fixes every ~8 s) unlocks on higher commercial tiers.

---

### AirLabs

[AirLabs](https://airlabs.co/) provides real-time position and schedule data in one API.
Positioning uses Aireon satellite ADS-B, so oceanic and polar coverage is included even on
entry-level plans.

**Pricing:**
| Plan | Monthly | Queries included |
|---|---|---|
| Free | $0 | ~1,000 |
| Developer | $49 | 25,000 |
| Business | $99 | 100,000 |
| Enterprise | $499 | 1,000,000 |

**Estimated cost at Aerly's scale:** ~15,000 queries/month fits the **Developer plan at
$49/month**. At a 5-minute poll interval (~3,000 queries/month) it falls within the free
tier.

**Upgrade path:** Simple step-up tiers with no per-query billing surprises.

---

### AviationStack

[AviationStack](https://aviationstack.com/) provides flight status, schedule, and live
position data. The entry price is lower, but positioning data quality and oceanic coverage
are less consistent than FlightAware or AirLabs.

**Pricing:**
- Free: 100 requests/month (exhausted in under a day at normal poll rates)
- Basic: ~$49.99/month (higher call volume, but tier details vary)

---

## Comparison table

| | OpenSky | AeroDataBox | FlightAware AeroAPI | AirLabs | AviationStack |
|---|---|---|---|---|---|
| **Provides scheduling** | No | Yes | Yes | Yes | Yes |
| **Provides live position** | Yes | No | Yes | Yes | Yes |
| **Position data sources** | Ground ADS-B only | — | Radar + ADS-B + satellite (Aireon) | Satellite ADS-B (Aireon) | Multiple (undisclosed) |
| **Oceanic / polar coverage** | Poor (gaps of hours) | — | Good (satellite on commercial tiers) | Good (all tiers) | Variable |
| **ADS-B OUT required** | Yes | — | No (radar fills gaps) | No | No |
| **icao24 required for tracking** | Yes | — | No (query by ident) | No (query by ident) | No |
| **Entry cost** | Free | Pay-per-call (~low) | $5/month credit (~$0.005/query) | Free (1,000 q) / $49/month | Free (100 q) / ~$50/month |
| **~cost at Aerly's scale** | Free | ~$1–5/month | $15–75/month (poll-interval dependent) | Free–$49/month | ~$50/month |
| **Upgrade path** | None (free only) | RapidAPI tiers | Flat subscription tiers | Flat plan tiers | Flat plan tiers |
| **Currently integrated** | Yes | Yes | No | No | No |

### Notes on the cost estimates

"Aerly's scale" here means roughly 5 simultaneously active flights, polled every 60 seconds
for enroute flights (5 minutes otherwise), with around 10 flight-days per month. Your
actual usage will vary.

The unified APIs (FlightAware, AirLabs, AviationStack) would replace **both** OpenSky and
AeroDataBox, simplifying the provider architecture to a single integration.
