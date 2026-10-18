import type { TimeBlock, TZSource } from '../api/types';

// Every time is spelt in British English whatever the browser's locale, so a
// shared screenshot reads the same for everyone and tests are deterministic.
const LOCALE = 'en-GB';

let viewerZoneOverride: string | null = null;

/** The zone the person looking at the screen is in: the browser's, unless
 * overridden. The time zone redesign's PR 4 overrides it with the user's home
 * zone, so nothing else may read the browser zone directly. */
export function viewerZone(): string {
  return viewerZoneOverride ?? Intl.DateTimeFormat().resolvedOptions().timeZone ?? 'UTC';
}

/** Override the viewer's zone; null goes back to the browser's. */
export function setViewerZone(tz: string | null): void {
  viewerZoneOverride = tz;
}

function instant(at: string | Date): Date | null {
  const d = typeof at === 'string' ? new Date(at) : at;
  return Number.isNaN(d.getTime()) ? null : d;
}

// A blank zone is a floating or unknown time: its UTC digits are what we show.
function zoneOf(b: TimeBlock): string {
  return b.tz || 'UTC';
}

function dateIn(d: Date, tz: string): string {
  return new Intl.DateTimeFormat(LOCALE, {
    timeZone: tz,
    weekday: 'short',
    day: 'numeric',
    month: 'short',
  }).format(d);
}

function clockIn(d: Date, tz: string): string {
  return new Intl.DateTimeFormat(LOCALE, {
    timeZone: tz,
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(d);
}

/** The UTC offset of tz at an instant: "UTC" at zero, else "UTC+5",
 * "UTC+5:30", "UTC-3:30". Empty for an unparseable instant. */
export function offsetLabel(at: string | Date, tz: string): string {
  const d = instant(at);
  if (!d) return '';
  const name =
    new Intl.DateTimeFormat(LOCALE, { timeZone: tz || 'UTC', timeZoneName: 'shortOffset' })
      .formatToParts(d)
      .find((p) => p.type === 'timeZoneName')?.value ?? 'GMT';
  // en-GB spells zero as "GMT+0" on current ICU and "GMT" on older builds.
  const off = name.replace(/^GMT/, 'UTC');
  return off === 'UTC+0' || off === 'UTC-0' ? 'UTC' : off;
}

// A derived arrival is worked out from the departure delay, not reported, so
// it is marked as approximate wherever it is shown.
function approx(b: TimeBlock): string {
  return b.kind === 'derived' ? '≈' : '';
}

/** The clock time alone in the block's zone, e.g. "14:00" or "≈16:07". */
export function fmtTime(b: TimeBlock): string {
  const d = instant(b.at);
  return d ? `${approx(b)}${clockIn(d, zoneOf(b))}` : '';
}

/** The day in the block's zone, e.g. "Sun 18 Oct". */
export function fmtDate(b: TimeBlock): string {
  const d = instant(b.at);
  return d ? dateIn(d, zoneOf(b)) : '';
}

/** The full form for detail views: "Sun 18 Oct, 14:00 Tashkent (UTC+5)", the
 * offset alone when there is no place, and no label at all with no zone. */
export function fmtFull(b: TimeBlock): string {
  const d = instant(b.at);
  if (!d) return '';
  const head = `${dateIn(d, zoneOf(b))}, ${fmtTime(b)}`;
  if (!b.tz) return head;
  const off = offsetLabel(d, b.tz);
  return b.place ? `${head} ${b.place} (${off})` : `${head} ${off}`;
}

/** The compact form for tiles: "14:00 UTC+5", or "14:00" with no zone. */
export function fmtCompact(b: TimeBlock): string {
  const d = instant(b.at);
  if (!d) return '';
  return b.tz ? `${fmtTime(b)} ${offsetLabel(d, b.tz)}` : fmtTime(b);
}

/** The block's moment in the viewer's zone, "Sun 18 Oct, 10:00 (UTC+1)", or
 * null when it would add nothing: no zone to compare against, or the viewer's
 * offset is the same as the block's at that instant. */
export function fmtViewer(b: TimeBlock, viewerTz: string = viewerZone()): string | null {
  const d = instant(b.at);
  if (!d || !b.tz) return null;
  const off = offsetLabel(d, viewerTz);
  if (off === offsetLabel(d, b.tz)) return null;
  return `${dateIn(d, viewerTz)}, ${clockIn(d, viewerTz)} (${off})`;
}

/** A sortable "YYYY-MM-DD" for the block's day in its own zone. en-CA formats
 * dates as ISO 8601, so string order is date order. */
export function dayKey(b: TimeBlock): string {
  const d = instant(b.at);
  return d ? new Intl.DateTimeFormat('en-CA', { timeZone: zoneOf(b) }).format(d) : b.at;
}

/** A timeline day heading in the block's zone, e.g. "Sun, 18 Oct 2026". */
export function fmtDayHeading(b: TimeBlock): string {
  const d = instant(b.at);
  if (!d) return b.at;
  return new Intl.DateTimeFormat(LOCALE, {
    timeZone: zoneOf(b),
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  }).format(d);
}

/** True when the block's zone is unknown or floating ("needs a time zone"). */
export function needsZone(b?: TimeBlock): boolean {
  return b != null && (b.tz_source === 'unknown' || b.tz_source === 'floating');
}

/** True when the block's zone was borrowed from a neighbour, not confirmed. */
export function zoneGuessed(b?: TimeBlock): boolean {
  return b != null && b.tz_source === 'inferred';
}

/** True when the block is a reported or worked-out time that differs from the
 * timetable by at least a minute. */
export function isRevised(b?: TimeBlock): boolean {
  return b != null && b.kind !== 'scheduled' && b.delay_min !== 0;
}

/** A scheduled block for a time that has no server block (a feed event, an
 * import proposal). A blank zone is floating; a zone with no source, confirmed. */
export function blockFor(at: string, tz = '', tzSource?: TZSource): TimeBlock {
  return {
    at,
    tz,
    tz_source: tzSource ?? (tz ? 'confirmed' : 'floating'),
    place: '',
    kind: 'scheduled',
    delay_min: 0,
  };
}

/** Whole clock minutes from start to end, or null with no end, an unparseable
 * instant, or an end that is not after the start. */
export function durationMin(start: TimeBlock, end?: TimeBlock): number | null {
  if (!end) return null;
  const s = instant(start.at);
  const e = instant(end.at);
  if (!s || !e) return null;
  const mins = Math.floor(e.getTime() / 60000) - Math.floor(s.getTime() / 60000);
  return mins > 0 ? mins : null;
}

/** "8h 5m", or "45m" under an hour. */
export function fmtDuration(mins: number): string {
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return h ? `${h}h ${m}m` : `${m}m`;
}

/** A Date's browser-local wall-clock digits as "YYYY-MM-DDTHH:MM", the form
 * the server's start_local/end_local fields take. */
export function localDigits(d: Date): string {
  const p = (n: number, w = 2) => String(n).padStart(w, '0');
  return `${p(d.getFullYear(), 4)}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}
