import { describe, it, expect } from 'vitest';

import type { ExternalEvent, Plan, PlanPart, Trip } from '../api/types';
import {
  buildExternalDays,
  buildTimeline,
  classifyTrip,
  fmtPartPlaces,
  flightStatusLabel,
  fmtPartDuration,
  fmtPartScheduledRange,
  startBlockOf,
  endBlockOf,
  fmtPartDateRange,
  fmtPartTimeRange,
  fmtPartTimeRangeText,
  fmtTripDates,
  plansOutsideTripDates,
  splitLocal,
  zonedTimeToUtc,
  bandEdgeLabels,
  bandSpanDays,
  isBandedPart,
  planTypeLabel,
  tripSpan,
  typeHasEnd,
  zoneConfirmPatch,
  countPlansNeedingZone,
  neighbourZone,
} from './trip-format';
import { block, partBlocks } from '../test/blocks';

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'Lisbon',
    destination: 'Lisbon, Portugal',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function part(over: Partial<PlanPart> = {}): PlanPart {
  const base: Partial<PlanPart> = {
    id: 1,
    plan_id: 1,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T09:00:00Z',
    start_tz: 'UTC',
    end_tz: 'UTC',
    start_label: 'LHR',
    end_label: 'LIS',
    status: 'planned',
    effective_at: '2026-10-12T09:00:00Z',
  };
  return { ...base, ...partBlocks(base, over), ...over } as PlanPart;
}

function plan(parts: PlanPart[], over: Partial<Plan> = {}): Plan {
  return {
    id: parts[0]?.plan_id ?? 1,
    trip_id: 1,
    type: parts[0]?.type ?? 'flight',
    title: 'Outbound',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

describe('tripSpan', () => {
  it('uses part min/max when plans are supplied', () => {
    const plans = [
      plan([
        part({ id: 1, starts_at: '2026-10-12T09:00:00Z', effective_at: '2026-10-12T09:00:00Z' }),
        part({
          id: 2,
          starts_at: '2026-10-18T09:00:00Z',
          effective_at: '2026-10-18T09:00:00Z',
          ends_at: '2026-10-18T11:00:00Z',
        }),
      ]),
    ];
    const span = tripSpan(trip(), plans);
    expect(span.start).toBe(new Date('2026-10-12T09:00:00Z').getTime());
    expect(span.end).toBe(new Date('2026-10-18T11:00:00Z').getTime());
  });

  it('ignores dismissed parts', () => {
    const plans = [
      plan([
        part({ id: 1, effective_at: '2026-10-12T09:00:00Z' }),
        part({ id: 2, effective_at: '2026-12-01T09:00:00Z', dismissed_at: '2026-10-01T00:00:00Z' }),
      ]),
    ];
    const span = tripSpan(trip(), plans);
    expect(span.end).toBe(new Date('2026-10-12T09:00:00Z').getTime());
  });

  it('falls back to starts_on/ends_on when no parts', () => {
    const span = tripSpan(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }));
    expect(span.start).toBe(new Date('2026-10-12T00:00:00Z').getTime());
    // The end is the *end* of the last day (start of the next), so the trip
    // stays current through all of 18 Oct rather than expiring at midnight.
    expect(span.end).toBe(new Date('2026-10-19T00:00:00Z').getTime());
  });

  it('returns nulls for a date-less trip', () => {
    expect(tripSpan(trip())).toEqual({ start: null, end: null });
  });
});

describe('classifyTrip', () => {
  const now = new Date('2026-10-15T12:00:00Z').getTime();

  it('future trip → upcoming', () => {
    expect(classifyTrip({ start: now + 1e9, end: now + 2e9 }, now)).toBe('upcoming');
  });

  it('spanning now → now', () => {
    expect(classifyTrip({ start: now - 1e9, end: now + 1e9 }, now)).toBe('now');
  });

  it('past trip → past', () => {
    expect(classifyTrip({ start: now - 2e9, end: now - 1e9 }, now)).toBe('past');
  });

  it('date-less → upcoming', () => {
    expect(classifyTrip({ start: null, end: null }, now)).toBe('upcoming');
  });

  it('a trip ending today is "now", not "past", past UTC midnight (issue #29)', () => {
    // Last leg departs 19:25 GMT+2 today; "now" is 17:55 GMT+2 (= 15:55 UTC).
    // The list payload only knows the end *date*, so tripSpan extends it to the
    // end of that day — the trip must read as in-progress, not past.
    const nowLocal = new Date('2026-06-07T15:55:00Z').getTime();
    const span = tripSpan(trip({ effective_start: '2026-06-05', effective_end: '2026-06-07' }));
    expect(classifyTrip(span, nowLocal)).toBe('now');
  });
});

describe('fmtTripDates', () => {
  it('formats a full range', () => {
    expect(fmtTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }))).toMatch(
      /Oct.*2026.*Oct.*2026/,
    );
  });
  it('handles no dates', () => {
    expect(fmtTripDates(trip())).toMatch(/decided/i);
  });
  it('falls back to the inferred span (marked ~) when no explicit dates', () => {
    const out = fmtTripDates(trip({ effective_start: '2026-10-20', effective_end: '2026-10-24' }));
    expect(out).toContain('~');
    expect(out).toMatch(/20.*Oct.*24.*Oct/);
  });
  it('prefers explicit dates over the inferred span (no ~)', () => {
    const out = fmtTripDates(
      trip({ starts_on: '2026-10-12', ends_on: '2026-10-18', effective_start: '2026-10-20' }),
    );
    expect(out).not.toContain('~');
  });
});

describe('plansOutsideTripDates', () => {
  const within = part({
    id: 1,
    starts_at: '2026-10-13T09:00:00Z',
    effective_at: '2026-10-13T09:00:00Z',
  });
  it('false when no explicit trip dates', () => {
    expect(plansOutsideTripDates(trip(), [plan([within])])).toBe(false);
  });
  it('false when all parts are within the dates', () => {
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [
        plan([within]),
      ]),
    ).toBe(false);
  });
  it('true when a part starts before the trip', () => {
    const early = part({
      id: 2,
      starts_at: '2026-10-01T09:00:00Z',
      effective_at: '2026-10-01T09:00:00Z',
    });
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [
        plan([early]),
      ]),
    ).toBe(true);
  });
  it('true when a part ends after the trip', () => {
    const late = part({
      id: 3,
      starts_at: '2026-10-25T09:00:00Z',
      effective_at: '2026-10-25T09:00:00Z',
    });
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [
        plan([late]),
      ]),
    ).toBe(true);
  });
});

