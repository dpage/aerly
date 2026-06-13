import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type { Plan, PlanPart, PlanType, TrackerPart } from '../api/types';
import { errorMessage } from './helpers';
import type { StoreState } from './store';

/** localStorage key for the tracker window, keyed per-tag (spec §7). The empty
 * tag ('') is the untagged "everyone" view. */
function windowKey(tag: string): string {
  return `tracker.window.${tag || '_all'}`;
}

/** Absolute From/To dates (YYYY-MM-DD) for the tracker window. Either may be
 * unset, in which case the server falls back to its default span. */
export interface TrackerWindow {
  from?: string;
  to?: string;
}

function loadWindow(tag: string): TrackerWindow {
  try {
    const raw = window.localStorage.getItem(windowKey(tag));
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<TrackerWindow>;
      const w: TrackerWindow = {};
      if (typeof parsed.from === 'string') w.from = parsed.from;
      if (typeof parsed.to === 'string') w.to = parsed.to;
      return w;
    }
  } catch {
    // SSR / privacy modes / malformed JSON — fall through to empty.
  }
  return {};
}

function persistWindow(tag: string, w: TrackerWindow): void {
  try {
    window.localStorage.setItem(windowKey(tag), JSON.stringify(w));
  } catch {
    // ignore — best effort
  }
}

/** localStorage key for the global Tracker filters (not per-tag). */
const FILTERS_KEY = 'tracker.filters';

const KNOWN_TYPES: readonly PlanType[] = [
  'flight',
  'train',
  'hotel',
  'ground',
  'dining',
  'excursion',
];

export interface TrackerFilters {
  mineOnly: boolean;
  hiddenTypes: PlanType[];
}

/** Read the persisted Tracker filters, tolerating SSR, privacy modes, and
 * malformed/stale JSON. Only an explicit boolean `true` enables mine-only, and
 * unknown type strings are dropped. */
export function loadFilters(): TrackerFilters {
  try {
    const raw = window.localStorage.getItem(FILTERS_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<TrackerFilters>;
      const hiddenTypes = Array.isArray(parsed.hiddenTypes)
        ? parsed.hiddenTypes.filter((t): t is PlanType =>
            (KNOWN_TYPES as readonly string[]).includes(t as string),
          )
        : [];
      return { mineOnly: parsed.mineOnly === true, hiddenTypes };
    }
  } catch {
    // SSR / privacy modes / malformed JSON — fall through to defaults.
  }
  return { mineOnly: false, hiddenTypes: [] };
}

function persistFilters(f: TrackerFilters): void {
  try {
    window.localStorage.setItem(FILTERS_KEY, JSON.stringify(f));
  } catch {
    // ignore — best effort
  }
}

/** State + actions for the unified tracker map+list view (spec §7, PRD §6.5).
 * Holds the full mappable parts in the window (any type), the per-tag From/To
 * window persisted to localStorage, and the live SSE merge. */
export interface TrackerSlice {
  /** Every mappable part in the window, as full PlanParts. */
  trackerParts: PlanPart[];
  trackerTag: string;
  trackerWindow: TrackerWindow;
  trackerLoading: boolean;
  /** Show only parts the current user is travelling on / owns. */
  trackerMineOnly: boolean;
  /** Plan types switched off in the Tracker (hidden). */
  trackerHiddenTypes: PlanType[];
  setTrackerMineOnly: (value: boolean) => void;
  toggleTrackerType: (type: PlanType) => void;

  loadTracker: (opts?: { tag?: string; window?: TrackerWindow }) => Promise<void>;
  setTrackerWindow: (w: Partial<TrackerWindow>) => Promise<void>;
  /** Apply a plan_part.updated SSE event (the poller broadcasts a thin
   * TrackerPartDTO). Folds the live status/position into the matching full part
   * in the tracker list AND the open trip's timeline — never replaces a row
   * wholesale (that would wipe its coordinates + detail). Idempotent: a part not
   * present in either is a no-op. */
  applyPlanPartUpdate: (part: TrackerPart) => void;
}

