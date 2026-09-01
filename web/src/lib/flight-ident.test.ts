import { describe, expect, it } from 'vitest';

import { airlineOf, normaliseIdent, splitIdent } from './flight-ident';

describe('normaliseIdent', () => {
  it('upper-cases and strips whitespace', () => {
    expect(normaliseIdent('ba 286')).toBe('BA286');
    expect(normaliseIdent('  BA  286 ')).toBe('BA286');
    expect(normaliseIdent('')).toBe('');
  });
});

describe('splitIdent', () => {
  it('splits an ordinary two-letter designator, spaced or not', () => {
    expect(splitIdent('BA286')).toEqual({ airline: 'BA', number: '286', suffix: '' });
    expect(splitIdent('ba 286')).toEqual({ airline: 'BA', number: '286', suffix: '' });
  });

  // Issue #118: a letters-only prefix rule reads "U21234" as airline "U".
  it('keeps a digit that belongs to the designator', () => {
    expect(splitIdent('U21234')).toEqual({ airline: 'U2', number: '1234', suffix: '' });
    expect(splitIdent('9W420')).toEqual({ airline: '9W', number: '420', suffix: '' });
    expect(splitIdent('4U2678')).toEqual({ airline: '4U', number: '2678', suffix: '' });
  });

  it('strips leading zeros and separates the operational suffix', () => {
    expect(splitIdent('BA0087')).toEqual({ airline: 'BA', number: '87', suffix: '' });
    expect(splitIdent('BA286A')).toEqual({ airline: 'BA', number: '286', suffix: 'A' });
  });

  it('falls back to a three-letter ICAO designator', () => {
    expect(splitIdent('BAW123')).toEqual({ airline: 'BAW', number: '123', suffix: '' });
  });

  it('rejects what is not a flight number', () => {
    for (const s of ['', 'BA', '12345', '1234', 'BA0000', 'AC12345', 'GIBBERISH', 'BA-286']) {
      expect(splitIdent(s), s).toBeNull();
    }
  });
});

describe('airlineOf', () => {
  it('returns the designator, or null', () => {
    expect(airlineOf('U2 8021')).toBe('U2');
    expect(airlineOf('nonsense')).toBeNull();
  });
});
