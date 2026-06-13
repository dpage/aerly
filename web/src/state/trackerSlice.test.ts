import { describe, it, expect, beforeEach, vi } from 'vitest';

import type { Plan, PlanPart, TrackerPart, Trip } from '../api/types';

vi.mock('../api/client', () => ({
  ApiError: class {},
  api: { getTracker: vi.fn() },
}));

import { api } from '../api/client';
import { useStore } from './store';
import { loadFilters } from './trackerSlice';

const mockApi = api as unknown as Record<string, ReturnType<typeof vi.fn>>;

function trackerPart(over: Partial<TrackerPart> = {}): TrackerPart {
  return {
    plan_part_id: 10,
    plan_id: 20,
    trip_id: 30,
    title: 'BA1',
    status: 'Scheduled',
    effective_at: '2024-01-01T10:00:00Z',
    ident: 'BA1',
    dest_iata: 'JFK',
    ...over,
  };
}

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 10,
    plan_id: 20,
    type: 'flight',
    seq: 0,
    starts_at: '2024-01-01T10:00:00Z',
    start_tz: 'UTC',
    end_tz: 'UTC',
    start_label: 'LHR',
    end_label: 'JFK',
    status: 'planned',
    effective_at: '2024-01-01T10:00:00Z',
    flight: {
      ident: 'BA1',
      callsign: 'BAW1',
      scheduled_out: '2024-01-01T10:00:00Z',
      scheduled_in: '2024-01-01T18:00:00Z',
      origin_iata: 'LHR',
      dest_iata: 'JFK',
      flight_status: 'Scheduled',
    },
    ...over,
  };
}

function trip(plans: Plan[]): Trip & { plans: Plan[] } {
  return {
    id: 30,
    name: 'NYC',
    destination: 'New York',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '',
    updated_at: '',
    plans,
  };
}

function plan(parts: PlanPart[]): Plan {
  return {
    id: 20,
    trip_id: 30,
    type: 'flight',
    title: 'Outbound',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts,
    created_at: '',
    updated_at: '',
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState(
    {
      trackerParts: [],
      currentTrip: null,
      trackerTag: '',
      trackerWindow: {},
      error: null,
      trackerMineOnly: false,
      trackerHiddenTypes: [],
    },
    false,
  );
});

describe('loadTracker', () => {
  it('loads parts using the tag and explicit window, persisting the window', async () => {
    const store: Record<string, string> = {};
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: (k: string, v: string) => {
          store[k] = String(v);
        },
        removeItem: (k: string) => {
          delete store[k];
        },
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    mockApi.getTracker.mockResolvedValue({ parts: [part({ id: 1 })] });
    await useStore.getState().loadTracker({ tag: 'work', window: { from: '2024-01-01' } });
    expect(mockApi.getTracker).toHaveBeenCalledWith({
      from: '2024-01-01',
      to: undefined,
      tag: 'work',
    });
    const s = useStore.getState();
    expect(s.trackerTag).toBe('work');
    expect(s.trackerParts.map((p) => p.id)).toEqual([1]);
    expect(s.trackerLoading).toBe(false);
    // window persisted under the target tag
    expect(store['tracker.window.work']).toBe(JSON.stringify({ from: '2024-01-01' }));
  });

  it('falls back to the current tag and its saved window when no opts given', async () => {
    useStore.setState({ trackerTag: '' });
    mockApi.getTracker.mockResolvedValue({ parts: [] });
    await useStore.getState().loadTracker();
    // empty tag -> tag: undefined in the request
    expect(mockApi.getTracker).toHaveBeenCalledWith({
      from: undefined,
      to: undefined,
      tag: undefined,
    });
  });

  it('sets error and clears loading when the API rejects', async () => {
    mockApi.getTracker.mockRejectedValue(new Error('boom'));
    await useStore.getState().loadTracker({ tag: 'work' });
    const s = useStore.getState();
    expect(s.error).toBe('boom');
    expect(s.trackerLoading).toBe(false);
  });

  it('stringifies a non-Error rejection', async () => {
    mockApi.getTracker.mockRejectedValue('strerr');
    await useStore.getState().loadTracker();
    expect(useStore.getState().error).toBe('strerr');
  });
});

