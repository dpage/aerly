import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { api } from './api/client';
import {
  currentPermission,
  disablePush,
  enablePush,
  iosNeedsInstall,
  isSubscribed,
  pushSupported,
  urlBase64ToUint8Array,
} from './push';

vi.mock('./api/client', () => ({
  api: {
    getPushVapidKey: vi.fn(),
    subscribePush: vi.fn(),
    unsubscribePush: vi.fn(),
  },
}));

// A controllable fake of the browser push environment.
interface FakeSub {
  endpoint: string;
  toJSON: () => { endpoint?: string; keys?: { p256dh?: string; auth?: string } };
  unsubscribe: () => Promise<boolean>;
}

function installPushEnv(opts: {
  permission?: NotificationPermission;
  requestResult?: NotificationPermission;
  existingSub?: FakeSub | null;
  subscribeResult?: FakeSub | Error;
}) {
  const subscribe = vi.fn(async () => {
    if (opts.subscribeResult instanceof Error) throw opts.subscribeResult;
    return opts.subscribeResult;
  });
  const getSubscription = vi.fn(async () => opts.existingSub ?? null);
  const registration = { pushManager: { subscribe, getSubscription } };

  Object.defineProperty(navigator, 'serviceWorker', {
    configurable: true,
    value: { ready: Promise.resolve(registration) },
  });
  const requestPermission = vi.fn(async () => opts.requestResult ?? 'granted');
  const NotificationStub = { permission: opts.permission ?? 'default', requestPermission };
  vi.stubGlobal('Notification', NotificationStub);
  (window as unknown as { PushManager: unknown }).PushManager = function PushManager() {};
  (window as unknown as { Notification: unknown }).Notification = NotificationStub;
  return { subscribe, getSubscription, requestPermission };
}

function clearPushEnv() {
  // Remove the markers pushSupported() checks so it reports false again.
  Reflect.deleteProperty(navigator as object, 'serviceWorker');
  delete (window as unknown as { PushManager?: unknown }).PushManager;
  delete (window as unknown as { Notification?: unknown }).Notification;
  vi.unstubAllGlobals();
}

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(() => {
  clearPushEnv();
});

describe('pushSupported', () => {
  it('is false without the APIs and true with them', () => {
    expect(pushSupported()).toBe(false);
    installPushEnv({});
    expect(pushSupported()).toBe(true);
  });
});

describe('urlBase64ToUint8Array', () => {
  it('decodes base64url (with - and _ and missing padding)', () => {
    // "Aerly" base64 is "QWVybHk="; base64url with no padding: "QWVybHk".
    const out = urlBase64ToUint8Array('QWVybHk');
    expect(Array.from(out)).toEqual([0x41, 0x65, 0x72, 0x6c, 0x79]);
  });
});

describe('iosNeedsInstall', () => {
  const setUA = (ua: string) =>
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: ua });

  afterEach(() => {
    Object.defineProperty(navigator, 'userAgent', { configurable: true, value: 'node' });
  });

  it('is false on a non-iOS browser', () => {
    setUA('Mozilla/5.0 (Windows NT 10.0)');
    expect(iosNeedsInstall()).toBe(false);
  });

  it('is true on iPhone Safari that is not installed', () => {
    setUA('Mozilla/5.0 (iPhone; CPU iPhone OS 16_4 like Mac OS X) Safari');
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList);
    expect(iosNeedsInstall()).toBe(true);
  });

  it('is false on iPhone when already installed (standalone)', () => {
    setUA('Mozilla/5.0 (iPhone; CPU iPhone OS 16_4 like Mac OS X) Safari');
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
    expect(iosNeedsInstall()).toBe(false);
  });
});

describe('currentPermission', () => {
  it('returns unsupported without the APIs', () => {
    expect(currentPermission()).toBe('unsupported');
  });
  it('returns the Notification permission when supported', () => {
    installPushEnv({ permission: 'granted' });
    expect(currentPermission()).toBe('granted');
  });
});

