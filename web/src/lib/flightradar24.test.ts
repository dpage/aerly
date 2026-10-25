import { describe, expect, it } from 'vitest';

import { flightradar24Url } from './flightradar24';

const DATA = 'https://www.flightradar24.com/data/flights/';
const LIVE = 'https://www.flightradar24.com/';

describe('flightradar24Url', () => {
  it('links an IATA flight number to its history page, lower-cased', () => {
    expect(flightradar24Url({ ident: 'TK1' })).toBe(`${DATA}tk1`);
    expect(flightradar24Url({ ident: 'tk1' })).toBe(`${DATA}tk1`);
    expect(flightradar24Url({ ident: 'U21234' })).toBe(`${DATA}u21234`);
  });

  it('strips spaces', () => {
    expect(flightradar24Url({ ident: 'TK 1' })).toBe(`${DATA}tk1`);
    expect(flightradar24Url({ ident: 'LH 400' })).toBe(`${DATA}lh400`);
  });

  it('strips leading zeros from the number', () => {
    expect(flightradar24Url({ ident: 'BA0117' })).toBe(`${DATA}ba117`);
    expect(flightradar24Url({ ident: 'BA117' })).toBe(`${DATA}ba117`);
  });

  it('keeps an operational suffix', () => {
    expect(flightradar24Url({ ident: 'BA286A' })).toBe(`${DATA}ba286a`);
  });

  it('prefers the IATA number over a callsign', () => {
    expect(flightradar24Url({ ident: 'BA117', callsign: 'BAW117' })).toBe(`${DATA}ba117`);
  });

  it('links an ICAO-form ident to the live map', () => {
    expect(flightradar24Url({ ident: 'THY1' })).toBe(`${LIVE}THY1`);
    expect(flightradar24Url({ ident: 'thy 001' })).toBe(`${LIVE}THY1`);
  });

  it('falls back to the callsign as-is when there is no usable ident', () => {
    expect(flightradar24Url({ ident: '', callsign: 'THY1' })).toBe(`${LIVE}THY1`);
    expect(flightradar24Url({ ident: 'nonsense', callsign: ' ezy12ab ' })).toBe(`${LIVE}EZY12AB`);
  });

  it('returns null with no usable ident or callsign', () => {
    for (const f of [
      { ident: '' },
      { ident: '', callsign: '' },
      { ident: 'GIBBERISH', callsign: null },
      { ident: 'BA-286', callsign: 'B-AW1' },
      { ident: '12345', callsign: 'BAW12345' },
      { ident: undefined as unknown as string },
    ]) {
      expect(flightradar24Url(f), JSON.stringify(f)).toBeNull();
    }
  });
});
