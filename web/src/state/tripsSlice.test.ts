import { describe, it, expect, beforeEach, vi } from 'vitest';

import type { Plan, Trip } from '../api/types';

vi.mock('../api/client', () => ({
  ApiError: class ApiError extends Error {
    constructor(
      public status: number,
      message: string,
    ) {
      super(message);
    }
  },
  api: {
    listTrips: vi.fn(),
    getTrip: vi.fn(),
    createTrip: vi.fn(),
    updateTrip: vi.fn(),
    deleteTrip: vi.fn(),
    addTripMember: vi.fn(),
    removeTripMember: vi.fn(),
    addTripPassenger: vi.fn(),
    removeTripPassenger: vi.fn(),
    setTripTags: vi.fn(),
    suggestTags: vi.fn(),
    setTripShareAllFriends: vi.fn(),
    notifyTripShares: vi.fn(),
  },
}));

import { api, ApiError } from '../api/client';
import { useStore } from './store';

const mockApi = api as unknown as Record<string, ReturnType<typeof vi.fn>>;

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'NYC',
    destination: 'New York',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '',
    updated_at: '',
    ...over,
  } as Trip;
}

function tripWithPlans(over: Partial<Trip> = {}, plans: Plan[] = []): Trip & { plans: Plan[] } {
  return { ...trip(over), plans } as Trip & { plans: Plan[] };
}

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState(
    { trips: [], currentTrip: null, tripsLoading: false, tagSuggestions: [], error: null },
    false,
  );
});

describe('listTrips', () => {
  it('loads trips on success', async () => {
    mockApi.listTrips.mockResolvedValue([trip({ id: 1 }), trip({ id: 2 })]);
    await useStore.getState().listTrips();
    const s = useStore.getState();
    expect(s.trips).toHaveLength(2);
    expect(s.tripsLoading).toBe(false);
  });

  it('sets error and clears loading on failure', async () => {
    mockApi.listTrips.mockRejectedValue(new Error('boom'));
    await useStore.getState().listTrips();
    const s = useStore.getState();
    expect(s.error).toBe('boom');
    expect(s.tripsLoading).toBe(false);
  });

  it('stringifies a non-Error rejection', async () => {
    mockApi.listTrips.mockRejectedValue('strerr');
    await useStore.getState().listTrips();
    expect(useStore.getState().error).toBe('strerr');
  });

  it('does not prefetch trip details when no service worker is present', async () => {
    // jsdom has no navigator.serviceWorker, so the offline cache warm-up is a
    // no-op and the list load makes no per-trip requests.
    mockApi.listTrips.mockResolvedValue([trip({ id: 1 }), trip({ id: 2 })]);
    await useStore.getState().listTrips();
    expect(mockApi.getTrip).not.toHaveBeenCalled();
  });

  it('prefetches each trip detail to warm the offline cache when controlled and online', async () => {
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: {} });
    try {
      mockApi.listTrips.mockResolvedValue([trip({ id: 1 }), trip({ id: 2 })]);
      mockApi.getTrip.mockResolvedValue(tripWithPlans({ id: 1 }));
      await useStore.getState().listTrips();
      expect(mockApi.getTrip).toHaveBeenCalledWith(1);
      expect(mockApi.getTrip).toHaveBeenCalledWith(2);
    } finally {
      delete (navigator as { serviceWorker?: unknown }).serviceWorker;
    }
  });
});

describe('loadTrip', () => {
  it('loads the trip into currentTrip on success', async () => {
    mockApi.getTrip.mockResolvedValue(tripWithPlans({ id: 5 }));
    await useStore.getState().loadTrip(5);
    expect(useStore.getState().currentTrip?.id).toBe(5);
    expect(useStore.getState().currentTripStatus).toBe('loaded');
  });

  it('sets error and an error status on failure when online', async () => {
    mockApi.getTrip.mockRejectedValue(new Error('nope'));
    await useStore.getState().loadTrip(5);
    expect(useStore.getState().error).toBe('nope');
    expect(useStore.getState().currentTripStatus).toBe('error');
  });

  it('marks the trip unavailable without a toast when offline', async () => {
    const original = navigator.onLine;
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false });
    try {
      mockApi.getTrip.mockRejectedValue(new Error('Failed to fetch'));
      await useStore.getState().loadTrip(5);
      expect(useStore.getState().currentTripStatus).toBe('error');
      // No noisy snackbar offline — the in-page message + offline banner suffice.
      expect(useStore.getState().error).toBeNull();
    } finally {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: original });
    }
  });

  it('silently clears a current trip that 404s (e.g. just deleted), no error', async () => {
    useStore.setState({ currentTrip: trip({ id: 5 }), error: null });
    mockApi.getTrip.mockRejectedValue(new ApiError(404, 'not found'));
    await useStore.getState().loadTrip(5);
    expect(useStore.getState().currentTrip).toBeNull();
    expect(useStore.getState().error).toBeNull();
  });

  it('leaves a different current trip intact on a 404 and stays quiet', async () => {
    useStore.setState({ currentTrip: trip({ id: 9 }), error: null });
    mockApi.getTrip.mockRejectedValue(new ApiError(404, 'not found'));
    await useStore.getState().loadTrip(5);
    expect(useStore.getState().currentTrip?.id).toBe(9);
    expect(useStore.getState().error).toBeNull();
  });
});

