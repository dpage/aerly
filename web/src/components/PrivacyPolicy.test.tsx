import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import PrivacyPolicy from './PrivacyPolicy';

describe('PrivacyPolicy', () => {
  it('renders the Privacy Policy heading', () => {
    render(<PrivacyPolicy />);
    expect(screen.getByRole('heading', { name: /privacy policy/i })).toBeInTheDocument();
  });

  it('renders the What we collect section', () => {
    render(<PrivacyPolicy />);
    expect(screen.getByRole('heading', { name: /what we collect/i })).toBeInTheDocument();
  });

  it('renders the Who can see your data section', () => {
    render(<PrivacyPolicy />);
    expect(screen.getByRole('heading', { name: /who can see your data/i })).toBeInTheDocument();
  });

  it('includes a link to Terms of Service', () => {
    render(<PrivacyPolicy />);
    const link = screen.getByRole('link', { name: /terms of service/i });
    expect(link).toHaveAttribute('href', '/terms');
  });
});
