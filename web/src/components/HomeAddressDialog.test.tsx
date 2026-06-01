import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';

const h = vi.hoisted(() => ({
  setHomeAddress: vi.fn(),
  setError: vi.fn(),
  state: { me: null as User | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      me: h.state.me,
      setHomeAddress: h.setHomeAddress,
      setError: h.setError,
    }),
}));

import HomeAddressDialog from './HomeAddressDialog';

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'octocat',
    name: 'Octo Cat',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    home_address: '',
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = user();
});

describe('HomeAddressDialog', () => {
  it('renders nothing visible when closed', () => {
    render(<HomeAddressDialog open={false} onClose={() => {}} />);
    expect(screen.queryByText('Home address')).not.toBeInTheDocument();
  });

  it('prefills the existing home address on open', () => {
    h.state.me = user({ home_address: '12 Acacia Avenue' });
    render(<HomeAddressDialog open onClose={() => {}} />);
    expect(screen.getByRole('textbox', { name: 'Home address' })).toHaveValue('12 Acacia Avenue');
  });

  it('falls back to empty when me / home_address is absent', () => {
    h.state.me = null;
    render(<HomeAddressDialog open onClose={() => {}} />);
    expect(screen.getByRole('textbox', { name: 'Home address' })).toHaveValue('');
  });

  it('saves the trimmed value and closes', async () => {
    h.setHomeAddress.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<HomeAddressDialog open onClose={onClose} />);
    const field = screen.getByRole('textbox', { name: 'Home address' });
    await userEvent.type(field, '  5 New Road  ');
    await userEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(h.setHomeAddress).toHaveBeenCalledWith('5 New Road'));
    expect(onClose).toHaveBeenCalled();
  });

  it('surfaces save errors via setError and stays open', async () => {
    h.setHomeAddress.mockRejectedValue(new Error('save boom'));
    const onClose = vi.fn();
    render(<HomeAddressDialog open onClose={onClose} />);
    await userEvent.type(screen.getByRole('textbox', { name: 'Home address' }), 'X');
    await userEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('coerces a non-Error rejection to a string', async () => {
    h.setHomeAddress.mockRejectedValue('nope');
    render(<HomeAddressDialog open onClose={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('nope'));
  });

  it('cancels without saving', async () => {
    const onClose = vi.fn();
    render(<HomeAddressDialog open onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onClose).toHaveBeenCalled();
    expect(h.setHomeAddress).not.toHaveBeenCalled();
  });
});
