// Display + classification helpers for the trip list and timeline (spec §11,
// PRD §6.1/§6.2). Pure functions, unit-tested in trip-format.test.ts.

import type {
  ExternalEvent,
  Plan,
  PlanPart,
  PlanType,
  TimeBlock,
  Trip,
  UpdatePlanPartInput,
} from '../api/types';
import {
  blockFor,
  dayKey,
  durationMin,
  fmtCompact,
  fmtDayHeading,
  fmtDuration,
  isRevised,
  needsZone,
} from './time';

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
      const s = parseInstant(startBlockOf(part).at);
      if (s != null) instants.push(s);
      const e = parseInstant(endBlockOf(part)?.at);
      if (e != null) instants.push(e);
    }
  }
  if (instants.length > 0) {
    return { start: Math.min(...instants), end: Math.max(...instants) };
  }
  // The server's span is in the first and last items' own zones, so a trip
  // ending in New York is still current through that evening.
  const ss = parseInstant(trip.span_start);
  const se = parseInstant(trip.span_end);
  if (ss != null || se != null) return { start: ss, end: se };
  // Dateless payloads and the detail payload: prefer the explicit dates, then
  // the span the server inferred from the trip's parts, read as UTC dates.
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
      const startDay = dayKey(startBlockOf(part));
      if (trip.starts_on && startDay < trip.starts_on) return true;
      const endDay = dayKey(endBlockOf(part) ?? startBlockOf(part));
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
  /** For a banded booking (see isBandedPart), which end of it this tile marks,
   * so that a multi-night stay shows a check-in tile on its first day and a
   * check-out tile on its last, and a multi-day car hire a pickup and a return.
   * Undefined for every other part (and for same-day bookings), which render as
   * a single tile. */
  edge?: BandEdge;
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
  // Each entry carries the instant to sort by and the block that places it on
  // a day (in that block's own zone), so a
  // banded booking (a multi-night stay, a multi-day hire) can contribute two
  // entries: one on the day it opens and one on the day it closes.
  interface Entry {
    tp: TimelinePart;
    instant: number;
    block: TimeBlock;
  }
  const flat: Entry[] = [];
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      if (isBandedPart(part) && part.ends_at) {
        flat.push({
          tp: { part, plan, edge: 'first' },
          // Sort by the smart opening time (effective_at: after the inbound
          // flight's arrival) so the booking doesn't jump ahead of the flight
          // that gets you there, matching the map's ordering. Keep the day
          // bucket on the booked opening (part.start, which is starts_at).
          instant: instantOf(part),
          block: startBlockOf(part),
        });
        flat.push({
          tp: { part, plan, edge: 'last' },
          instant: parseInstant(part.ends_at) ?? 0,
          block: endBlockOf(part) ?? startBlockOf(part),
        });
      } else {
        flat.push({ tp: { part, plan }, instant: instantOf(part), block: startBlockOf(part) });
      }
    }
  }
  flat.sort((a, b) => a.instant - b.instant);

  const days = new Map<string, TimelineDay>();
  for (const e of flat) {
    const key = dayKey(e.block);
    let day = days.get(key);
    if (!day) {
      day = { dayKey: key, label: fmtDayHeading(e.block), parts: [] };
      days.set(key, day);
    }
    day.parts.push(e.tp);
  }
  return [...days.values()];
}

/** One local day's worth of external (iCal feed) events under one header. */
export interface ExternalDay {
  dayKey: string;
  label: string;
  events: ExternalEvent[];
}

/** Group external feed events by local calendar day, chronologically, using the
 * same day-keying as buildTimeline so a day's events line up with the bookings
 * on that day when the two lists are merged. All-day events are keyed in UTC
 * (the feed gave a date, not a wall-clock instant). */
export function buildExternalDays(events: ExternalEvent[]): ExternalDay[] {
  const sorted = [...events].sort((a, b) => Date.parse(a.starts_at) - Date.parse(b.starts_at));
  const days = new Map<string, ExternalDay>();
  for (const e of sorted) {
    const tz = e.all_day ? 'UTC' : e.start_tz || '';
    const b = blockFor(e.starts_at, tz);
    const key = dayKey(b);
    let day = days.get(key);
    if (!day) {
      day = { dayKey: key, label: fmtDayHeading(b), events: [] };
      days.set(key, day);
    }
    day.events.push(e);
  }
  return [...days.values()];
}

