import type { Flight } from '../api/types';
import { greatCircleMiles } from '../lib/great-circle';

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
  earthLaps: number; // raw ratio; UI hides tile when < 0.1
};

export type Stats = {
  flown: Bucket;
  upcoming: Bucket;
  highlight: Highlight;
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
  if (
    f.origin_lat == null ||
    f.origin_lon == null ||
    f.dest_lat == null ||
    f.dest_lon == null
  ) {
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

export function computeStats(flights: Flight[], meId: number): Stats {
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
    excluded,
  };
}

function computeHighlight(flown: Flight[], totalMiles: number): Highlight {
  return {
    longest: longestFlight(flown),
    mostVisited: mostVisitedAirport(flown),
    distinctAirlines: distinctAirlines(flown),
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
    if (
      winner === null ||
      count > winner.count ||
      (count === winner.count && iata < winner.iata)
    ) {
      winner = { iata, count };
    }
  }
  return winner;
}

function distinctAirlines(flown: Flight[]): number {
  const codes = new Set<string>();
  for (const f of flown) {
    const m = AIRLINE_RE.exec(f.ident);
    if (m) codes.add(m[1].toUpperCase());
  }
  return codes.size;
}
