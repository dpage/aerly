import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => ({ state: { emailEnabled: false } }));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ capabilities: { email_ingest_enabled: h.state.emailEnabled } }),
}));

vi.mock('./AlertPrefsSection', () => ({ default: () => <div data-testid="sec-alerts" /> }));
vi.mock('./AutoShareSection', () => ({ default: () => <div data-testid="sec-sharing" /> }));
vi.mock('./HomeAddressSection', () => ({ default: () => <div data-testid="sec-home" /> }));
vi.mock('./PaperSizeSection', () => ({ default: () => <div data-testid="sec-itinerary" /> }));
vi.mock('./EmailsSection', () => ({ default: () => <div data-testid="sec-emails" /> }));
vi.mock('./PushSection', () => ({ default: () => <div data-testid="sec-push" /> }));

import PreferencesDialog from './PreferencesDialog';

beforeEach(() => {
  vi.clearAllMocks();
  h.state.emailEnabled = false;
});

describe('PreferencesDialog', () => {
  it('renders nothing when closed', () => {
    render(<PreferencesDialog open={false} onClose={() => {}} />);
    expect(screen.queryByText('Preferences')).not.toBeInTheDocument();
  });

  it('opens on the Alerts tab', () => {
    render(<PreferencesDialog open onClose={() => {}} />);
    expect(screen.getByRole('tab', { name: 'Alerts' })).toBeInTheDocument();
    expect(screen.getByTestId('sec-alerts')).toBeInTheDocument();
  });

  it('switches to the Sharing tab', async () => {
    render(<PreferencesDialog open onClose={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Sharing' }));
    expect(screen.getByTestId('sec-sharing')).toBeInTheDocument();
  });

  it('switches to the Push tab', async () => {
    render(<PreferencesDialog open onClose={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Push' }));
    expect(screen.getByTestId('sec-push')).toBeInTheDocument();
  });

  it('hides the Emails tab when ingest is disabled', () => {
    render(<PreferencesDialog open onClose={() => {}} />);
    expect(screen.queryByRole('tab', { name: 'Emails' })).not.toBeInTheDocument();
  });

  it('shows and opens the Emails tab when ingest is enabled', async () => {
    h.state.emailEnabled = true;
    render(<PreferencesDialog open onClose={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Emails' }));
    expect(screen.getByTestId('sec-emails')).toBeInTheDocument();
  });

  it('switches to the Home tab', async () => {
    render(<PreferencesDialog open onClose={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Home' }));
    expect(screen.getByTestId('sec-home')).toBeInTheDocument();
  });

  it('switches to the Itinerary tab', async () => {
    render(<PreferencesDialog open onClose={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Itinerary' }));
    expect(screen.getByTestId('sec-itinerary')).toBeInTheDocument();
  });

  it('resets to the Alerts tab when reopened', async () => {
    const { rerender } = render(<PreferencesDialog open onClose={() => {}} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Home' }));
    expect(screen.getByTestId('sec-home')).toBeInTheDocument();
    // Close, then reopen — the last-viewed tab must not be remembered.
    rerender(<PreferencesDialog open={false} onClose={() => {}} />);
    rerender(<PreferencesDialog open onClose={() => {}} />);
    expect(screen.getByTestId('sec-alerts')).toBeInTheDocument();
    expect(screen.queryByTestId('sec-home')).not.toBeInTheDocument();
  });

  it('calls onClose from the Close button', async () => {
    const onClose = vi.fn();
    render(<PreferencesDialog open onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(onClose).toHaveBeenCalled();
  });
});
