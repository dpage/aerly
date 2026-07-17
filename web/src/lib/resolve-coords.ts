import { api } from '../api/client';
import { coordsFromText, isMapsUrl } from './maps-url';

/** The message shown when we can't read an exact location from the input, or
 * when a geocoded guess is declined. Most often hit for an iOS "Share" link,
 * which names the place by a Google feature ID and carries no coordinates
 * (and Google won't hand its pin to a server without an API key). Rather than
 * drop a wrong marker, we tell the user how to get an exact one. Worded to fit
 * a coordinate-less link, plain typed text, and a rejected guess alike. */
export const MAPS_NO_COORDS =
  'Couldn\'t read an exact location from that. In Google Maps, long-press the pin ' +
  'and paste the "lat, lng" it copies — a plain "Share" link has no coordinates in it.';

export type ResolvedLocation = {
  lat: number;
  lon: number;
  /** The formatted address the backend geocoded, present whenever
   * `needsConfirmation` is true. */
  label?: string;
  /** True when the coordinates were geocoded from the link's text rather than
   * read from the link itself. The caller MUST show `label` and let the user
   * accept or reject before pinning: a geocoded link is a good lead, not the
   * pin the user chose. */
  needsConfirmation: boolean;
};

/** Resolve free text to a location without ever silently pinning a guess.
 *
 * A bare "lat, lng" pair and a full Maps URL that already carries coordinates
 * are read locally and never need confirming, whilst any other Google Maps
 * URL (a short link, or a full "/maps/place/…" link that names a place
 * without coordinates) is sent to the backend, which follows or geocodes it.
 * When the backend had to geocode the link's text rather than read an actual
 * coordinate, the result carries `needs_confirmation: true` and the caller
 * must show `label` and let the user accept or reject it before pinning.
 *
 * Returns null when nothing could be resolved at all, whether that's a
 * non-location string or a link the backend declined. Never throws: a failed
 * backend lookup resolves to null so the caller shows one consistent message. */
export async function resolveCoordsFromInput(raw: string): Promise<ResolvedLocation | null> {
  const s = raw.trim();
  const local = coordsFromText(s);
  if (local) return { ...local, needsConfirmation: false };
  if (!isMapsUrl(s)) return null;
  try {
    const r = await api.resolveMapsUrl(s);
    return {
      lat: r.lat,
      lon: r.lon,
      label: r.label,
      needsConfirmation: r.needs_confirmation === true,
    };
  } catch {
    return null;
  }
}
