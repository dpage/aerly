import { afterEach, describe, expect, it } from 'vitest';
import { block } from '../test/blocks';
import {
  blockFor,
  dayKey,
  durationMin,
  fmtCompact,
  fmtDate,
  fmtDayHeading,
  fmtDuration,
  fmtFull,
  fmtTime,
  fmtViewer,
  isRevised,
  localDigits,
  needsZone,
  offsetLabel,
  setViewerZone,
  viewerZone,
  zoneGuessed,
} from './time';

const TAS = block('2026-10-18T09:00:00Z', 'Asia/Tashkent', { place: 'Tashkent' });

afterEach(() => setViewerZone(null));

describe('offsetLabel', () => {
  it('reads plain UTC at offset zero', () => {
    expect(offsetLabel('2026-10-18T09:00:00Z', 'UTC')).toBe('UTC');
  });
  it('gives whole and half hours', () => {
    expect(offsetLabel('2026-10-18T09:00:00Z', 'Asia/Tashkent')).toBe('UTC+5');
    expect(offsetLabel('2026-10-18T09:00:00Z', 'Asia/Kolkata')).toBe('UTC+5:30');
  });
  it('follows a DST change on either side', () => {
    expect(offsetLabel('2026-10-24T09:00:00Z', 'Europe/London')).toBe('UTC+1');
    expect(offsetLabel('2026-10-26T09:00:00Z', 'Europe/London')).toBe('UTC');
  });
  it('is empty for an unparseable instant', () => {
    expect(offsetLabel('not a time', 'UTC')).toBe('');
  });
  it('gives negative and 45-minute offsets', () => {
    expect(offsetLabel('2026-01-15T12:00:00Z', 'America/St_Johns')).toBe('UTC-3:30');
    expect(offsetLabel('2026-01-15T12:00:00Z', 'Asia/Kathmandu')).toBe('UTC+5:45');
    expect(fmtFull(block('2026-01-15T12:00:00Z', 'Asia/Kathmandu'))).toBe('Thu 15 Jan, 17:45 UTC+5:45');
  });
  it('falls back to UTC for an invalid zone rather than throwing', () => {
    expect(offsetLabel('2026-10-18T09:00:00Z', 'Not/AZone')).toBe('UTC');
    const bad = block('2026-10-18T09:00:00Z', 'Not/AZone');
    expect(fmtTime(bad)).toBe('09:00');
    expect(fmtDate(bad)).toBe('Sun 18 Oct');
    expect(fmtFull(bad)).toBe('Sun 18 Oct, 09:00 UTC');
    expect(fmtCompact(bad)).toBe('09:00 UTC');
    expect(dayKey(bad)).toBe('2026-10-18');
    expect(fmtDayHeading(bad)).toBe('Sun, 18 Oct 2026');
    expect(fmtViewer(TAS, 'Not/AZone')).toBe('Sun 18 Oct, 09:00 (UTC)');
  });
});

describe('fmtFull and fmtCompact', () => {
  it('names the place and offset', () => {
    expect(fmtFull(TAS)).toBe('Sun 18 Oct, 14:00 Tashkent (UTC+5)');
    expect(fmtCompact(TAS)).toBe('14:00 UTC+5');
    expect(fmtDate(TAS)).toBe('Sun 18 Oct');
    expect(fmtTime(TAS)).toBe('14:00');
  });
  it('gives the offset alone with no place', () => {
    expect(fmtFull(block('2026-10-18T09:00:00Z', 'Asia/Kolkata'))).toBe('Sun 18 Oct, 14:30 UTC+5:30');
  });
  it('renders the UTC digits with no label when the time is floating', () => {
    const b = block('2026-10-18T14:00:00Z', '');
    setViewerZone('Asia/Tokyo');
    expect(fmtFull(b)).toBe('Sun 18 Oct, 14:00');
    expect(fmtCompact(b)).toBe('14:00');
    expect(fmtTime(b)).toBe('14:00');
    expect(fmtDate(b)).toBe('Sun 18 Oct');
  });
  // The unknown cases mirror internal/timing/label_test.go (TestLabels).
  describe('an unknown block', () => {
    const unknown = (over = {}) =>
      block('2026-10-18T09:00:00Z', '', { tz_source: 'unknown', place: 'Ignored', ...over });
    it('reads in the viewer zone, with an offset', () => {
      setViewerZone('Asia/Tokyo');
      expect(fmtFull(unknown())).toBe('Sun 18 Oct, 18:00 UTC+9');
      expect(fmtCompact(unknown())).toBe('18:00 UTC+9');
      expect(fmtTime(unknown())).toBe('18:00');
      expect(fmtDate(unknown({ at: '2026-10-18T22:30:00Z' }))).toBe('Mon 19 Oct');
      expect(fmtDayHeading(unknown({ at: '2026-10-18T22:30:00Z' }))).toBe('Mon, 19 Oct 2026');
    });
    it('ignores the place', () => {
      setViewerZone('Europe/London');
      expect(fmtFull(unknown())).toBe('Sun 18 Oct, 10:00 UTC+1');
    });
    it('reads in UTC with an offset when the viewer zone is not a zone', () => {
      setViewerZone('Mars/Olympus');
      expect(fmtFull(unknown())).toBe('Sun 18 Oct, 09:00 UTC');
      expect(fmtCompact(unknown())).toBe('09:00 UTC');
    });
    it('marks a derived time and treats a blank source the same', () => {
      setViewerZone('Europe/London');
      expect(fmtCompact(unknown({ kind: 'derived' }))).toBe('≈10:00 UTC+1');
      expect(fmtCompact(unknown({ tz_source: 'confirmed' }))).toBe('10:00 UTC+1');
    });
    it('has no separate viewer time', () => {
      setViewerZone('Asia/Tokyo');
      expect(fmtViewer(unknown(), 'Asia/Tokyo')).toBeNull();
    });
    it('keys its day in the viewer zone', () => {
      setViewerZone('Asia/Tokyo');
      expect(dayKey(unknown({ at: '2026-10-18T22:30:00Z' }))).toBe('2026-10-19');
      setViewerZone('Europe/London');
      expect(dayKey(unknown({ at: '2026-10-18T22:30:00Z' }))).toBe('2026-10-18');
    });
  });
  it('marks a derived time as approximate', () => {
    const b = block('2026-10-18T11:07:00Z', 'Asia/Tashkent', { kind: 'derived', delay_min: 22 });
    expect(fmtCompact(b)).toBe('≈16:07 UTC+5');
  });
  it('reads the same instant either side of a DST change', () => {
    expect(fmtCompact(block('2026-10-24T09:00:00Z', 'Europe/London'))).toBe('10:00 UTC+1');
    expect(fmtCompact(block('2026-10-26T09:00:00Z', 'Europe/London'))).toBe('09:00 UTC');
  });
  it('is empty for an unparseable instant', () => {
    expect(fmtFull(block('nope'))).toBe('');
    expect(fmtCompact(block('nope'))).toBe('');
  });
});

