# API reference

Aerly uses external APIs for flight scheduling/metadata, live positioning, and points-of-interest
discovery. This document covers each, explains their limitations, and compares the
alternative unified APIs that handle the flight concerns from a single integration.

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
around a location, either the trip destination (geocoded via Nominatim) or a hotel's pinned
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
