import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Friendship, TripMember, User } from '../api/types';

const h = vi.hoisted(() => ({
  addTripMember: vi.fn(),
  removeTripMember: vi.fn(),
  addTripPassenger: vi.fn(),
  removeTripPassenger: vi.fn(),
  setError: vi.fn(),
  openHelp: vi.fn(),
  state: {
    users: [] as User[],
    friendships: [] as Friendship[],
    me: null as User | null,
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      users: h.state.users,
      friendships: h.state.friendships,
      me: h.state.me,
      addTripMember: h.addTripMember,
      removeTripMember: h.removeTripMember,
      addTripPassenger: h.addTripPassenger,
      removeTripPassenger: h.removeTripPassenger,
      setError: h.setError,
      openHelp: h.openHelp,
    }),
}));

import TripMembersDialog from './TripMembersDialog';

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'octocat',
    name: '',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    ...over,
  };
}

function accepted(friendId: number): Friendship {
  return { friend_id: friendId, status: 'accepted', requested_at: '2026-01-01T00:00:00Z' };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = user({ id: 100, username: 'me', name: 'Me' });
  h.state.users = [
    user({ id: 100, username: 'me', name: 'Me' }),
    user({ id: 2, username: 'bob', name: 'Bob' }),
    user({ id: 3, username: 'carol', name: 'Carol' }),
  ];
  h.state.friendships = [accepted(2), accepted(3)];
});

function render_(
  members: TripMember[],
  role: 'owner' | 'editor' | 'viewer' = 'owner',
  passengerIds: number[] = [],
) {
  return render(
    <TripMembersDialog
      open
      tripId={7}
      myRole={role}
      members={members}
      passengerIds={passengerIds}
      onClose={() => {}}
    />,
  );
}

describe('TripMembersDialog', () => {
  it('opens the sharing help via "How sharing works"', async () => {
    render_([{ user_id: 100, role: 'owner' }]);
    await userEvent.click(screen.getByRole('button', { name: /how sharing works/i }));
    expect(h.openHelp).toHaveBeenCalledWith('sharing');
  });

  it('lists current members with their roles', () => {
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'editor' },
    ]);
    expect(screen.getByText('Me')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
    // Owner role is shown as a static chip, not editable.
    expect(screen.getByText('owner')).toBeInTheDocument();
  });

  it('adds a friend as a viewer', async () => {
    h.addTripMember.mockResolvedValue(undefined);
    render_([{ user_id: 100, role: 'owner' }]);

    await userEvent.click(screen.getByLabelText('Friend'));
    await userEvent.click(await screen.findByRole('option', { name: 'Carol' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));

    await waitFor(() =>
      expect(h.addTripMember).toHaveBeenCalledWith(7, { user_id: 3, role: 'viewer' }),
    );
  });

  it('adds a friend as an editor when the role is changed', async () => {
    h.addTripMember.mockResolvedValue(undefined);
    render_([{ user_id: 100, role: 'owner' }]);

    await userEvent.click(screen.getByLabelText('Friend'));
    await userEvent.click(await screen.findByRole('option', { name: 'Bob' }));
    await userEvent.click(screen.getByLabelText('Role'));
    await userEvent.click(await screen.findByRole('option', { name: 'Editor' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));

    await waitFor(() =>
      expect(h.addTripMember).toHaveBeenCalledWith(7, { user_id: 2, role: 'editor' }),
    );
  });

  it('adds a friend as a trip passenger when the Passenger role is chosen (#20)', async () => {
    h.addTripPassenger.mockResolvedValue(undefined);
    render_([{ user_id: 100, role: 'owner' }]);

    await userEvent.click(screen.getByLabelText('Friend'));
    await userEvent.click(await screen.findByRole('option', { name: 'Carol' }));
    await userEvent.click(screen.getByLabelText('Role'));
    await userEvent.click(await screen.findByRole('option', { name: 'Passenger' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));

    await waitFor(() => expect(h.addTripPassenger).toHaveBeenCalledWith(7, 3));
    expect(h.addTripMember).not.toHaveBeenCalled();
  });

  it("shows a trip passenger as 'passenger' and removes via the passenger endpoint (#20)", async () => {
    h.removeTripPassenger.mockResolvedValue(undefined);
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    // Bob is a viewer member who is also a trip passenger.
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'viewer' },
    ], 'owner', [2]);

    // Displayed as a passenger, not a plain viewer.
    expect(screen.getByText('passenger')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /remove bob/i }));
    await waitFor(() => expect(h.removeTripPassenger).toHaveBeenCalledWith(7, 2));
    expect(h.removeTripMember).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('excludes existing members from the friend picker', async () => {
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'editor' },
    ]);
    await userEvent.click(screen.getByLabelText('Friend'));
    expect(await screen.findByRole('option', { name: 'Carol' })).toBeInTheDocument();
    // Bob is already a member, so he must not be selectable.
    expect(screen.queryByRole('option', { name: 'Bob' })).not.toBeInTheDocument();
  });

  it('changes a member role via the per-row select', async () => {
    h.addTripMember.mockResolvedValue(undefined);
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'viewer' },
    ]);

    await userEvent.click(screen.getByLabelText('Role for Bob'));
    await userEvent.click(await screen.findByRole('option', { name: 'Editor' }));

    await waitFor(() =>
      expect(h.addTripMember).toHaveBeenCalledWith(7, { user_id: 2, role: 'editor' }),
    );
  });

  it('removes a member after confirm', async () => {
    h.removeTripMember.mockResolvedValue(undefined);
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'editor' },
    ]);

    await userEvent.click(screen.getByRole('button', { name: /remove bob/i }));
    await waitFor(() => expect(h.removeTripMember).toHaveBeenCalledWith(7, 2));
    confirmSpy.mockRestore();
  });

  it('does not remove when confirm is cancelled', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'editor' },
    ]);
    await userEvent.click(screen.getByRole('button', { name: /remove bob/i }));
    expect(h.removeTripMember).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('surfaces add errors via setError', async () => {
    h.addTripMember.mockRejectedValue(new Error('add boom'));
    render_([{ user_id: 100, role: 'owner' }]);
    await userEvent.click(screen.getByLabelText('Friend'));
    await userEvent.click(await screen.findByRole('option', { name: 'Bob' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('add boom'));
  });

  it('stringifies non-Error remove rejections', async () => {
    h.removeTripMember.mockRejectedValue('remove kaboom');
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'editor' },
    ]);
    await userEvent.click(screen.getByRole('button', { name: /remove bob/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('remove kaboom'));
    confirmSpy.mockRestore();
  });

  it('hides management controls for viewers', () => {
    render_(
      [
        { user_id: 100, role: 'owner' },
        { user_id: 2, role: 'viewer' },
      ],
      'viewer',
    );
    expect(screen.queryByLabelText('Friend')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /remove bob/i })).not.toBeInTheDocument();
    // Roles render as static chips for non-managers.
    const rows = screen.getAllByRole('row');
    expect(within(rows[2]).getByText('viewer')).toBeInTheDocument();
  });

  it('falls back to User #id when the user record is unknown', () => {
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 999, role: 'editor' },
    ]);
    expect(screen.getByText('User #999')).toBeInTheDocument();
  });
});
