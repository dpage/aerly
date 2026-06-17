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

import AlertPrefsSection from './AlertPrefsSection';

beforeEach(() => {
  vi.clearAllMocks();
  h.state.alertPrefs = { in_app: true, email: false, min_delay_min: 15 };
  h.loadAlertPrefs.mockResolvedValue(undefined);
  h.updateAlertPrefs.mockResolvedValue(undefined);
});

describe('AlertPrefsSection', () => {
  it('loads prefs on mount and reflects them', async () => {
    render(<AlertPrefsSection />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    expect(screen.getByRole('checkbox', { name: /in-app/i })).toBeChecked();
    expect(screen.getByRole('checkbox', { name: /email/i })).not.toBeChecked();
    expect((screen.getByLabelText(/minimum delay in minutes/i) as HTMLInputElement).value).toBe('15');
  });

  it('has no Save button (auto-save)', async () => {
    render(<AlertPrefsSection />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: /^save$/i })).not.toBeInTheDocument();
  });

  it('persists immediately when a channel is toggled', async () => {
    render(<AlertPrefsSection />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    await userEvent.click(screen.getByRole('checkbox', { name: /email/i }));
    await waitFor(() =>
      expect(h.updateAlertPrefs).toHaveBeenCalledWith({ in_app: true, email: true, min_delay_min: 15 }),
    );
  });

  it('persists immediately when the in-app channel is toggled off', async () => {
    render(<AlertPrefsSection />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    await userEvent.click(screen.getByRole('checkbox', { name: /in-app/i }));
    await waitFor(() =>
      expect(h.updateAlertPrefs).toHaveBeenCalledWith({ in_app: false, email: false, min_delay_min: 15 }),
    );
  });

  it('persists the threshold on blur, clamping invalid input to 0', async () => {
    render(<AlertPrefsSection />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    const field = screen.getByLabelText(/minimum delay in minutes/i);
    await userEvent.clear(field);
    await userEvent.tab(); // blur with empty value
    await waitFor(() =>
      expect(h.updateAlertPrefs).toHaveBeenCalledWith(expect.objectContaining({ min_delay_min: 0 })),
    );
  });

  it('surfaces a save error and reloads the canonical prefs', async () => {
    h.updateAlertPrefs.mockRejectedValue(new Error('save boom'));
    render(<AlertPrefsSection />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    h.loadAlertPrefs.mockClear();
    await userEvent.click(screen.getByRole('checkbox', { name: /email/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
    expect(h.loadAlertPrefs).toHaveBeenCalled(); // reload-on-failure
  });

  it('falls back to defaults when prefs are absent', async () => {
    h.state.alertPrefs = null;
    render(<AlertPrefsSection />);
    await waitFor(() => expect(h.loadAlertPrefs).toHaveBeenCalled());
    expect(screen.getByRole('checkbox', { name: /in-app/i })).toBeChecked();
    expect((screen.getByLabelText(/minimum delay in minutes/i) as HTMLInputElement).value).toBe('15');
  });
});
