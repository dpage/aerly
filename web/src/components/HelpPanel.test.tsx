import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => ({
  closeHelp: vi.fn(),
  state: { helpOpen: false, helpPage: null as string | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ helpOpen: h.state.helpOpen, helpPage: h.state.helpPage, closeHelp: h.closeHelp }),
}));

import HelpPanel from './HelpPanel';

beforeEach(() => {
  vi.clearAllMocks();
  h.state.helpOpen = false;
  h.state.helpPage = null;
});

describe('HelpPanel', () => {
  it('renders nothing while closed', () => {
    render(<HelpPanel />);
    expect(screen.queryByText('Help & guide')).not.toBeInTheDocument();
  });

  it('opens on the overview by default', () => {
    h.state.helpOpen = true;
    render(<HelpPanel />);
    expect(screen.getByText('Help & guide')).toBeInTheDocument();
    // Overview content present.
    expect(screen.getByText(/Getting started/i)).toBeInTheDocument();
  });

  it('opens straight to a seeded topic (sharing)', () => {
    h.state.helpOpen = true;
    h.state.helpPage = 'sharing';
    render(<HelpPanel />);
    // Breadcrumb shows the topic; sharing content is present.
    expect(screen.getByText('Owner')).toBeInTheDocument();
    expect(screen.getByText('Editor')).toBeInTheDocument();
  });

  it('navigates between topics and back to the overview', async () => {
    const user = userEvent.setup();
    h.state.helpOpen = true;
    render(<HelpPanel />);
    // Click the Sharing nav item.
    await user.click(screen.getByRole('button', { name: /friends & sharing/i }));
    expect(screen.getByText('Passengers')).toBeInTheDocument();
    // Back returns to the overview.
    await user.click(screen.getByRole('button', { name: /back to help overview/i }));
    expect(screen.getByText(/Getting started/i)).toBeInTheDocument();
  });

  it('returns to the overview via the breadcrumb link', async () => {
    const user = userEvent.setup();
    h.state.helpOpen = true;
    h.state.helpPage = 'plans';
    render(<HelpPanel />);
    await user.click(screen.getByRole('button', { name: 'Help' }));
    expect(screen.getByText(/Getting started/i)).toBeInTheDocument();
  });

  it('closes via the close button', async () => {
    const user = userEvent.setup();
    h.state.helpOpen = true;
    render(<HelpPanel />);
    await user.click(screen.getByRole('button', { name: /close help/i }));
    expect(h.closeHelp).toHaveBeenCalled();
  });
});
