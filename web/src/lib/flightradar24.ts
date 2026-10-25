/**
 * Links to a flight on Flightradar24.
 *
 * An IATA flight number links to the flight's history page,
 * https://www.flightradar24.com/data/flights/<ident>, with the ident
 * lower-cased and its leading zeros stripped ("BA0117" -> "ba117"). That page
 * exists whether or not the flight is airborne, so it is the one link we give
 * for an IATA flight, en route or not.
 *
 * Only when there is no IATA flight number, because the stored ident is in its
 * ICAO form ("THY1") or there is just a callsign, do we fall back to the live
 * map, https://www.flightradar24.com/<CALLSIGN>, since the history page is
 * keyed on the IATA number. With neither, there is no link.
 *
 * These are plain https links rather than an app scheme, so that a tap can
 * hand off to the Flightradar24 app through iOS universal links or Android app
 * links where it is installed, and open the website where it isn't.
 */
import type { FlightDetail } from '../api/types';
import { normaliseIdent, splitIdent } from './flight-ident';

const FR24 = 'https://www.flightradar24.com';

/** An ICAO callsign: a three-letter designator and up to four more characters. */
const CALLSIGN_RE = /^[A-Z]{3}[A-Z0-9]{1,4}$/;

/** The Flightradar24 URL for a flight, or null when it has no usable ident. */
export function flightradar24Url(
  flight: Pick<FlightDetail, 'ident'> & { callsign?: string | null },
): string | null {
  const split = splitIdent(flight.ident ?? '');
  if (split) {
    const ident = split.airline + split.number + split.suffix;
    if (split.airline.length === 2) return `${FR24}/data/flights/${ident.toLowerCase()}`;
    return `${FR24}/${ident}`;
  }
  const callsign = normaliseIdent(flight.callsign ?? '');
  return CALLSIGN_RE.test(callsign) ? `${FR24}/${callsign}` : null;
}
