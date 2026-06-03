// Coloured per-type map markers + a labelled popover, shared by the trip map
// and the tracker convergence map (PRD §6.5 / §11). Each plan type gets its own
// colour and a glyph so a glance at the map distinguishes a hotel from a
// restaurant from an airport, rather than a field of identical dots.

import type { PlanType } from '../api/types';
import { fmtLocalDateTime, planTypeLabel } from './trip-format';

interface TypeStyle {
  /** Pin fill colour. */
  color: string;
  /** Inner SVG markup (a single 24×24 path, filled white) for the glyph. */
  glyph: string;
}

// Material-style single-path glyphs, drawn white inside the coloured teardrop.
const FLIGHT =
  '<path d="M21 16v-2l-8-5V3.5c0-.83-.67-1.5-1.5-1.5S10 2.67 10 3.5V9l-8 5v2l8-2.5V19l-2 1.5V22l3.5-1 3.5 1v-1.5L13 19v-5.5l8 2.5z"/>';
const TRAIN =
  '<path d="M12 2c-4 0-8 .5-8 4v9.5C4 17.43 5.57 19 7.5 19L6 20.5v.5h12v-.5L16.5 19c1.93 0 3.5-1.57 3.5-3.5V6c0-3.5-3.58-4-8-4zM7.5 17c-.83 0-1.5-.67-1.5-1.5S6.67 14 7.5 14s1.5.67 1.5 1.5S8.33 17 7.5 17zM11 10H6V6h5v4zm2 0V6h5v4h-5zm3.5 7c-.83 0-1.5-.67-1.5-1.5s.67-1.5 1.5-1.5 1.5.67 1.5 1.5-.67 1.5-1.5 1.5z"/>';
const HOTEL =
  '<path d="M7 13c1.66 0 3-1.34 3-3S8.66 7 7 7s-3 1.34-3 3 1.34 3 3 3zm12-6h-8v7H3V5H1v15h2v-3h18v3h2v-9c0-2.21-1.79-4-4-4z"/>';
const GROUND =
  '<path d="M18.92 6.01C18.72 5.42 18.16 5 17.5 5h-11c-.66 0-1.21.42-1.42 1.01L3 12v8c0 .55.45 1 1 1h1c.55 0 1-.45 1-1v-1h12v1c0 .55.45 1 1 1h1c.55 0 1-.45 1-1v-8l-2.08-5.99zM6.5 16c-.83 0-1.5-.67-1.5-1.5S5.67 13 6.5 13s1.5.67 1.5 1.5S7.33 16 6.5 16zm11 0c-.83 0-1.5-.67-1.5-1.5s.67-1.5 1.5-1.5 1.5.67 1.5 1.5-.67 1.5-1.5 1.5zM5 11l1.5-4.5h11L19 11H5z"/>';
const DINING =
  '<path d="M8.1 13.34l2.83-2.83L3.91 3.5c-1.56 1.56-1.56 4.09 0 5.66l4.19 4.18zm6.78-1.81c1.53.71 3.68.21 5.27-1.38 1.91-1.91 2.28-4.65.81-6.12-1.46-1.46-4.2-1.1-6.12.81-1.59 1.59-2.09 3.74-1.38 5.27L3.7 19.87l1.41 1.41L12 14.41l6.88 6.88 1.41-1.41L13.41 13l1.47-1.47z"/>';
const EXCURSION =
  '<path d="M14 6l-3.75 5 2.85 3.8-1.6 1.2C9.81 13.75 7 10 7 10l-6 8h22L14 6z"/>';

const TYPE_STYLE: Record<PlanType, TypeStyle> = {
  flight: { color: '#1f5fa8', glyph: FLIGHT },
  train: { color: '#6d28d9', glyph: TRAIN },
  hotel: { color: '#b45309', glyph: HOTEL },
  ground: { color: '#0f766e', glyph: GROUND },
  dining: { color: '#be123c', glyph: DINING },
  excursion: { color: '#15803d', glyph: EXCURSION },
};

const FALLBACK: TypeStyle = { color: '#6b7280', glyph: '<circle cx="12" cy="12" r="6"/>' };

function styleFor(type: PlanType): TypeStyle {
  return TYPE_STYLE[type] ?? FALLBACK;
}

/** The marker colour for a plan type (e.g. for a legend or leg line). */
export function planTypeColor(type: PlanType): string {
  return styleFor(type).color;
}

// The teardrop outline: a circular head (centre ≈ 12,12) tapering to a tip at
// (12,33). Shared by the pin body and the strokes layered over it.
const TEARDROP =
  'M12 0.5 C5.6 0.5 0.5 5.6 0.5 12 C0.5 20.5 12 33.5 12 33.5 C12 33.5 23.5 20.5 23.5 12 C23.5 5.6 18.4 0.5 12 0.5 Z';

