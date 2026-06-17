import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { AutoShare, Friendship, User } from '../api/types';

const h = vi.hoisted(() => ({
  setAutoShare: vi.fn(),
  removeAutoShare: vi.fn(),
  setError: vi.fn(),
  state: {
    me: { id: 1 } as { id: number },
    users: [] as User[],
    friendships: [] as Friendship[],
    autoShares: [] as AutoShare[],
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      me: h.state.me,
      users: h.state.users,
      friendships: h.state.friendships,
      autoShares: h.state.autoShares,
      setAutoShare: h.setAutoShare,
      removeAutoShare: h.removeAutoShare,
      setError: h.setError,
    }),
}));

import AutoShareSection from './AutoShareSection';

function user(id: number, name: string): User {
  return {
    id,
    username: name.toLowerCase(),
    name,
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    home_address: '',
  };
}

const wife = user(2, 'Wife');
const pa = user(3, 'Assistant');

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = { id: 1 };
  h.state.users = [user(1, 'Me'), wife, pa];
  h.state.friendships = [
    { friend_id: 2, status: 'accepted', requested_at: '2026-01-01T00:00:00Z' },
    { friend_id: 3, status: 'accepted', requested_at: '2026-01-01T00:00:00Z' },
  ] as Friendship[];
  h.state.autoShares = [];
});

describe('AutoShareSection', () => {
  it('shows an empty state when there are no defaults', () => {
    render(<AutoShareSection />);
    expect(screen.getByText(/No one yet/i)).toBeInTheDocument();
  });

  it('adds a friend at the chosen role', async () => {
    h.setAutoShare.mockResolvedValue(undefined);
    render(<AutoShareSection />);

    // Pick the friend.
    await userEvent.click(screen.getByRole('combobox', { name: 'Friend' }));
    await userEvent.click(await screen.findByRole('option', { name: 'Wife' }));
    // Add (defaults to viewer).
    await userEvent.click(screen.getByRole('button', { name: /add/i }));

    await waitFor(() => expect(h.setAutoShare).toHaveBeenCalledWith(2, 'viewer'));
  });

  it('lists existing defaults and removes one', async () => {
    h.state.autoShares = [{ user_id: 2, role: 'viewer' }];
    h.removeAutoShare.mockResolvedValue(undefined);
    render(<AutoShareSection />);

    const row = screen.getByText('Wife').closest('tr');
    expect(row).not.toBeNull();
    await userEvent.click(within(row as HTMLElement).getByRole('button', { name: /remove wife/i }));
    await waitFor(() => expect(h.removeAutoShare).toHaveBeenCalledWith(2));
  });

  it('excludes already-shared friends from the add picker', async () => {
    h.state.autoShares = [{ user_id: 2, role: 'editor' }];
    render(<AutoShareSection />);

    await userEvent.click(screen.getByRole('combobox', { name: 'Friend' }));
    // Assistant is still pickable; Wife (already shared) is not offered.
    expect(await screen.findByRole('option', { name: 'Assistant' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Wife' })).not.toBeInTheDocument();
  });

  it('changes the role of an existing default', async () => {
    h.state.autoShares = [{ user_id: 2, role: 'viewer' }];
    h.setAutoShare.mockResolvedValue(undefined);
    render(<AutoShareSection />);

    await userEvent.click(screen.getByRole('combobox', { name: 'Role for Wife' }));
    await userEvent.click(await screen.findByRole('option', { name: 'Passenger' }));
    await waitFor(() => expect(h.setAutoShare).toHaveBeenCalledWith(2, 'passenger'));
  });

  it('surfaces errors from add, role change, and remove via setError', async () => {
    h.state.autoShares = [{ user_id: 2, role: 'viewer' }];
    render(<AutoShareSection />);

    // Role change failure.
    h.setAutoShare.mockRejectedValueOnce(new Error('role boom'));
    await userEvent.click(screen.getByRole('combobox', { name: 'Role for Wife' }));
    await userEvent.click(await screen.findByRole('option', { name: 'Editor' }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('role boom'));

    // Remove failure.
    h.removeAutoShare.mockRejectedValueOnce(new Error('remove boom'));
    const row = screen.getByText('Wife').closest('tr');
    await userEvent.click(within(row as HTMLElement).getByRole('button', { name: /remove wife/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('remove boom'));

    // Add failure.
    h.setAutoShare.mockRejectedValueOnce(new Error('add boom'));
    await userEvent.click(screen.getByRole('combobox', { name: 'Friend' }));
    await userEvent.click(await screen.findByRole('option', { name: 'Assistant' }));
    await userEvent.click(screen.getByRole('button', { name: /add/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('add boom'));
  });

  it('renders a role chip and #id label when the shared user is not loaded', () => {
    // A default whose target isn't in the users cache (e.g. not yet fetched):
    // falls back to a static chip + "User #<id>" label rather than a Select.
    h.state.users = [user(1, 'Me')];
    h.state.autoShares = [{ user_id: 42, role: 'passenger' }];
    render(<AutoShareSection />);

    expect(screen.getByText('User #42')).toBeInTheDocument();
    expect(screen.getByText('Passenger')).toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: /role for/i })).not.toBeInTheDocument();
  });
});
