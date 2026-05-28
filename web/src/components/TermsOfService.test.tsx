import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import TermsOfService from './TermsOfService';

describe('TermsOfService', () => {
  it('renders the Terms of Service heading', () => {
    render(<TermsOfService />);
    expect(screen.getByRole('heading', { name: /terms of service/i })).toBeInTheDocument();
  });

  it('renders the No warranty section', () => {
    render(<TermsOfService />);
    expect(screen.getByRole('heading', { name: /no warranty/i })).toBeInTheDocument();
  });

  it('renders the Service availability section', () => {
    render(<TermsOfService />);
    expect(screen.getByRole('heading', { name: /service availability/i })).toBeInTheDocument();
  });

  it('includes a link to Privacy Policy', () => {
    render(<TermsOfService />);
    const link = screen.getByRole('link', { name: /privacy policy/i });
    expect(link).toHaveAttribute('href', '/privacy');
  });
});
