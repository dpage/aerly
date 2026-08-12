import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';

import type { PlanType } from '../api/types';
import PlanTypeIcon from './PlanTypeIcon';

const TYPES: PlanType[] = [
  'flight',
  'train',
  'hotel',
  'ground',
  'vehicle_hire',
  'dining',
  'excursion',
  'ice_cream',
  'meeting',
  'event',
];

describe('PlanTypeIcon', () => {
  it.each(TYPES)('renders an svg icon for %s', (type) => {
    const { container } = render(<PlanTypeIcon type={type} data-testid="icon" />);
    const svg = container.querySelector('svg');
    expect(svg).not.toBeNull();
  });

  it('renders the fallback Place icon for an unknown type', () => {
    const { container } = render(<PlanTypeIcon type={'mystery' as PlanType} data-testid="icon" />);
    expect(container.querySelector('svg')).not.toBeNull();
  });

  it('forwards SvgIconProps through to the icon', () => {
    const { container } = render(<PlanTypeIcon type="flight" fontSize="small" />);
    const svg = container.querySelector('svg');
    expect(svg?.classList.toString()).toMatch(/MuiSvgIcon/);
  });

  it('uses a different glyph for vehicle_hire than for ground, matching the map marker', () => {
    // The map marker (plan-marker.ts) deliberately distinguishes a hire's
    // CarRental glyph from ground's plain DirectionsCar silhouette; the icon
    // used here must draw the same distinction rather than sharing one glyph.
    const ground = render(<PlanTypeIcon type="ground" />).container.querySelector('path');
    const hire = render(<PlanTypeIcon type="vehicle_hire" />).container.querySelector('path');
    expect(hire?.getAttribute('d')).not.toEqual(ground?.getAttribute('d'));
  });
});
