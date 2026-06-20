import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

// The module keeps a process-wide singleton (current + listeners), so each test
// resets the module registry and storage to start from a known state.
const STORAGE_KEY = 'aerly:show_external_plans';

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('showExternalPlans', () => {
  it('defaults to off when nothing is stored', async () => {
    const m = await import('./showExternalPlans');
    expect(m.showExternalPlansEnabled()).toBe(false);
  });

  it('reads a stored "1" as on', async () => {
    localStorage.setItem(STORAGE_KEY, '1');
    const m = await import('./showExternalPlans');
    expect(m.showExternalPlansEnabled()).toBe(true);
  });

  it('persists when turned on and clears the key when turned off', async () => {
    const m = await import('./showExternalPlans');
    m.setShowExternalPlans(true);
    expect(localStorage.getItem(STORAGE_KEY)).toBe('1');
    expect(m.showExternalPlansEnabled()).toBe(true);
    m.setShowExternalPlans(false);
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(m.showExternalPlansEnabled()).toBe(false);
  });

  it('the hook reflects external changes and stops listening after unmount', async () => {
    const m = await import('./showExternalPlans');
    const { result, unmount } = renderHook(() => m.useShowExternalPlans());
    expect(result.current[0]).toBe(false);
    act(() => m.setShowExternalPlans(true));
    expect(result.current[0]).toBe(true);
    unmount();
    // Once unmounted the listener is gone, so a later change neither throws nor
    // updates the captured value.
    act(() => m.setShowExternalPlans(false));
    expect(result.current[0]).toBe(true);
    expect(m.showExternalPlansEnabled()).toBe(false);
  });

  it("the hook's setter updates the shared value", async () => {
    const m = await import('./showExternalPlans');
    const { result } = renderHook(() => m.useShowExternalPlans());
    act(() => result.current[1](true));
    expect(result.current[0]).toBe(true);
    expect(m.showExternalPlansEnabled()).toBe(true);
  });

  it('falls back to off and swallows persistence errors when storage is blocked', async () => {
    // jsdom puts the Storage methods on the instance, not Storage.prototype, so
    // replace window.localStorage wholesale with a throwing stand-in (mimics a
    // privacy-mode browser where every access throws).
    const original = Object.getOwnPropertyDescriptor(window, 'localStorage');
    const blocked = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
      removeItem: () => {
        throw new Error('blocked');
      },
    };
    Object.defineProperty(window, 'localStorage', { configurable: true, value: blocked });
    try {
      const m = await import('./showExternalPlans');
      // load() swallows the read error and defaults off.
      expect(m.showExternalPlansEnabled()).toBe(false);
      // setShowExternalPlans swallows both the write (on) and the remove (off).
      expect(() => m.setShowExternalPlans(true)).not.toThrow();
      expect(() => m.setShowExternalPlans(false)).not.toThrow();
    } finally {
      if (original) Object.defineProperty(window, 'localStorage', original);
    }
  });
});
