import { describe, it, expect } from 'vitest';

import type { Plan, PlanPart, Trip } from '../api/types';
import {
  buildTimeline,
  classifyTrip,
  fmtPartTimeRange,
  fmtTripDates,
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

describe('fmtPartTimeRange', () => {
  it('renders a single time without an end', () => {
    expect(fmtPartTimeRange(part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined }))).toBe(
      '09:00',
    );
  });

  it('adds a UTC suffix when the tz is unknown', () => {
    expect(
      fmtPartTimeRange(part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined, start_tz: '' })),
    ).toBe('09:00 UTC');
  });
  it('renders a range with each end in its own tz', () => {
    const out = fmtPartTimeRange(
      part({
        starts_at: '2026-07-01T10:00:00Z',
        ends_at: '2026-07-01T14:00:00Z',
        start_tz: 'Europe/London',
        end_tz: 'America/New_York',
      }),
    );
    expect(out).toBe('11:00 → 10:00');
  });
});

describe('planTypeLabel', () => {
  it('labels ground transport', () => {
    expect(planTypeLabel('ground')).toBe('Ground transport');
  });
});