export const createTrackerSlice: StateCreator<StoreState, [], [], TrackerSlice> = (set, get) => {
  const filters = loadFilters();
  return {
    trackerParts: [],
    trackerTag: '',
    trackerWindow: loadWindow(''),
    trackerLoading: false,
    trackerMineOnly: filters.mineOnly,
    trackerHiddenTypes: filters.hiddenTypes,

    setTrackerMineOnly(value) {
      set({ trackerMineOnly: value });
      persistFilters({ mineOnly: value, hiddenTypes: get().trackerHiddenTypes });
    },

    toggleTrackerType(type) {
      const cur = get().trackerHiddenTypes;
      const next = cur.includes(type) ? cur.filter((t) => t !== type) : [...cur, type];
      set({ trackerHiddenTypes: next });
      persistFilters({ mineOnly: get().trackerMineOnly, hiddenTypes: next });
    },

    async loadTracker(opts) {
      const tag = opts?.tag ?? get().trackerTag;
      // An explicit window (seeded from a tag's span on tag change, or set via the
      // date pickers) is persisted under the *target* tag and used for this load;
      // otherwise fall back to that tag's saved window.
      const w = opts?.window ?? loadWindow(tag);
      if (opts?.window) persistWindow(tag, opts.window);
      set({ trackerTag: tag, trackerWindow: w, trackerLoading: true });
      try {
        const { parts } = await api.getTracker({ from: w.from, to: w.to, tag: tag || undefined });
        set({ trackerParts: parts, trackerLoading: false });
      } catch (err) {
        set({ error: errorMessage(err), trackerLoading: false });
      }
    },

    async setTrackerWindow(patch) {
      const tag = get().trackerTag;
      const next = { ...get().trackerWindow, ...patch };
      await get().loadTracker({ tag, window: next });
    },

    applyPlanPartUpdate(update) {
      set((s) => {
        // 1. Tracker list: fold the live fields into the matching full part in
        //    place. Don't insert a part that isn't already listed — the list is
        //    window/visibility-scoped server-side.
        const trackerParts = s.trackerParts.some((p) => p.id === update.plan_part_id)
          ? s.trackerParts.map((p) => (p.id === update.plan_part_id ? foldUpdate(p, update) : p))
          : s.trackerParts;

        // 2. Open trip timeline: same merge for the trip currently on screen.
        let currentTrip = s.currentTrip;
        if (currentTrip && currentTrip.id === update.trip_id) {
          let touched = false;
          const plans: Plan[] = currentTrip.plans.map((plan) => {
            if (plan.id !== update.plan_id) return plan;
            const parts = plan.parts.map((pp) => {
              if (pp.id !== update.plan_part_id) return pp;
              touched = true;
              return foldUpdate(pp, update);
            });
            return touched ? { ...plan, parts } : plan;
          });
          if (touched) currentTrip = { ...currentTrip, plans };
        }

        return { trackerParts, currentTrip };
      });
    },
  };
};

/** Fold a thin live update (status / effective_at / latest position) into a full
 * PlanPart, leaving its coordinates + type detail untouched. */
function foldUpdate(pp: PlanPart, u: TrackerPart): PlanPart {
  return {
    ...pp,
    status: (u.status || pp.status) as typeof pp.status,
    effective_at: u.effective_at || pp.effective_at,
    flight: pp.flight
      ? {
          ...pp.flight,
          flight_status: u.status || pp.flight.flight_status,
          latest_position: u.latest_position ?? pp.flight.latest_position,
          // Keep "Last polled" live and let the flown-track polyline grow with
          // the plane: the broadcast carries the fresh values, so prefer them
          // over the (now-stale) ones from the last full HTTP fetch.
          last_polled_at: u.last_polled_at ?? pp.flight.last_polled_at,
          track: u.track ?? pp.flight.track,
        }
      : pp.flight,
  };
}