describe('buildTimeline', () => {
  it('sorts by effective_at and groups by local day', () => {
    const plans = [
      plan([part({ id: 2, effective_at: '2026-10-13T09:00:00Z' })], { id: 2 }),
      plan([part({ id: 1, effective_at: '2026-10-12T09:00:00Z' })], { id: 1 }),
    ];
    const days = buildTimeline(plans);
    expect(days).toHaveLength(2);
    expect(days[0].dayKey).toBe('2026-10-12');
    expect(days[1].dayKey).toBe('2026-10-13');
  });

  it('drops dismissed parts', () => {
    const plans = [plan([part({ id: 1, dismissed_at: '2026-09-01T00:00:00Z' })])];
    expect(buildTimeline(plans)).toHaveLength(0);
  });

  it('splits a multi-night hotel into a check-in and a check-out tile', () => {
    const plans = [
      plan(
        [
          part({
            id: 9,
            type: 'hotel',
            starts_at: '2026-10-12T15:00:00Z',
            effective_at: '2026-10-12T15:00:00Z',
            ends_at: '2026-10-14T10:00:00Z',
            start_label: 'Hotel Lisboa',
            end_label: '',
          }),
        ],
        { id: 2, type: 'hotel' },
      ),
    ];
    const days = buildTimeline(plans);
    expect(days.map((d) => d.dayKey)).toEqual(['2026-10-12', '2026-10-14']);
    expect(days[0].parts).toHaveLength(1);
    expect(days[0].parts[0].edge).toBe('first');
    expect(days[1].parts[0].edge).toBe('last');
  });

  // The multi-night hotel contract, asserted through what a reader of the
  // timeline actually gets (how many entries, on which days, tied to which
  // booking) rather than through the spelling of the edge marker, so that the
  // same assertions hold before and after the banding rule was generalised
  // beyond hotels. Every trip with a stay in it depends on this.
  describe('multi-night hotel banding, unchanged by generalisation', () => {
    const stay = () =>
      plan(
        [
          part({
            id: 9,
            plan_id: 2,
            type: 'hotel',
            starts_at: '2026-10-12T15:00:00Z',
            effective_at: '2026-10-12T15:00:00Z',
            ends_at: '2026-10-15T10:00:00Z',
            start_label: 'Hotel Lisboa',
            end_label: '',
          }),
        ],
        { id: 2, type: 'hotel' },
      );

    it('places one entry on the first day and one on the last, and none between', () => {
      const days = buildTimeline([stay()]);
      expect(days.map((d) => d.dayKey)).toEqual(['2026-10-12', '2026-10-15']);
      expect(days.map((d) => d.parts.length)).toEqual([1, 1]);
      // Both entries are the same booking, shown twice.
      expect(days.flatMap((d) => d.parts.map((p) => p.part.id))).toEqual([9, 9]);
    });

    it('marks the two entries as two distinct, identified ends of one booking', () => {
      const edges = buildTimeline([stay()]).flatMap((d) => d.parts.map((p) => p.edge));
      expect(edges.every((e) => Boolean(e))).toBe(true);
      expect(new Set(edges).size).toBe(2);
    });

    it('leaves a same-day stay as a single, unmarked entry', () => {
      const days = buildTimeline([
        plan(
          [
            part({
              id: 9,
              plan_id: 2,
              type: 'hotel',
              starts_at: '2026-10-12T09:00:00Z',
              effective_at: '2026-10-12T09:00:00Z',
              ends_at: '2026-10-12T17:00:00Z',
            }),
          ],
          { id: 2, type: 'hotel' },
        ),
      ]);
      expect(days.flatMap((d) => d.parts)).toHaveLength(1);
      expect(days[0].parts[0].edge).toBeUndefined();
    });
  });

  it('bands a multi-day vehicle hire into a pickup and a return entry', () => {
    const days = buildTimeline([
      plan(
        [
          part({
            id: 12,
            plan_id: 4,
            type: 'vehicle_hire',
            starts_at: '2026-09-09T15:30:00Z',
            effective_at: '2026-09-09T15:30:00Z',
            ends_at: '2026-09-11T14:00:00Z',
            start_label: 'Geneva Airport',
            end_label: 'Lyon Part-Dieu',
          }),
        ],
        { id: 4, type: 'vehicle_hire' },
      ),
    ]);
    expect(days.map((d) => d.dayKey)).toEqual(['2026-09-09', '2026-09-11']);
    expect(days.flatMap((d) => d.parts.map((p) => p.edge))).toEqual(['first', 'last']);
  });

  it('does not band a same-day vehicle hire', () => {
    const days = buildTimeline([
      plan(
        [
          part({
            id: 12,
            plan_id: 4,
            type: 'vehicle_hire',
            starts_at: '2026-09-09T09:00:00Z',
            effective_at: '2026-09-09T09:00:00Z',
            ends_at: '2026-09-09T17:00:00Z',
          }),
        ],
        { id: 4, type: 'vehicle_hire' },
      ),
    ]);
    expect(days.flatMap((d) => d.parts)).toHaveLength(1);
    expect(days[0].parts[0].edge).toBeUndefined();
  });

  it('never bands a red-eye flight, however far into the next day it lands', () => {
    // Banding is opt-in by type: a journey that happens to cross midnight is
    // still one journey, and must not be split into a departure and an arrival.
    const days = buildTimeline([
      plan(
        [
          part({
            id: 1,
            type: 'flight',
            starts_at: '2026-09-09T23:00:00Z',
            effective_at: '2026-09-09T23:00:00Z',
            ends_at: '2026-09-10T08:00:00Z',
          }),
        ],
        { id: 1, type: 'flight' },
      ),
    ]);
    expect(days.flatMap((d) => d.parts)).toHaveLength(1);
    expect(days[0].parts[0].edge).toBeUndefined();
  });

  it('orders a hotel check-in by its smart effective_at, after a same-day flight', () => {
    // A flight arriving 18:00, and a hotel whose raw check-in is 15:00 but whose
    // smart effective_at (after arrival) is 19:00. The check-in tile must sort
    // after the flight, not ahead of it, while still bucketing on the booked day.
    const plans = [
      plan(
        [part({ id: 1, starts_at: '2026-10-12T15:30:00Z', effective_at: '2026-10-12T15:30:00Z' })],
        {
          id: 1,
        },
      ),
      plan(
        [
          part({
            id: 9,
            type: 'hotel',
            starts_at: '2026-10-12T15:00:00Z',
            effective_at: '2026-10-12T19:00:00Z', // smart check-in, after the flight
            ends_at: '2026-10-14T10:00:00Z',
            start_label: 'Hotel Lisboa',
            end_label: '',
          }),
        ],
        { id: 2, type: 'hotel' },
      ),
    ];
    const days = buildTimeline(plans);
    // Same booked day; the flight (id 1) precedes the hotel check-in (id 9).
    expect(days[0].dayKey).toBe('2026-10-12');
    expect(days[0].parts.map((p) => p.part.id)).toEqual([1, 9]);
    expect(days[0].parts[1].edge).toBe('first');
  });

  it('groups a red-eye on its departure day in the start tz', () => {
    // 23:30 local Oct 12 in NY → arrives Oct 13; header is the departure day.
    const plans = [
      plan([
        part({
          id: 1,
          starts_at: '2026-10-13T03:30:00Z', // 23:30 EDT on Oct 12
          effective_at: '2026-10-13T03:30:00Z',
          start_tz: 'America/New_York',
          ends_at: '2026-10-13T11:00:00Z',
          end_tz: 'Europe/London',
        }),
      ]),
    ];
    const days = buildTimeline(plans);
    expect(days[0].dayKey).toBe('2026-10-12');
  });
});

