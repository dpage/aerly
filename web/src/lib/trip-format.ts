// Display + classification helpers for the trip list and timeline (spec §11,
// PRD §6.1/§6.2). Pure functions, unit-tested in trip-format.test.ts.

import type { Plan, PlanPart, PlanType, Trip } from '../api/types';

/** Which home-screen group a trip falls under. */
export type TripBucket = 'upcoming' | 'now' | 'past';

/** The effective time span of a trip, as epoch millis. A bound is `null` when
 * it can't be derived from parts or `starts_on`/`ends_on`. */
export interface TripSpan {
  start: number | null;
  end: number | null;
}

/** Compute a trip's effective span: the min/max of its parts' instants,
 * falling back to `starts_on`/`ends_on` (parsed as UTC midnight). Trips with
 * neither parts nor fixed dates get `{ start: null, end: null }`.
 *
 * `plans` is optional because the trip-list payload (`/api/trips`) carries no
 * parts — there we fall back to the date columns. The detail payload does
 * carry plans, so the timeline / classification can use the richer span. */
export function tripSpan(trip: Trip, plans?: Plan[]): TripSpan {
  const instants: number[] = [];
  for (const plan of plans ?? []) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      const s = parseInstant(part.effective_at ?? part.starts_at);
      if (s != null) instants.push(s);
      const e = parseInstant(part.ends_at);
      if (e != null) instants.push(e);
    }
  }
  if (instants.length > 0) {
    return { start: Math.min(...instants), end: Math.max(...instants) };
  }
  // No parts in this payload: prefer the explicit dates, then the span the
  // server inferred from the trip's parts (the list payload carries no parts).
  // A date-only end is the *last day* of the trip, through which it's still
  // ongoing — so extend it to the end of that day. Without this a trip ending
  // today reads as "past" the moment UTC midnight rolls over, mis-filing a
  // still-in-progress trip under Past (issue #29).
  const start = parseDateOnly(trip.starts_on) ?? parseDateOnly(trip.effective_start);
  const end = parseDateOnlyEnd(trip.ends_on) ?? parseDateOnlyEnd(trip.effective_end);
  return { start, end };
}

/** Classify a trip into Upcoming / Happening now / Past against `now`.
 *
 * - wholly in the future (starts after now) → upcoming
 * - spans now (started, not yet ended) → now
 * - wholly in the past (ended before now) → past
 * - date-less (no derivable span) → upcoming (PRD §6.1: they sort under it). */
export function classifyTrip(span: TripSpan, now: number = Date.now()): TripBucket {
  const { start, end } = span;
  if (start == null && end == null) return 'upcoming';
  // An end with no start: treat the end as both bounds.
  const lo = start ?? end!;
  const hi = end ?? start!;
  if (lo > now) return 'upcoming';
  if (hi < now) return 'past';
  return 'now';
}

/** Format a trip's date range for a card subtitle, e.g. "12–18 Oct 2026" or
 * "Oct 2026" (no fixed dates). Uses UTC so YYYY-MM-DD columns render on the
 * day the user typed regardless of runtime locale. */
export function fmtTripDates(trip: Trip): string {
  // Explicit dates win; otherwise fall back to the span inferred from the
  // trip's plans, marked with "~" so it reads as a guess. Only "Dates to be
  // decided" when there's nothing to go on at all.
  const explicit = Boolean(trip.starts_on || trip.ends_on);
  const s = trip.starts_on ?? trip.effective_start;
  const e = trip.ends_on ?? trip.effective_end;
  if (!s && !e) return 'Dates to be decided';
  const prefix = explicit ? '' : '~';
  if (s && !e) return `${prefix}${fmtDay(s)}`;
  if (!s && e) return `until ${fmtDay(e)}`;
  return `${prefix}${fmtDay(s!)} – ${fmtDay(e!)}`;
}

/** True when the trip has explicit dates and at least one (non-dismissed) part
 * falls outside them — so the UI can flag a likely mistake. Compares the part's
 * local day (in its own tz) against the trip's YYYY-MM-DD bounds. */
export function plansOutsideTripDates(trip: Trip, plans: Plan[]): boolean {
  if (!trip.starts_on && !trip.ends_on) return false;
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      const startDay = localDayKey(part.effective_at ?? part.starts_at, part.start_tz);
      if (trip.starts_on && startDay < trip.starts_on) return true;
      const endDay = localDayKey(part.ends_at ?? part.starts_at, part.end_tz || part.start_tz);
      if (trip.ends_on && endDay > trip.ends_on) return true;
    }
  }
  return false;
}

function fmtDay(dateOnly: string): string {
  const d = new Date(`${dateOnly}T00:00:00Z`);
  if (Number.isNaN(d.getTime())) return dateOnly;
  return d.toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    timeZone: 'UTC',
  });
}

