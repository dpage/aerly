import { describe, it, expect } from 'vitest';
import type { PlanPart } from '../api/types';
import { startUnlocated, endUnlocated, isUnlocated, parseLatLon, unlocatedCount } from './geo';

function part(over: Partial<PlanPart>): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'hotel',
    seq: 0,
    status: 'planned',
    starts_at: '2026-06-08T15:00:00Z',
    start_tz: '',
    end_tz: '',
    start_label: '',
    start_address: '',
    end_label: '',
    end_address: '',
    ...over,
  } as PlanPart;
}

describe('unlocated predicates', () => {
  it('start address with no coord is unlocated', () => {
    expect(startUnlocated(part({ start_address: 'Some Hotel, Portugal' }))).toBe(true);
  });
  it('start address with a coord is located', () => {
    expect(startUnlocated(part({ start_address: 'X', start_lat: 1, start_lon: 2 }))).toBe(false);
  });
  it('no address is never unlocated', () => {
    expect(startUnlocated(part({ start_label: 'FAO' }))).toBe(false);
  });
  it('a flight leg (IATA labels, no addresses) is not flagged', () => {
    expect(isUnlocated(part({ type: 'flight', start_label: 'NQY', end_label: 'FAO' }))).toBe(false);
  });
  it('end address with no coord is unlocated', () => {
    expect(endUnlocated(part({ end_address: 'Pickup point' }))).toBe(true);
  });
  it('isUnlocated is the OR of both ends', () => {
    expect(isUnlocated(part({ end_address: 'Pickup point' }))).toBe(true);
  });
  it('unlocatedCount counts only unlocated, non-dismissed parts', () => {
    const parts = [
      part({ id: 1, start_address: 'A' }), // unlocated
      part({ id: 2, start_address: 'B', start_lat: 1, start_lon: 2 }), // located
      part({ id: 3, start_address: 'C', dismissed_at: '2026-01-01T00:00:00Z' }), // dismissed
    ];
    expect(unlocatedCount(parts)).toBe(1);
  });
});

describe('parseLatLon', () => {
  it('parses a comma-separated Google Maps pin', () => {
    expect(parseLatLon('48.2105, 4.0823')).toEqual({ lat: 48.2105, lon: 4.0823 });
  });
  it('tolerates no space, extra space, and a space separator', () => {
    expect(parseLatLon('48.2105,4.0823')).toEqual({ lat: 48.2105, lon: 4.0823 });
    expect(parseLatLon('  -33.86  ,  151.21 ')).toEqual({ lat: -33.86, lon: 151.21 });
    expect(parseLatLon('51.5 -0.12')).toEqual({ lat: 51.5, lon: -0.12 });
  });
  it('rejects out-of-range, partial, and non-numeric input', () => {
    expect(parseLatLon('91, 0')).toBeNull(); // lat > 90
    expect(parseLatLon('0, 181')).toBeNull(); // lon > 180
    expect(parseLatLon('48.21')).toBeNull(); // only one number
    expect(parseLatLon('FWJ9+PP')).toBeNull(); // a plus code, not coords
    expect(parseLatLon('')).toBeNull();
  });
});
