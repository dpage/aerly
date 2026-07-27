import { useEffect, useState } from 'react';

// Per-viewer "trip description collapsed" preference. A trip's description can
// be long, so whether to fold the panel down is a personal display choice —
// stored in localStorage, EXPANDED by default, and never sent to the server.
//
// A module-level pub/sub keeps every consumer in sync without a React context,
// mirroring the pattern in showExternalPlans.ts / theme.ts.

const STORAGE_KEY = 'aerly:trip_description_collapsed';

function load(): boolean {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === '1';
  } catch {
    // Storage blocked (private mode): default to expanded.
    return false;
  }
}

const listeners = new Set<(v: boolean) => void>();
let current: boolean | null = null;

/** The current preference without subscribing. */
export function tripDescriptionCollapsed(): boolean {
  if (current === null) current = load();
  return current;
}

export function setTripDescriptionCollapsed(v: boolean): void {
  current = v;
  try {
    if (v) window.localStorage.setItem(STORAGE_KEY, '1');
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Ignore persistence failures; keep the runtime value in sync.
  }
  for (const l of listeners) l(v);
}

/** React hook: the collapsed flag plus a setter, kept in sync across components. */
export function useTripDescriptionCollapsed(): [boolean, (v: boolean) => void] {
  const [collapsed, setCollapsed] = useState<boolean>(tripDescriptionCollapsed);
  useEffect(() => {
    const onChange = (v: boolean) => setCollapsed(v);
    listeners.add(onChange);
    return () => {
      listeners.delete(onChange);
    };
  }, []);
  return [collapsed, setTripDescriptionCollapsed];
}