describe('clearCurrentTrip', () => {
  it('nulls the current trip', () => {
    useStore.setState({ currentTrip: tripWithPlans({ id: 1 }) });
    useStore.getState().clearCurrentTrip();
    expect(useStore.getState().currentTrip).toBeNull();
  });
});

describe('createTrip', () => {
  it('appends the created trip and returns it', async () => {
    useStore.setState({ trips: [trip({ id: 1 })] });
    mockApi.createTrip.mockResolvedValue(trip({ id: 2 }));
    const result = await useStore.getState().createTrip({ name: 'Paris' } as never);
    expect(result?.id).toBe(2);
    expect(useStore.getState().trips.map((t) => t.id)).toEqual([1, 2]);
  });

  it('sets error and returns undefined on failure', async () => {
    mockApi.createTrip.mockRejectedValue(new Error('fail'));
    const result = await useStore.getState().createTrip({ name: 'Paris' } as never);
    expect(result).toBeUndefined();
    expect(useStore.getState().error).toBe('fail');
  });
});

describe('updateTrip', () => {
  it('replaces the trip in the list and patches currentTrip when it matches', async () => {
    useStore.setState({
      trips: [trip({ id: 1, name: 'old' })],
      currentTrip: tripWithPlans({ id: 1, name: 'old' }, [{ id: 9 } as Plan]),
    });
    mockApi.updateTrip.mockResolvedValue(trip({ id: 1, name: 'new' }));
    await useStore.getState().updateTrip(1, { name: 'new' });
    const s = useStore.getState();
    expect(s.trips[0].name).toBe('new');
    expect(s.currentTrip?.name).toBe('new');
    // plans preserved
    expect(s.currentTrip?.plans.map((p) => p.id)).toEqual([9]);
  });

  it('leaves currentTrip untouched when ids differ', async () => {
    useStore.setState({
      trips: [trip({ id: 2 })],
      currentTrip: tripWithPlans({ id: 99, name: 'mine' }),
    });
    mockApi.updateTrip.mockResolvedValue(trip({ id: 2, name: 'new' }));
    await useStore.getState().updateTrip(2, { name: 'new' });
    expect(useStore.getState().currentTrip?.name).toBe('mine');
  });

  it('handles a null currentTrip', async () => {
    useStore.setState({ trips: [trip({ id: 2 })], currentTrip: null });
    mockApi.updateTrip.mockResolvedValue(trip({ id: 2, name: 'new' }));
    await useStore.getState().updateTrip(2, { name: 'new' });
    expect(useStore.getState().currentTrip).toBeNull();
  });
});

describe('deleteTrip', () => {
  it('removes the trip and clears currentTrip when it matches', async () => {
    useStore.setState({
      trips: [trip({ id: 1 }), trip({ id: 2 })],
      currentTrip: tripWithPlans({ id: 1 }),
    });
    mockApi.deleteTrip.mockResolvedValue(undefined);
    await useStore.getState().deleteTrip(1);
    const s = useStore.getState();
    expect(s.trips.map((t) => t.id)).toEqual([2]);
    expect(s.currentTrip).toBeNull();
  });

  it('keeps currentTrip when a different trip is deleted', async () => {
    useStore.setState({
      trips: [trip({ id: 1 }), trip({ id: 2 })],
      currentTrip: tripWithPlans({ id: 2 }),
    });
    mockApi.deleteTrip.mockResolvedValue(undefined);
    await useStore.getState().deleteTrip(1);
    expect(useStore.getState().currentTrip?.id).toBe(2);
  });
});

