import { describe, it, expect } from 'vitest';

import type { Flight, FlightStatus, Trip } from '../api/types';
import { computeStats } from './stats';

const ME = 1;
const OTHER = 2;

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'Trip',
    destination: '',
    starts_on: '2000-01-01',
    ends_on: '2000-01-05',
    members: [{ user_id: ME, role: 'owner' }],
    country_code: 'gb',
    ...over,
  } as Trip;
}

function f(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA286',
    scheduled_out: '2026-01-01T10:00:00Z',
    scheduled_in: '2026-01-01T14:00:00Z',
    origin_iata: 'SFO',
    origin_lat: 37.6213,
    origin_lon: -122.379,
    dest_iata: 'LHR',
    dest_lat: 51.47,
    dest_lon: -0.4543,
    status: 'Arrived',
    notes: '',
    passenger_ids: [ME],
    is_public: false,
    shared_user_ids: [],
    ...over,
  } as Flight;
}

describe('computeStats', () => {
  it('returns zeros for empty input', () => {
    const s = computeStats([], ME);
    expect(s.flown).toEqual({ count: 0, miles: 0, minutes: 0, airports: 0 });
    expect(s.upcoming).toEqual({ count: 0, miles: 0, minutes: 0, airports: 0 });
    expect(s.highlight.longest).toBeNull();
    expect(s.highlight.mostVisited).toBeNull();
    expect(s.highlight.distinctAirlines).toBe(0);
    expect(s.highlight.mostAirline).toBeNull();
    expect(s.highlight.earthLaps).toBe(0);
    expect(s.countries).toBe(0);
    expect(s.excluded).toBe(0);
  });

  it('drops flights where I am not a passenger', () => {
    const s = computeStats([f({ passenger_ids: [OTHER] })], ME);
    expect(s.flown.count).toBe(0);
    expect(s.excluded).toBe(0);
  });

  it('buckets by status', () => {
    const flights: Flight[] = [
      f({ id: 1, status: 'Arrived' }),
      f({ id: 2, status: 'Scheduled' }),
      f({ id: 3, status: 'Boarding' }),
      f({ id: 4, status: 'Departed' }),
      f({ id: 5, status: 'Enroute' }),
      f({ id: 6, status: 'Cancelled' }),
      f({ id: 7, status: 'Diverted' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.flown.count).toBe(1);
    expect(s.upcoming.count).toBe(4);
    expect(s.excluded).toBe(2);
  });

  it('treats unknown statuses as excluded (defensive)', () => {
    const s = computeStats([f({ status: 'WeirdStatus' as FlightStatus })], ME);
    expect(s.flown.count).toBe(0);
    expect(s.upcoming.count).toBe(0);
    expect(s.excluded).toBe(1);
  });

  it('sums miles from origin/dest coordinates', () => {
    // SFO → LHR ≈ 5,367 mi
    const s = computeStats([f()], ME);
    expect(s.flown.miles).toBeGreaterThan(5300);
    expect(s.flown.miles).toBeLessThan(5430);
  });

  it('counts flights with missing coordinates but adds 0 miles', () => {
    const flights = [
      f({ id: 1, origin_lat: undefined, origin_lon: undefined }),
      f({ id: 2, dest_lat: undefined, dest_lon: undefined }),
    ];
    const s = computeStats(flights, ME);
    expect(s.flown.count).toBe(2);
    expect(s.flown.miles).toBe(0);
    // Both flights still contribute to the airports set:
    expect(s.flown.airports).toBe(2); // SFO and LHR
  });

  it('uses actual_in/actual_out when both are present', () => {
    const s = computeStats(
      [
        f({
          scheduled_out: '2026-01-01T10:00:00Z',
          scheduled_in: '2026-01-01T14:00:00Z', // 4h scheduled
          actual_out: '2026-01-01T10:30:00Z',
          actual_in: '2026-01-01T15:30:00Z', // 5h actual
        }),
      ],
      ME,
    );
    expect(s.flown.minutes).toBe(300);
  });

  it('falls back to scheduled times when actuals are missing', () => {
    const s = computeStats([f()], ME); // 4h scheduled, no actuals
    expect(s.flown.minutes).toBe(240);
  });

  it('skips negative or zero durations', () => {
    const s = computeStats(
      [
        f({
          scheduled_out: '2026-01-01T10:00:00Z',
          scheduled_in: '2026-01-01T10:00:00Z',
        }),
      ],
      ME,
    );
    expect(s.flown.minutes).toBe(0);
  });

  it('counts unique airports across the bucket', () => {
    const flights = [
      f({ id: 1, origin_iata: 'SFO', dest_iata: 'LHR' }),
      f({ id: 2, origin_iata: 'LHR', dest_iata: 'JFK' }),
      f({ id: 3, origin_iata: 'JFK', dest_iata: 'SFO' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.flown.airports).toBe(3); // SFO, LHR, JFK
  });

  it('picks the longest flown flight for the highlight', () => {
    const flights = [
      f({ id: 1, ident: 'BA286', origin_iata: 'SFO', dest_iata: 'LHR' }), // ~5,367 mi
      f({
        id: 2,
        ident: 'UA1',
        origin_iata: 'SFO',
        origin_lat: 37.6213,
        origin_lon: -122.379,
        dest_iata: 'SYD',
        dest_lat: -33.94,
        dest_lon: 151.18,
      }), // ~7,400 mi
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.longest?.ident).toBe('UA1');
    expect(s.highlight.longest?.origin).toBe('SFO');
    expect(s.highlight.longest?.dest).toBe('SYD');
  });

  it('breaks longest-flight ties by most recent scheduled_out', () => {
    const flights = [
      f({ id: 1, ident: 'A1', scheduled_out: '2025-01-01T00:00:00Z' }),
      f({ id: 2, ident: 'A2', scheduled_out: '2026-01-01T00:00:00Z' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.longest?.ident).toBe('A2');
  });

  it('returns null longest when no flown flight has coordinates', () => {
    const flights = [
      f({ origin_lat: undefined, origin_lon: undefined }),
      f({ id: 2, dest_lat: undefined, dest_lon: undefined }),
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.longest).toBeNull();
  });

  it('picks the most-visited airport from flown flights', () => {
    const flights = [
      f({ id: 1, origin_iata: 'LHR', dest_iata: 'JFK' }),
      f({ id: 2, origin_iata: 'JFK', dest_iata: 'LHR' }),
      f({ id: 3, origin_iata: 'LHR', dest_iata: 'CDG' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.mostVisited).toEqual({ iata: 'LHR', count: 3 });
  });

  it('breaks most-visited ties alphabetically', () => {
    const flights = [
      f({ id: 1, origin_iata: 'CDG', dest_iata: 'LHR' }),
      f({ id: 2, origin_iata: 'CDG', dest_iata: 'LHR' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.mostVisited?.iata).toBe('CDG');
  });

  it('counts distinct airline codes from idents', () => {
    const flights = [
      f({ id: 1, ident: 'BA286' }),
      f({ id: 2, ident: 'BA999' }),
      f({ id: 3, ident: 'EZY2823' }),
      f({ id: 4, ident: 'UA1' }),
      f({ id: 5, ident: 'XX' }), // no digits — skipped
      f({ id: 6, ident: '123' }), // no letters — skipped
      f({ id: 7, ident: '' }), // empty — skipped
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.distinctAirlines).toBe(3); // BA, EZY, UA
  });

  it('picks the most-used airline by flight count', () => {
    const flights = [
      f({ id: 1, ident: 'BA286' }),
      f({ id: 2, ident: 'BA999' }),
      f({ id: 3, ident: 'BA111' }),
      f({ id: 4, ident: 'UA1' }),
      f({ id: 5, ident: 'EZY2823' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.mostAirline).toEqual({ code: 'BA', count: 3 });
  });

  it('breaks most-used airline ties alphabetically', () => {
    const flights = [
      f({ id: 1, ident: 'UA1' }),
      f({ id: 2, ident: 'BA286' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.mostAirline).toEqual({ code: 'BA', count: 1 });
  });

  it('counts most-used airline from flown flights only', () => {
    const flights = [
      f({ id: 1, status: 'Arrived', ident: 'BA286' }),
      f({ id: 2, status: 'Scheduled', ident: 'UA1' }),
      f({ id: 3, status: 'Scheduled', ident: 'UA2' }),
    ];
    const s = computeStats(flights, ME);
    expect(s.highlight.mostAirline).toEqual({ code: 'BA', count: 1 });
  });

  it('counts distinct visited countries from past/ongoing trips', () => {
    const trips = [
      trip({ id: 1, country_code: 'gb' }),
      trip({ id: 2, country_code: 'GB' }), // same country, different case
      trip({ id: 3, country_code: 'fr' }),
    ];
    const s = computeStats([], ME, trips);
    expect(s.countries).toBe(2); // gb, fr
  });

  it('ignores upcoming and date-less trips when counting countries', () => {
    const trips = [
      trip({ id: 1, country_code: 'gb' }), // past
      trip({ id: 2, country_code: 'fr', starts_on: '2999-01-01', ends_on: '2999-01-05' }),
      trip({ id: 3, country_code: 'de', starts_on: undefined, ends_on: undefined }),
    ];
    const s = computeStats([], ME, trips);
    expect(s.countries).toBe(1); // only gb
  });

  it('ignores trips with no country code or where I am not a member', () => {
    const trips = [
      trip({ id: 1, country_code: undefined }),
      trip({ id: 2, country_code: 'fr', members: [{ user_id: OTHER, role: 'owner' }] }),
      trip({ id: 3, country_code: 'gb' }),
    ];
    const s = computeStats([], ME, trips);
    expect(s.countries).toBe(1); // only gb
  });

  it('returns the raw earth-laps ratio (compute), 0 when nothing flown', () => {
    expect(computeStats([], ME).highlight.earthLaps).toBe(0);
    const s = computeStats([f()], ME); // ~5,367 mi → 5367/24901 ≈ 0.21
    expect(s.highlight.earthLaps).toBeGreaterThan(0.2);
    expect(s.highlight.earthLaps).toBeLessThan(0.22);
  });

  it('computes highlights from flown only — upcoming flights do not contribute', () => {
    const flights = [f({ id: 1, status: 'Scheduled', ident: 'AA999', origin_iata: 'LHR' })];
    const s = computeStats(flights, ME);
    expect(s.highlight.longest).toBeNull();
    expect(s.highlight.mostVisited).toBeNull();
    expect(s.highlight.distinctAirlines).toBe(0);
    expect(s.upcoming.count).toBe(1);
  });

  it('falls back to scheduled times when only one actual is present', () => {
    // actual_out only — should NOT be mixed with scheduled_in.
    const s = computeStats(
      [
        f({
          scheduled_out: '2026-01-01T10:00:00Z',
          scheduled_in: '2026-01-01T14:00:00Z', // 4h scheduled
          actual_out: '2026-01-01T10:30:00Z',
          // actual_in absent
        }),
      ],
      ME,
    );
    expect(s.flown.minutes).toBe(240);
  });

  it('falls back to scheduled times when only actual_in is present', () => {
    const s = computeStats(
      [
        f({
          scheduled_out: '2026-01-01T10:00:00Z',
          scheduled_in: '2026-01-01T14:00:00Z',
          actual_in: '2026-01-01T15:30:00Z',
        }),
      ],
      ME,
    );
    expect(s.flown.minutes).toBe(240);
  });
});
