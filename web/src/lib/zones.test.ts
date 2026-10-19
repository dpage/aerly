import { afterEach, describe, expect, it, vi } from 'vitest';

import { canonicalZone, ZONES } from './zones';

describe('ZONES', () => {
  it('always lists UTC', () => {
    expect(ZONES).toContain('UTC');
    expect(ZONES).toContain('Europe/London');
  });
});

describe('loadZones fallbacks', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('falls back to UTC alone when the runtime cannot list zones', async () => {
    vi.spyOn(
      Intl as unknown as { supportedValuesOf: () => string[] },
      'supportedValuesOf',
    ).mockImplementation(() => {
      throw new Error('unsupported');
    });
    vi.resetModules();
    const { ZONES: zones } = await import('./zones');
    expect(zones).toEqual(['UTC']);
  });
});

describe('canonicalZone', () => {
  it('spells a listed zone as the list does, whatever the case', () => {
    expect(canonicalZone('europe/london')).toBe('Europe/London');
    expect(canonicalZone('  ASIA/TASHKENT ')).toBe('Asia/Tashkent');
    expect(canonicalZone('utc')).toBe('UTC');
  });

  it('falls back to the browser spelling for a zone the list leaves out', () => {
    expect(canonicalZone('etc/gmt+5')).toBe('Etc/GMT+5');
  });

  it('is null for blank text or a name that is not a zone', () => {
    expect(canonicalZone('')).toBeNull();
    expect(canonicalZone('   ')).toBeNull();
    expect(canonicalZone('Mars/Olympus')).toBeNull();
  });
});