describe('addTripMember', () => {
  it('replaces the matching trip with the updated one', async () => {
    useStore.setState({ trips: [trip({ id: 1, name: 'old' }), trip({ id: 2 })] });
    mockApi.addTripMember.mockResolvedValue(trip({ id: 1, name: 'updated' }));
    await useStore.getState().addTripMember(1, { user_id: 7 } as never);
    expect(useStore.getState().trips.find((t) => t.id === 1)?.name).toBe('updated');
  });
});

describe('removeTripMember', () => {
  it('refetches the trip via loadTrip', async () => {
    mockApi.removeTripMember.mockResolvedValue(undefined);
    mockApi.getTrip.mockResolvedValue(tripWithPlans({ id: 3, name: 'reloaded' }));
    await useStore.getState().removeTripMember(3, 7);
    expect(mockApi.removeTripMember).toHaveBeenCalledWith(3, 7);
    expect(useStore.getState().currentTrip?.name).toBe('reloaded');
  });
});

describe('addTripPassenger', () => {
  it('adds the passenger then refetches the trip via loadTrip', async () => {
    mockApi.addTripPassenger.mockResolvedValue(trip({ id: 4 }));
    mockApi.getTrip.mockResolvedValue(tripWithPlans({ id: 4, name: 'reloaded' }));
    await useStore.getState().addTripPassenger(4, 7);
    expect(mockApi.addTripPassenger).toHaveBeenCalledWith(4, 7);
    expect(useStore.getState().currentTrip?.name).toBe('reloaded');
  });
});

describe('removeTripPassenger', () => {
  it('removes the passenger then refetches the trip via loadTrip', async () => {
    mockApi.removeTripPassenger.mockResolvedValue(undefined);
    mockApi.getTrip.mockResolvedValue(tripWithPlans({ id: 4, name: 'reloaded' }));
    await useStore.getState().removeTripPassenger(4, 7);
    expect(mockApi.removeTripPassenger).toHaveBeenCalledWith(4, 7);
    expect(useStore.getState().currentTrip?.name).toBe('reloaded');
  });
});

describe('setTripTags', () => {
  it('replaces the matching trip with the tagged one', async () => {
    useStore.setState({ trips: [trip({ id: 1 })] });
    mockApi.setTripTags.mockResolvedValue(
      trip({ id: 1, tags: [{ id: 1, label: 'beach' }] as never }),
    );
    await useStore.getState().setTripTags(1, ['beach']);
    expect(useStore.getState().trips[0].tags).toHaveLength(1);
  });
});

describe('suggestTags', () => {
  it('stores suggestions on success', async () => {
    mockApi.suggestTags.mockResolvedValue([{ label: 'beach', trip_count: 2 }]);
    await useStore.getState().suggestTags('be');
    expect(useStore.getState().tagSuggestions).toHaveLength(1);
  });

  it('swallows errors leaving state untouched', async () => {
    useStore.setState({ tagSuggestions: [{ label: 'x', trip_count: 1 } as never] });
    mockApi.suggestTags.mockRejectedValue(new Error('boom'));
    await useStore.getState().suggestTags('x');
    expect(useStore.getState().tagSuggestions).toHaveLength(1);
    expect(useStore.getState().error).toBeNull();
  });
});

describe('setTripShareAllFriends', () => {
  it('replaces the matching trip with the updated one', async () => {
    useStore.setState({ trips: [trip({ id: 1, name: 'old' }), trip({ id: 2 })] });
    mockApi.setTripShareAllFriends.mockResolvedValue(trip({ id: 1, name: 'updated' }));
    await useStore.getState().setTripShareAllFriends(1, 'viewer');
    expect(mockApi.setTripShareAllFriends).toHaveBeenCalledWith(1, 'viewer');
    expect(useStore.getState().trips.find((t) => t.id === 1)?.name).toBe('updated');
    // unrelated trip untouched
    expect(useStore.getState().trips.find((t) => t.id === 2)?.id).toBe(2);
  });

  it('passes null role through to the client', async () => {
    useStore.setState({ trips: [trip({ id: 3 })] });
    mockApi.setTripShareAllFriends.mockResolvedValue(trip({ id: 3 }));
    await useStore.getState().setTripShareAllFriends(3, null);
    expect(mockApi.setTripShareAllFriends).toHaveBeenCalledWith(3, null);
  });
});

describe('notifyTripShares', () => {
  it('calls the client with the given tripId and input', async () => {
    mockApi.notifyTripShares.mockResolvedValue(undefined);
    const input = { user_ids: [7, 8], emails: ['a@b.com'] };
    await useStore.getState().notifyTripShares(1, input);
    expect(mockApi.notifyTripShares).toHaveBeenCalledWith(1, input);
  });
});
