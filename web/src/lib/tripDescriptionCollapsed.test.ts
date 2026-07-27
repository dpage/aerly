import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

// The module keeps a process-wide singleton (current + listeners), so each test
// resets the module registry and storage to start from a known state.
const STORAGE_KEY = 'aerly:trip_description_collapsed';

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('tripDescriptionCollapsed', () => {
  it('defaults to expanded (not collapsed) when nothing is stored', async () => {
    const m = await import('./tripDescriptionCollapsed');
    expect(m.tripDescriptionCollapsed()).toBe(false);
  });

  it('reads a stored "1" as collapsed', async () => {
    localStorage.setItem(STORAGE_KEY, '1');
    const m = await import('./tripDescriptionCollapsed');
    expect(m.tripDescriptionCollapsed()).toBe(true);
  });

  it('persists when collapsed and clears the key when expanded', async () => {
    const m = await import('./tripDescriptionCollapsed');
    m.setTripDescriptionCollapsed(true);
    expect(localStorage.getItem(STORAGE_KEY)).toBe('1');
    expect(m.tripDescriptionCollapsed()).toBe(true);
    m.setTripDescriptionCollapsed(false);
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(m.tripDescriptionCollapsed()).toBe(false);
  });

  it('the hook reflects external changes and stops listening after unmount', async () => {
    const m = await import('./tripDescriptionCollapsed');
    const { result, unmount } = renderHook(() => m.useTripDescriptionCollapsed());
    expect(result.current[0]).toBe(false);
    act(() => m.setTripDescriptionCollapsed(true));
    expect(result.current[0]).toBe(true);
    unmount();
    // Once unmounted the listener is gone, so a later change neither throws nor
    // updates the captured value.
    act(() => m.setTripDescriptionCollapsed(false));
    expect(result.current[0]).toBe(true);
    expect(m.tripDescriptionCollapsed()).toBe(false);
  });

  it("the hook's setter updates the shared value", async () => {
    const m = await import('./tripDescriptionCollapsed');
    const { result } = renderHook(() => m.useTripDescriptionCollapsed());
    act(() => result.current[1](true));
    expect(result.current[0]).toBe(true);
    expect(m.tripDescriptionCollapsed()).toBe(true);
  });

  it('falls back to expanded and swallows persistence errors when storage is blocked', async () => {
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
      const m = await import('./tripDescriptionCollapsed');
      expect(m.tripDescriptionCollapsed()).toBe(false);
      expect(() => m.setTripDescriptionCollapsed(true)).not.toThrow();
      expect(() => m.setTripDescriptionCollapsed(false)).not.toThrow();
    } finally {
      if (original) Object.defineProperty(window, 'localStorage', original);
    }
  });
});
