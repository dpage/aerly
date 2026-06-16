import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import { HELP_PAGES, contextToPageId } from './HelpContent';

describe('HelpContent', () => {
  it('exposes the topics in nav order', () => {
    expect(HELP_PAGES.map((p) => p.id)).toEqual([
      'overview',
      'trips',
      'plans',
      'tracker',
      'sharing',
      'alerts',
      'account',
    ]);
    for (const p of HELP_PAGES) {
      expect(p.label).toBeTruthy();
      expect(p.Icon).toBeTruthy();
    }
  });

  it('renders every page body (exercises the content primitives)', () => {
    for (const p of HELP_PAGES) {
      const { unmount } = render(<div>{p.body}</div>);
      unmount();
    }
  });

  it('the sharing page explains the three roles, per-plan privacy and passengers', () => {
    const sharing = HELP_PAGES.find((p) => p.id === 'sharing')!;
    render(<div>{sharing.body}</div>);
    expect(screen.getByText('Owner')).toBeInTheDocument();
    expect(screen.getByText('Editor')).toBeInTheDocument();
    expect(screen.getByText('Viewer')).toBeInTheDocument();
    expect(screen.getByText(/Who can see this plan/i)).toBeInTheDocument();
    expect(screen.getByText('Passengers')).toBeInTheDocument();
    // The HelpTip callout renders.
    expect(screen.getByText('Tip:')).toBeInTheDocument();
  });

  it('the alerts page covers the inbox, preferences and reminders', () => {
    const alerts = HELP_PAGES.find((p) => p.id === 'alerts')!;
    render(<div>{alerts.body}</div>);
    expect(screen.getByText('Alerts inbox')).toBeInTheDocument();
    expect(screen.getByText('Alert preferences')).toBeInTheDocument();
    expect(screen.getByText('Notify me of changes')).toBeInTheDocument();
    expect(screen.getByText('Reminders')).toBeInTheDocument();
  });

  it('the account page covers statistics, home address and calendar feeds', () => {
    const account = HELP_PAGES.find((p) => p.id === 'account')!;
    render(<div>{account.body}</div>);
    expect(screen.getByText('Statistics')).toBeInTheDocument();
    expect(screen.getByText('Home address')).toBeInTheDocument();
    expect(screen.getByText('Subscribe to calendar')).toBeInTheDocument();
  });

  it('the sharing page also explains adding friends', () => {
    const sharing = HELP_PAGES.find((p) => p.id === 'sharing')!;
    render(<div>{sharing.body}</div>);
    expect(screen.getByText('Add a friend')).toBeInTheDocument();
    expect(screen.getByText('Unfriend')).toBeInTheDocument();
  });

  it('maps context hints to topic pages, defaulting to the overview', () => {
    expect(contextToPageId('sharing')).toBe('sharing');
    expect(contextToPageId('privacy')).toBe('sharing');
    expect(contextToPageId('trip')).toBe('plans');
    expect(contextToPageId('plans')).toBe('plans');
    expect(contextToPageId('tracker')).toBe('tracker');
    expect(contextToPageId('map')).toBe('tracker');
    expect(contextToPageId('trips')).toBe('trips');
    expect(contextToPageId('overview')).toBe('overview');
    expect(contextToPageId('something-else')).toBe('overview');
    expect(contextToPageId(null)).toBe('overview');
    expect(contextToPageId(undefined)).toBe('overview');
  });
});
