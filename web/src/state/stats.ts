import type { Flight, Trip } from '../api/types';
import { greatCircleMiles } from '../lib/great-circle';
import { classifyTrip, tripSpan } from '../lib/trip-format';

export type Bucket = {
  count: number;
  miles: number;
  minutes: number;
  airports: number;
};

export type Highlight = {
  longest: { ident: string; origin: string; dest: string; miles: number } | null;
  mostVisited: { iata: string; count: number } | null;
  distinctAirlines: number;
  mostAirline: { code: string; count: number } | null;
  earthLaps: number; // raw ratio; UI hides tile when < 0.1
};

export type Stats = {
  flown: Bucket;
  upcoming: Bucket;
  highlight: Highlight;
  /** Distinct countries the user has actually visited, derived from trips. */
  countries: number;
  excluded: number;
};

const EARTH_CIRCUMFERENCE_MI = 24901;
const AIRLINE_RE = /^([A-Z]{2,3})\d+$/i;

const UPCOMING_STATUSES: ReadonlySet<string> = new Set([
  'Scheduled',
  'Boarding',
  'Departed',
  'Enroute',
]);
const EXCLUDED_STATUSES: ReadonlySet<string> = new Set(['Cancelled', 'Diverted']);

function emptyBucket(): Bucket {
  return { count: 0, miles: 0, minutes: 0, airports: 0 };
}

function flightMiles(f: Flight): number {
  if (f.origin_lat == null || f.origin_lon == null || f.dest_lat == null || f.dest_lon == null) {
    return 0;
  }
  return greatCircleMiles(f.origin_lat, f.origin_lon, f.dest_lat, f.dest_lon);
}

function flightMinutes(f: Flight): number {
  const useActuals = f.actual_out != null && f.actual_in != null;
  const out = useActuals ? f.actual_out! : f.scheduled_out;
  const inT = useActuals ? f.actual_in! : f.scheduled_in;
  if (!out || !inT) return 0;
  const ms = new Date(inT).getTime() - new Date(out).getTime();
  if (!Number.isFinite(ms) || ms <= 0) return 0;
  return Math.round(ms / 60000);
}

function addToBucket(b: Bucket, f: Flight, airports: Set<string>): void {
  b.count += 1;
  b.miles += flightMiles(f);
  b.minutes += flightMinutes(f);
  if (f.origin_iata) airports.add(f.origin_iata);
  if (f.dest_iata) airports.add(f.dest_iata);
}

export function computeStats(flights: Flight[], meId: number, trips: Trip[] = []): Stats {
  const flown = emptyBucket();
  const upcoming = emptyBucket();
  const flownAirports = new Set<string>();
  const upcomingAirports = new Set<string>();
  const flownFlights: Flight[] = [];
  let excluded = 0;

  for (const f of flights) {
    if (!f.passenger_ids.includes(meId)) continue;
    if (f.status === 'Arrived') {
      addToBucket(flown, f, flownAirports);
      flownFlights.push(f);
    } else if (UPCOMING_STATUSES.has(f.status)) {
      addToBucket(upcoming, f, upcomingAirports);
    } else if (EXCLUDED_STATUSES.has(f.status)) {
      excluded += 1;
    } else {
      excluded += 1; // defensive
    }
  }

  flown.airports = flownAirports.size;
  upcoming.airports = upcomingAirports.size;

  return {
    flown,
    upcoming,
    highlight: computeHighlight(flownFlights, flown.miles),
    countries: countriesVisited(trips, meId),
    excluded,
  };
}

/** Count the distinct countries the user has actually been to. We lean on the
 * geocoded `country_code` the server attaches to each trip, and on the shared
 * trip classifier so "visited" means a trip that's happening now or already
 * past — an upcoming trip doesn't count, as you haven't been there yet. Trips
 * without a derivable span classify as upcoming (the app-wide convention), so
 * date-less trips are excluded too. Only trips the user is a passenger on are
 * considered. */
function countriesVisited(trips: Trip[], meId: number, now: number = Date.now()): number {
  const seen = new Set<string>();
  for (const t of trips) {
    if (!t.country_code) continue;
    if (!t.passenger_ids.includes(meId)) continue;
    if (classifyTrip(tripSpan(t), now) === 'upcoming') continue;
    seen.add(t.country_code.toLowerCase());
  }
  return seen.size;
}

function computeHighlight(flown: Flight[], totalMiles: number): Highlight {
  const airlines = airlineStats(flown);
  return {
    longest: longestFlight(flown),
    mostVisited: mostVisitedAirport(flown),
    distinctAirlines: airlines.distinct,
    mostAirline: airlines.most,
    earthLaps: totalMiles / EARTH_CIRCUMFERENCE_MI,
  };
}

function longestFlight(flown: Flight[]): Highlight['longest'] {
  let best: { f: Flight; miles: number } | null = null;
  for (const f of flown) {
    const miles = flightMiles(f);
    if (miles <= 0) continue;
    if (
      best === null ||
      miles > best.miles ||
      (miles === best.miles &&
        new Date(f.scheduled_out).getTime() > new Date(best.f.scheduled_out).getTime())
    ) {
      best = { f, miles };
    }
  }
  if (best === null) return null;
  return {
    ident: best.f.ident,
    origin: best.f.origin_iata,
    dest: best.f.dest_iata,
    miles: best.miles,
  };
}

function mostVisitedAirport(flown: Flight[]): Highlight['mostVisited'] {
  const counts = new Map<string, number>();
  for (const f of flown) {
    if (f.origin_iata) counts.set(f.origin_iata, (counts.get(f.origin_iata) ?? 0) + 1);
    if (f.dest_iata) counts.set(f.dest_iata, (counts.get(f.dest_iata) ?? 0) + 1);
  }
  let winner: { iata: string; count: number } | null = null;
  for (const [iata, count] of counts) {
    if (winner === null || count > winner.count || (count === winner.count && iata < winner.iata)) {
      winner = { iata, count };
    }
  }
  return winner;
}

/** Tally airline codes parsed from flight idents in a single pass, returning
 * both the number of distinct airlines and the most-used one. Ties on count
 * break alphabetically so the result is stable. We only ever have the IATA
 * code (e.g. "BA"), not the carrier's name, so the UI shows the code. */
function airlineStats(flown: Flight[]): {
  distinct: number;
  most: { code: string; count: number } | null;
} {
  const counts = new Map<string, number>();
  for (const f of flown) {
    const m = AIRLINE_RE.exec(f.ident);
    if (!m) continue;
    const code = m[1].toUpperCase();
    counts.set(code, (counts.get(code) ?? 0) + 1);
  }
  let most: { code: string; count: number } | null = null;
  for (const [code, count] of counts) {
    if (most === null || count > most.count || (count === most.count && code < most.code)) {
      most = { code, count };
    }
  }
  return { distinct: counts.size, most };
}
