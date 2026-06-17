import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Drive vite-plugin-pwa's registration hook from the test: `mock.needRefresh`
// controls whether a waiting worker is reported, `updateServiceWorker` is the
// spy applyUpdate should call, and `onRegisteredSW` captures the callback so we
// can simulate registration completing.
const mock = vi.hoisted(() => ({
  needRefresh: false,
  updateServiceWorker: vi.fn(),
  onRegisteredSW: undefined as
    | undefined
    | ((swUrl: string, registration?: ServiceWorkerRegistration) => void),
}));

vi.mock('virtual:pwa-register/react', () => ({
  useRegisterSW: (opts: {
    onRegisteredSW?: (swUrl: string, registration?: ServiceWorkerRegistration) => void;
  }) => {
    mock.onRegisteredSW = opts.onRegisteredSW;
    return {
      needRefresh: [mock.needRefresh, vi.fn()],
      offlineReady: [false, vi.fn()],
      updateServiceWorker: mock.updateServiceWorker,
    };
  },
}));

import { useOnlineStatus, useServiceWorkerUpdate } from './pwa';

beforeEach(() => {
  mock.needRefresh = false;
  mock.updateServiceWorker.mockReset();
  mock.onRegisteredSW = undefined;
});

describe('useServiceWorkerUpdate', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('reports an available update and activates the waiting worker on apply', () => {
    mock.needRefresh = true;
    const { result } = renderHook(() => useServiceWorkerUpdate());

    expect(result.current.updateAvailable).toBe(true);

    act(() => result.current.applyUpdate());
    expect(mock.updateServiceWorker).toHaveBeenCalledWith(true);
  });

  it('nudges the registration to re-check when no worker is waiting yet', () => {
    vi.useFakeTimers();
    const registration = { update: vi.fn() } as unknown as ServiceWorkerRegistration;
    const { result } = renderHook(() => useServiceWorkerUpdate());

    // Registration completes: captures the registration and starts polling.
    act(() => mock.onRegisteredSW?.('/sw.js', registration));

    expect(result.current.updateAvailable).toBe(false);

    act(() => result.current.applyUpdate());
    expect(registration.update).toHaveBeenCalledTimes(1);
    expect(mock.updateServiceWorker).not.toHaveBeenCalled();

    // The slow poll re-checks for a newer worker without a navigation.
    act(() => vi.advanceTimersByTime(5 * 60 * 1000));
    expect(registration.update).toHaveBeenCalledTimes(2);
  });

  it('replaces the poll timer when registration happens again', () => {
    vi.useFakeTimers();
    const first = { update: vi.fn() } as unknown as ServiceWorkerRegistration;
    const second = { update: vi.fn() } as unknown as ServiceWorkerRegistration;
    renderHook(() => useServiceWorkerUpdate());

    act(() => mock.onRegisteredSW?.('/sw.js', first));
    // A second registration (HMR / remount) must clear the first poller so
    // only the latest registration is polled.
    act(() => mock.onRegisteredSW?.('/sw.js', second));

    act(() => vi.advanceTimersByTime(5 * 60 * 1000));
    expect(first.update).not.toHaveBeenCalled();
    expect(second.update).toHaveBeenCalledTimes(1);
  });

  it('ignores a null registration and falls back to a reload when unsupported', () => {
    const reload = vi.fn();
    const orig = window.location;
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { ...orig, reload },
    });
    try {
      const { result } = renderHook(() => useServiceWorkerUpdate());
      // A browser without service-worker support registers nothing.
      act(() => mock.onRegisteredSW?.('/sw.js', undefined));

      act(() => result.current.applyUpdate());
      expect(reload).toHaveBeenCalled();
      expect(mock.updateServiceWorker).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(window, 'location', { configurable: true, value: orig });
    }
  });
});

describe('useOnlineStatus', () => {
  it('tracks online/offline transitions', () => {
    const { result } = renderHook(() => useOnlineStatus());
    expect(result.current).toBe(true);

    act(() => window.dispatchEvent(new Event('offline')));
    expect(result.current).toBe(false);

    act(() => window.dispatchEvent(new Event('online')));
    expect(result.current).toBe(true);
  });
});
