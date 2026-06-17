// Clearing Workbox runtime caches on logout.
//
// The service worker (see web/vite.config.ts) runtime-caches account-scoped
// API responses under the `aerly-api` cache so trips/timelines are readable
// offline. On logout we delete that cache so the previous user's data can't be
// read offline on a shared device. The map tile/asset caches are not
// user-specific, so they're left in place.

/** Workbox cache name holding account-scoped `/api/*` responses. Must match the
 * `cacheName` in the NetworkFirst runtime-caching rule in web/vite.config.ts. */
export const ACCOUNT_CACHE = 'aerly-api';

/** Delete the account-scoped offline cache. Best-effort and safe to call when
 * Cache Storage is unavailable (older browsers, tests) — it never throws, so it
 * can't block the logout flow. */
export async function clearOfflineCaches(): Promise<void> {
  if (typeof caches === 'undefined') return;
  try {
    await caches.delete(ACCOUNT_CACHE);
  } catch {
    // A CacheStorage failure must not prevent sign-out from completing.
  }
}
