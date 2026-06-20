import { useEffect, useState } from 'react';

// Per-viewer "Show external plans" preference. External (iCal feed) events are
// reference items, not the viewer's own bookings, so whether to show them is a
// personal display filter — stored in localStorage, OFF by default, and never
// sent to the server (the feed itself is trip config; seeing it is personal).
//
// A module-level pub/sub keeps every consumer in sync without a React context:
// the toggle in the timeline and the PDF-export action in the trip header both
// read the same value. Mirrors the pattern in theme.ts.

const STORAGE_KEY = 'aerly:show_external_plans';

function load(): boolean {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === '1';
  } catch {
    // Storage blocked (private mode): default to off.
    return false;
  }
}

const listeners = new Set<(v: boolean) => void>();
let current: boolean | null = null;

/** The current preference without subscribing — for non-React callers (e.g. an
 * imperative PDF-export click handler). */
export function showExternalPlansEnabled(): boolean {
  if (current === null) current = load();
  return current;
}

export function setShowExternalPlans(v: boolean): void {
  current = v;
  try {
    if (v) window.localStorage.setItem(STORAGE_KEY, '1');
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Ignore persistence failures; keep the runtime value in sync.
  }
  for (const l of listeners) l(v);
}

/** React hook: the preference plus a setter, kept in sync across components. */
export function useShowExternalPlans(): [boolean, (v: boolean) => void] {
  const [show, setShow] = useState<boolean>(showExternalPlansEnabled);
  useEffect(() => {
    const onChange = (v: boolean) => setShow(v);
    listeners.add(onChange);
    return () => {
      listeners.delete(onChange);
    };
  }, []);
  return [show, setShowExternalPlans];
}
