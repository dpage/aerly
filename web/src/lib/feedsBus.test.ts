import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

// The module keeps a process-wide set of listeners, so reset the registry
// between tests to start from a known state.
beforeEach(() => {
  vi.resetModules();
});

describe('feedsBus', () => {
  it('increments the count for each notify while a listener is mounted', async () => {
    const m = await import('./feedsBus');
    const { result } = renderHook(() => m.useFeedsChangedCount());
    expect(result.current).toBe(0);
    act(() => m.notifyFeedsChanged());
    expect(result.current).toBe(1);
    act(() => m.notifyFeedsChanged());
    expect(result.current).toBe(2);
  });

  it('stops counting after the listener unmounts', async () => {
    const m = await import('./feedsBus');
    const { result, unmount } = renderHook(() => m.useFeedsChangedCount());
    act(() => m.notifyFeedsChanged());
    expect(result.current).toBe(1);
    unmount();
    // The listener is removed on unmount, so a later notify neither throws nor
    // updates the captured value.
    act(() => m.notifyFeedsChanged());
    expect(result.current).toBe(1);
  });

  it('notifying with no listeners is a no-op', async () => {
    const m = await import('./feedsBus');
    expect(() => m.notifyFeedsChanged()).not.toThrow();
  });

  it('keeps independent hook instances in sync', async () => {
    const m = await import('./feedsBus');
    const a = renderHook(() => m.useFeedsChangedCount());
    const b = renderHook(() => m.useFeedsChangedCount());
    act(() => m.notifyFeedsChanged());
    expect(a.result.current).toBe(1);
    expect(b.result.current).toBe(1);
  });
});