/** A part annotated with its parent plan, for timeline rendering. */
export interface TimelinePart {
  part: PlanPart;
  plan: Plan;
  /** For a multi-night hotel stay, which end of the stay this tile marks — so
   * the stay shows a check-in tile on its first day and a check-out tile on its
   * last day. Undefined for every other part (and same-day stays), which render
   * as a single tile. */
  edge?: 'check-in' | 'check-out';
}

/** A single day's worth of timeline parts under one local-day header. */
export interface TimelineDay {
  /** YYYY-MM-DD key in the part's own local tz; used for the sticky header. */
  dayKey: string;
  /** Human header label, e.g. "Mon 12 Oct 2026". */
  label: string;
  parts: TimelinePart[];
}

/** Build the day-grouped, chronologically-sorted timeline from a trip's plans.
 *
 * - Dismissed parts are dropped entirely (PRD §6.2).
 * - Superseded-but-not-dismissed parts stay (the page greys them).
 * - Parts sort by `effective_at`; days group by the local calendar day in the
 *   part's `start_tz` so a red-eye lands on its departure day's header and the
 *   header reads in the local time of where it happens. */
export function buildTimeline(plans: Plan[]): TimelineDay[] {
  // Each entry carries the instant + iso/tz used to place and sort it, so a
  // multi-night hotel can contribute two entries: a check-in on its first day
  // and a check-out on its last day.
  interface Entry {
    tp: TimelinePart;
    instant: number;
    iso: string;
    tz?: string;
  }
  const flat: Entry[] = [];
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      if (isHotelBand(part) && part.ends_at) {
        flat.push({
          tp: { part, plan, edge: 'check-in' },
          // Sort by the smart check-in (effective_at: after the inbound
          // flight's arrival) so the stay doesn't jump ahead of the flight
          // that gets you there — matching the map's ordering. Keep the day
          // bucket on the booked check-in date (iso = starts_at).
          instant: instantOf(part),
          iso: part.starts_at,
          tz: part.start_tz,
        });
        flat.push({
          tp: { part, plan, edge: 'check-out' },
          instant: parseInstant(part.ends_at) ?? 0,
          iso: part.ends_at,
          tz: part.end_tz || part.start_tz,
        });
      } else {
        const iso = part.effective_at ?? part.starts_at;
        flat.push({ tp: { part, plan }, instant: instantOf(part), iso, tz: part.start_tz });
      }
    }
  }
  flat.sort((a, b) => a.instant - b.instant);

  const days = new Map<string, TimelineDay>();
  for (const e of flat) {
    const key = localDayKey(e.iso, e.tz);
    let day = days.get(key);
    if (!day) {
      day = { dayKey: key, label: fmtDayHeader(e.iso, e.tz), parts: [] };
      days.set(key, day);
    }
    day.parts.push(e.tp);
  }
  return [...days.values()];
}

/** True when a part represents a multi-night hotel stay that should render as
 * a band rather than two points (PRD §6.2): a hotel with an end on a later
 * local day than its start. */
export function isHotelBand(part: PlanPart): boolean {
  if (part.type !== 'hotel' || !part.ends_at) return false;
  const startDay = localDayKey(part.starts_at, part.start_tz);
  const endDay = localDayKey(part.ends_at, part.end_tz || part.start_tz);
  return endDay > startDay;
}

/** Nights covered by a hotel band, for the "N nights" label. */
export function hotelNights(part: PlanPart): number {
  if (!part.ends_at) return 0;
  const start = parseInstant(part.starts_at);
  const end = parseInstant(part.ends_at);
  if (start == null || end == null) return 0;
  const ms = end - start;
  return Math.max(1, Math.round(ms / (24 * 60 * 60 * 1000)));
}

/** A time-of-day in the given tz, e.g. "14:30". 24-hour for determinism, same
 * convention as `fmtDateTime`. */
export function fmtTimeOfDay(iso: string, tz?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const base = d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: tz || 'UTC',
  });
  // Always carry the local tz abbreviation so every plan reads consistently in
  // local time (PRD §6.2) — falling back to "UTC" when the zone is unknown.
  return `${base} ${tzAbbrev(iso, tz)}`;
}

/** The local timezone abbreviation for an instant in a tz, e.g. "BST", "EDT",
 * "UTC". Falls back to "UTC" when the tz is unknown (the instant is stored UTC
 * and the digits are the wall-clock the booking stated). */
export function tzAbbrev(iso: string, tz?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: tz || 'UTC',
    timeZoneName: 'short',
  }).formatToParts(d);
  return parts.find((p) => p.type === 'timeZoneName')?.value ?? 'UTC';
}

/** A full local date + time + tz abbreviation for a marker tooltip, e.g.
 * "Sun 25 Oct, 16:00 BST". Empty for an unparseable instant. */
export function fmtLocalDateTime(iso: string, tz?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const date = d.toLocaleDateString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    timeZone: tz || 'UTC',
  });
  const time = d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: tz || 'UTC',
  });
  return `${date}, ${time} ${tzAbbrev(iso, tz)}`;
}

/** Split an instant into its local date ("YYYY-MM-DD") + time ("HH:MM") in the
 * given tz, for date/time form inputs. Empty strings for an unparseable iso. */