describe('buildExternalDays', () => {
  const ev = (over: Partial<ExternalEvent>): ExternalEvent => ({
    id: 1,
    feed_id: 1,
    title: 'Session',
    starts_at: '2026-10-20T09:00:00Z',
    all_day: false,
    ...over,
  });

  it('groups events by local day, chronologically', () => {
    const days = buildExternalDays([
      ev({ id: 2, starts_at: '2026-10-21T09:00:00Z' }),
      ev({ id: 1, starts_at: '2026-10-20T09:00:00Z' }),
      ev({ id: 3, starts_at: '2026-10-20T14:00:00Z' }),
    ]);
    expect(days.map((d) => d.dayKey)).toEqual(['2026-10-20', '2026-10-21']);
    expect(days[0].events.map((e) => e.id)).toEqual([1, 3]);
    expect(days[1].events.map((e) => e.id)).toEqual([2]);
  });

  it('keys to the event timezone', () => {
    // 23:30 UTC on the 20th is 01:30 on the 21st in CEST (UTC+2).
    const days = buildExternalDays([
      ev({ starts_at: '2026-10-20T23:30:00Z', start_tz: 'Europe/Berlin' }),
    ]);
    expect(days[0].dayKey).toBe('2026-10-21');
  });
});

describe('banding', () => {
  it('isBandedPart true for a multi-night hotel', () => {
    const p = part({
      type: 'hotel',
      starts_at: '2026-10-12T15:00:00Z',
      ends_at: '2026-10-15T10:00:00Z',
    });
    expect(isBandedPart(p)).toBe(true);
    expect(bandSpanDays(p)).toBe(3);
  });

  it('isBandedPart true for a multi-day vehicle hire', () => {
    const p = part({
      type: 'vehicle_hire',
      starts_at: '2026-09-09T15:30:00Z',
      ends_at: '2026-09-11T14:00:00Z',
    });
    expect(isBandedPart(p)).toBe(true);
    expect(bandSpanDays(p)).toBe(2);
  });

  it('isBandedPart false for a flight, however late it lands', () => {
    // The red-eye case: an unbanded type never bands, even though it ends on a
    // later local day than it started.
    expect(isBandedPart(part({ type: 'flight', ends_at: '2026-10-12T11:00:00Z' }))).toBe(false);
    expect(
      isBandedPart(
        part({
          type: 'flight',
          starts_at: '2026-09-09T23:00:00Z',
          ends_at: '2026-09-10T08:00:00Z',
        }),
      ),
    ).toBe(false);
  });

  it('isBandedPart false for a same-day booking of a banded type', () => {
    expect(
      isBandedPart(
        part({
          type: 'vehicle_hire',
          starts_at: '2026-09-09T09:00:00Z',
          ends_at: '2026-09-09T17:00:00Z',
        }),
      ),
    ).toBe(false);
  });

  it('labels hire edges Pickup and Return, and stay edges Check in and Check out', () => {
    expect(bandEdgeLabels('vehicle_hire')).toEqual(['Pickup', 'Return']);
    expect(bandEdgeLabels('hotel')).toEqual(['Check in', 'Check out']);
    expect(bandEdgeLabels('ground')).toBeNull();
    expect(bandEdgeLabels('flight')).toBeNull();
  });
});

describe('fmtPartPlaces', () => {
  it('shows an arrow for a transfer between two places', () => {
    expect(fmtPartPlaces('flight', 'LHR', 'JFK')).toBe('LHR → JFK');
    expect(fmtPartPlaces('ground', 'Home', 'LHR T5')).toBe('Home → LHR T5');
  });
  it('shows a single venue for a hotel, never "X → X"', () => {
    expect(fmtPartPlaces('hotel', 'Tysons Corner Marriott', 'Tysons Corner Marriott')).toBe(
      'Tysons Corner Marriott',
    );
    expect(fmtPartPlaces('dining', 'Nobu')).toBe('Nobu');
  });
  it('collapses identical transfer endpoints to one', () => {
    expect(fmtPartPlaces('train', 'Paddington', 'Paddington')).toBe('Paddington');
  });
});

