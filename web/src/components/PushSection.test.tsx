import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { PushKind } from '../api/types';

const h = vi.hoisted(() => ({
  loadPushState: vi.fn(),
  enablePush: vi.fn(),
  disablePush: vi.fn(),
  setPushKind: vi.fn(),
  state: {
    pushSupported: true,
    pushPermission: 'default' as NotificationPermission | 'unsupported',
    pushIosHint: false,
    pushSubscribed: false,
    pushPrefs: null as Record<PushKind, boolean> | null,
    pushBusy: false,
    pushLastError: null as string | null,
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      ...h.state,
      loadPushState: h.loadPushState,
      enablePush: h.enablePush,
      disablePush: h.disablePush,
      setPushKind: h.setPushKind,
    }),
}));

import PushSection from './PushSection';

beforeEach(() => {
  vi.clearAllMocks();
  h.state = {
    pushSupported: true,
    pushPermission: 'default',
    pushIosHint: false,
    pushSubscribed: false,
    pushPrefs: null,
    pushBusy: false,
    pushLastError: null,
  };
  h.loadPushState.mockResolvedValue(undefined);
  h.enablePush.mockResolvedValue(undefined);
  h.disablePush.mockResolvedValue(undefined);
  h.setPushKind.mockResolvedValue(undefined);
});

describe('PushSection', () => {
  it('loads push state on mount', async () => {
    render(<PushSection />);
    await waitFor(() => expect(h.loadPushState).toHaveBeenCalled());
  });

  it('shows an unsupported message when push is unavailable', () => {
    h.state.pushSupported = false;
    render(<PushSection />);
    expect(screen.getByText(/doesn.t support push/i)).toBeInTheDocument();
  });

  it('shows the iOS install hint', () => {
    h.state.pushIosHint = true;
    render(<PushSection />);
    expect(screen.getByText(/add aerly to your home screen/i)).toBeInTheDocument();
  });

  it('enables push when the master switch is turned on', async () => {
    render(<PushSection />);
    await userEvent.click(screen.getByRole('checkbox', { name: /enable push on this device/i }));
    expect(h.enablePush).toHaveBeenCalled();
  });

  it('disables push when the master switch is turned off', async () => {
    h.state.pushSubscribed = true;
    h.state.pushPrefs = { alert: true, share: true };
    render(<PushSection />);
    await userEvent.click(screen.getByRole('checkbox', { name: /enable push on this device/i }));
    expect(h.disablePush).toHaveBeenCalled();
  });

  it('disables the master switch whilst busy', () => {
    h.state.pushBusy = true;
    render(<PushSection />);
    expect(screen.getByRole('checkbox', { name: /enable push on this device/i })).toBeDisabled();
  });

  it('warns when notifications are blocked (permission denied)', () => {
    h.state.pushPermission = 'denied';
    render(<PushSection />);
    expect(screen.getByText(/notifications are blocked/i)).toBeInTheDocument();
  });

  it('warns when the last enable attempt was denied', () => {
    h.state.pushLastError = 'denied';
    render(<PushSection />);
    expect(screen.getByText(/notifications are blocked/i)).toBeInTheDocument();
  });

  it('explains a server that has push disabled', () => {
    h.state.pushLastError = 'disabled';
    render(<PushSection />);
    expect(screen.getByText(/isn.t configured on this server/i)).toBeInTheDocument();
  });

  it('shows a generic error on failure', () => {
    h.state.pushLastError = 'error';
    render(<PushSection />);
    expect(screen.getByText(/couldn.t enable push/i)).toBeInTheDocument();
  });

  it('renders per-kind toggles when subscribed and toggles a kind', async () => {
    h.state.pushSubscribed = true;
    h.state.pushPrefs = { alert: true, share: false };
    render(<PushSection />);

    const alert = screen.getByRole('checkbox', { name: /flight alerts/i });
    const share = screen.getByRole('checkbox', { name: /trip shares/i });
    expect(alert).toBeChecked();
    expect(share).not.toBeChecked();

    await userEvent.click(share);
    expect(h.setPushKind).toHaveBeenCalledWith('share', true);

    // Toggling the flight-alerts switch off exercises its handler too.
    await userEvent.click(alert);
    expect(h.setPushKind).toHaveBeenCalledWith('alert', false);
  });

  it('hides per-kind toggles when not subscribed', () => {
    render(<PushSection />);
    expect(screen.queryByRole('checkbox', { name: /flight alerts/i })).not.toBeInTheDocument();
  });
});
