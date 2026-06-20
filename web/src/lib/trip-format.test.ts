import { describe, it, expect } from 'vitest';

import type { ExternalEvent, Plan, PlanPart, Trip } from '../api/types';
import {
  buildExternalDays,
  buildTimeline,
  classifyTrip,
  fmtPartPlaces,
  fmtPartTimeRange,
  fmtLocalDateTime,
  fmtTripDates,
  plansOutsideTripDates,
  splitLocal,
  tzAbbrev,
  zonedTimeToUtc,
  hotelNights,
  isHotelBand,
  planTypeLabel,
  tripSpan,
} from './trip-format';

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
  return {
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
    ...over,
  };
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
    expect(days[0].parts[0].edge).toBe('check-in');
    expect(days[1].parts[0].edge).toBe('check-out');
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
    expect(days[0].parts[1].edge).toBe('check-in');
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

describe('hotel band', () => {
  it('isHotelBand true for a multi-night hotel', () => {
    const p = part({
      type: 'hotel',
      starts_at: '2026-10-12T15:00:00Z',
      ends_at: '2026-10-15T10:00:00Z',
    });
    expect(isHotelBand(p)).toBe(true);
    expect(hotelNights(p)).toBe(3);
  });

  it('isHotelBand false for a flight', () => {
    expect(isHotelBand(part({ type: 'flight', ends_at: '2026-10-12T11:00:00Z' }))).toBe(false);
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

describe('fmtPartTimeRange', () => {
  it('renders a single time with its tz abbreviation', () => {
    expect(fmtPartTimeRange(part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined }))).toBe(
      '09:00 UTC',
    );
  });

  it('falls back to a UTC suffix when the tz is unknown', () => {
    expect(
      fmtPartTimeRange(
        part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined, start_tz: '' }),
      ),
    ).toBe('09:00 UTC');
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
    // 11:00 BST (London summer) → 10:00 EDT (New York summer); the exact London
    // abbreviation (BST vs GMT+1) depends on the runtime ICU, so match loosely.
    expect(out).toMatch(/^11:00 \S+ → 10:00 EDT$/);
  });
});

describe('tzAbbrev', () => {
  it('returns a real abbreviation for a known zone', () => {
    expect(tzAbbrev('2026-07-01T14:00:00Z', 'America/New_York')).toBe('EDT');
  });
  it('falls back to UTC when the zone is unknown', () => {
    expect(tzAbbrev('2026-07-01T14:00:00Z', '')).toBe('UTC');
  });
});

describe('fmtLocalDateTime', () => {
  it('renders date + local time + tz abbreviation', () => {
    // 14:00Z → 10:00 EDT on the same day.
    expect(fmtLocalDateTime('2026-07-01T14:00:00Z', 'America/New_York')).toMatch(/Jul.*10:00 EDT$/);
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
    expect(planTypeLabel('hotel')).toBe('Hotel');
    expect(planTypeLabel('dining')).toBe('Dining');
    expect(planTypeLabel('excursion')).toBe('Excursion');
    expect(planTypeLabel('ice_cream')).toBe('Ice cream');
    expect(planTypeLabel('meeting')).toBe('Meeting');
    expect(planTypeLabel('event')).toBe('Event');
  });
  it('falls back to the raw type for an unknown value', () => {
    expect(planTypeLabel('mystery' as unknown as Parameters<typeof planTypeLabel>[0])).toBe(
      'mystery',
    );
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

  it('isHotelBand: false when a hotel has no end', () => {
    expect(isHotelBand(part({ type: 'hotel', ends_at: undefined }))).toBe(false);
  });
  it('hotelNights: 0 when there is no end or an unparseable instant', () => {
    expect(hotelNights(part({ type: 'hotel', ends_at: undefined }))).toBe(0);
    expect(
      hotelNights(part({ type: 'hotel', starts_at: 'nope', ends_at: '2026-10-15T10:00:00Z' })),
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
    // localDayKey + fmtDayHeader both fall back to the raw iso on NaN.
    expect(days[0].dayKey).toBe('not-a-date');
    expect(days[0].label).toBe('not-a-date');
  });

  it('fmtTimeOfDay / tzAbbrev / fmtLocalDateTime / splitLocal: empty for unparseable iso', () => {
    expect(fmtPartTimeRange(part({ starts_at: 'not-a-date', ends_at: undefined }))).toBe('');
    expect(tzAbbrev('not-a-date', 'UTC')).toBe('');
    expect(fmtLocalDateTime('not-a-date', 'UTC')).toBe('');
    expect(splitLocal('not-a-date', 'UTC')).toEqual({ date: '', time: '' });
  });

  it('zonedTimeToUtc: empty for a malformed date, defaults a blank time to 00:00', () => {
    expect(zonedTimeToUtc('', '09:00', 'UTC')).toBe('');
    expect(zonedTimeToUtc('2026-10-12', '', 'UTC')).toBe('2026-10-12T00:00:00.000Z');
  });
});
