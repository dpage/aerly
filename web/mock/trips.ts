/**
 * Mock trip data for MOCK=1 dev mode.
 * Generates 100 past trips + a couple of upcoming/now trips so the full
 * TripList layout (all three buckets) is exercised.
 */

const DESTINATIONS = [
  { name: 'Stockholm, Sweden',        code: 'se', tags: ['ARN', 'Nordic'] },
  { name: 'Athens, Greece',           code: 'gr', tags: ['ATH', 'Arnack', 'Mediterranean'] },
  { name: 'New York, USA',            code: 'us', tags: ['JFK', 'City break'] },
  { name: 'Tokyo, Japan',             code: 'jp', tags: ['NRT', 'Asia'] },
  { name: 'Paris, France',            code: 'fr', tags: ['CDG', 'Europe'] },
  { name: 'Sydney, Australia',        code: 'au', tags: ['SYD', 'Pacific'] },
  { name: 'Dubai, UAE',               code: 'ae', tags: ['DXB', 'Middle East'] },
  { name: 'Reykjavik, Iceland',       code: 'is', tags: ['KEF', 'Nordic'] },
  { name: 'Cape Town, South Africa',  code: 'za', tags: ['CPT', 'Africa'] },
  { name: 'Rio de Janeiro, Brazil',   code: 'br', tags: ['GIG', 'South America'] },
  { name: 'Berlin, Germany',          code: 'de', tags: ['BER', 'Europe'] },
  { name: 'Singapore',                code: 'sg', tags: ['SIN', 'Asia'] },
  { name: 'Lisbon, Portugal',         code: 'pt', tags: ['LIS', 'Europe'] },
  { name: 'Toronto, Canada',          code: 'ca', tags: ['YYZ', 'North America'] },
  { name: 'Barcelona, Spain',         code: 'es', tags: ['BCN', 'Mediterranean'] },
  { name: 'Amsterdam, Netherlands',   code: 'nl', tags: ['AMS', 'Europe'] },
  { name: 'Mumbai, India',            code: 'in', tags: ['BOM', 'Asia'] },
  { name: 'Oslo, Norway',             code: 'no', tags: ['OSL', 'Nordic'] },
  { name: 'Nairobi, Kenya',           code: 'ke', tags: ['NBO', 'Africa'] },
  { name: 'Mexico City, Mexico',      code: 'mx', tags: ['MEX', 'Latin America'] },
];

const TRIP_NAMES = [
  'Summer Escape', 'Business Summit', 'Family Reunion', 'Winter Getaway',
  'Conference', 'Honeymoon', 'Adventure Trek', 'City Exploration',
  'Product Launch', 'Team Offsite', 'Annual Retreat', 'Culture Tour',
  'Photography Trip', 'Food & Wine Tour', 'Ski Holiday', 'Beach Week',
  'Tech Conference', 'Art Festival', 'Music Tour', 'Nature Walk',
];

function pad(n: number) { return String(n).padStart(2, '0'); }
function fmtDate(d: Date) {
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}

function makeTrip(
  id: number,
  name: string,
  destination: string,
  countryCode: string,
  startsOn: string,
  endsOn: string,
  tags: string[],
) {
  return {
    id,
    name,
    destination,
    starts_on: startsOn,
    ends_on: endsOn,
    my_role: 'owner' as const,
    viewer_is_passenger: false,
    members: [{ user_id: 1, role: 'owner' as const, accepted: true }],
    passenger_ids: [] as number[],
    tags,
    country_code: countryCode,
    reminder_opted_in: false,
    reminder_lead_hours: 24,
    created_at: startsOn + 'T00:00:00Z',
    updated_at: startsOn + 'T00:00:00Z',
  };
}

export function generateMockTrips() {
  const trips = [];
  const now = new Date();
  let id = 1;

  // 100 past trips spread over 8 years. Use a deterministic stride rather
  // than Math.random() so the list is stable across hot-reloads.
  for (let i = 0; i < 100; i++) {
    const daysAgo = 30 + i * 29; // 30 days ago … ~8 years ago
    const startMs = now.getTime() - daysAgo * 24 * 60 * 60 * 1000;
    const durDays = 2 + (i % 13); // 2–14 days
    const startDate = new Date(startMs);
    const endDate = new Date(startMs + durDays * 24 * 60 * 60 * 1000);

    const dest = DESTINATIONS[i % DESTINATIONS.length];
    const tripName = `${TRIP_NAMES[i % TRIP_NAMES.length]} — ${dest.name.split(',')[0]}`;

    trips.push(makeTrip(
      id++,
      tripName,
      dest.name,
      dest.code,
      fmtDate(startDate),
      fmtDate(endDate),
      dest.tags,
    ));
  }

  // One trip happening right now, so the "Happening now" bucket is visible.
  const nowStart = new Date(now.getTime() - 2 * 24 * 60 * 60 * 1000);
  const nowEnd   = new Date(now.getTime() + 3 * 24 * 60 * 60 * 1000);
  trips.push(makeTrip(
    id++,
    'PostgreSQL Conference — Berlin',
    'Berlin, Germany',
    'de',
    fmtDate(nowStart),
    fmtDate(nowEnd),
    ['BER', 'PGConf', 'Europe'],
  ));

  // Two upcoming trips so the Upcoming bucket is also populated.
  const u1Start = new Date(now.getTime() + 10 * 24 * 60 * 60 * 1000);
  const u1End   = new Date(now.getTime() + 17 * 24 * 60 * 60 * 1000);
  trips.push(makeTrip(
    id++,
    'Summer Escape — Reykjavik',
    'Reykjavik, Iceland',
    'is',
    fmtDate(u1Start),
    fmtDate(u1End),
    ['KEF', 'Nordic'],
  ));

  const u2Start = new Date(now.getTime() + 45 * 24 * 60 * 60 * 1000);
  const u2End   = new Date(now.getTime() + 52 * 24 * 60 * 60 * 1000);
  trips.push(makeTrip(
    id++,
    'Tech Conference — Tokyo',
    'Tokyo, Japan',
    'jp',
    fmtDate(u2Start),
    fmtDate(u2End),
    ['NRT', 'Asia'],
  ));

  return trips;
}
