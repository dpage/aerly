import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';

import type { Flight } from '../api/types';
import { useStore } from './store';
import { isOld, useVisibleFlights, OLD_TICK_MS } from './visibleFlights';

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA1',
    scheduled_out: '2024-01-01T08:00:00Z',
    scheduled_in: '2024-01-01T10:00:00Z',
    origin_iata: 'LHR',
    dest_iata: 'JFK',
    status: 'Scheduled',
    notes: '',
    passenger_ids: [],
    is_public: false,
    shared_user_ids: [],
    ...over,
  };
}

const initial = useStore.getState();

beforeEach(() => {
  useStore.setState({ ...initial, flights: [], showOld: false }, true);
});

describe('isOld', () => {
  const now = Date.parse('2024-01-02T12:00:00Z'); // reference clock
  const dayMs = 24 * 60 * 60 * 1000;

  it('uses actual_in when present', () => {
    const f = flight({ actual_in: '2024-01-01T11:00:00Z' }); // 25h ago
    expect(isOld(f, now)).toBe(true);
  });

  it('falls back to estimated_in when actual_in is missing', () => {
    const f = flight({ estimated_in: '2024-01-01T11:30:00Z' }); // 24.5h ago
    expect(isOld(f, now)).toBe(true);
  });

  it('falls back to scheduled_in when both actual and estimated are missing', () => {
    const f = flight({ scheduled_in: '2024-01-02T00:00:00Z' }); // 12h ago
    expect(isOld(f, now)).toBe(false);
  });

  it('treats a flight at exactly 24h as not-old (matches server >= predicate)', () => {
    const f = flight({ actual_in: new Date(now - dayMs).toISOString() });
    expect(isOld(f, now)).toBe(false);
  });

  it('treats an unparseable timestamp as not-old', () => {
    const f = flight({ scheduled_in: 'not a date' });
    expect(isOld(f, now)).toBe(false);
  });
});

describe('useVisibleFlights', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2024-01-02T12:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns the full list when showOld is true', () => {
    useStore.setState(
      {
        flights: [
          flight({ id: 1, scheduled_in: '2024-01-02T11:30:00Z' }),
          flight({ id: 2, actual_in: '2024-01-01T10:00:00Z' }),
        ],
        showOld: true,
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1, 2]);
  });

  it('hides old flights when showOld is false', () => {
    useStore.setState(
      {
        flights: [
          flight({ id: 1, scheduled_in: '2024-01-02T11:30:00Z' }),
          flight({ id: 2, actual_in: '2024-01-01T10:00:00Z' }), // 26h ago
        ],
        showOld: false,
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current.map((f) => f.id)).toEqual([1]);
  });

  it('drops a flight that ages past 24h on the next tick', () => {
    // Flight will cross the 24h threshold 30 minutes into the future.
    const arrIso = new Date(Date.now() - 23.5 * 60 * 60 * 1000).toISOString();
    useStore.setState(
      {
        flights: [flight({ id: 1, actual_in: arrIso })],
        showOld: false,
      },
      false,
    );
    const { result } = renderHook(() => useVisibleFlights());
    expect(result.current).toHaveLength(1);

    // Advance system clock by 31 minutes (now ~24.0h since arrival), then
    // fire the OLD_TICK_MS interval that the hook installed.
    act(() => {
      vi.setSystemTime(new Date(Date.now() + 31 * 60 * 1000));
      vi.advanceTimersByTime(OLD_TICK_MS);
    });
    expect(result.current).toHaveLength(0);
  });
});
