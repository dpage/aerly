import { describe, it, expect } from 'vitest';
import { canRouteTo, directionsUrl } from './directions';

describe('canRouteTo', () => {
  it('is true with coordinates', () => {
    expect(canRouteTo({ lat: 48.1, lon: 16.5 })).toBe(true);
  });
  it('is true with a non-blank query', () => {
    expect(canRouteTo({ query: 'Calella' })).toBe(true);
  });
  it('is false with neither (or a blank query / half-coords)', () => {
    expect(canRouteTo({})).toBe(false);
    expect(canRouteTo({ query: '   ' })).toBe(false);
    expect(canRouteTo({ lat: 48.1 })).toBe(false); // lon missing
  });
});

describe('directionsUrl (coordinates preferred)', () => {
  const t = { lat: 48.1103, lon: 16.5697, query: 'Vienna Airport' };

  it('builds an Apple Maps driving link to the coordinates', () => {
    expect(directionsUrl('apple', t)).toBe(
      'https://maps.apple.com/?daddr=48.1103,16.5697&dirflg=d',
    );
  });
  it('builds a Google Maps directions link to the coordinates', () => {
    expect(directionsUrl('google', t)).toBe(
      'https://www.google.com/maps/dir/?api=1&destination=48.1103,16.5697',
    );
  });
  it('builds a Waze navigate link to the coordinates', () => {
    expect(directionsUrl('waze', t)).toBe('https://waze.com/ul?ll=48.1103,16.5697&navigate=yes');
  });
});

describe('directionsUrl (query fallback, URL-encoded)', () => {
  const t = { query: 'Hotel President, Calella' };

  it('encodes the query for Apple Maps', () => {
    expect(directionsUrl('apple', t)).toBe(
      'https://maps.apple.com/?daddr=Hotel%20President%2C%20Calella&dirflg=d',
    );
  });
  it('encodes the query for Google Maps', () => {
    expect(directionsUrl('google', t)).toBe(
      'https://www.google.com/maps/dir/?api=1&destination=Hotel%20President%2C%20Calella',
    );
  });
  it('uses Waze search (q=) for a text destination', () => {
    expect(directionsUrl('waze', t)).toBe(
      'https://waze.com/ul?q=Hotel%20President%2C%20Calella&navigate=yes',
    );
  });
});
