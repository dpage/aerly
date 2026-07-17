import { describe, expect, it, vi, beforeEach } from 'vitest';
import { resolveCoordsFromInput } from './resolve-coords';
import { api } from '../api/client';

vi.mock('../api/client', () => ({ api: { resolveMapsUrl: vi.fn() } }));

describe('resolveCoordsFromInput', () => {
  beforeEach(() => vi.resetAllMocks());

  it('reads an exact "lat, lng" locally without a request', async () => {
    const got = await resolveCoordsFromInput('51.5, -0.14');
    expect(got).toEqual({ lat: 51.5, lon: -0.14, needsConfirmation: false });
    expect(api.resolveMapsUrl).not.toHaveBeenCalled();
  });

  it('flags a geocoded link as needing confirmation', async () => {
    vi.mocked(api.resolveMapsUrl).mockResolvedValue({
      lat: 51.5, lon: -0.14, label: 'Test Hotel, London', needs_confirmation: true,
    });
    const got = await resolveCoordsFromInput('https://maps.app.goo.gl/abc');
    expect(got).toEqual({
      lat: 51.5, lon: -0.14, label: 'Test Hotel, London', needsConfirmation: true,
    });
  });

  it('does not flag coordinates read from the link itself', async () => {
    vi.mocked(api.resolveMapsUrl).mockResolvedValue({ lat: 51.5, lon: -0.14 });
    const got = await resolveCoordsFromInput('https://maps.app.goo.gl/abc');
    expect(got?.needsConfirmation).toBe(false);
  });

  it('returns null when the backend declines', async () => {
    vi.mocked(api.resolveMapsUrl).mockRejectedValue(new Error('422'));
    expect(await resolveCoordsFromInput('https://maps.app.goo.gl/abc')).toBeNull();
  });

  it('routes a place-only full Maps URL (no short link) to the backend too', async () => {
    // A "/maps/place/..." link carries no coordinates of its own but is not a
    // short link either; it must still reach the backend so its readable name
    // can be geocoded, rather than being dropped as unresolvable.
    vi.mocked(api.resolveMapsUrl).mockResolvedValue({
      lat: 48.85, lon: 2.29, label: 'Test Cafe, Example Street', needs_confirmation: true,
    });
    const got = await resolveCoordsFromInput('https://www.google.com/maps/place/Somewhere');
    expect(api.resolveMapsUrl).toHaveBeenCalledWith('https://www.google.com/maps/place/Somewhere');
    expect(got).toEqual({
      lat: 48.85, lon: 2.29, label: 'Test Cafe, Example Street', needsConfirmation: true,
    });
  });

  it('returns null for plain text that is not a Maps URL, without a request', async () => {
    expect(await resolveCoordsFromInput('somewhere near the shops')).toBeNull();
    expect(api.resolveMapsUrl).not.toHaveBeenCalled();
  });
});
