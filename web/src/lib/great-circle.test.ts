import { describe, it, expect } from 'vitest';

import { greatCircle, greatCircleMiles, toMultiLine } from './great-circle';

describe('greatCircle', () => {
  it('returns a single point for (near-)identical endpoints (Δ<1e-9 early return)', () => {
    const pts = greatCircle(51.5, -0.12, 51.5, -0.12);
    expect(pts).toEqual([[-0.12, 51.5]]);
  });

  it('samples steps+1 points for a normal non-crossing route', () => {
    const pts = greatCircle(51.47, -0.45, 40.64, -73.78, 8); // LHR -> JFK
    expect(pts).toHaveLength(9);
    expect(pts[0][0]).toBeCloseTo(-0.45, 1);
    expect(pts[0][1]).toBeCloseTo(51.47, 1);
    // No NaN sentinels for a route that doesn't cross the antimeridian.
    expect(pts.some(([lon]) => Number.isNaN(lon))).toBe(false);
  });

  it('inserts a NaN sentinel when crossing the antimeridian (Tokyo -> LA)', () => {
    const pts = greatCircle(35.55, 139.78, 33.94, -118.4, 64);
    const hasSentinel = pts.some(([lon, lat]) => Number.isNaN(lon) && Number.isNaN(lat));
    expect(hasSentinel).toBe(true);
  });

  it('inserts a NaN sentinel for a 170 -> -170 lon jump', () => {
    const pts = greatCircle(10, 170, 10, -170, 64);
    expect(pts.some(([lon]) => Number.isNaN(lon))).toBe(true);
  });
});

describe('toMultiLine', () => {
  it('splits on NaN sentinels and filters parts with <=1 point', () => {
    const coords: [number, number][] = [
      [1, 1],
      [2, 2],
      [NaN, NaN],
      [3, 3], // single-point part -> filtered out
      [NaN, NaN],
      [4, 4],
      [5, 5],
    ];
    const parts = toMultiLine(coords);
    expect(parts).toEqual([
      [
        [1, 1],
        [2, 2],
      ],
      [
        [4, 4],
        [5, 5],
      ],
    ]);
  });

  it('returns a single part when there is no discontinuity', () => {
    const parts = toMultiLine([
      [1, 1],
      [2, 2],
      [3, 3],
    ]);
    expect(parts).toHaveLength(1);
    expect(parts[0]).toHaveLength(3);
  });

  it('returns [] when nothing has more than one point', () => {
    expect(toMultiLine([[1, 1]])).toEqual([]);
    expect(toMultiLine([])).toEqual([]);
  });
});

describe('greatCircleMiles', () => {
  it('returns 0 for identical points', () => {
    expect(greatCircleMiles(51.47, -0.45, 51.47, -0.45)).toBe(0);
  });

  it('matches the LHR → JFK reference distance within 1%', () => {
    // LHR (51.4700, -0.4543) → JFK (40.6413, -73.7781) ≈ 3,451 mi
    const got = greatCircleMiles(51.47, -0.4543, 40.6413, -73.7781);
    expect(got).toBeGreaterThan(3420);
    expect(got).toBeLessThan(3480);
  });

  it('is symmetric', () => {
    const ab = greatCircleMiles(51.47, -0.4543, 40.6413, -73.7781);
    const ba = greatCircleMiles(40.6413, -73.7781, 51.47, -0.4543);
    expect(Math.abs(ab - ba)).toBeLessThan(0.001);
  });
});
