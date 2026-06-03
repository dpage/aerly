// Per-person map colours (issue #13). Each person's trips are drawn in one
// stable hue derived from a hash of their identity, so on a shared map "my
// trips" and "friend X's trips" are visually distinguishable at a glance. The
// type is still encoded by the pin glyph; this only drives the pin ring, the
// leg lines, the plane icon, and the list-row dot border.

// Fixed saturation/lightness chosen so every hue lands medium-dark and
// saturated — enough contrast against the light OpenStreetMap basemap and the
// white pin halo (the constraint raised on the issue). Only the hue varies.
const SATURATION = 70;
const LIGHTNESS = 42;

/**
 * A deterministic, map-contrasting colour for a person identity (e.g. a trip
 * owner's user id). The same key always yields the same colour; different keys
 * spread around the hue wheel. Returns null when the key is missing/0 so
 * callers can fall back to the existing type colour.
 */
export function personColor(key: number | string | null | undefined): string | null {
  if (key == null || key === 0 || key === '') return null;
  const hue = hashHue(String(key));
  return `hsl(${hue}, ${SATURATION}%, ${LIGHTNESS}%)`;
}

// FNV-1a (32-bit) → a hue in [0, 360). Cheap, dependency-free, and well spread
// for the small integer keys we hash.
function hashHue(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0) % 360;
}