describe('flightStatusLabel', () => {
  // delay is minutes past the timetable: the departure block then reads as an
  // airline estimate, the way the server sends it.
  const f = (over: Partial<PlanPart['flight']> = {}, delay = 0) =>
    part({
      ...(delay
        ? {
            start: block('2026-08-20T15:05:00Z', 'UTC', {
              at: new Date(Date.parse('2026-08-20T15:05:00Z') + delay * 60000).toISOString(),
              kind: 'estimated',
              delay_min: delay,
            }),
          }
        : {}),
      starts_at: '2026-08-20T15:05:00Z',
      ends_at: '2026-08-20T17:15:00Z',
      flight: {
        ident: 'OS967',
        scheduled_out: '2026-08-20T15:05:00Z',
        scheduled_in: '2026-08-20T17:15:00Z',
        flight_status: 'Scheduled',
        ...over,
      } as PlanPart['flight'],
    });

  it('reads Delayed when the airline has moved the departure later', () => {
    // Stored as Scheduled, because the aircraft is still on stand. True, and
    // useless to somebody two hours into a delay.
    expect(flightStatusLabel(f({}, 115))).toEqual({
      label: 'Delayed',
      tone: 'warning',
    });
  });

  it('reads Delayed for a flight that got airborne late as well', () => {
    expect(flightStatusLabel(f({ flight_status: 'Enroute' }, 115))).toEqual({
      label: 'Delayed',
      tone: 'warning',
    });
  });

  it('lets a cancellation beat a delay, a journey not happening beating a late one', () => {
    expect(flightStatusLabel(f({ flight_status: 'Cancelled' }, 115))).toEqual({
      label: 'Cancelled',
      tone: 'error',
    });
    expect(flightStatusLabel(f({ flight_status: 'Diverted' }))).toEqual({
      label: 'Diverted',
      tone: 'error',
    });
  });

  it('lets an arrival beat a delay, since how late it ran is then history', () => {
    expect(flightStatusLabel(f({ flight_status: 'Arrived' }, 115))).toEqual({
      label: 'Arrived',
      tone: 'normal',
    });
  });

  it('does not call an early departure a delay', () => {
    expect(flightStatusLabel(f({}, -25))).toEqual({
      label: 'Scheduled',
      tone: 'normal',
    });
  });

  it('passes an unremarkable status through unchanged', () => {
    expect(flightStatusLabel(f({ flight_status: 'Enroute' }))).toEqual({
      label: 'Enroute',
      tone: 'normal',
    });
  });

  it('says nothing for a non-flight part, or a flight with no status yet', () => {
    expect(flightStatusLabel(part({ type: 'hotel', flight: undefined }))).toBeNull();
    expect(flightStatusLabel(f({ flight_status: '' }))).toBeNull();
  });
});

describe('block-based part times', () => {
  const lhr = block('2026-10-18T09:00:00Z', 'Europe/London');
  const jfk = block('2026-10-18T17:05:00Z', 'America/New_York');
  const flight = (over: Partial<PlanPart>): PlanPart =>
    ({
      id: 1,
      plan_id: 1,
      type: 'flight',
      seq: 0,
      status: 'planned',
      starts_at: '2026-10-18T09:00:00Z',
      ends_at: '2026-10-18T17:05:00Z',
      start_tz: 'Europe/London',
      end_tz: 'America/New_York',
      start_tz_source: 'confirmed',
      end_tz_source: 'confirmed',
      start_label: 'LHR',
      end_label: 'JFK',
      start: lhr,
      end: jfk,
      flight: {
        ident: 'XX100',
        scheduled_out: '2026-10-18T09:00:00Z',
        scheduled_in: '2026-10-18T17:05:00Z',
      },
      ...over,
    }) as PlanPart;

  it('reads the range and duration from the blocks', () => {
    expect(fmtPartTimeRange(flight({}))).toBe('10:00 UTC+1 → 13:05 UTC-4');
    expect(fmtPartDuration(flight({}))).toBe('8h 5m');
    expect(fmtPartScheduledRange(flight({}))).toBeNull();
  });

  it('keeps the timetable beside a delayed estimate', () => {
    const p = flight({
      start: { ...lhr, at: '2026-10-18T09:40:00Z', kind: 'estimated', delay_min: 40 },
      end: { ...jfk, at: '2026-10-18T17:45:00Z', kind: 'derived', delay_min: 40 },
    });
    expect(fmtPartTimeRange(p)).toBe('10:40 UTC+1 → ≈13:45 UTC-4');
    expect(fmtPartScheduledRange(p)).toBe('10:00 UTC+1 → 13:05 UTC-4');
    expect(fmtPartTimeRangeText(p)).toBe(
      '10:40 UTC+1 → ≈13:45 UTC-4 (scheduled 10:00 UTC+1 → 13:05 UTC-4)',
    );
    expect(flightStatusLabel(p)).toEqual({ label: 'Delayed', tone: 'warning' });
  });

  it('shows a floating timetable time with no zone label', () => {
    const p = flight({
      starts_at: '2026-10-18T10:00:00Z',
      start_tz: '',
      start_tz_source: 'floating',
      flight: { ident: 'XX100', scheduled_out: '2026-10-18T10:00:00Z' },
      start: {
        ...lhr,
        at: '2026-10-18T09:40:00Z',
        tz_source: 'floating',
        kind: 'estimated',
        delay_min: 40,
      },
      ends_at: undefined,
      end: undefined,
    } as Partial<PlanPart>);
    expect(fmtPartScheduledRange(p)).toBe('10:00');
  });

  it('revises only the arrival when the departure stands', () => {
    const p = flight({
      end: { ...jfk, at: '2026-10-18T17:45:00Z', kind: 'estimated', delay_min: 40 },
    });
    expect(fmtPartScheduledRange(p)).toBe('10:00 UTC+1 → 13:05 UTC-4');
  });

  it('handles a revised part with no end', () => {
    const p = flight({
      ends_at: undefined,
      end: undefined,
      start: { ...lhr, at: '2026-10-18T09:40:00Z', kind: 'estimated', delay_min: 40 },
    });
    expect(fmtPartScheduledRange(p)).toBe('10:00 UTC+1');
  });

  it('does not call an early or on-time flight delayed', () => {
    const early = flight({ start: { ...lhr, kind: 'estimated', delay_min: -5 } });
    expect(flightStatusLabel(early)).toBeNull();
  });

  it('gives the date range in each end’s zone', () => {
    expect(fmtPartDateRange(flight({}))).toBe('Sun, 18 Oct 2026');
    const overnight = flight({
      start: block('2026-10-18T23:00:00Z', 'UTC'),
      end: block('2026-10-19T07:00:00Z', 'Asia/Tokyo'),
    });
    expect(fmtPartDateRange(overnight)).toBe('Sun, 18 Oct 2026 → Mon, 19 Oct 2026');
    expect(fmtPartDateRange(flight({ end: undefined }))).toBe('Sun, 18 Oct 2026');
  });

  it('shows only the start of a revised part with no scheduled end', () => {
    const p = flight({
      ends_at: undefined,
      end: jfk,
      flight: { ident: 'XX100', scheduled_out: '2026-10-18T09:00:00Z' },
      start: { ...lhr, at: '2026-10-18T09:40:00Z', kind: 'estimated', delay_min: 40 },
    } as Partial<PlanPart>);
    expect(fmtPartScheduledRange(p)).toBe('10:00 UTC+1');
  });

  it('reads a part with no blocks from its starts_at and zones', () => {
    const p = flight({ start: undefined, end: undefined } as unknown as Partial<PlanPart>);
    expect(startBlockOf(p).at).toBe('2026-10-18T09:00:00Z');
    expect(fmtPartTimeRange(p)).toBe('10:00 UTC+1 → 13:05 UTC-4');
    expect(fmtPartTimeRangeText(p)).toBe('10:00 UTC+1 → 13:05 UTC-4');
    expect(fmtPartDateRange(p)).toBe('Sun, 18 Oct 2026');
    expect(fmtPartDuration(p)).toBe('8h 5m');
    expect(flightStatusLabel(p)).toBeNull();
    const noEnd = flight({
      start: undefined,
      end: undefined,
      ends_at: undefined,
    } as unknown as Partial<PlanPart>);
    expect(endBlockOf(noEnd)).toBeUndefined();
  });

  it('builds missing blocks as the server would from the raw zones and sources', () => {
    const p = flight({
      start: undefined,
      end: undefined,
      start_tz: 'Europe/London',
      start_tz_source: 'floating',
      end_tz: '',
      end_tz_source: 'unknown',
    } as unknown as Partial<PlanPart>);
    // A floating start's raw zone is the airport fallback; its digits have none.
    expect(startBlockOf(p)).toMatchObject({ tz: '', tz_source: 'floating' });
    // An end with no zone of its own does not borrow the start's.
    expect(endBlockOf(p)).toMatchObject({ tz: '', tz_source: 'unknown' });
    const inferred = flight({
      start: undefined,
      end: undefined,
      start_tz_source: 'inferred',
      end_tz_source: 'confirmed',
    } as unknown as Partial<PlanPart>);
    expect(startBlockOf(inferred)).toMatchObject({ tz: 'Europe/London', tz_source: 'inferred' });
    expect(endBlockOf(inferred)).toMatchObject({
      tz: 'America/New_York',
      tz_source: 'confirmed',
    });
  });

  it('has no duration for a single-place type', () => {
    expect(fmtPartDuration(flight({ type: 'hotel' }))).toBeNull();
  });
});

