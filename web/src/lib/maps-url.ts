import { parseLatLon } from './geo';

const MAPS_HOST_RE = /(^|\.)google\.[a-z.]+$/;
const GOOGLE_MAPS_HOSTS = new Set(['maps.google.com']);
const SHORT_HOSTS = new Set(['maps.app.goo.gl', 'goo.gl', 'app.goo.gl', 'g.co']);

function parseUrl(s: string): URL | null {
  try {
    const u = new URL(s.trim());
    return u.protocol === 'http:' || u.protocol === 'https:' ? u : null;
  } catch {
    return null;
  }
}

/** True when `s` is an http(s) URL on a Google Maps or goo.gl host. */
export function isMapsUrl(s: string): boolean {
  const u = parseUrl(s);
  if (!u) return false;
  const host = u.hostname.toLowerCase();
  return (
    SHORT_HOSTS.has(host) ||
    GOOGLE_MAPS_HOSTS.has(host) ||
    (MAPS_HOST_RE.test(host) && u.pathname.startsWith('/maps'))
  );
}

/** True for a Google short link, which carries no coordinates and must be
 * resolved server-side by following its redirect. */
export function isShortMapsUrl(s: string): boolean {
  const u = parseUrl(s);
  return u != null && SHORT_HOSTS.has(u.hostname.toLowerCase());
}

function inRange(lat: number, lon: number): boolean {
  return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180;
}

function pick(lat: number, lon: number): { lat: number; lon: number } | null {
  return inRange(lat, lon) ? { lat, lon } : null;
}

const NUM = '(-?\\d+(?:\\.\\d+)?)';
// The pinned place (data segment) is the most accurate.
const PLACE_RE = new RegExp(`!3d${NUM}!4d${NUM}`);
// Explicit query coordinates.
const QUERY_RE = new RegExp(`[?&](?:q|ll|query|destination|center)=${NUM},${NUM}`);
// The map viewport centre — a fallback, close to but not always the pin.
const AT_RE = new RegExp(`@${NUM},${NUM}`);

/** Pull coordinates out of a full Google Maps URL, in precedence order:
 * the pinned place (!3d!4d), then explicit query coords, then the @ viewport.
 * Returns null for a place-only URL or out-of-range values. Percent-encoded
 * commas are decoded first. */
export function extractLatLonFromMapsUrl(url: string): { lat: number; lon: number } | null {
  let s = url;
  try {
    s = decodeURIComponent(url);
  } catch {
    // Malformed escape sequence — fall back to the raw string.
  }
  for (const re of [PLACE_RE, QUERY_RE, AT_RE]) {
    const m = s.match(re);
    if (m) {
      const hit = pick(parseFloat(m[1]), parseFloat(m[2]));
      if (hit) return hit;
    }
  }
  return null;
}

/** Coordinates from free text: a bare "lat, lng" pair, else a full Maps URL.
 * Returns null for short links (resolve those via the backend) and anything
 * else. */
export function coordsFromText(s: string): { lat: number; lon: number } | null {
  return parseLatLon(s) ?? (isMapsUrl(s) ? extractLatLonFromMapsUrl(s) : null);
}