export function splitLocal(iso: string, tz?: string): { date: string; time: string } {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return { date: '', time: '' };
  const zone = tz || 'UTC';
  const date = d.toLocaleDateString('en-CA', { timeZone: zone });
  const time = d.toLocaleTimeString('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
    timeZone: zone,
  });
  return { date, time };
}

/** Combine a local date ("YYYY-MM-DD") + time ("HH:MM") interpreted in tz into a
 * UTC instant (ISO string) — the inverse of splitLocal. Handles DST via the
 * zone's offset at that wall-clock. Returns "" for a malformed date. */
export function zonedTimeToUtc(date: string, time: string, tz?: string): string {
  const [y, mo, d] = date.split('-').map(Number);
  const [h, mi] = (time || '00:00').split(':').map(Number);
  if (!y || !mo || !d) return '';
  const guess = Date.UTC(y, mo - 1, d, h || 0, mi || 0);
  return new Date(guess - tzOffsetMs(tz || 'UTC', guess)).toISOString();
}

/** The offset (ms) of tz at the given UTC instant: how far the zone's local
 * wall-clock is ahead of UTC. Used to invert a local time back to an instant. */
function tzOffsetMs(tz: string, utcMs: number): number {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: tz,
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
    .formatToParts(new Date(utcMs))
    .reduce<Record<string, string>>((a, p) => {
      a[p.type] = p.value;
      return a;
    }, {});
  const asUTC = Date.UTC(
    +parts.year,
    +parts.month - 1,
    +parts.day,
    +parts.hour,
    +parts.minute,
    +parts.second,
  );
  return asUTC - utcMs;
}

/** A part's local-time range: "14:30" for an instant, "14:30 → 18:05" when it
 * has an end. Ends render in their own tz so a flight reads in arrival-local. */
export function fmtPartTimeRange(part: PlanPart): string {
  const start = fmtTimeOfDay(part.starts_at, part.start_tz);
  if (!part.ends_at) return start;
  const end = fmtTimeOfDay(part.ends_at, part.end_tz || part.start_tz);
  return `${start} → ${end}`;
}

// --- internals --------------------------------------------------------------

function instantOf(part: PlanPart): number {
  return parseInstant(part.effective_at ?? part.starts_at) ?? 0;
}

function parseInstant(iso?: string): number | null {
  if (!iso) return null;
  const t = new Date(iso).getTime();
  return Number.isNaN(t) ? null : t;
}

function parseDateOnly(dateOnly?: string): number | null {
  if (!dateOnly) return null;
  const t = new Date(`${dateOnly}T00:00:00Z`).getTime();
  return Number.isNaN(t) ? null : t;
}

/** Like parseDateOnly but returns the *end* of the given day (the start of the
 * next UTC day) — the inclusive upper bound for a date-only trip end, so a trip
 * stays current through its whole last day rather than expiring at midnight. */
function parseDateOnlyEnd(dateOnly?: string): number | null {
  const t = parseDateOnly(dateOnly);
  return t == null ? null : t + 24 * 60 * 60 * 1000;
}

/** A sortable YYYY-MM-DD key for an instant in the given tz. Uses en-CA which
 * formats as ISO-8601 dates, so string comparison orders chronologically. */
function localDayKey(iso: string, tz?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString('en-CA', { timeZone: tz || 'UTC' });
}

function fmtDayHeader(iso: string, tz?: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    timeZone: tz || 'UTC',
  });
}

const TRANSFER_TYPES = new Set<PlanType>(['flight', 'train', 'ground']);

/** Point-to-point types that go from one place to another. Others (hotel,
 * dining, excursion) happen at a single place. */
export function isTransferType(type: PlanType): boolean {
  return TRANSFER_TYPES.has(type);
}

/** Types that carry a distinct end the user can set: transfers (an arrival)
 * and hotels (a check-out). Single-place types (dining, excursion) have only a
 * start. Drives which dialogs offer an end date/time. */
export function typeHasEnd(type: PlanType): boolean {
  return isTransferType(type) || type === 'hotel';
}

/** The place line for a part: "A → B" for a transfer between two distinct
 * places, otherwise just the single venue — never "X → X" (a hotel's start and
 * end label are both the property, which shouldn't read like a flight). */
export function fmtPartPlaces(type: PlanType, startLabel?: string, endLabel?: string): string {
  const start = (startLabel ?? '').trim();
  const end = (endLabel ?? '').trim();
  if (isTransferType(type) && end && end !== start) return `${start} → ${end}`;
  return start || end;
}

/** Display label for a plan type, e.g. "Hotel", "Ground transport". */
export function planTypeLabel(type: PlanType): string {
  switch (type) {
    case 'flight':
      return 'Flight';
    case 'train':
      return 'Train';
    case 'hotel':
      return 'Hotel';
    case 'ground':
      return 'Ground transport';
    case 'dining':
      return 'Dining';
    case 'excursion':
      return 'Excursion';
    default:
      return type;
  }
}
