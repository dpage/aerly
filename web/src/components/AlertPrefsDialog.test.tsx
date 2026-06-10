import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { AlertPrefs } from '../api/types';

const h = vi.hoisted(() => ({
  loadAlertPrefs: vi.fn(),
  updateAlertPrefs: vi.fn(),
  setError: vi.fn(),
  state: { alertPrefs: null as AlertPrefs | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      alertPrefs: h.state.alertPrefs,
      loadAlertPrefs: h.loadAlertPrefs,
      updateAlertPrefs: h.updateAlertPrefs,
      setError: h.setError,
    }),
}));

import AlertPrefsDialog from './AlertPrefsDialog';

beforeEach(() => {
  vi.clearAllMocks();
  h.state.alertPrefs = { in_app: true, email: false, min_delay_min: 15 };
  h.loadAlertPrefs.mockResolvedValue(undefined);
  h.updateAlertPrefs.mockResolvedValue(undefined);
});

describe('AlertPrefsDialog', () => {
  it('loads prefs on open and reflects them in the controls', async () => {
    render(<AlertPrefsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    expect(screen.getByRole('checkbox', { name: /in-app/i })).toBeChecked();
    expect(screen.getByRole('checkbox', { name: /email/i })).not.toBeChecked();
    expect((screen.getByLabelText(/minimum delay in minutes/i) as HTMLInputElement).value).toBe(
      '15',
    );
  });

  it('does not load while closed', () => {
    render(<AlertPrefsDialog open={false} onClose={() => {}} />);
    expect(h.loadAlertPrefs).not.toHaveBeenCalled();
  });

  it('saves the edited prefs', async () => {
    const onClose = vi.fn();
    render(<AlertPrefsDialog open onClose={onClose} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());

    await userEvent.click(screen.getByRole('checkbox', { name: /email/i }));
    const field = screen.getByLabelText(/minimum delay in minutes/i);
    await userEvent.clear(field);
    await userEvent.type(field, '30');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() =>
      expect(h.updateAlertPrefs).toHaveBeenCalledWith({
        in_app: true,
        email: true,
        min_delay_min: 30,
      }),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it('clamps a blank/invalid threshold to 0', async () => {
    render(<AlertPrefsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());

    const field = screen.getByLabelText(/minimum delay in minutes/i);
    await userEvent.clear(field);
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() =>
      expect(h.updateAlertPrefs).toHaveBeenCalledWith(
        expect.objectContaining({ min_delay_min: 0 }),
      ),
    );
  });

  it('surfaces save errors via setError', async () => {
    h.updateAlertPrefs.mockRejectedValue(new Error('save boom'));
    render(<AlertPrefsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
  });

  it('toggles the in-app channel off and saves it', async () => {
    render(<AlertPrefsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    await userEvent.click(screen.getByRole('checkbox', { name: /in-app/i }));
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updateAlertPrefs).toHaveBeenCalledWith(expect.objectContaining({ in_app: false })),
    );
  });

  it('clamps a negative threshold to 0', async () => {
    render(<AlertPrefsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    const field = screen.getByLabelText(/minimum delay in minutes/i);
    await userEvent.clear(field);
    await userEvent.type(field, '-5');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updateAlertPrefs).toHaveBeenCalledWith(
        expect.objectContaining({ min_delay_min: 0 }),
      ),
    );
  });

  it('stringifies a non-Error save failure', async () => {
    h.updateAlertPrefs.mockRejectedValue('plain boom');
    render(<AlertPrefsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('plain boom'));
  });

  it('closes via Cancel without saving', async () => {
    const onClose = vi.fn();
    render(<AlertPrefsDialog open onClose={onClose} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    await userEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onClose).toHaveBeenCalled();
    expect(h.updateAlertPrefs).not.toHaveBeenCalled();
  });

  it('falls back to defaults when prefs are absent', async () => {
    h.state.alertPrefs = null;
    render(<AlertPrefsDialog open onClose={() => {}} />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    // Defaults: in-app on, email off, threshold 15.
    expect(screen.getByRole('checkbox', { name: /in-app/i })).toBeChecked();
    expect((screen.getByLabelText(/minimum delay in minutes/i) as HTMLInputElement).value).toBe(
      '15',
    );
  });
});