describe('fmtPartTimeRangeText', () => {
  it('leads with the revised times and keeps the timetable behind them', () => {
    expect(
      fmtPartTimeRangeText(
        part({
          starts_at: '2026-08-20T15:05:00Z',
          ends_at: '2026-08-20T17:15:00Z',
          flight: {
            ident: 'OS967',
            scheduled_out: '2026-08-20T15:05:00Z',
            scheduled_in: '2026-08-20T17:15:00Z',
          } as PlanPart['flight'],
          start: block('2026-08-20T17:00:00Z', 'UTC', { kind: 'estimated', delay_min: 115 }),
          end: block('2026-08-20T19:00:00Z', 'UTC', { kind: 'estimated', delay_min: 105 }),
        }),
      ),
    ).toBe('17:00 UTC → 19:00 UTC (scheduled 15:05 UTC → 17:15 UTC)');
  });

  it('collapses to the plain range when nothing has been revised', () => {
    expect(
      fmtPartTimeRangeText(
        part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined, flight: undefined }),
      ),
    ).toBe('09:00 UTC');
  });
});

describe('fmtPartDuration', () => {
  it('measures the instants, so a range across time zones reads true', () => {
    // 22:30 in Paris (CET) → 10:00 next day in Helsinki (EET) is 10h 30m.
    expect(
      fmtPartDuration(
        part({
          starts_at: '2026-11-10T21:30:00Z',
          ends_at: '2026-11-11T08:00:00Z',
          start_tz: 'Europe/Paris',
          end_tz: 'Europe/Helsinki',
          flight: undefined,
        }),
      ),
    ).toBe('10h 30m');
  });

  it('drops the hours under an hour', () => {
    expect(
      fmtPartDuration(
        part({
          starts_at: '2026-10-12T09:00:00Z',
          ends_at: '2026-10-12T09:45:00Z',
          flight: undefined,
        }),
      ),
    ).toBe('45m');
  });

  it('follows the times the flight is actually running to', () => {
    expect(
      fmtPartDuration(
        part({
          starts_at: '2026-08-20T15:05:00Z',
          ends_at: '2026-08-20T17:15:00Z',
          flight: { ident: 'OS967' } as PlanPart['flight'],
          start: block('2026-08-20T17:00:00Z', 'UTC', { kind: 'estimated', delay_min: 115 }),
          end: block('2026-08-20T19:00:00Z', 'UTC', { kind: 'estimated', delay_min: 105 }),
        }),
      ),
    ).toBe('2h');
  });

  it('is null with no end, and for single-place types', () => {
    expect(fmtPartDuration(part({ ends_at: undefined, flight: undefined }))).toBeNull();
    expect(
      fmtPartDuration(
        part({
          type: 'hotel',
          starts_at: '2026-10-12T14:00:00Z',
          ends_at: '2026-10-14T10:00:00Z',
          flight: undefined,
        }),
      ),
    ).toBeNull();
  });
});

describe('fmtPartTimeRange', () => {
  it('renders a single time with its tz abbreviation', () => {
    expect(fmtPartTimeRange(part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined }))).toBe(
      '09:00 UTC',
    );
  });

  it('shows the time with no zone label when the tz is unknown', () => {
    expect(
      fmtPartTimeRange(
        part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined, start_tz: '' }),
      ),
    ).toBe('09:00');
  });
  it('renders a range with each end in its own tz + abbreviation', () => {
    const out = fmtPartTimeRange(
      part({
        starts_at: '2026-07-01T10:00:00Z',
        ends_at: '2026-07-01T14:00:00Z',
        start_tz: 'Europe/London',
        end_tz: 'America/New_York',
      }),
    );
    expect(out).toBe('11:00 UTC+1 → 10:00 UTC-4');
  });
});

describe('splitLocal / zonedTimeToUtc', () => {
  it('splits an instant into local date + time in its tz', () => {
    // 11:35Z is 12:35 in London (BST) on 12 Oct... actually Oct is BST→ +1.
    expect(splitLocal('2026-10-12T11:35:00Z', 'Europe/London')).toEqual({
      date: '2026-10-12',
      time: '12:35',
    });
    expect(splitLocal('2026-10-12T19:55:00Z', 'America/New_York')).toEqual({
      date: '2026-10-12',
      time: '15:55',
    });
  });
  it('recombines local date + time + tz back to the same instant', () => {
    expect(zonedTimeToUtc('2026-10-12', '12:35', 'Europe/London')).toBe('2026-10-12T11:35:00.000Z');
    expect(zonedTimeToUtc('2026-10-12', '15:55', 'America/New_York')).toBe(
      '2026-10-12T19:55:00.000Z',
    );
  });
  it('treats a blank tz as UTC', () => {
    expect(zonedTimeToUtc('2026-10-12', '09:00', '')).toBe('2026-10-12T09:00:00.000Z');
  });
});

