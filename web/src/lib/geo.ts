import type { PlanPart } from '../api/types';

// An endpoint is "unlocated" when the user gave an address but it didn't resolve
// to coordinates. This targets geocode failures only — flights carry IATA labels,
// not addresses, so resolver/quota gaps are NOT flagged here.
export function startUnlocated(p: PlanPart): boolean {
  return !!p.start_address && p.start_lat == null;
}

export function endUnlocated(p: PlanPart): boolean {
  return !!p.end_address && p.end_lat == null;
}

export function isUnlocated(p: PlanPart): boolean {
  return startUnlocated(p) || endUnlocated(p);
}

// unlocatedCount is the number of non-dismissed parts with an unresolved address —
// the figure shown in the map's "couldn't be placed" notice.
export function unlocatedCount(parts: PlanPart[]): number {
  return parts.filter((p) => !p.dismissed_at && isUnlocated(p)).length;
}

// parseLatLon reads a "lat, lng" pair as copied from a Google Maps pin
// (e.g. "48.2105, 4.0823", comma- or space-separated), returning null when it
// isn't two in-range numbers. Used by the edit dialog's coordinate override.
export function parseLatLon(s: string): { lat: number; lon: number } | null {
  const m = s.trim().match(/^(-?\d+(?:\.\d+)?)\s*[, ]\s*(-?\d+(?:\.\d+)?)$/);
  if (!m) return null;
  const lat = parseFloat(m[1]);
  const lon = parseFloat(m[2]);
  if (lat < -90 || lat > 90 || lon < -180 || lon > 180) return null;
  return { lat, lon };
}