describe('window persistence branches', () => {
  it('loads a persisted window (from + to) for the target tag', async () => {
    const store: Record<string, string> = {
      'tracker.window.trip': JSON.stringify({ from: '2024-03-01', to: '2024-03-31' }),
    };
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: (k: string, v: string) => {
          store[k] = String(v);
        },
        removeItem: () => {},
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    mockApi.getTracker.mockResolvedValue({ parts: [] });
    await useStore.getState().loadTracker({ tag: 'trip' });
    expect(mockApi.getTracker).toHaveBeenCalledWith({
      from: '2024-03-01',
      to: '2024-03-31',
      tag: 'trip',
    });
    expect(useStore.getState().trackerWindow).toEqual({ from: '2024-03-01', to: '2024-03-31' });
  });

  it('ignores non-string from/to in a persisted window', async () => {
    const store: Record<string, string> = {
      'tracker.window.bad': JSON.stringify({ from: 5, to: null }),
    };
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: () => {},
        removeItem: () => {},
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    mockApi.getTracker.mockResolvedValue({ parts: [] });
    await useStore.getState().loadTracker({ tag: 'bad' });
    expect(useStore.getState().trackerWindow).toEqual({});
  });

  it('swallows malformed JSON / localStorage throws when loading the window', async () => {
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: () => {
          throw new Error('blocked');
        },
        setItem: () => {
          throw new Error('blocked');
        },
        removeItem: () => {},
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    mockApi.getTracker.mockResolvedValue({ parts: [] });
    // No explicit window -> loadWindow('x') runs and its getItem throws,
    // exercising the loadWindow catch; falls back to {}.
    await useStore.getState().loadTracker({ tag: 'x' });
    expect(useStore.getState().trackerWindow).toEqual({});
    expect(useStore.getState().trackerParts).toEqual([]);
  });
});

describe('applyPlanPartUpdate fold fallbacks', () => {
  it('keeps existing status/position when the update omits them', () => {
    const pos = { ts: '2024-01-01T12:00:00Z', lat: 1, lon: 2, is_estimated: false };
    useStore.setState({
      trackerParts: [
        part({
          id: 10,
          status: 'planned',
          flight: {
            ident: 'BA1',
            callsign: 'BAW1',
            scheduled_out: '',
            scheduled_in: '',
            origin_iata: 'LHR',
            dest_iata: 'JFK',
            flight_status: 'Scheduled',
            latest_position: pos,
          },
        }),
      ],
    });
    // status: '' and effective_at: '' force the `|| pp.*` fallbacks; no
    // latest_position forces the `?? pp.flight.latest_position` fallback.
    useStore
      .getState()
      .applyPlanPartUpdate(trackerPart({ plan_part_id: 10, status: '', effective_at: '' }));
    const row = useStore.getState().trackerParts[0];
    expect(row.status).toBe('planned');
    expect(row.flight?.flight_status).toBe('Scheduled');
    expect(row.flight?.latest_position).toEqual(pos);
  });

  it('folds into a part without flight detail (flight is undefined)', () => {
    useStore.setState({ trackerParts: [part({ id: 10, flight: undefined })] });
    useStore.getState().applyPlanPartUpdate(trackerPart({ plan_part_id: 10, status: 'Enroute' }));
    const row = useStore.getState().trackerParts[0];
    expect(row.status).toBe('Enroute');
    expect(row.flight).toBeUndefined();
  });

  it('keeps existing track/last_polled_at when the update omits them', () => {
    const track = [{ ts: '2024-01-01T11:00:00Z', lat: 1, lon: 2, is_estimated: false }];
    useStore.setState({
      trackerParts: [
        part({
          id: 10,
          flight: {
            ident: 'BA1',
            callsign: 'BAW1',
            scheduled_out: '',
            scheduled_in: '',
            origin_iata: 'LHR',
            dest_iata: 'JFK',
            flight_status: 'Enroute',
            last_polled_at: '2024-01-01T11:30:00Z',
            track,
          },
        }),
      ],
    });
    useStore.getState().applyPlanPartUpdate(trackerPart({ plan_part_id: 10, status: 'Enroute' }));
    const row = useStore.getState().trackerParts[0];
    expect(row.flight?.last_polled_at).toBe('2024-01-01T11:30:00Z');
    expect(row.flight?.track).toEqual(track);
  });
});

describe('setTrackerWindow', () => {
  it('merges the patch into the window and reloads via the current tag', async () => {
    useStore.setState({ trackerTag: 'fam', trackerWindow: { from: '2024-01-01' } });
    mockApi.getTracker.mockResolvedValue({ parts: [] });
    await useStore.getState().setTrackerWindow({ to: '2024-02-01' });
    expect(mockApi.getTracker).toHaveBeenCalledWith({
      from: '2024-01-01',
      to: '2024-02-01',
      tag: 'fam',
    });
  });
});

