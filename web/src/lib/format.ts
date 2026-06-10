// Shared display formatters used across the flight list and detail panel.

import type { User } from '../api/types';

// userName picks the user's display name when set, falling back to the
// username. Use this anywhere we'd otherwise show the bare username — most
// users won't recognise "dpage", but they will recognise "Dave Page".
export function userName(u: User): string {
  return u.name?.trim() || u.username;
}

// userInitial returns a single uppercase letter for avatar fallbacks,
// derived from whatever userName() would render.
export function userInitial(u: User): string {
  return userName(u).charAt(0).toUpperCase();
}

// fmtDateTime renders an ISO timestamp in airport-local time. tz is the IANA
// zone of the relevant airport (origin for departures, destination for
// arrivals); when it's missing or empty we fall back to UTC and add a "UTC"
// suffix so the user knows which clock they're looking at. hour12:false keeps
// the output deterministic across runtime locales (and matches the 24-hour
// convention airlines and schedule sources actually use).
export function fmtDateTime(iso: string, tz?: string): string {
  const d = new Date(iso);
  const base = d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: tz || 'UTC',
  });
  return tz ? base : `${base} UTC`;
}

// fmtUTC renders the same instant in UTC for the secondary line beneath an
// airport-local time. Always includes the "UTC" suffix.
export function fmtUTC(iso: string): string {
  return fmtDateTime(iso, undefined);
}

// formatCost renders a booking total (issue #22). With a valid ISO 4217 code
// it uses the locale's currency formatting (e.g. "£250.00"); with a missing or
// unrecognised code it falls back to the bare amount plus whatever code we have
// ("250.00 XYZ"), so a stray currency string can never throw. Returns null when
// there's no amount to show.
export function formatCost(amount?: number | null, currency?: string): string | null {
  if (amount == null) return null;
  const code = (currency ?? '').trim().toUpperCase();
  if (/^[A-Z]{3}$/.test(code)) {
    try {
      return new Intl.NumberFormat(undefined, { style: 'currency', currency: code }).format(amount);
    } catch {
      // Well-formed but not a currency Intl knows — fall through to the plain form.
    }
  }
  const n = amount.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
  return code ? `${n} ${code}` : n;
}

// fmtRelative turns "seconds since X" into a compact human label, e.g.
// "42s", "3m", "1h 12m". Negative inputs are clamped to 0.
export function fmtRelative(sec: number): string {
  if (sec < 0) sec = 0;
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60);
  if (m < 60) {
    const s = sec % 60;
    return s === 0 ? `${m}m` : `${m}m ${s}s`;
  }
  const h = Math.floor(m / 60);
  const rm = m % 60;
  return rm === 0 ? `${h}h` : `${h}h ${rm}m`;
}

// fmtAgo returns how long ago an ISO timestamp was, relative to `now`.
// Returns "just now" for fixes under 5 seconds old.
export function fmtAgo(iso: string, now: number = Date.now()): string {
  const sec = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000));
  if (sec < 5) return 'just now';
  return `${fmtRelative(sec)} ago`;
}
