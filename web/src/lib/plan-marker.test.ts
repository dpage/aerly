import { describe, it, expect } from 'vitest';

import type { PlanType } from '../api/types';
import { buildMarkerPopup, buildPinEl, planTypeColor } from './plan-marker';

const TYPES: PlanType[] = ['flight', 'train', 'hotel', 'ground', 'dining', 'excursion'];

describe('planTypeColor', () => {
  it.each(TYPES)('returns a distinct hex colour for %s', (type) => {
    expect(planTypeColor(type)).toMatch(/^#[0-9a-f]{6}$/i);
  });

  it('returns the fallback grey for an unknown type', () => {
    expect(planTypeColor('mystery' as PlanType)).toBe('#6b7280');
  });
});

describe('buildPinEl', () => {
  it.each(TYPES)('builds a teardrop pin carrying the %s colour and glyph', (type) => {
    const el = buildPinEl(type);
    expect(el).toBeInstanceOf(HTMLElement);
    expect(el.style.cursor).toBe('pointer');
    const svg = el.querySelector('svg');
    expect(svg).not.toBeNull();
    // The teardrop is filled with the type colour.
    expect(el.outerHTML).toContain(planTypeColor(type));
    // The glyph group is present.
    expect(el.querySelector('g')).not.toBeNull();
  });

  it('uses the fallback glyph (a circle) for an unknown type', () => {
    const el = buildPinEl('mystery' as PlanType);
    expect(el.querySelector('circle')).not.toBeNull();
    expect(el.outerHTML).toContain('#6b7280');
  });

  it('draws the person ring colour as a stroke when given (issue #13)', () => {
    const el = buildPinEl('flight', 'hsl(120, 70%, 42%)');
    // Type colour still fills the body; the person colour rings it.
    expect(el.outerHTML).toContain(planTypeColor('flight'));
    expect(el.outerHTML).toContain('stroke="hsl(120, 70%, 42%)"');
  });

  it('falls back to a white outline when no ring colour is given', () => {
    const el = buildPinEl('flight', null);
    // The body stroke is white (no person colour present).
    expect(el.outerHTML).toContain('stroke="#fff"');
    expect(el.outerHTML).not.toContain('hsl(');
  });
});

describe('buildMarkerPopup', () => {
  it('renders the title in the header', () => {
    const el = buildMarkerPopup({ title: 'Hotel Lisboa', type: 'hotel' });
    expect(el).toBeInstanceOf(HTMLElement);
    expect(el.textContent).toContain('Hotel Lisboa');
    expect(el.querySelector('svg')).not.toBeNull();
  });

  it('renders the Type label row for every type', () => {
    for (const type of TYPES) {
      const el = buildMarkerPopup({ title: 'X', type });
      expect(el.textContent).toContain('Type');
    }
  });

  it('shows a Location row when location differs from the title', () => {
    const el = buildMarkerPopup({
      title: 'Hotel Lisboa',
      type: 'hotel',
      location: 'Rua Augusta 1, Lisbon',
    });
    expect(el.textContent).toContain('Location');
    expect(el.textContent).toContain('Rua Augusta 1, Lisbon');
  });

  it('omits the Location row when location equals the title', () => {
    const el = buildMarkerPopup({ title: 'Nobu', type: 'dining', location: 'Nobu' });
    expect(el.textContent).not.toContain('Location');
  });

  it('omits the Location row when location is absent', () => {
    const el = buildMarkerPopup({ title: 'Nobu', type: 'dining' });
    expect(el.textContent).not.toContain('Location');
  });

  it('renders a When row when an iso is supplied', () => {
    const el = buildMarkerPopup({
      title: 'BA217',
      type: 'flight',
      iso: '2026-07-01T14:00:00Z',
      tz: 'America/New_York',
    });
    expect(el.textContent).toContain('When');
    expect(el.textContent).toMatch(/EDT/);
  });

  it('omits the When row when no iso is supplied', () => {
    const el = buildMarkerPopup({ title: 'BA217', type: 'flight' });
    expect(el.textContent).not.toContain('When');
  });

  it('falls back to the grey style for an unknown type', () => {
    const el = buildMarkerPopup({ title: 'X', type: 'mystery' as PlanType });
    expect(el.outerHTML).toContain('#6b7280');
  });

  it('shows the owner as an "Added by" row', () => {
    const el = buildMarkerPopup({ title: 'BA217', type: 'flight', owner: 'Dave Page' });
    expect(el.textContent).toContain('Added by');
    expect(el.textContent).toContain('Dave Page');
  });

  it('renders passenger avatars: a gravatar image and an initials fallback', () => {
    const el = buildMarkerPopup({
      title: 'BA217',
      type: 'flight',
      passengers: [
        { name: 'Avatar Person', avatarUrl: 'https://gravatar/x.png' },
        { name: 'No Pic' },
      ],
    });
    expect(el.textContent).toContain('On board');
    const img = el.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://gravatar/x.png');
    expect(img?.getAttribute('title')).toBe('Avatar Person');
    // The pic-less passenger renders an initial.
    expect(el.textContent).toContain('N');
  });

  it('omits the passenger row when there are none', () => {
    const el = buildMarkerPopup({ title: 'BA217', type: 'flight', passengers: [] });
    expect(el.textContent).not.toContain('On board');
  });
});
