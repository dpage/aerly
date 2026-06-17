import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import AerlyLogo from './AerlyLogo';

describe('AerlyLogo', () => {
  it('renders the single brand mark', () => {
    render(<AerlyLogo />);
    const img = screen.getByAltText('Aerly');
    expect(img).toHaveAttribute('src', '/aerly-mark.png');
  });

  it('applies the requested size as width and height', () => {
    render(<AerlyLogo size={56} />);
    const img = screen.getByAltText('Aerly');
    // MUI's Box maps width/height to CSS (an emotion class), not HTML
    // attributes, so assert the resolved style rather than getAttribute.
    expect(img).toHaveStyle({ width: '56px', height: '56px' });
  });
});