// Banding is the rule that turns one ranged booking into two timeline tiles:
// one on the day it opens and one on the day it closes, rather than a single
// tile on the opening day with the closing time buried inside it. A three-night
// stay therefore reads as a check-in on the Monday and a check-out on the
// Thursday, and a multi-day car hire as a pickup and a return (issue #101). The
// same rule runs server-side for the iCal feed and the printed itinerary
// (internal/handlers/banding.go), and the two definitions must agree on which
// types band, or the same booking reads differently in each place.

/** Which end of a banded booking a tile marks: the day it opens or the day it
 * closes. */
export type BandEdge = 'first' | 'last';

/** Plan types that band, each with its opening and closing labels. A type
 * absent from this map never bands, however long it runs.
 *
 * The labels are the web's own wording, and are not quite the server's:
 * hotel reads "Check in"/"Check out" here and "Check-in"/"Check-out" in the
 * feed and the PDF, which is long-standing copy on both sides that this map
 * preserves rather than unifies. */
const BANDED: Partial<Record<PlanType, [string, string]>> = {
  hotel: ['Check in', 'Check out'],
  vehicle_hire: ['Pickup', 'Return'],
};

/** A banded type's [opening, closing] labels, or null for a type that doesn't
 * band. */
export function bandEdgeLabels(type: PlanType): [string, string] | null {
  return BANDED[type] ?? null;
}

/** True when a part is a ranged booking that should render as two tiles rather
 * than one (PRD §6.2): a banded type whose end falls on a later local day than
 * its start, each end resolved in its own zone since a car collected in Geneva
 * can be dropped in Lyon.
 *
 * Banding is opt-in by type, deliberately so: generalising it to "any part
 * whose end falls on a later day" would split every red-eye flight and every
 * overnight sleeper into a departure tile and an arrival tile, which is not how
 * a journey should read. A journey is one continuous thing that happens to
 * cross midnight, whilst a stay or a hire is a pair of appointments with a gap
 * in between. */
export function isBandedPart(part: PlanPart): boolean {
  if (!BANDED[part.type] || !part.ends_at) return false;
  const sb = startBlockOf(part);
  return dayKey(endBlockOf(part) ?? sb) > dayKey(sb);
}

/** The length of a banded booking in whole days, for its span chip: the nights
 * of a stay, or the days of a hire. */