describe('planTypeLabel', () => {
  it('labels ground transport', () => {
    expect(planTypeLabel('ground')).toBe('Ground transport');
  });
  it('labels every known plan type', () => {
    expect(planTypeLabel('flight')).toBe('Flight');
    expect(planTypeLabel('train')).toBe('Train');
    expect(planTypeLabel('hotel')).toBe('Accommodation');
    expect(planTypeLabel('dining')).toBe('Dining');
    expect(planTypeLabel('excursion')).toBe('Excursion');
    expect(planTypeLabel('ice_cream')).toBe('Ice cream');
    expect(planTypeLabel('meeting')).toBe('Meeting');
    expect(planTypeLabel('event')).toBe('Event');
  });
  it('labels vehicle_hire as Car hire, matching the PDF and calendar labels', () => {
    expect(planTypeLabel('vehicle_hire')).toBe('Car hire');
  });
  it('labels the hotel plan type as accommodation, since it covers campsites and vans too', () => {
    expect(planTypeLabel('hotel')).toBe('Accommodation');
  });
  it('falls back to the raw type for an unknown value', () => {
    expect(planTypeLabel('mystery' as unknown as Parameters<typeof planTypeLabel>[0])).toBe(
      'mystery',
    );
  });
});

describe('typeHasEnd', () => {
  it('gives vehicle_hire an editable end (the return), so a hire can be corrected and bands', () => {
    expect(typeHasEnd('vehicle_hire')).toBe(true);
  });
  it('leaves hotel unchanged (still has an editable end, the check-out)', () => {
    expect(typeHasEnd('hotel')).toBe(true);
  });
  it('leaves a single-point type unchanged (no editable end)', () => {
    expect(typeHasEnd('dining')).toBe(false);
  });
  it('still recognises the existing transfer types', () => {
    expect(typeHasEnd('flight')).toBe(true);
    expect(typeHasEnd('train')).toBe(true);
    expect(typeHasEnd('ground')).toBe(true);
  });
});

describe('branch edge cases', () => {
  it('fmtTripDates: start-only (explicit) has no ~ prefix', () => {
    expect(fmtTripDates(trip({ starts_on: '2026-10-12' }))).toMatch(/^12 Oct 2026$/);
  });
  it('fmtTripDates: start-only (inferred) carries the ~ prefix', () => {
    expect(fmtTripDates(trip({ effective_start: '2026-10-12' }))).toMatch(/^~12 Oct 2026$/);
  });
  it('fmtTripDates: end-only renders "until …"', () => {
    expect(fmtTripDates(trip({ ends_on: '2026-10-18' }))).toMatch(/^until 18 Oct 2026$/);
  });
  it('fmtDay: returns the raw string for an unparseable date-only value', () => {
    // An invalid date column falls through to the raw value rather than NaN.
    expect(fmtTripDates(trip({ starts_on: 'not-a-date' }))).toBe('not-a-date');
  });

  it('tripSpan: falls back to effective_* when no starts_on/ends_on', () => {
    const span = tripSpan(trip({ effective_start: '2026-10-12', effective_end: '2026-10-18' }));
    expect(span.start).toBe(new Date('2026-10-12T00:00:00Z').getTime());
    // End extended to the end of the last day (start of the next).
    expect(span.end).toBe(new Date('2026-10-19T00:00:00Z').getTime());
  });
  it('tripSpan: uses a part with no effective_at (falls back to starts_at)', () => {
    const plans = [
      plan([
        part({
          id: 1,
          effective_at: undefined,
          starts_at: '2026-10-12T09:00:00Z',
          ends_at: undefined,
        }),
      ]),
    ];
    const span = tripSpan(trip(), plans);
    expect(span.start).toBe(new Date('2026-10-12T09:00:00Z').getTime());
  });

  it('classifyTrip: an end with no start treats the end as both bounds (past)', () => {
    const now = new Date('2026-10-15T12:00:00Z').getTime();
    expect(classifyTrip({ start: null, end: now - 1e9 }, now)).toBe('past');
  });
  it('classifyTrip: a start with no end treats the start as both bounds (upcoming)', () => {
    const now = new Date('2026-10-15T12:00:00Z').getTime();
    expect(classifyTrip({ start: now + 1e9, end: null }, now)).toBe('upcoming');
  });

  it('plansOutsideTripDates: skips dismissed parts', () => {
    const early = part({
      id: 2,
      starts_at: '2026-10-01T09:00:00Z',
      effective_at: '2026-10-01T09:00:00Z',
      dismissed_at: '2026-09-01T00:00:00Z',
    });
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [
        plan([early]),
      ]),
    ).toBe(false);
  });
  it('plansOutsideTripDates: end-only trip flags a late part', () => {
    const late = part({
      id: 3,
      starts_at: '2026-10-25T09:00:00Z',
      effective_at: '2026-10-25T09:00:00Z',
    });
    expect(plansOutsideTripDates(trip({ ends_on: '2026-10-18' }), [plan([late])])).toBe(true);
  });
  it('plansOutsideTripDates: start-only trip flags an early part', () => {
    const early = part({
      id: 4,
      starts_at: '2026-10-01T09:00:00Z',
      effective_at: '2026-10-01T09:00:00Z',
    });
    expect(plansOutsideTripDates(trip({ starts_on: '2026-10-12' }), [plan([early])])).toBe(true);
  });
  it('plansOutsideTripDates: a part with no end uses its start for the end check', () => {
    const within = part({
      id: 5,
      starts_at: '2026-10-13T09:00:00Z',
      effective_at: '2026-10-13T09:00:00Z',
      ends_at: undefined,
      end_tz: '',
    });
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [
        plan([within]),
      ]),
    ).toBe(false);
  });

  it('buildTimeline: a same-day hotel renders as a single tile (not a band)', () => {
    const plans = [
      plan(
        [
          part({
            id: 9,
            type: 'hotel',
            starts_at: '2026-10-12T09:00:00Z',
            effective_at: '2026-10-12T09:00:00Z',
            ends_at: '2026-10-12T18:00:00Z',
          }),
        ],
        { id: 2, type: 'hotel' },
      ),
    ];
    const days = buildTimeline(plans);
    expect(days).toHaveLength(1);
    expect(days[0].parts[0].edge).toBeUndefined();
  });
  it('buildTimeline: a hotel with no end is a single tile', () => {
    const plans = [
      plan([part({ id: 9, type: 'hotel', ends_at: undefined })], { id: 2, type: 'hotel' }),
    ];
    expect(buildTimeline(plans)[0].parts[0].edge).toBeUndefined();
  });

  it('isBandedPart: false when a banded type has no end', () => {
    expect(isBandedPart(part({ type: 'hotel', ends_at: undefined }))).toBe(false);
    expect(isBandedPart(part({ type: 'vehicle_hire', ends_at: undefined }))).toBe(false);
  });
  it('bandSpanDays: 0 when there is no end or an unparseable instant', () => {
    expect(bandSpanDays(part({ type: 'hotel', ends_at: undefined }))).toBe(0);
    expect(
      bandSpanDays(part({ type: 'hotel', starts_at: 'nope', ends_at: '2026-10-15T10:00:00Z' })),
    ).toBe(0);
  });

  it('fmtPartPlaces: end-only transfer still renders the arrow form', () => {
    // A transfer with a distinct (non-empty) end still reads as "start → end".
    expect(fmtPartPlaces('flight', '', 'JFK')).toBe(' → JFK');
  });
  it('fmtPartPlaces: transfer with no labels at all is empty', () => {
    expect(fmtPartPlaces('flight', '', '')).toBe('');
  });
  it('fmtPartPlaces: non-transfer with only an end label shows the end', () => {
    expect(fmtPartPlaces('hotel', '', 'The Savoy')).toBe('The Savoy');
  });
  it('fmtPartPlaces: treats undefined labels as empty', () => {
    expect(fmtPartPlaces('hotel', undefined, undefined)).toBe('');
    expect(fmtPartPlaces('hotel', 'Nobu', undefined)).toBe('Nobu');
  });

  it('buildTimeline: an unparseable instant falls back to the raw iso for its day key/label', () => {
    const plans = [
      plan([
        part({ id: 1, starts_at: 'not-a-date', effective_at: 'not-a-date', ends_at: undefined }),
      ]),
    ];
    const days = buildTimeline(plans);
    expect(days).toHaveLength(1);
    // dayKey and fmtDayHeading both fall back to the raw iso on NaN.
    expect(days[0].dayKey).toBe('not-a-date');
    expect(days[0].label).toBe('not-a-date');
  });

  it('splitLocal: empty for unparseable iso', () => {
    expect(fmtPartTimeRange(part({ starts_at: 'not-a-date', ends_at: undefined }))).toBe('');
    expect(splitLocal('not-a-date', 'UTC')).toEqual({ date: '', time: '' });
  });

  it('zonedTimeToUtc: empty for a malformed date, defaults a blank time to 00:00', () => {
    expect(zonedTimeToUtc('', '09:00', 'UTC')).toBe('');
    expect(zonedTimeToUtc('2026-10-12', '', 'UTC')).toBe('2026-10-12T00:00:00.000Z');
  });
});

