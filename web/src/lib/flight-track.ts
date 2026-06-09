import type { PlanPart, Position } from '../api/types';
import { initialBearing } from './great-circle';

/** Pure flight position/track/window helpers shared by the map view: where a
 * plane icon belongs (live or scrubbed back in time), when it's visible, and
 * the flown-track polyline. Kept out of the React component so they can be unit
 * tested directly (and so the component file only exports components). */

/** Where to draw a flight's plane icon, and how: a live position with its
 * reported heading when airborne, else parked at the origin (not departed) or
 * destination (arrived) and oriented along the route. */
export interface PlanePlacement {
  lon: number;
  lat: number;
  heading: number;
  estimated: boolean;
}

/** How long before departure / after arrival a flight's plane icon is shown. */
const PLANE_WINDOW_MS = 2 * 60 * 60 * 1000;

/** Parse an ISO timestamp to epoch ms, or null when absent/unparseable. */
export function parseMs(iso?: string | null): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : t;
}

/** A flight leg's effective departure / arrival epoch ms (actual → estimated →
 * scheduled, falling back to the part's own start/end), or null when unknown. */
function flightDeparture(p: PlanPart): number | null {
  const f = p.flight;
  return parseMs(f?.actual_out) ?? parseMs(f?.estimated_out) ?? parseMs(f?.scheduled_out) ?? parseMs(p.starts_at);
}
function flightArrival(p: PlanPart): number | null {
  const f = p.flight;
  return parseMs(f?.actual_in) ?? parseMs(f?.estimated_in) ?? parseMs(f?.scheduled_in) ?? parseMs(p.ends_at);
}

/** The live plane placement (latest reported fix). Landed: park at the
 * destination; airborne (or dead-reckoned): the live position + reported
 * heading; not departed: park at the origin, oriented along the route. null for
 * non-flight parts or a flight with no usable coordinate. */
export function planePlacement(p: PlanPart): PlanePlacement | null {
  if (p.type !== 'flight') return null;
  const hasStart = p.start_lat != null && p.start_lon != null;
  const hasEnd = p.end_lat != null && p.end_lon != null;
  // When parked on the ground, point the icon along the route (origin → dest)
  // rather than due north. Needs both endpoints; falls back to north otherwise.
  const routeHeading =
    hasStart && hasEnd ? initialBearing(p.start_lat!, p.start_lon!, p.end_lat!, p.end_lon!) : 0;
  // Landed: park at the destination, regardless of the last tracked fix.
  if (p.flight?.flight_status === 'Arrived' && hasEnd) {
    return { lon: p.end_lon!, lat: p.end_lat!, heading: routeHeading, estimated: false };
  }
  // Airborne (or dead-reckoned through an ADS-B gap): the live position and its
  // reported heading (route bearing as a fallback when the fix lacks one).
  const pos = p.flight?.latest_position;
  if (pos) {
    return {
      lon: pos.lon,
      lat: pos.lat,
      heading: pos.heading_deg ?? routeHeading,
      estimated: pos.is_estimated,
    };
  }
  // Not departed yet: park at the origin, oriented along the route.
  if (hasStart) return { lon: p.start_lon!, lat: p.start_lat!, heading: routeHeading, estimated: false };
  if (hasEnd) return { lon: p.end_lon!, lat: p.end_lat!, heading: routeHeading, estimated: false };
  return null;
}

/** Where a flight's plane icon belongs at a past instant `t` (the scrubber):
 * interpolated along its flown track when airborne, else parked at the origin
 * before push-back or the destination once it had landed. Mirrors
 * planePlacement's endpoint fallbacks for a flight with only partial data. */
export function planePlacementAt(p: PlanPart, t: number): PlanePlacement | null {
  if (p.type !== 'flight') return null;
  const hasStart = p.start_lat != null && p.start_lon != null;
  const hasEnd = p.end_lat != null && p.end_lon != null;
  const routeHeading =
    hasStart && hasEnd ? initialBearing(p.start_lat!, p.start_lon!, p.end_lat!, p.end_lon!) : 0;
  const origin = (): PlanePlacement | null =>
    hasStart ? { lon: p.start_lon!, lat: p.start_lat!, heading: routeHeading, estimated: false } : null;
  const dest = (): PlanePlacement | null =>
    hasEnd ? { lon: p.end_lon!, lat: p.end_lat!, heading: routeHeading, estimated: false } : null;
  const dep = flightDeparture(p);
  const arr = flightArrival(p);

  // Before push-back: parked at the origin, oriented along the route.
  if (dep != null && t < dep) return origin() ?? dest();
  // Landed before `t`: parked at the destination.
  if (p.flight?.flight_status === 'Arrived' && arr != null && t >= arr) return dest() ?? origin();
  // Airborne: interpolate along the flown track.
  const pos = positionAt(p.flight?.track ?? [], t);
  if (pos) {
    return { lon: pos.lon, lat: pos.lat, heading: pos.heading ?? routeHeading, estimated: pos.estimated };
  }
  // No track sample covers `t` (departed but pre-coverage, or no track at all):
  // fall back by phase.
  if (arr != null && t >= arr) return dest() ?? origin();
  return origin() ?? dest();
}

/** The interpolated position along an oldest→newest track at instant `t`.
 * Returns null when `t` precedes the first sample (the caller parks at the
 * origin); at/after the last sample returns that sample. */
