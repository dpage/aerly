/**
 * Flight-number parsing, mirroring `internal/flightident` on the server.
 *
 * An IATA airline designator may itself contain a digit: easyJet is "U2",
 * Jet Airways "9W", Germanwings "4U". A letters-only prefix rule reads
 * "U21234" as flight 21234 on airline "U", which is nobody's flight. So we
 * take the two-character designator IATA actually assigns, and only fall back
 * to a three-letter ICAO one (BAW, DLH) when two characters leave a remainder
 * that isn't a flight number.
 */

/** Digits with an optional trailing operational suffix letter. */
const NUMBER_RE = /^([0-9]+)([A-Z]?)$/;
/** IATA flight numbers run to four significant digits. */
const MAX_NUMBER_DIGITS = 4;
/** Two alphanumeric characters, at least one of them a letter. */
const IATA_RE = /^(?=.*[A-Z])[A-Z0-9]{2}$/;
/** Three letters. */
const ICAO_RE = /^[A-Z]{3}$/;

export type SplitIdent = {
  /** Airline designator, e.g. "BA", "U2", "BAW". */
  airline: string;
  /** Flight number with leading zeros stripped, e.g. "286". */
  number: string;
  /** Trailing operational suffix letter, or "". */
  suffix: string;
};

/**
 * Canonicalise a hand-written flight number: upper-cased with all whitespace
 * removed, so "ba 286", " BA286 " and "BA  286" all become "BA286".
 */
export function normaliseIdent(s: string): string {
  return s.toUpperCase().replace(/\s+/g, '');
}

/**
 * Split a flight number into its airline designator and number, or null when
 * it isn't one. The input is normalised first, so "U2 1234" splits as readily
 * as "U21234".
 */
export function splitIdent(s: string): SplitIdent | null {
  const ident = normaliseIdent(s);
  for (const [len, re] of [
    [2, IATA_RE],
    [3, ICAO_RE],
  ] as const) {
    if (ident.length <= len) continue;
    const prefix = ident.slice(0, len);
    if (!re.test(prefix)) continue;
    const m = NUMBER_RE.exec(ident.slice(len));
    if (!m) continue;
    // Leading zeros are stripped before the width is checked, so an
    // over-padded "BA00087" still reads as 87 whilst a genuinely five-digit
    // "AC12345" is rejected as not a flight number.
    const number = m[1].replace(/^0+/, '');
    if (number === '' || number.length > MAX_NUMBER_DIGITS) continue;
    return { airline: prefix, number, suffix: m[2] };
  }
  return null;
}

/** The airline designator of a flight number, or null when it isn't one. */
export function airlineOf(s: string): string | null {
  return splitIdent(s)?.airline ?? null;
}
