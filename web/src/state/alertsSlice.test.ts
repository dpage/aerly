import { describe, it, expect, beforeEach, vi } from 'vitest';

import type { AlertPrefs, Plan, Trip } from '../api/types';

vi.mock('../api/client', () => ({
  ApiError: class {},
  api: {
    getAlertPrefs: vi.fn(),
    updateAlertPrefs: vi.fn(),
    optInPlanAlerts: vi.fn(),
    optOutPlanAlerts: vi.fn(),
    getTrip: vi.fn(),
  },
}));

import { api } from '../api/client';
import { useStore } from './store';

const mockApi = api as unknown as Record<string, ReturnType<typeof vi.fn>>;

function prefs(over: Partial<AlertPrefs> = {}): AlertPrefs {
  return { ...over } as AlertPrefs;
}

function tripWithPlans(id: number): Trip & { plans: Plan[] } {
  return {
    id,
    name: 'NYC',
    destination: 'New York',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '',
    updated_at: '',
    plans: [],
  } as Trip & { plans: Plan[] };
}

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState({ alertPrefs: null, currentTrip: null, error: null }, false);
});

describe('loadAlertPrefs', () => {
  it('stores prefs on success', async () => {
    mockApi.getAlertPrefs.mockResolvedValue(prefs({ email_enabled: true } as never));
    await useStore.getState().loadAlertPrefs();
    expect(useStore.getState().alertPrefs).not.toBeNull();
  });

  it('sets error on failure', async () => {
    mockApi.getAlertPrefs.mockRejectedValue(new Error('boom'));
    await useStore.getState().loadAlertPrefs();
    expect(useStore.getState().error).toBe('boom');
  });

  it('stringifies a non-Error rejection', async () => {
    mockApi.getAlertPrefs.mockRejectedValue('strerr');
    await useStore.getState().loadAlertPrefs();
    expect(useStore.getState().error).toBe('strerr');
  });
});

describe('updateAlertPrefs', () => {
  it('stores the updated prefs', async () => {
    mockApi.updateAlertPrefs.mockResolvedValue(prefs({ email_enabled: false } as never));
    await useStore.getState().updateAlertPrefs({ email_enabled: false } as never);
    expect(mockApi.updateAlertPrefs).toHaveBeenCalledWith({ email_enabled: false });
    expect(useStore.getState().alertPrefs).not.toBeNull();
  });
});

describe('optInPlanAlerts', () => {
  it('reloads the open trip after opting in', async () => {
    useStore.setState({ currentTrip: tripWithPlans(7) });
    mockApi.optInPlanAlerts.mockResolvedValue(undefined);
    mockApi.getTrip.mockResolvedValue(tripWithPlans(7));
    await useStore.getState().optInPlanAlerts(3);
    expect(mockApi.optInPlanAlerts).toHaveBeenCalledWith(3);
    expect(mockApi.getTrip).toHaveBeenCalledWith(7);
  });

  it('does not reload when no trip is open', async () => {
    mockApi.optInPlanAlerts.mockResolvedValue(undefined);
    await useStore.getState().optInPlanAlerts(3);
    expect(mockApi.getTrip).not.toHaveBeenCalled();
  });
});

describe('optOutPlanAlerts', () => {
  it('reloads the open trip after opting out', async () => {
    useStore.setState({ currentTrip: tripWithPlans(7) });
    mockApi.optOutPlanAlerts.mockResolvedValue(undefined);
    mockApi.getTrip.mockResolvedValue(tripWithPlans(7));
    await useStore.getState().optOutPlanAlerts(3);
    expect(mockApi.optOutPlanAlerts).toHaveBeenCalledWith(3);
    expect(mockApi.getTrip).toHaveBeenCalledWith(7);
  });
});