export function positionAt(
  track: Position[],
  t: number,
): { lat: number; lon: number; heading?: number; estimated: boolean } | null {
  if (track.length === 0) return null;
  if (t < Date.parse(track[0].ts)) return null;
  const lastPt = track[track.length - 1];
  if (t >= Date.parse(lastPt.ts)) {
    return { lat: lastPt.lat, lon: lastPt.lon, heading: lastPt.heading_deg, estimated: lastPt.is_estimated };
  }
  // `t` sits strictly inside the track: find the bracketing pair and lerp.
  let i = 0;
  while (i + 2 < track.length && Date.parse(track[i + 1].ts) <= t) i++;
  const a = track[i];
  const b = track[i + 1];
  const ta = Date.parse(a.ts);
  const tb = Date.parse(b.ts);
  const f = tb > ta ? (t - ta) / (tb - ta) : 0;
  return {
    lat: a.lat + (b.lat - a.lat) * f,
    lon: a.lon + (b.lon - a.lon) * f,
    heading: b.heading_deg ?? a.heading_deg ?? initialBearing(a.lat, a.lon, b.lat, b.lon),
    estimated: a.is_estimated || b.is_estimated,
  };
}

/** The visibility window [start, end) per flight part. A booking's connecting
 * legs share one Plan (plan_id), so we group by it: each leg shows from 2h
 * before its departure to 2h after its arrival, and where consecutive legs would
 * overlap we hand the icon off at the midpoint of the layover. That guarantees a
 * single plane per journey at any instant. Non-flight parts are skipped. */
export function planeWindows(parts: PlanPart[]): Map<number, { start: number; end: number }> {
  const out = new Map<number, { start: number; end: number }>();
  const groups = new Map<number, PlanPart[]>();
  for (const p of parts) {
    if (p.type !== 'flight') continue;
    const g = groups.get(p.plan_id) ?? [];
    g.push(p);
    groups.set(p.plan_id, g);
  }
  for (const legs of groups.values()) {
    const wins = legs
      .map((p) => ({ id: p.id, dep: flightDeparture(p), arr: flightArrival(p) }))
      .filter((w): w is { id: number; dep: number; arr: number | null } => w.dep != null)
      .sort((a, b) => a.dep - b.dep)
      .map((w) => ({
        id: w.id,
        dep: w.dep,
        arr: w.arr ?? w.dep,
        start: w.dep - PLANE_WINDOW_MS,
        end: (w.arr ?? w.dep) + PLANE_WINDOW_MS,
      }));
    // Clamp overlapping consecutive legs to a single handoff at the layover's
    // midpoint, so the previous leg's icon hides exactly as the next appears.
    for (let i = 0; i + 1 < wins.length; i++) {
      const a = wins[i];
      const b = wins[i + 1];
      if (a.end > b.start) {
        const mid = (a.arr + b.dep) / 2;
        a.end = mid;
        b.start = mid;
      }
    }
    for (const w of wins) out.set(w.id, { start: w.start, end: w.end });
  }
  return out;
}

/** The selected flight's flown-track polyline. When scrubbing (`until` is an
 * epoch ms) it's clipped to the samples up to that instant plus an interpolated
 * tip, so the orange trail ends exactly under the scrubbed plane. */
export function trackFC(selected: PlanPart | null, until: number | null): GeoJSON.FeatureCollection {
  const track = selected?.flight?.track ?? [];
  if (track.length < 2) return emptyFC();
  const coords = clipTrackCoords(track, until);
  if (coords.length < 2) return emptyFC();
  return {
    type: 'FeatureCollection',
    features: [{ type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates: coords } }],
  };
}

/** The flown trails of *every* given flight part, each clipped to `until` (an
 * epoch ms) with an interpolated tip — used when scrubbing so replaying the
 * past shows where each active plane had been, not just the selected one. Parts
 * with fewer than two resulting points are skipped. */
export function tracksFC(parts: PlanPart[], until: number): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const p of parts) {
    const track = p.flight?.track ?? [];
    if (track.length < 2) continue;
    const coords = clipTrackCoords(track, until);
    if (coords.length < 2) continue;
    features.push({
      type: 'Feature',
      properties: { partId: p.id },
      geometry: { type: 'LineString', coordinates: coords },
    });
  }
  return { type: 'FeatureCollection', features };
}

/** The track's [lon, lat] coordinates, optionally clipped to instant `until`
 * (epoch ms) with an interpolated tip so the trail ends under the scrubbed
 * plane. `until == null` returns the full track. */
function clipTrackCoords(track: Position[], until: number | null): [number, number][] {
  if (until == null) return track.map((t) => [t.lon, t.lat]);
  const coords: [number, number][] = [];
  for (const t of track) {
    const ms = parseMs(t.ts);
    if (ms != null && ms > until) break;
    coords.push([t.lon, t.lat]);
  }
  const tip = positionAt(track, until);
  const lastCoord = coords[coords.length - 1];
  if (tip && (!lastCoord || lastCoord[0] !== tip.lon || lastCoord[1] !== tip.lat)) {
    coords.push([tip.lon, tip.lat]);
  }
  return coords;
}

/** The scrubbed instant in the viewer's local time, e.g. "Mon 12 Oct, 11:30".
 * The map can span many timezones, so local (browser) time is the one frame
 * everything shares. */
export function fmtScrubTime(ms: number): string {
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

function emptyFC(): GeoJSON.FeatureCollection {
  return { type: 'FeatureCollection', features: [] };
}
