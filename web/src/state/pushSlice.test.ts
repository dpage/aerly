import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../api/client', () => ({
  ApiError: class {},
  api: {
    getPushPrefs: vi.fn(),
    updatePushPref: vi.fn(),
  },
}));

vi.mock('../push', () => ({
  pushSupported: vi.fn(),
  currentPermission: vi.fn(),
  iosNeedsInstall: vi.fn(),
  isSubscribed: vi.fn(),
  enablePush: vi.fn(),
  disablePush: vi.fn(),
}));

import { api } from '../api/client';
import * as pushlib from '../push';
import { useStore } from './store';

const mockApi = api as unknown as Record<string, ReturnType<typeof vi.fn>>;
const mockPush = pushlib as unknown as Record<string, ReturnType<typeof vi.fn>>;

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState(
    {
      pushSupported: false,
      pushPermission: 'unsupported',
      pushIosHint: false,
      pushSubscribed: false,
      pushPrefs: null,
      pushBusy: false,
      pushLastError: null,
    },
    false,
  );
  mockPush.currentPermission.mockReturnValue('default');
  mockPush.iosNeedsInstall.mockReturnValue(false);
});

describe('loadPushState', () => {
  it('records unsupported without probing the subscription', async () => {
    mockPush.pushSupported.mockReturnValue(false);
    await useStore.getState().loadPushState();
    const s = useStore.getState();
    expect(s.pushSupported).toBe(false);
    expect(mockPush.isSubscribed).not.toHaveBeenCalled();
  });

  it('loads prefs when supported and already subscribed', async () => {
    mockPush.pushSupported.mockReturnValue(true);
    mockPush.currentPermission.mockReturnValue('granted');
    mockPush.isSubscribed.mockResolvedValue(true);
    mockApi.getPushPrefs.mockResolvedValue({ kinds: { alert: true, share: false } });
    await useStore.getState().loadPushState();
    const s = useStore.getState();
    expect(s.pushSubscribed).toBe(true);
    expect(s.pushPermission).toBe('granted');
    expect(s.pushPrefs).toEqual({ alert: true, share: false });
  });

  it('skips prefs when supported but not subscribed', async () => {
    mockPush.pushSupported.mockReturnValue(true);
    mockPush.isSubscribed.mockResolvedValue(false);
    await useStore.getState().loadPushState();
    expect(useStore.getState().pushPrefs).toBeNull();
    expect(mockApi.getPushPrefs).not.toHaveBeenCalled();
  });

  it('tolerates a prefs fetch failure', async () => {
    mockPush.pushSupported.mockReturnValue(true);
    mockPush.isSubscribed.mockResolvedValue(true);
    mockApi.getPushPrefs.mockRejectedValue(new Error('boom'));
    await useStore.getState().loadPushState();
    expect(useStore.getState().pushSubscribed).toBe(true);
    expect(useStore.getState().pushPrefs).toBeNull();
  });
});

describe('enablePush', () => {
  it('subscribes and loads prefs on success', async () => {
    mockPush.enablePush.mockResolvedValue({ ok: true });
    mockPush.currentPermission.mockReturnValue('granted');
    mockApi.getPushPrefs.mockResolvedValue({ kinds: { alert: true, share: true } });
    await useStore.getState().enablePush();
    const s = useStore.getState();
    expect(s.pushSubscribed).toBe(true);
    expect(s.pushBusy).toBe(false);
    expect(s.pushLastError).toBeNull();
    expect(s.pushPrefs).toEqual({ alert: true, share: true });
  });

  it('records the failure reason and clears busy', async () => {
    mockPush.enablePush.mockResolvedValue({ ok: false, reason: 'denied' });
    mockPush.currentPermission.mockReturnValue('denied');
    await useStore.getState().enablePush();
    const s = useStore.getState();
    expect(s.pushSubscribed).toBe(false);
    expect(s.pushBusy).toBe(false);
    expect(s.pushLastError).toBe('denied');
    expect(s.pushPermission).toBe('denied');
  });

  it('defaults the reason to error when none is given', async () => {
    mockPush.enablePush.mockResolvedValue({ ok: false });
    await useStore.getState().enablePush();
    expect(useStore.getState().pushLastError).toBe('error');
  });
});

describe('disablePush', () => {
  it('clears the subscription flag', async () => {
    useStore.setState({ pushSubscribed: true }, false);
    mockPush.disablePush.mockResolvedValue(undefined);
    await useStore.getState().disablePush();
    const s = useStore.getState();
    expect(s.pushSubscribed).toBe(false);
    expect(s.pushBusy).toBe(false);
  });

  it('still clears state when the unsubscribe throws', async () => {
    useStore.setState({ pushSubscribed: true }, false);
    mockPush.disablePush.mockRejectedValue(new Error('network'));
    await expect(useStore.getState().disablePush()).rejects.toThrow('network');
    expect(useStore.getState().pushSubscribed).toBe(false);
    expect(useStore.getState().pushBusy).toBe(false);
  });
});

describe('setPushKind', () => {
  it('persists and stores the returned prefs', async () => {
    mockApi.updatePushPref.mockResolvedValue({ kinds: { alert: false, share: true } });
    await useStore.getState().setPushKind('alert', false);
    expect(mockApi.updatePushPref).toHaveBeenCalledWith('alert', false);
    expect(useStore.getState().pushPrefs).toEqual({ alert: false, share: true });
  });
});
