import { describe, it, expect } from 'vitest';

import type { PlanType } from '../api/types';
import { buildMarkerPopup, buildPinEl, planTypeColor } from './plan-marker';

const TYPES: PlanType[] = [
  'flight',
  'train',
  'hotel',
  'ground',
  'dining',
  'excursion',
  'ice_cream',
];

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

  it('draws ice cream as a cone marker, not the generic teardrop', () => {
    const el = buildPinEl('ice_cream');
    // The scoop carries the ice-cream colour and the cone has a round scoop.
    expect(el.outerHTML).toContain(planTypeColor('ice_cream'));
    expect(el.querySelector('circle')).not.toBeNull();
    // The cone is its own shape, so the shared teardrop path is absent.
    expect(el.outerHTML).not.toContain('C5.6 0.5 0.5 5.6');
  });

  it('rings the ice cream scoop with the person colour when given', () => {
    const el = buildPinEl('ice_cream', 'hsl(200, 70%, 42%)');
    expect(el.outerHTML).toContain('stroke="hsl(200, 70%, 42%)"');
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

  it('shows an Address row alongside the place name', () => {
    const el = buildMarkerPopup({
      title: 'Rain or Shine',
      type: 'ice_cream',
      location: 'Rain or Shine',
      address: '812 Homer St, Vancouver, British Columbia V6B 2W5',
    });
    expect(el.textContent).toContain('Address');
    expect(el.textContent).toContain('812 Homer St, Vancouver, British Columbia V6B 2W5');
  });

  it('shows the Address even when the place name is just the type fallback', () => {
    // A part with no label falls back to the type as its title; the address
    // should still surface.
    const el = buildMarkerPopup({
      title: 'Ice cream',
      type: 'ice_cream',
      address: '812 Homer St, Vancouver',
    });
    expect(el.textContent).toContain('Address');
    expect(el.textContent).toContain('812 Homer St, Vancouver');
  });

  it('omits the Address row when it merely repeats the title or location', () => {
    const sameAsTitle = buildMarkerPopup({
      title: '812 Homer St',
      type: 'ice_cream',
      address: '812 Homer St',
    });
    expect(sameAsTitle.textContent).not.toContain('Address');
    const sameAsLocation = buildMarkerPopup({
      title: 'Rain or Shine',
      type: 'ice_cream',
      location: '812 Homer St',
      address: '812 Homer St',
    });
    expect(sameAsLocation.textContent).not.toContain('Address');
  });

  it('omits the Address row when no address is supplied', () => {
    const el = buildMarkerPopup({ title: 'BA217', type: 'flight' });
    expect(el.textContent).not.toContain('Address');
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
