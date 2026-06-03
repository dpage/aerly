import type { StoreState } from './store';

/** Extract a human-readable message from an unknown thrown value. */
export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

/** Reload the currently-open trip, if one is open. A no-op otherwise. */
export async function reloadCurrent(get: () => StoreState): Promise<void> {
  const id = get().currentTrip?.id;
  if (id != null) await get().loadTrip(id);
}
