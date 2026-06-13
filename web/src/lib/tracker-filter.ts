import type { PlanPart, PlanType } from '../api/types';

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
