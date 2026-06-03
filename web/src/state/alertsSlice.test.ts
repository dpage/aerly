import { describe, it, expect, beforeEach, vi } from 'vitest';

import type { AlertPrefs, FlightAlert, Plan, Trip } from '../api/types';

vi.mock('../api/client', () => ({
  ApiError: class {},
  api: {
    getAlertPrefs: vi.fn(),
    updateAlertPrefs: vi.fn(),
    optInPlanAlerts: vi.fn(),
    optOutPlanAlerts: vi.fn(),
    getTrip: vi.fn(),
    getAlerts: vi.fn(),
    markAlertsRead: vi.fn(),
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

const mk = (id: number, msg: string): FlightAlert => ({
  id, plan_part_id: 1, plan_id: 1, trip_id: 1, ident: 'BA286',
  kind: 'gate', status: 'Scheduled', message: msg, created_at: '2026-06-01T00:00:00Z',
});

describe('alertsSlice inbox', () => {
  beforeEach(() => {
    useStore.setState({
      alerts: [],
      notifications: { friend_requests_pending: 0, unread_alerts: 0 },
    });
  });

  it('loadAlerts fills the list (unread count is owned by notifications, not loadAlerts)', async () => {
    mockApi.getAlerts.mockResolvedValue([mk(1, 'a'), mk(2, 'b')]);
    await useStore.getState().loadAlerts();
    expect(useStore.getState().alerts).toHaveLength(2);
  });

  it('applyIncomingAlert prepends and increments notifications.unread_alerts (the badge counter)', () => {
    useStore.setState({
      alerts: [mk(1, 'a')],
      notifications: { friend_requests_pending: 0, unread_alerts: 1 },
    });
    useStore.getState().applyIncomingAlert(mk(2, 'b'));
    expect(useStore.getState().alerts[0].id).toBe(2);
    // notifications.unread_alerts is the value the avatar badge reads — it must
    // increment when a live alert arrives, so the badge updates without a reload.
    expect(useStore.getState().notifications.unread_alerts).toBe(2);
  });

  it('markAlertsRead zeroes notifications.unread_alerts and stamps read_at locally', async () => {
    mockApi.markAlertsRead.mockResolvedValue(undefined);
    useStore.setState({
      alerts: [mk(1, 'a')],
      notifications: { friend_requests_pending: 0, unread_alerts: 1 },
    });
    await useStore.getState().markAlertsRead();
    expect(useStore.getState().notifications.unread_alerts).toBe(0);
    expect(useStore.getState().alerts[0].read_at).toBeTruthy();
  });
});
