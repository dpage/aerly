import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type { TrackerPart } from '../api/types';
import type { StoreState } from './store';

/** Default convergence window when the user hasn't set one. */
const DEFAULT_WINDOW_BEFORE = '7d';
const DEFAULT_WINDOW_AFTER = '7d';

/** localStorage key for the tracker window, keyed per-tag exactly as the
 * `showAll`/`showOld` flags are persisted in coreSlice (spec §7). The empty
 * tag ('') is the untagged "everyone" view. */
function windowKey(tag: string): string {
  return `tracker.window.${tag || '_all'}`;
}

interface TrackerWindow {
  before: string;
  after: string;
}

function loadWindow(tag: string): TrackerWindow {
  try {
    const raw = window.localStorage.getItem(windowKey(tag));
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<TrackerWindow>;
      if (typeof parsed.before === 'string' && typeof parsed.after === 'string') {
        return { before: parsed.before, after: parsed.after };
      }
    }
  } catch {
    // SSR / privacy modes / malformed JSON — fall through to defaults.
  }
  return { before: DEFAULT_WINDOW_BEFORE, after: DEFAULT_WINDOW_AFTER };
}

function persistWindow(tag: string, w: TrackerWindow): void {
  try {
    window.localStorage.setItem(windowKey(tag), JSON.stringify(w));
  } catch {
    // ignore — best effort
  }
}

/** State + actions for the tracker convergence view (spec §7).
 *
 * Wave 0b: a typed fetch into `trackerParts` plus the per-tag window flag
 * persisted to localStorage. Wave 1C/2 fleshes out single-part focus and the
 * map rendering. */
export interface TrackerSlice {
  trackerParts: TrackerPart[];
  trackerTag: string;
  trackerWindow: TrackerWindow;
  trackerLoading: boolean;

  loadTracker: (opts?: { tag?: string }) => Promise<void>;
  setTrackerWindow: (w: Partial<TrackerWindow>) => Promise<void>;
}

export const createTrackerSlice: StateCreator<StoreState, [], [], TrackerSlice> = (set, get) => ({
  trackerParts: [],
  trackerTag: '',
  trackerWindow: loadWindow(''),
  trackerLoading: false,

  async loadTracker(opts) {
    const tag = opts?.tag ?? get().trackerTag;
    const w = loadWindow(tag);
    set({ trackerTag: tag, trackerWindow: w, trackerLoading: true });
    try {
      const trackerParts = await api.getTracker({
        windowBefore: w.before,
        windowAfter: w.after,
        tag: tag || undefined,
      });
      set({ trackerParts, trackerLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), trackerLoading: false });
    }
  },

  async setTrackerWindow(patch) {
    const tag = get().trackerTag;
    const next = { ...get().trackerWindow, ...patch };
    persistWindow(tag, next);
    set({ trackerWindow: next });
    await get().loadTracker({ tag });
  },
});

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
