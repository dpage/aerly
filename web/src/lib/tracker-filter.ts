import type { PlanPart, PlanType } from '../api/types';

/** Plan types offered as Tracker show/hide chips, in display order (transport
 * grouped first, then places, then the diary entries).
 *
 * Every plan type gets a chip: anything that can be drawn on the map has to be
 * hideable, or it's the one thing you can't clear off a busy view. Meetings and
 * events were missing for exactly that reason — they had marker colours and
 * drew on the map, but no chip could turn them off. A test asserts this covers
 * PLAN_TYPES exactly, so a new type can't slip through the same way; the
 * persisted-filter validation in trackerSlice.ts reads PLAN_TYPES directly. */
export const FILTER_TYPES: PlanType[] = [
  'flight',
  'train',
  'ground',
  'vehicle_hire',
  'hotel',
  'dining',
  'excursion',
  'ice_cream',
  'meeting',
  'event',
];

export interface TrackerFilterOpts {
  /** Keep only parts the current user is travelling on / owns. */
  mineOnly: boolean;
  /** Plan types switched off (hidden). */
  hiddenTypes: PlanType[];
  /** The current user's id; required for mineOnly to match anything. */
  meId?: number;
}

/** The visible subset of tracker parts after applying the type and ownership
 * filters. A part is "mine" when the current user is among its passengers or
 * is the part's owner (the latter covers plans with no passenger list). */
export function filterTrackerParts(parts: PlanPart[], opts: TrackerFilterOpts): PlanPart[] {
  const { mineOnly, hiddenTypes, meId } = opts;
  const hidden = new Set<PlanType>(hiddenTypes);
  return parts.filter((p) => {
    if (hidden.has(p.type)) return false;
    if (mineOnly) {
      if (meId == null) return false;
      const isPassenger = p.passengers?.some((u) => u.id === meId) ?? false;
      const isOwner = p.owner?.id === meId;
      if (!isPassenger && !isOwner) return false;
    }
    return true;
  });
}