describe('fmtViewer', () => {
  it("gives the viewer's time when the offset differs", () => {
    expect(fmtViewer(TAS, 'Europe/London')).toBe('Sun 18 Oct, 10:00 (UTC+1)');
  });
  it('is null when the offsets match at that instant', () => {
    expect(fmtViewer(TAS, 'Asia/Samarkand')).toBeNull();
    expect(fmtViewer(block('2026-07-01T09:00:00Z', 'Europe/London'), 'Europe/London')).toBeNull();
  });
  it('is null with no zone to compare', () => {
    expect(fmtViewer(block('2026-10-18T14:00:00Z', ''), 'Europe/London')).toBeNull();
  });
  it('uses the injectable viewer zone by default', () => {
    setViewerZone('America/New_York');
    expect(viewerZone()).toBe('America/New_York');
    expect(fmtViewer(TAS)).toBe('Sun 18 Oct, 05:00 (UTC-4)');
  });
  it('falls back to the browser zone when not overridden', () => {
    expect(viewerZone()).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone);
  });
});

describe('days', () => {
  it("keys and heads a day in the block's zone", () => {
    const late = block('2026-10-18T20:00:00Z', 'Asia/Tashkent');
    expect(dayKey(late)).toBe('2026-10-19');
    expect(fmtDayHeading(TAS)).toBe('Sun, 18 Oct 2026');
  });
  it('keys a floating time by its digits', () => {
    expect(dayKey(block('2026-10-18T23:30:00Z', ''))).toBe('2026-10-18');
  });
});

describe('markers', () => {
  it('flags unknown and floating zones', () => {
    expect(needsZone(block('2026-10-18T09:00:00Z', ''))).toBe(true);
    expect(needsZone(block('2026-10-18T09:00:00Z', '', { tz_source: 'unknown' }))).toBe(true);
    expect(needsZone(TAS)).toBe(false);
    expect(needsZone(undefined)).toBe(false);
  });
  it('flags a guessed zone', () => {
    expect(zoneGuessed({ ...TAS, tz_source: 'inferred' })).toBe(true);
    expect(zoneGuessed(TAS)).toBe(false);
    expect(zoneGuessed(undefined)).toBe(false);
  });
  it('treats a delayed estimate as revised, and a timetable time as not', () => {
    expect(isRevised({ ...TAS, kind: 'estimated', delay_min: 25 })).toBe(true);
    expect(isRevised({ ...TAS, kind: 'estimated', delay_min: 0 })).toBe(false);
    expect(isRevised(TAS)).toBe(false);
    expect(isRevised(undefined)).toBe(false);
  });
});

describe('blockFor', () => {
  it('builds a scheduled block, floating with no zone', () => {
    expect(blockFor('2026-10-18T09:00:00Z')).toEqual(block('2026-10-18T09:00:00Z', ''));
    expect(blockFor('2026-10-18T09:00:00Z', 'Asia/Tashkent', 'inferred').tz_source).toBe('inferred');
    expect(blockFor('2026-10-18T09:00:00Z', 'Asia/Tashkent').tz_source).toBe('confirmed');
  });
});

describe('durations', () => {
  it('measures end minus start across zones', () => {
    const lhr = block('2026-10-18T09:00:00Z', 'Europe/London');
    const jfk = block('2026-10-18T17:05:00Z', 'America/New_York');
    expect(durationMin(lhr, jfk)).toBe(485);
    expect(fmtDuration(485)).toBe('8h 5m');
    expect(fmtDuration(45)).toBe('45m');
    expect(fmtDuration(90)).toBe('1h 30m');
    expect(fmtDuration(120)).toBe('2h');
  });
  it('is null with no end, a bad instant, or an end not after the start', () => {
    const s = block('2026-10-18T09:00:00Z');
    expect(durationMin(s)).toBeNull();
    expect(durationMin(s, block('nope'))).toBeNull();
    expect(durationMin(s, s)).toBeNull();
  });
});

describe('localDigits', () => {
  it("gives a Date's browser-local wall-clock digits", () => {
    expect(localDigits(new Date(2026, 6, 1, 15, 0))).toBe('2026-07-01T15:00');
  });
});
