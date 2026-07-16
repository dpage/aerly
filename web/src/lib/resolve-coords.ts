import { api } from '../api/client';
import { coordsFromText, isMapsUrl } from './maps-url';

/** Turn free text into coordinates, following Google's URLs when needed.
 *
 * A bare "lat, lng" pair and a full Maps URL that already carries coordinates
 * are read locally (no network). Any other Google Maps URL — a short
 * maps.app.goo.gl link, or a /maps/place URL that names a place without
 * embedding its coordinates — is handed to the backend, which follows the
 * redirect and reads the coordinates off the resulting map page.
 *
 * Returns null when the text is neither coordinates nor a Maps URL (the caller
 * can treat that as "not a location"). Rejects only when the backend lookup
 * itself fails — including a place whose location genuinely could not be read. */
export async function resolveCoordsFromInput(
  raw: string,
): Promise<{ lat: number; lon: number } | null> {
  const s = raw.trim();
  if (s === '') return null;
  const local = coordsFromText(s);
  if (local) return local;
  if (!isMapsUrl(s)) return null;
  return api.resolveMapsUrl(s);
}