describe('zone confirm and count', () => {
  const hotel = (over: Partial<PlanPart>): PlanPart =>
    ({
      id: 7,
      plan_id: 3,
      type: 'hotel',
      seq: 0,
      status: 'planned',
      starts_at: '2026-10-18T09:00:00Z',
      ends_at: '2026-10-21T01:00:00Z',
      start_tz: 'Asia/Tashkent',
      end_tz: 'Asia/Tashkent',
      start_tz_source: 'inferred',
      end_tz_source: 'inferred',
      start_label: 'Test Hotel',
      start: block('2026-10-18T09:00:00Z', 'Asia/Tashkent', { tz_source: 'inferred' }),
      end: block('2026-10-21T01:00:00Z', 'Asia/Tashkent', { tz_source: 'inferred' }),
      ...over,
    }) as PlanPart;

  it('confirms the shown zone without touching the time', () => {
    expect(zoneConfirmPatch(hotel({}), 'start', 'Asia/Tashkent')).toEqual({
      start_tz: 'Asia/Tashkent',
      start_tz_source: 'confirmed',
    });
  });

  it('keeps the shown wall-clock time when a different zone is picked', () => {
    expect(zoneConfirmPatch(hotel({}), 'start', 'Asia/Samarkand')).toEqual({
      start_tz: 'Asia/Samarkand',
      start_tz_source: 'confirmed',
      start_local: '2026-10-18T14:00',
    });
    expect(zoneConfirmPatch(hotel({}), 'end', 'Europe/London')).toEqual({
      end_tz: 'Europe/London',
      end_tz_source: 'confirmed',
      end_local: '2026-10-21T06:00',
    });
  });

  it('confirms the shown end zone without touching the time', () => {
    expect(zoneConfirmPatch(hotel({}), 'end', 'Asia/Tashkent')).toEqual({
      end_tz: 'Asia/Tashkent',
      end_tz_source: 'confirmed',
    });
  });

  it('reads a floating time by its digits', () => {
    const p = hotel({
      start_tz: '',
      start_tz_source: 'floating',
      starts_at: '2026-10-18T14:00:00Z',
      start: block('2026-10-18T14:00:00Z', ''),
    });
    expect(zoneConfirmPatch(p, 'start', 'Asia/Tashkent').start_local).toBe('2026-10-18T14:00');
  });

  it('reads a floating flight by its digits whatever its raw zone says', () => {
    // As the server sends one: the raw zone is the airport fallback.
    const p = hotel({
      type: 'flight',
      start_tz: 'Europe/London',
      start_tz_source: 'floating',
      starts_at: '2026-10-18T14:00:00Z',
      start: block('2026-10-18T14:00:00Z', ''),
    });
    expect(zoneConfirmPatch(p, 'start', 'Europe/London')).toEqual({
      start_tz: 'Europe/London',
      start_tz_source: 'confirmed',
      start_local: '2026-10-18T14:00',
    });
  });

  it('confirms an unknown zone without moving its instant', () => {
    // An unknown end's time is a true instant; only where it is was missing.
    const p = hotel({
      start_tz: '',
      start_tz_source: 'unknown',
      end_tz: '',
      end_tz_source: 'unknown',
      start: block('2026-10-18T09:00:00Z', '', { tz_source: 'unknown' }),
      end: block('2026-10-21T01:00:00Z', '', { tz_source: 'unknown' }),
    });
    expect(zoneConfirmPatch(p, 'start', 'Asia/Tashkent')).toEqual({
      start_tz: 'Asia/Tashkent',
      start_tz_source: 'confirmed',
    });
    expect(zoneConfirmPatch(p, 'end', 'Asia/Tashkent')).toEqual({
      end_tz: 'Asia/Tashkent',
      end_tz_source: 'confirmed',
    });
  });

  it('takes the scheduled time from starts_at when the start block is an estimate', () => {
    const p = hotel({
      start: block('2026-10-18T10:30:00Z', 'Asia/Tashkent', { kind: 'estimated', delay_min: 90 }),
    });
    expect(zoneConfirmPatch(p, 'start', 'Asia/Samarkand').start_local).toBe('2026-10-18T14:00');
  });

  it('counts plans with a non-dismissed part needing a zone', () => {
    const plans = [
      { parts: [hotel({ start: block('2026-10-18T09:00:00Z', '') })] },
      { parts: [hotel({ end: block('2026-10-21T01:00:00Z', '', { tz_source: 'unknown' }) })] },
      {
        parts: [
          hotel({ start: block('2026-10-18T09:00:00Z', ''), dismissed_at: '2026-10-01T00:00:00Z' }),
        ],
      },
      { parts: [hotel({})] },
    ] as Plan[];
    expect(countPlansNeedingZone(plans)).toBe(2);
  });

  it('counts a plan once when several of its parts need a zone', () => {
    const legs = [
      hotel({ start: block('2026-10-18T09:00:00Z', '') }),
      hotel({ start: block('2026-10-19T09:00:00Z', '') }),
      hotel({ start: block('2026-10-20T09:00:00Z', '') }),
    ];
    expect(countPlansNeedingZone([{ parts: legs }] as Plan[])).toBe(1);
  });
});

