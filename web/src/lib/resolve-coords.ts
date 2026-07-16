import { api } from '../api/client';
import { coordsFromText, isShortMapsUrl } from './maps-url';

/** The message shown when we can't read an exact location from the input — most
 * often an iOS "Share" link, which names the place by a Google feature ID and
 * carries no coordinates (and Google won't hand its pin to a server without an
 * API key). Rather than drop a wrong marker, we tell the user how to get an
 * exact one. Worded to fit both a coordinate-less link and plain typed text. */
export const MAPS_NO_COORDS =
  'Couldn\'t read an exact location from that. In Google Maps, long-press the pin ' +
  'and paste the "lat, lng" it copies — a plain "Share" link has no coordinates in it.';

/** Resolve free text to coordinates without ever guessing.
 *
 * A bare "lat, lng" pair and a full Maps URL that already carries coordinates
 * are read locally. A short maps.app.goo.gl link is followed server-side, which
 * yields coordinates only when its destination URL actually contains them.
 *
 * Returns null when nothing exact could be read — a non-location string, or a
 * link that names a place without any coordinates. Never throws: a failed
 * backend lookup resolves to null so the caller shows one consistent message. */
export async function resolveCoordsFromInput(
  raw: string,
): Promise<{ lat: number; lon: number } | null> {
  const s = raw.trim();
  const local = coordsFromText(s);
  if (local) return local;
  if (!isShortMapsUrl(s)) return null;
  try {
    return await api.resolveMapsUrl(s);
  } catch {
    return null;
  }
}
