import type { StoreState } from './store';

/** Extract a human-readable message from an unknown thrown value. */
export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

/** True when an error message looks like a connectivity failure — offline, the
 * server unreachable, or the service worker having no cached response — rather
 * than a real application error. Connectivity is flaky around online/offline
 * transitions (and navigator.onLine can read "online" with no real internet),
 * so we keep these raw browser/Workbox strings ("Failed to fetch",
 * "FetchEvent.respondWith received an error", …) out of the error toast and let
 * the offline banner be the channel for connectivity. */
export function isNetworkError(message: string): boolean {
  const m = message.toLowerCase();
  return (
    m.includes('failed to fetch') ||
    m.includes('load failed') ||
    m.includes('networkerror') ||
    m.includes('network request failed') ||
    m.includes('respondwith received an error') ||
    m.includes('no-response') ||
    m.includes('the internet connection appears to be offline')
  );
}

/** Reload the currently-open trip, if one is open. A no-op otherwise. */
export async function reloadCurrent(get: () => StoreState): Promise<void> {
  const id = get().currentTrip?.id;
  if (id != null) await get().loadTrip(id);
}