/** A coloured teardrop map-pin element carrying the plan type's glyph. The pin
 * tip is at the bottom-centre, so place the marker with `anchor: 'bottom'`.
 *
 * The fill encodes the plan type; `ringColor` (when given) draws a ring around
 * the pin to encode whose trip it is (issue #13). A white halo underneath keeps
 * the pin legible against the map whatever the ring hue. With no ring colour
 * the pin falls back to a plain white outline. */
export function buildPinEl(type: PlanType, ringColor?: string | null): HTMLElement {
  const s = styleFor(type);
  const ring = ringColor ?? '#fff';
  const el = document.createElement('div');
  el.style.cursor = 'pointer';
  el.style.lineHeight = '0';
  // Three layers: a white halo for map contrast, the type-coloured body with a
  // person-coloured ring, then the white type glyph centred in the head.
  el.innerHTML = `
    <svg width="26" height="36" viewBox="0 0 24 34" xmlns="http://www.w3.org/2000/svg"
         style="filter: drop-shadow(0 1px 2px rgba(0,0,0,0.4))">
      <path d="${TEARDROP}" fill="none" stroke="#fff" stroke-width="4" stroke-linejoin="round"/>
      <path d="${TEARDROP}" fill="${s.color}" stroke="${ring}" stroke-width="2.5" stroke-linejoin="round"/>
      <g transform="translate(4.8,5) scale(0.6)" fill="#fff">${s.glyph}</g>
    </svg>`;
  return el;
}

/** A labelled popover for a map marker: a coloured type glyph + the title, then
 * a Type / Location / When field list. Built with textContent so extracted
 * strings can't inject markup. Pass `iso`+`tz` to render a local When line. */
export interface MarkerPerson {
  name: string;
  avatarUrl?: string;
}

export function buildMarkerPopup(opts: {
  title: string;
  type: PlanType;
  location?: string;
  iso?: string;
  tz?: string;
  /** Who added the plan (shown as text). */
  owner?: string;
  /** Who's on the plan (shown as small avatars). */
  passengers?: MarkerPerson[];
}): HTMLElement {
  const s = styleFor(opts.type);
  const root = document.createElement('div');
  root.style.font = '12px/1.5 system-ui,-apple-system,sans-serif';
  root.style.minWidth = '150px';

  const header = document.createElement('div');
  header.style.display = 'flex';
  header.style.alignItems = 'center';
  header.style.gap = '6px';
  header.style.marginBottom = '4px';
  const icon = document.createElement('span');
  icon.style.lineHeight = '0';
  icon.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="${s.color}">${s.glyph}</svg>`;
  header.append(icon);
  const title = document.createElement('span');
  title.style.fontWeight = '600';
  title.textContent = opts.title;
  header.append(title);
  root.append(header);

  const when = opts.iso ? fmtLocalDateTime(opts.iso, opts.tz) : '';
  const rows: [string, string][] = [
    ['Type', planTypeLabel(opts.type)],
    ['Location', opts.location && opts.location !== opts.title ? opts.location : ''],
    ['When', when],
    ['Added by', opts.owner ?? ''],
  ];
  const grid = document.createElement('div');
  grid.style.display = 'grid';
  grid.style.gridTemplateColumns = 'auto 1fr';
  grid.style.columnGap = '8px';
  grid.style.rowGap = '2px';
  for (const [label, value] of rows) {
    if (!value) continue;
    const k = document.createElement('div');
    k.style.color = '#888';
    k.textContent = label;
    const v = document.createElement('div');
    v.style.color = '#222';
    v.textContent = value;
    grid.append(k, v);
  }
  root.append(grid);

  // Passengers as a row of small avatars (gravatar image, or initials).
  if (opts.passengers && opts.passengers.length > 0) {
    const paxRow = document.createElement('div');
    paxRow.style.display = 'flex';
    paxRow.style.alignItems = 'center';
    paxRow.style.gap = '4px';
    paxRow.style.marginTop = '6px';
    const label = document.createElement('span');
    label.style.color = '#888';
    label.textContent = 'On board';
    paxRow.append(label);
    for (const p of opts.passengers) {
      paxRow.append(avatarEl(p));
    }
    root.append(paxRow);
  }
  return root;
}

/** A 20px round avatar: the gravatar image when available, else an initial. */
function avatarEl(person: MarkerPerson): HTMLElement {
  const initial = (person.name.trim()[0] ?? '?').toUpperCase();
  if (person.avatarUrl) {
    const img = document.createElement('img');
    img.src = person.avatarUrl;
    img.alt = person.name;
    img.title = person.name;
    img.width = 20;
    img.height = 20;
    img.style.borderRadius = '50%';
    img.style.display = 'block';
    return img;
  }
  const el = document.createElement('span');
  el.title = person.name;
  el.textContent = initial;
  el.style.cssText =
    'display:inline-flex;align-items:center;justify-content:center;width:20px;height:20px;' +
    'border-radius:50%;background:#bbb;color:#fff;font-size:11px;font-weight:600;flex:none';
  return el;
}
