import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';

import type { PlanType } from '../api/types';
import PlanTypeIcon from './PlanTypeIcon';

const TYPES: PlanType[] = [
  'flight',
  'train',
  'hotel',
  'ground',
  'dining',
  'excursion',
  'ice_cream',
  'meeting',
  'event',
];
=======
const TYPES: PlanType[] = ['flight', 'train', 'hotel', 'ground', 'dining', 'excursion', 'meeting', 'event'];
>>>>>>> 1732f31 (feat: add Meeting and Event plan types)

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
});