export function bandSpanDays(part: PlanPart): number {
  if (!part.ends_at) return 0;
  const start = parseInstant(part.starts_at);
  const end = parseInstant(part.ends_at);
  if (start == null || end == null) return 0;
  const ms = end - start;
  return Math.max(1, Math.round(ms / (24 * 60 * 60 * 1000)));
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

/** A part's start block: the server's, or for a response that predates blocks
 * (a stale cache, the mock API) a scheduled one built from starts_at/start_tz
 * the way the server would, with a blank zone giving no label. */
export function startBlockOf(part: PlanPart): TimeBlock {
  return part.start ?? blockFor(part.starts_at, part.start_tz || '');
}

/** A part's end block, as startBlockOf; absent when the part has no end. */
export function endBlockOf(part: PlanPart): TimeBlock | undefined {
  if (part.end) return part.end;
  return part.ends_at ? blockFor(part.ends_at, part.end_tz || part.start_tz || '') : undefined;
}

/** A part's current times: "14:00 UTC+5", or "14:00 UTC+5 → ≈18:05 UTC+1" with
 * an end. Each end reads in its own zone, from the server's blocks. */
export function fmtPartTimeRange(part: PlanPart): string {
  const start = fmtCompact(startBlockOf(part));
  const end = endBlockOf(part);
  return end ? `${start} → ${fmtCompact(end)}` : start;
}

// The timetable time behind a revised block, in the same zone. A floating
// end's timetable is wall-clock digits, so it never takes the zone a provider
// estimate carries (newTimeBlock in internal/api/dto.go does the same).
function timetableBlock(b: TimeBlock, at: string): TimeBlock {
  return { ...b, at, tz: b.tz_source === 'floating' ? '' : b.tz, kind: 'scheduled', delay_min: 0 };
}

/** The timetable range, when either end is running to a reported or worked-out
 * time at least a minute away from it; null when the part is on schedule. */
export function fmtPartScheduledRange(part: PlanPart): string | null {
  const sb = startBlockOf(part);
  const eb = endBlockOf(part);
  if (!isRevised(sb) && !isRevised(eb)) return null;
  const f = part.flight;
  const start = fmtCompact(timetableBlock(sb, f?.scheduled_out ?? part.starts_at));
  const endAt = f?.scheduled_in ?? part.ends_at;
  if (!eb || !endAt) return start;
  return `${start} → ${fmtCompact(timetableBlock(eb, endAt))}`;
}

/** A part's local date(s) for the share text, e.g. "Sun, 18 Oct 2026", or a
 * range when it ends on a different local day. */
export function fmtPartDateRange(part: PlanPart): string {
  const start = fmtDayHeading(startBlockOf(part));
  const eb = endBlockOf(part);
  if (!eb) return start;
  const end = fmtDayHeading(eb);
  return end === start ? start : `${start} → ${end}`;
}

/** How long a transfer takes, end block minus start block, e.g. "8h 5m". Null
 * for a single-place type, a part with no end, or an end not after the start. */
export function fmtPartDuration(part: PlanPart): string | null {
  if (!isTransferType(part.type)) return null;
  const mins = durationMin(startBlockOf(part), endBlockOf(part));
  return mins == null ? null : fmtDuration(mins);
}

export interface FlightStatusLabel {
  label: string;
  /** How loudly to say it: 'error' for a journey that is not happening,
   * 'warning' for one that is happening late, 'normal' for the rest. */
  tone: 'normal' | 'warning' | 'error';
}

/** How a flight's state should read on a tile, which is not quite what we
 * store. Two departures from the stored value, both in the interest of saying
 * the useful thing rather than the literal one:
 *
 * A flight held on stand through a delay is stored as Scheduled, because the
 * status derivation asks where the aircraft is and the answer is "still here".
 * That is correct and unhelpful: two hours late reading as "Scheduled" tells
 * the traveller nothing. Where the airline has moved the departure later, the
 * line says Delayed instead.
 *
 * Cancelled and Diverted always win, including over a delay, because a journey
 * that is not happening is not merely a late one. Arrived wins over a delay
 * too: once the aircraft is down, how late it ran is history.
 *
 * Null for anything that is not a flight, since no other plan type has a state
 * that changes under the traveller.
 */
export function flightStatusLabel(part: PlanPart): FlightStatusLabel | null {
  if (part.type !== 'flight' || !part.flight) return null;
  const status = part.flight.flight_status?.trim() ?? '';
  if (status === 'Cancelled' || status === 'Diverted') return { label: status, tone: 'error' };
  // A departure the airline has reported or estimated later than the timetable.
  const sb = startBlockOf(part);
  const late = (sb.kind === 'estimated' || sb.kind === 'actual') && sb.delay_min > 0;
  if (status !== 'Arrived' && late) return { label: 'Delayed', tone: 'warning' };
  return status ? { label: status, tone: 'normal' } : null;
}

/** The one-line plain-text form for the map popup and share text, which cannot
 * strike text through: the current times, then the timetable when revised. */
export function fmtPartTimeRangeText(part: PlanPart): string {
  const scheduled = fmtPartScheduledRange(part);
  const now = fmtPartTimeRange(part);
  return scheduled ? `${now} (scheduled ${scheduled})` : now;
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

const TRANSFER_TYPES = new Set<PlanType>(['flight', 'train', 'ground']);

/** Point-to-point types that go from one place to another. Others (hotel,
 * dining, excursion) happen at a single place. */
export function isTransferType(type: PlanType): boolean {
  return TRANSFER_TYPES.has(type);
}

/** Types that carry a distinct end the user can set: transfers (an arrival),
 * hotels (a check-out), and vehicle hires (the return). Single-place types
 * (dining, excursion, meeting, event) have only a start. Drives which dialogs
 * offer an end date/time. */
export function typeHasEnd(type: PlanType): boolean {
  return isTransferType(type) || type === 'hotel' || type === 'vehicle_hire';
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

/** Display label for every plan type, in the order the New plan dialog offers
 * them (transport first, then places, then the rest).
 *
 * Typed as a full Record, so adding a member to `PlanType` without giving it a
 * label is a compile error rather than a plan that quietly shows its raw wire
 * key. Several hand-maintained lists elsewhere key off a plan type and none of
 * them are checked; this one at least is. */
export const PLAN_TYPE_LABELS: Record<PlanType, string> = {
  flight: 'Flight',
  train: 'Train',
  hotel: 'Accommodation',
  ground: 'Ground transport',
  vehicle_hire: 'Car hire',
  dining: 'Dining',
  excursion: 'Excursion',
  ice_cream: 'Ice cream',
  meeting: 'Meeting',
  event: 'Event',
};

/** Every plan type, in display order. Derived from PLAN_TYPE_LABELS so the two
 * can't drift: callers that need to enumerate the types (the New plan picker,
 * the help page's list) read this rather than keeping their own copy. */
export const PLAN_TYPES = Object.keys(PLAN_TYPE_LABELS) as PlanType[];

/** Common sorts of accommodation, offered as suggestions on the stay forms.
 * Deliberately not a constrained set: the long tail is genuinely long, so the
 * inputs accept any text and these merely save typing the usual ones. */
export const ACCOMMODATION_KINDS = [
  'Hotel',
  'B&B',
  'Hostel',
  'Apartment',
  'Campsite',
  'Caravan park',
  'Staying with friends',
  'Wild camping',
] as const;

/** Display label for a plan type, e.g. "Accommodation", "Ground transport".
 * Falls back to the raw key for a type the server knows about and this client
 * doesn't. */
export function planTypeLabel(type: PlanType): string {
  return PLAN_TYPE_LABELS[type] ?? type;
}

/** The PATCH that confirms the zone of one end of a part. When the zone picked
 * differs from the one shown, the scheduled wall-clock time is sent too (taken
 * from starts_at/ends_at, not from the block, so an estimate or actual, which
 * is a real instant, is never moved). The scheduled wall-clock time is kept and
 * the instant moves: a guessed zone was a guess about where, not about when. A
 * floating end's digits are already wall-clock. */
export function zoneConfirmPatch(
  part: PlanPart,
  which: 'start' | 'end',
  tz: string,
): UpdatePlanPartInput {
  const b = which === 'start' ? startBlockOf(part) : endBlockOf(part);
  const shown = b && b.tz_source !== 'floating' ? b.tz : '';
  if (which === 'start') {
    const patch: UpdatePlanPartInput = { start_tz: tz, start_tz_source: 'confirmed' };
    if (tz !== shown) {
      const { date, time } = splitLocal(part.starts_at, shown);
      patch.start_local = `${date}T${time}`;
    }
    return patch;
  }
  const patch: UpdatePlanPartInput = { end_tz: tz, end_tz_source: 'confirmed' };
  if (tz !== shown && part.ends_at) {
    const { date, time } = splitLocal(part.ends_at, shown);
    patch.end_local = `${date}T${time}`;
  }
  return patch;
}

/** How many live parts have a start or end whose zone is unknown or floating. */
export function countPartsNeedingZone(plans: Plan[]): number {
  let n = 0;
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      if (needsZone(startBlockOf(part)) || needsZone(endBlockOf(part))) n++;
    }
  }
  return n;
}

/** The zone a new item at `atMs` most likely sits in: where the traveller is
 * after the latest live part at or before it (its end's zone, else its
 * start's), else the zone of the first part after it. Blank zones are skipped;
 * '' when nothing is left. The caller sends a zone found this way as inferred. */
export function neighbourZone(plans: Plan[], atMs: number): string {
  if (Number.isNaN(atMs)) return '';
  let before: { at: number; tz: string } | null = null;
  let after: { at: number; tz: string } | null = null;
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      const sb = startBlockOf(part);
      const at = Date.parse(sb.at);
      if (Number.isNaN(at)) continue;
      const tz = endBlockOf(part)?.tz || sb.tz;
      if (at <= atMs && tz && (!before || at > before.at)) before = { at, tz };
      if (at > atMs && sb.tz && (!after || at < after.at)) after = { at, tz: sb.tz };
    }
  }
  return before?.tz ?? after?.tz ?? '';
}
