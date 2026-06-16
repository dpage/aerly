import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import AerlyLogo from './AerlyLogo';
import { setThemePreference } from '../theme';

describe('AerlyLogo', () => {
  it('renders the metallic mark in light mode', () => {
    setThemePreference('light');
    render(<AerlyLogo />);
    const img = screen.getByAltText('Aerly');
    expect(img).toHaveAttribute('src', '/aerly-mark-light.png');
  });

  it('renders the glowing mark in dark mode', () => {
    setThemePreference('dark');
    render(<AerlyLogo />);
    const img = screen.getByAltText('Aerly');
    expect(img).toHaveAttribute('src', '/aerly-mark-dark.png');
  });

  it('applies the requested size as width and height', () => {
    setThemePreference('light');
    render(<AerlyLogo size={56} />);
    const img = screen.getByAltText('Aerly');
    // MUI's Box maps width/height to CSS (an emotion class), not HTML
    // attributes, so assert the resolved style rather than getAttribute.
    expect(img).toHaveStyle({ width: '56px', height: '56px' });
  });
});
