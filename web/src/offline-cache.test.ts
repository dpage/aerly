import { afterEach, describe, expect, it, vi } from 'vitest';

import { ACCOUNT_CACHE, clearOfflineCaches } from './offline-cache';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('clearOfflineCaches', () => {
  it('is a no-op when Cache Storage is unavailable', async () => {
    // jsdom has no `caches` global by default.
    await expect(clearOfflineCaches()).resolves.toBeUndefined();
  });

  it('deletes the account-scoped cache', async () => {
    const del = vi.fn().mockResolvedValue(true);
    vi.stubGlobal('caches', { delete: del });
    await clearOfflineCaches();
    expect(del).toHaveBeenCalledWith(ACCOUNT_CACHE);
  });

  it('swallows Cache Storage errors so sign-out can complete', async () => {
    const del = vi.fn().mockRejectedValue(new Error('boom'));
    vi.stubGlobal('caches', { delete: del });
    await expect(clearOfflineCaches()).resolves.toBeUndefined();
  });
});
