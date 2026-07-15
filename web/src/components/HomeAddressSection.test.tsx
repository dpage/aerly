import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';

const h = vi.hoisted(() => ({
  setHomeAddress: vi.fn(),
  setHomeCoords: vi.fn(),
  setError: vi.fn(),
  state: { me: null as User | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      me: h.state.me,
      setHomeAddress: h.setHomeAddress,
      setHomeCoords: h.setHomeCoords,
      setError: h.setError,
    }),
}));

import HomeAddressSection from './HomeAddressSection';

function user(over: Partial<User> = {}): User {
  return {
    id: 1, username: 'octocat', name: 'Octo Cat', avatar_url: '',
    is_superuser: false, is_active: true, has_logged_in: true, home_address: '', ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = user();
  h.setHomeAddress.mockResolvedValue(undefined);
  h.setHomeCoords.mockResolvedValue(undefined);
});

describe('HomeAddressSection', () => {
  it('prefills the existing home address', () => {
    h.state.me = user({ home_address: '12 Acacia Avenue' });
    render(<HomeAddressSection />);
    expect(screen.getByRole('textbox', { name: 'Home address' })).toHaveValue('12 Acacia Avenue');
  });

  it('has no Save or Cancel button (auto-save)', () => {
    render(<HomeAddressSection />);
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /cancel/i })).not.toBeInTheDocument();
  });

  it('saves the trimmed value on blur', async () => {
    render(<HomeAddressSection />);
    const field = screen.getByRole('textbox', { name: 'Home address' });
    await userEvent.type(field, '  5 New Road  ');
    await userEvent.tab();
    await waitFor(() => expect(h.setHomeAddress).toHaveBeenCalledWith('5 New Road'));
  });

  it('does not save on blur when the value is unchanged', async () => {
    h.state.me = user({ home_address: '5 New Road' });
    render(<HomeAddressSection />);
    const field = screen.getByRole('textbox', { name: 'Home address' });
    await userEvent.click(field);
    await userEvent.tab();
    expect(h.setHomeAddress).not.toHaveBeenCalled();
  });

  it('surfaces a save error and restores the canonical value', async () => {
    h.state.me = user({ home_address: 'Old Place' });
    h.setHomeAddress.mockRejectedValue(new Error('save boom'));
    render(<HomeAddressSection />);
    const field = screen.getByRole('textbox', { name: 'Home address' });
    await userEvent.clear(field);
    await userEvent.type(field, 'New Place');
    await userEvent.tab();
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
    expect(field).toHaveValue('Old Place');
  });

  it('shows "Not pinned" when no exact location is set', () => {
    render(<HomeAddressSection />);
    expect(screen.getByText(/not pinned/i)).toBeInTheDocument();
  });

  it('pins coordinates parsed from a "lat, lon" pair', async () => {
    render(<HomeAddressSection />);
    const field = screen.getByRole('textbox', { name: 'Map link or coordinates' });
    await userEvent.type(field, '51.50735, -0.12776');
    await userEvent.click(screen.getByRole('button', { name: 'Pin' }));
    await waitFor(() =>
      expect(h.setHomeCoords).toHaveBeenCalledWith({ lat: 51.50735, lon: -0.12776 }),
    );
    expect(h.setError).not.toHaveBeenCalled();
  });

  it('rejects unparseable pin input with an error and no store call', async () => {
    render(<HomeAddressSection />);
    const field = screen.getByRole('textbox', { name: 'Map link or coordinates' });
    await userEvent.type(field, 'somewhere near the shops');
    await userEvent.click(screen.getByRole('button', { name: 'Pin' }));
    await waitFor(() => expect(h.setError).toHaveBeenCalled());
    expect(h.setHomeCoords).not.toHaveBeenCalled();
  });

  it('shows the pinned coordinates and clears them', async () => {
    h.state.me = user({ home_lat: 51.5, home_lon: -0.12 });
    render(<HomeAddressSection />);
    expect(screen.getByText(/pinned at 51\.50000, -0\.12000/i)).toBeInTheDocument();
    // With a pin set the apply button relabels to "Update".
    expect(screen.getByRole('button', { name: 'Update' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Clear' }));
    await waitFor(() => expect(h.setHomeCoords).toHaveBeenCalledWith(null));
  });

  it('surfaces an error when clearing the pin fails', async () => {
    h.state.me = user({ home_lat: 51.5, home_lon: -0.12 });
    h.setHomeCoords.mockRejectedValue(new Error('clear boom'));
    render(<HomeAddressSection />);
    await userEvent.click(screen.getByRole('button', { name: 'Clear' }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('clear boom'));
  });

  it('surfaces an error when pinning fails', async () => {
    h.setHomeCoords.mockRejectedValue(new Error('pin boom'));
    render(<HomeAddressSection />);
    await userEvent.type(
      screen.getByRole('textbox', { name: 'Map link or coordinates' }),
      '51.5, -0.12',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Pin' }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('pin boom'));
  });
});
