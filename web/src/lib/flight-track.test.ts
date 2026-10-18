import { describe, it, expect } from 'vitest';

import { fmtScrubTime } from './flight-track';

describe('fmtScrubTime', () => {
  it("labels the scrub time in the viewer's zone with its offset", () => {
    expect(fmtScrubTime(Date.parse('2026-10-18T09:00:00Z'), 'Europe/London')).toBe('Sun 18 Oct, 10:00 UTC+1');
    expect(fmtScrubTime(Number.NaN, 'Europe/London')).toBe('');
  });

  it('defaults to the viewer zone', () => {
    expect(fmtScrubTime(Date.parse('2026-10-18T09:00:00Z'))).not.toBe('');
  });
});
