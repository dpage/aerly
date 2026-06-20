import { useEffect, useState } from 'react';

// A tiny module-level signal that a trip's iCal feeds changed (added, edited or
// removed) in the Edit trip dialog. The timeline listens so it can re-fetch its
// feeds + external events without a page reload — the dialog and the timeline
// are separate components, so a shared bus is the lightest coupling. Mirrors the
// pub/sub style in showExternalPlans.ts.

const listeners = new Set<() => void>();

/** Notify listeners that feeds changed (call after add/edit/remove). */
export function notifyFeedsChanged(): void {
  for (const l of listeners) l();
}

/** A counter that increments whenever feeds change; use it in an effect's deps
 * to re-run a fetch. */
export function useFeedsChangedCount(): number {
  const [n, setN] = useState(0);
  useEffect(() => {
    const cb = () => setN((x) => x + 1);
    listeners.add(cb);
    return () => {
      listeners.delete(cb);
    };
  }, []);
  return n;
}
