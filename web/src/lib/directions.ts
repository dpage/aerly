// Deep links that open a native maps app with turn-by-turn directions to a
// plan's location. We only ever supply the destination; every maps app fills in
// the origin from the device's own current location, so there's no need for the
// browser Geolocation API (and no permission prompt).

export type MapsProvider = 'apple' | 'google' | 'waze';

/** Where to route to. Coordinates are preferred (unambiguous); the text query
 * (a place name or address) is the fallback when a plan has no coordinates. */
export interface DirectionsTarget {
  lat?: number | null;
  lon?: number | null;
  query?: string;
}

/** The maps apps offered, in menu order, with their display labels. */
export const MAPS_PROVIDERS: { value: MapsProvider; label: string }[] = [
  { value: 'apple', label: 'Apple Maps' },
  { value: 'google', label: 'Google Maps' },
  { value: 'waze', label: 'Waze' },
];

/** True when the target has something routable: coordinates, or a non-blank
 * query. Used to decide whether to show the Directions control at all. */
export function canRouteTo(t: DirectionsTarget): boolean {
  return hasCoords(t) || (t.query ?? '').trim() !== '';
}

/** Build the Directions target for a plan endpoint, choosing between its
 * coordinates and its human address:
 *
 *   - Pinned coordinates (dropped-pin / map link / Explore POI) are
 *     authoritative, so route to the exact point.
 *   - Auto-geocoded coordinates are only as good as Nominatim's guess (which can
 *     miss a rural address by a few hundred metres), so prefer the address text
 *     and let the maps app apply its own, usually better, geocoder.
 *   - Fall back to coordinates when there's no usable address at all.
 */
export function planDirectionsTarget(p: {
  lat?: number | null;
  lon?: number | null;
  pinned?: boolean;
  address?: string;
  label?: string;
}): DirectionsTarget {
  const coords = { lat: p.lat, lon: p.lon };
  const query = (p.address || p.label || '').trim();
  if (p.pinned && hasCoords(coords)) return coords;
  if (query) return { query };
  return coords;
}

function hasCoords(t: DirectionsTarget): boolean {
  return t.lat != null && t.lon != null;
}

/** Build the directions deep link for a provider. Prefers coordinates; falls
 * back to the URL-encoded text query. The origin is deliberately omitted so the
 * maps app routes from the user's current location. Callers must gate on
 * canRouteTo first (a target with neither coords nor query yields an
 * empty-destination URL). */
export function directionsUrl(provider: MapsProvider, t: DirectionsTarget): string {
  const coords = hasCoords(t) ? `${t.lat},${t.lon}` : '';
  const q = encodeURIComponent((t.query ?? '').trim());
  const useCoords = coords !== '';
  switch (provider) {
    case 'apple':
      // daddr accepts "lat,lon" or an address; dirflg=d requests driving.
      return `https://maps.apple.com/?daddr=${useCoords ? coords : q}&dirflg=d`;
    case 'google':
      // The official Maps URLs API: destination as "lat,lng" or an address.
      return `https://www.google.com/maps/dir/?api=1&destination=${useCoords ? coords : q}`;
    case 'waze':
      // Waze takes coordinates via ll= and a free-text search via q=.
      return useCoords
        ? `https://waze.com/ul?ll=${coords}&navigate=yes`
        : `https://waze.com/ul?q=${q}&navigate=yes`;
  }
}
