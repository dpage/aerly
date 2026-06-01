import { describe, it, expect } from 'vitest';

import type { Plan, PlanPart, Trip } from '../api/types';
import {
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
        part({ id: 2, starts_at: '2026-10-18T09:00:00Z', effective_at: '2026-10-18T09:00:00Z', ends_at: '2026-10-18T11:00:00Z' }),
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
    expect(span.end).toBe(new Date('2026-10-18T00:00:00Z').getTime());
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
  const within = part({ id: 1, starts_at: '2026-10-13T09:00:00Z', effective_at: '2026-10-13T09:00:00Z' });
  it('false when no explicit trip dates', () => {
    expect(plansOutsideTripDates(trip(), [plan([within])])).toBe(false);
  });
  it('false when all parts are within the dates', () => {
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [plan([within])]),
    ).toBe(false);
  });
  it('true when a part starts before the trip', () => {
    const early = part({ id: 2, starts_at: '2026-10-01T09:00:00Z', effective_at: '2026-10-01T09:00:00Z' });
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [plan([early])]),
    ).toBe(true);
  });
  it('true when a part ends after the trip', () => {
    const late = part({ id: 3, starts_at: '2026-10-25T09:00:00Z', effective_at: '2026-10-25T09:00:00Z' });
    expect(
      plansOutsideTripDates(trip({ starts_on: '2026-10-12', ends_on: '2026-10-18' }), [plan([late])]),
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
    const plans = [
      plan([part({ id: 1, dismissed_at: '2026-09-01T00:00:00Z' })]),
    ];
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
      fmtPartTimeRange(part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined, start_tz: '' })),
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
    expect(fmtLocalDateTime('2026-07-01T14:00:00Z', 'America/New_York')).toMatch(
      /Jul.*10:00 EDT$/,
    );
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
});
