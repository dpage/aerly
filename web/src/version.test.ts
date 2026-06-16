import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';

const h = vi.hoisted(() => ({ getVersion: vi.fn() }));
vi.mock('./api/client', () => ({ api: { getVersion: h.getVersion } }));

import { shortCommit, isNewerBuild, uiBuildLabel, useUpdateAvailable } from './version';

const SHA = '0123456789abcdef0123456789abcdef01234567';

beforeEach(() => {
  vi.clearAllMocks();
});

describe('shortCommit', () => {
  it('truncates a long SHA to 12 chars', () => {
    expect(shortCommit(SHA)).toBe('0123456789ab');
  });
  it('leaves a short value untouched', () => {
    expect(shortCommit('abc123')).toBe('abc123');
  });
});

describe('isNewerBuild', () => {
  it('is true only when both commits are known and differ', () => {
    expect(isNewerBuild('aaa', 'bbb')).toBe(true);
  });
  it('is false when they match', () => {
    expect(isNewerBuild('aaa', 'aaa')).toBe(false);
  });
  it('is false when either side is empty', () => {
    expect(isNewerBuild('', 'bbb')).toBe(false);
    expect(isNewerBuild('aaa', '')).toBe(false);
  });
});

describe('uiBuildLabel', () => {
  it('shows the short commit for a stamped build', () => {
    expect(uiBuildLabel(SHA)).toBe('0123456789ab');
  });
  it('shows "dev" for an unstamped build', () => {
    expect(uiBuildLabel('')).toBe('dev');
  });
});

describe('useUpdateAvailable', () => {
  it('does nothing when inactive', () => {
    const { result } = renderHook(() => useUpdateAvailable(false, 'aaa'));
    expect(result.current).toBe(false);
    expect(h.getVersion).not.toHaveBeenCalled();
  });

  it('does nothing when the UI commit is unknown (dev build)', () => {
    const { result } = renderHook(() => useUpdateAvailable(true, ''));
    expect(result.current).toBe(false);
    expect(h.getVersion).not.toHaveBeenCalled();
  });

  it('flips to true when the server reports a newer build', async () => {
    h.getVersion.mockResolvedValue({ commit: 'server-new', short: 'server-new', build_time: '' });
    const { result } = renderHook(() => useUpdateAvailable(true, 'ui-old'));
    await waitFor(() => expect(result.current).toBe(true));
  });

  it('stays false when the server build matches', async () => {
    h.getVersion.mockResolvedValue({ commit: 'same', short: 'same', build_time: '' });
    const { result } = renderHook(() => useUpdateAvailable(true, 'same'));
    await waitFor(() => expect(h.getVersion).toHaveBeenCalled());
    expect(result.current).toBe(false);
  });

  it('ignores a failed check and stays false', async () => {
    h.getVersion.mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useUpdateAvailable(true, 'ui-old'));
    await waitFor(() => expect(h.getVersion).toHaveBeenCalled());
    expect(result.current).toBe(false);
  });

  it('re-checks when the tab regains focus', async () => {
    h.getVersion.mockResolvedValue({ commit: 'same', short: 'same', build_time: '' });
    const { result } = renderHook(() => useUpdateAvailable(true, 'same'));
    await waitFor(() => expect(h.getVersion).toHaveBeenCalledTimes(1));

    h.getVersion.mockResolvedValue({ commit: 'newer', short: 'newer', build_time: '' });
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    await waitFor(() => expect(result.current).toBe(true));
  });

  it('re-checks on the poll interval and cleans up on unmount', async () => {
    vi.useFakeTimers();
    try {
      h.getVersion.mockResolvedValue({ commit: 'same', short: 'same', build_time: '' });
      const { unmount } = renderHook(() => useUpdateAvailable(true, 'same'));
      await vi.advanceTimersByTimeAsync(0); // initial check
      expect(h.getVersion).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(5 * 60 * 1000); // one interval tick
      expect(h.getVersion).toHaveBeenCalledTimes(2);

      unmount();
      await vi.advanceTimersByTimeAsync(5 * 60 * 1000); // no further checks after unmount
      expect(h.getVersion).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});

afterEach(() => {
  vi.useRealTimers();
});