describe('isSubscribed', () => {
  it('is false when unsupported', async () => {
    expect(await isSubscribed()).toBe(false);
  });
  it('reflects an existing subscription', async () => {
    installPushEnv({ existingSub: { endpoint: 'e', toJSON: () => ({}), unsubscribe: vi.fn() } });
    expect(await isSubscribed()).toBe(true);
  });
  it('is false with no existing subscription', async () => {
    installPushEnv({ existingSub: null });
    expect(await isSubscribed()).toBe(false);
  });
});

describe('enablePush', () => {
  it('returns unsupported when the APIs are missing', async () => {
    expect(await enablePush()).toEqual({ ok: false, reason: 'unsupported' });
  });

  it('returns disabled when the server has no VAPID key', async () => {
    installPushEnv({});
    vi.mocked(api.getPushVapidKey).mockResolvedValue({ enabled: false });
    expect(await enablePush()).toEqual({ ok: false, reason: 'disabled' });
  });

  it('returns denied when permission is refused', async () => {
    installPushEnv({ requestResult: 'denied' });
    vi.mocked(api.getPushVapidKey).mockResolvedValue({ enabled: true, public_key: 'QWVybHk' });
    expect(await enablePush()).toEqual({ ok: false, reason: 'denied' });
  });

  it('subscribes and registers with the backend on success', async () => {
    const sub: FakeSub = {
      endpoint: 'https://push/x',
      toJSON: () => ({ endpoint: 'https://push/x', keys: { p256dh: 'p', auth: 'a' } }),
      unsubscribe: vi.fn(),
    };
    const env = installPushEnv({ requestResult: 'granted', subscribeResult: sub });
    vi.mocked(api.getPushVapidKey).mockResolvedValue({ enabled: true, public_key: 'QWVybHk' });
    vi.mocked(api.subscribePush).mockResolvedValue(undefined);

    expect(await enablePush()).toEqual({ ok: true });
    expect(env.subscribe).toHaveBeenCalledWith(
      expect.objectContaining({ userVisibleOnly: true }),
    );
    expect(api.subscribePush).toHaveBeenCalledWith({
      endpoint: 'https://push/x',
      keys: { p256dh: 'p', auth: 'a' },
    });
  });

  it('returns error when the subscription JSON is incomplete', async () => {
    const sub: FakeSub = {
      endpoint: 'https://push/x',
      toJSON: () => ({ endpoint: 'https://push/x' }), // no keys
      unsubscribe: vi.fn(),
    };
    installPushEnv({ requestResult: 'granted', subscribeResult: sub });
    vi.mocked(api.getPushVapidKey).mockResolvedValue({ enabled: true, public_key: 'QWVybHk' });
    expect(await enablePush()).toEqual({ ok: false, reason: 'error' });
  });

  it('returns error when subscribe throws', async () => {
    installPushEnv({ requestResult: 'granted', subscribeResult: new Error('boom') });
    vi.mocked(api.getPushVapidKey).mockResolvedValue({ enabled: true, public_key: 'QWVybHk' });
    expect(await enablePush()).toEqual({ ok: false, reason: 'error' });
  });
});

describe('disablePush', () => {
  it('is a no-op when unsupported', async () => {
    await disablePush();
    expect(api.unsubscribePush).not.toHaveBeenCalled();
  });

  it('is a no-op when not subscribed', async () => {
    installPushEnv({ existingSub: null });
    await disablePush();
    expect(api.unsubscribePush).not.toHaveBeenCalled();
  });

  it('unsubscribes the backend and the browser', async () => {
    const unsubscribe = vi.fn(async () => true);
    installPushEnv({
      existingSub: { endpoint: 'https://push/x', toJSON: () => ({}), unsubscribe },
    });
    vi.mocked(api.unsubscribePush).mockResolvedValue(undefined);
    await disablePush();
    expect(api.unsubscribePush).toHaveBeenCalledWith('https://push/x');
    expect(unsubscribe).toHaveBeenCalled();
  });
});