describe('applyPlanPartUpdate', () => {
  it('folds a live update into the matching full part in place', () => {
    const pos = { ts: '2024-01-01T12:00:00Z', lat: 51, lon: -1, is_estimated: false };
    useStore.setState({ trackerParts: [part()] });
    useStore
      .getState()
      .applyPlanPartUpdate(trackerPart({ status: 'Enroute', latest_position: pos }));
    const row = useStore.getState().trackerParts[0];
    // The thin update folds in without wiping the part's coords/detail.
    expect(row.status).toBe('Enroute');
    expect(row.flight?.flight_status).toBe('Enroute');
    expect(row.flight?.latest_position).toEqual(pos);
    expect(row.start_label).toBe('LHR'); // untouched
  });

  it('folds the live track + last_polled_at so the polyline grows and freshness stays live', () => {
    const track = [
      { ts: '2024-01-01T12:00:00Z', lat: 51, lon: -1, is_estimated: false },
      { ts: '2024-01-01T12:01:00Z', lat: 52, lon: -2, is_estimated: false },
    ];
    useStore.setState({ trackerParts: [part()] });
    useStore
      .getState()
      .applyPlanPartUpdate(
        trackerPart({ status: 'Enroute', last_polled_at: '2024-01-01T12:01:30Z', track }),
      );
    const row = useStore.getState().trackerParts[0];
    expect(row.flight?.track).toEqual(track);
    expect(row.flight?.last_polled_at).toBe('2024-01-01T12:01:30Z');
  });

  it('does not insert a part absent from the list (window/visibility-scoped)', () => {
    useStore.setState({ trackerParts: [part({ id: 1 })] });
    useStore.getState().applyPlanPartUpdate(trackerPart({ plan_part_id: 999 }));
    expect(useStore.getState().trackerParts.map((p) => p.id)).toEqual([1]);
  });

  it('folds live status/position into the matching part of the open trip', () => {
    useStore.setState({ currentTrip: trip([plan([part()])]) });
    const pos = {
      ts: '2024-01-01T12:00:00Z',
      lat: 51,
      lon: -1,
      is_estimated: false,
    };
    useStore
      .getState()
      .applyPlanPartUpdate(trackerPart({ status: 'Enroute', latest_position: pos }));
    const updated = useStore.getState().currentTrip!.plans[0].parts[0];
    expect(updated.status).toBe('Enroute');
    expect(updated.flight?.flight_status).toBe('Enroute');
    expect(updated.flight?.latest_position).toEqual(pos);
  });

  it('ignores an event for a trip other than the one on screen', () => {
    const open = trip([plan([part()])]);
    useStore.setState({ currentTrip: open });
    useStore.getState().applyPlanPartUpdate(trackerPart({ trip_id: 999, status: 'Enroute' }));
    // The open trip is untouched (same object identity, unchanged status).
    expect(useStore.getState().currentTrip).toBe(open);
    expect(useStore.getState().currentTrip!.plans[0].parts[0].status).toBe('planned');
  });
});

describe('tracker filters', () => {
  function stubLocalStorage(seed: Record<string, string> = {}): Record<string, string> {
    const store = { ...seed };
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: (k: string, v: string) => {
          store[k] = String(v);
        },
        removeItem: (k: string) => {
          delete store[k];
        },
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    return store;
  }

  it('toggles "mine only" and persists it', () => {
    const store = stubLocalStorage();
    useStore.getState().setTrackerMineOnly(true);
    expect(useStore.getState().trackerMineOnly).toBe(true);
    expect(JSON.parse(store['tracker.filters'])).toEqual({ mineOnly: true, hiddenTypes: [] });
  });

  it('toggles a plan type on and off, persisting each change', () => {
    const store = stubLocalStorage();
    useStore.getState().toggleTrackerType('excursion');
    expect(useStore.getState().trackerHiddenTypes).toEqual(['excursion']);
    useStore.getState().toggleTrackerType('dining');
    expect(useStore.getState().trackerHiddenTypes).toEqual(['excursion', 'dining']);
    useStore.getState().toggleTrackerType('excursion');
    expect(useStore.getState().trackerHiddenTypes).toEqual(['dining']);
    expect(JSON.parse(store['tracker.filters'])).toEqual({
      mineOnly: false,
      hiddenTypes: ['dining'],
    });
  });

  it('loadFilters reads valid persisted filters', () => {
    stubLocalStorage({
      'tracker.filters': JSON.stringify({ mineOnly: true, hiddenTypes: ['hotel', 'dining'] }),
    });
    expect(loadFilters()).toEqual({ mineOnly: true, hiddenTypes: ['hotel', 'dining'] });
  });

  it('loadFilters falls back to defaults on malformed JSON', () => {
    stubLocalStorage({ 'tracker.filters': '{ not json' });
    expect(loadFilters()).toEqual({ mineOnly: false, hiddenTypes: [] });
  });

  it('loadFilters drops unknown types and only treats true as mine-only', () => {
    stubLocalStorage({
      'tracker.filters': JSON.stringify({ mineOnly: 'yes', hiddenTypes: ['flight', 'bogus'] }),
    });
    expect(loadFilters()).toEqual({ mineOnly: false, hiddenTypes: ['flight'] });
  });
});