describe('neighbourZone', () => {
  const mk = (id: number, at: string, tz: string, endTz?: string, endAt?: string): PlanPart =>
    ({
      id,
      plan_id: id,
      type: 'hotel',
      seq: 0,
      status: 'planned',
      starts_at: at,
      start: block(at, tz),
      end: endAt ? block(endAt, endTz ?? tz) : undefined,
    }) as PlanPart;
  const plans = [
    {
      parts: [
        mk(1, '2026-10-18T04:00:00Z', 'Europe/London', 'Asia/Tashkent', '2026-10-18T12:00:00Z'),
      ],
    },
    { parts: [mk(2, '2026-10-25T04:00:00Z', '')] },
  ] as Plan[];

  it('takes the zone you are in after the latest earlier part', () => {
    expect(neighbourZone(plans, Date.parse('2026-10-19T09:00:00Z'))).toBe('Asia/Tashkent');
  });
  it('falls forward to the first later part with a zone', () => {
    expect(neighbourZone(plans, Date.parse('2026-10-01T09:00:00Z'))).toBe('Europe/London');
  });
  it('is blank with nothing to borrow from', () => {
    expect(neighbourZone([], Date.parse('2026-10-19T09:00:00Z'))).toBe('');
    expect(neighbourZone(plans, Number.NaN)).toBe('');
  });
  it('skips dismissed parts and parts without a usable time', () => {
    const dismissed = {
      ...mk(3, '2026-10-19T00:00:00Z', 'Asia/Tokyo'),
      dismissed_at: '2026-10-19T01:00:00Z',
    };
    const bad = mk(4, 'not a time', 'Asia/Seoul');
    const withEnd = [
      { parts: [dismissed, bad, mk(5, '2026-10-17T00:00:00Z', 'Europe/Paris')] },
    ] as Plan[];
    expect(neighbourZone(withEnd, Date.parse('2026-10-20T00:00:00Z'))).toBe('Europe/Paris');
  });
  it('reads a part that predates blocks through its starts_at and start_tz', () => {
    const old = [
      {
        parts: [
          {
            id: 6,
            plan_id: 6,
            type: 'hotel',
            seq: 0,
            status: 'planned',
            starts_at: '2026-10-17T00:00:00Z',
            start_tz: 'Europe/Rome',
          },
        ],
      },
    ] as Plan[];
    expect(neighbourZone(old, Date.parse('2026-10-20T00:00:00Z'))).toBe('Europe/Rome');
  });
});

describe('zone-aware days and spans', () => {
  it('uses the server span when present', () => {
    const trip = {
      starts_on: '2026-10-18',
      ends_on: '2026-10-25',
      span_start: '2026-10-17T19:00:00Z',
      span_end: '2026-10-26T04:00:00Z',
    } as Trip;
    expect(tripSpan(trip)).toEqual({
      start: Date.parse('2026-10-17T19:00:00Z'),
      end: Date.parse('2026-10-26T04:00:00Z'),
    });
    // 20:00 in New York on the last day is still during the trip.
    expect(classifyTrip(tripSpan(trip), Date.parse('2026-10-26T00:00:00Z'))).toBe('now');
  });

  it('falls back to UTC dates without a server span', () => {
    expect(tripSpan({ starts_on: '2026-10-18' } as Trip).start).toBe(
      Date.parse('2026-10-18T00:00:00Z'),
    );
  });

  it("groups the timeline by each block's own zone", () => {
    const late = {
      id: 1,
      plan_id: 1,
      type: 'dining',
      seq: 0,
      status: 'planned',
      starts_at: '2026-10-18T20:00:00Z',
      effective_at: '2026-10-18T20:00:00Z',
      start_tz: 'Asia/Tashkent',
      start: block('2026-10-18T20:00:00Z', 'Asia/Tashkent'),
    } as PlanPart;
    const days = buildTimeline([{ id: 1, parts: [late] } as Plan]);
    expect(days[0].dayKey).toBe('2026-10-19');
    expect(days[0].label).toBe('Mon, 19 Oct 2026');
  });

  it('keys external events in their own zone, UTC for all-day', () => {
    const ev = (o: Partial<ExternalEvent>) =>
      ({ starts_at: '2026-10-18T20:00:00Z', ...o }) as ExternalEvent;
    expect(buildExternalDays([ev({ start_tz: 'Asia/Tashkent' })])[0].dayKey).toBe('2026-10-19');
    expect(buildExternalDays([ev({ start_tz: 'Asia/Tashkent', all_day: true })])[0].dayKey).toBe(
      '2026-10-18',
    );
  });

  it('flags a part outside the trip dates in its own zone', () => {
    const part = {
      id: 1,
      plan_id: 1,
      type: 'dining',
      seq: 0,
      status: 'planned',
      starts_at: '2026-10-18T20:00:00Z',
      start: block('2026-10-18T20:00:00Z', 'Asia/Tashkent'),
    } as PlanPart;
    const plans = [{ id: 1, parts: [part] } as Plan];
    expect(
      plansOutsideTripDates({ starts_on: '2026-10-18', ends_on: '2026-10-18' } as Trip, plans),
    ).toBe(true);
    expect(
      plansOutsideTripDates({ starts_on: '2026-10-19', ends_on: '2026-10-19' } as Trip, plans),
    ).toBe(false);
  });
});
