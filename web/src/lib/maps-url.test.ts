import { describe, it, expect } from 'vitest';

import { coordsFromText, extractLatLonFromMapsUrl, isMapsUrl } from './maps-url';

describe('extractLatLonFromMapsUrl', () => {
  it('prefers the !3d!4d place coordinates over the @ viewport', () => {
    const url =
      'https://www.google.com/maps/place/Eiffel/@48.8600,2.2900,17z/data=!3d48.8584!4d2.2945';
    expect(extractLatLonFromMapsUrl(url)).toEqual({ lat: 48.8584, lon: 2.2945 });
  });

  it('reads the @lat,lon viewport when no place data is present', () => {
    expect(extractLatLonFromMapsUrl('https://www.google.com/maps/@51.5074,-0.1278,15z')).toEqual({
      lat: 51.5074,
      lon: -0.1278,
    });
  });

  it('reads ?q=, ?ll= and &query= pairs', () => {
    expect(extractLatLonFromMapsUrl('https://maps.google.com/?q=40.7128,-74.006')).toEqual({
      lat: 40.7128,
      lon: -74.006,
    });
    expect(extractLatLonFromMapsUrl('https://maps.google.com/?ll=40.7128,-74.006')).toEqual({
      lat: 40.7128,
      lon: -74.006,
    });
    expect(
      extractLatLonFromMapsUrl('https://www.google.com/maps/search/?api=1&query=40.7128,-74.006'),
    ).toEqual({ lat: 40.7128, lon: -74.006 });
  });

  it('decodes percent-encoded commas in query coordinates', () => {
    expect(extractLatLonFromMapsUrl('https://maps.google.com/?q=40.7128%2C-74.006')).toEqual({
      lat: 40.7128,
      lon: -74.006,
    });
  });

  it('returns null when no coordinates are present (place-only)', () => {
    expect(extractLatLonFromMapsUrl('https://www.google.com/maps/place/Somewhere+Cafe')).toBeNull();
  });

  it('returns null for out-of-range values', () => {
    expect(extractLatLonFromMapsUrl('https://www.google.com/maps/@91,0,5z')).toBeNull();
    expect(extractLatLonFromMapsUrl('https://maps.google.com/?q=0,181')).toBeNull();
  });

  it('falls back to the raw URL when percent-decoding fails', () => {
    // A lone % makes decodeURIComponent throw; the raw string still has @coords.
    expect(extractLatLonFromMapsUrl('https://www.google.com/maps/@40.5,-70.25,15z?x=%')).toEqual({
      lat: 40.5,
      lon: -70.25,
    });
  });
});

describe('isMapsUrl', () => {
  it('accepts Google Maps and goo.gl hosts over https', () => {
    expect(isMapsUrl('https://www.google.com/maps/@1,2,3z')).toBe(true);
    expect(isMapsUrl('https://maps.google.com/?q=1,2')).toBe(true);
    expect(isMapsUrl('https://maps.app.goo.gl/abcD123')).toBe(true);
    expect(isMapsUrl('https://goo.gl/maps/abcD123')).toBe(true);
  });

  it('rejects non-Maps URLs and plain text', () => {
    expect(isMapsUrl('48.2105, 4.0823')).toBe(false);
    expect(isMapsUrl('https://example.com/maps')).toBe(false);
    expect(isMapsUrl('not a url')).toBe(false);
  });
});

describe('coordsFromText', () => {
  it('accepts a bare lat,lng pair', () => {
    expect(coordsFromText('48.2105, 4.0823')).toEqual({ lat: 48.2105, lon: 4.0823 });
  });

  it('falls back to extracting from a full Maps URL', () => {
    expect(coordsFromText('https://www.google.com/maps/@51.5,-0.12,15z')).toEqual({
      lat: 51.5,
      lon: -0.12,
    });
  });

  it('returns null for a short link (needs the backend resolver)', () => {
    expect(coordsFromText('https://maps.app.goo.gl/abcD123')).toBeNull();
  });
});
