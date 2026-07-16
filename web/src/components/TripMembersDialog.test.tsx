import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Friendship, TripMember, User } from '../api/types';

const h = vi.hoisted(() => ({
  addTripMember: vi.fn(),
  removeTripMember: vi.fn(),
  addTripPassenger: vi.fn(),
  removeTripPassenger: vi.fn(),
  setTripShareAllFriends: vi.fn(),
  shareTripByEmail: vi.fn(),
  notifyTripShares: vi.fn(),
  setError: vi.fn(),
  openHelp: vi.fn(),
  candidates: [] as { user: User; pending: boolean }[],
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
      setTripShareAllFriends: h.setTripShareAllFriends,
      shareTripByEmail: h.shareTripByEmail,
      notifyTripShares: h.notifyTripShares,
      setError: h.setError,
      openHelp: h.openHelp,
    }),
}));

vi.mock('../state/friendUsers', () => ({
  useFriendCandidates: () => h.candidates,
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
  // The dialog sources its picker from useFriendCandidates(); mirror the two
  // accepted friends as candidates by default.
  h.candidates = [
    { user: user({ id: 2, username: 'bob', name: 'Bob' }), pending: false },
    { user: user({ id: 3, username: 'carol', name: 'Carol' }), pending: false },
  ];
});

function render_(
  members: TripMember[],
  role: 'owner' | 'editor' | 'viewer' = 'owner',
  passengerIds: number[] = [],
  opts: {
    shareAllFriendsRole?: 'viewer' | 'editor';
    onClose?: () => void;
  } = {},
) {
  return render(
    <TripMembersDialog
      open
      tripId={7}
      myRole={role}
      members={members}
      passengerIds={passengerIds}
      shareAllFriendsRole={opts.shareAllFriendsRole}
      onClose={opts.onClose ?? (() => {})}
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
    render_(
      [
        { user_id: 100, role: 'owner' },
        { user_id: 2, role: 'viewer' },
      ],
      'owner',
      [2],
    );

    // The role picker reflects passenger status, not the underlying viewer role.
    expect(screen.getByLabelText('Role for Bob')).toHaveTextContent('Passenger');

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

  it('converts an existing member into a trip passenger via the per-row select (#20)', async () => {
    h.addTripPassenger.mockResolvedValue(undefined);
    // Bob was auto-shared as a viewer; retag him as a fellow traveller.
    render_([
      { user_id: 100, role: 'owner' },
      { user_id: 2, role: 'viewer' },
    ]);

    await userEvent.click(screen.getByLabelText('Role for Bob'));
    await userEvent.click(await screen.findByRole('option', { name: 'Passenger' }));

    await waitFor(() => expect(h.addTripPassenger).toHaveBeenCalledWith(7, 2));
    expect(h.addTripMember).not.toHaveBeenCalled();
    expect(h.removeTripPassenger).not.toHaveBeenCalled();
  });

  it('converts a trip passenger back to an editor, dropping passenger status first (#20)', async () => {
    h.removeTripPassenger.mockResolvedValue(undefined);
    h.addTripMember.mockResolvedValue(undefined);
    // Bob is a viewer member who is also a trip passenger.
    render_(
      [
        { user_id: 100, role: 'owner' },
        { user_id: 2, role: 'viewer' },
      ],
      'owner',
      [2],
    );

    await userEvent.click(screen.getByLabelText('Role for Bob'));
    await userEvent.click(await screen.findByRole('option', { name: 'Editor' }));

    await waitFor(() => expect(h.removeTripPassenger).toHaveBeenCalledWith(7, 2));
    expect(h.addTripMember).toHaveBeenCalledWith(7, { user_id: 2, role: 'editor' });
  });

  it("shows a viewer their own 'passenger' status as a read-only chip they can leave (#20)", async () => {
    // A non-managing viewer (canManage=false) can't retag anyone, so every row
    // renders as a static chip — a passenger's shows 'passenger', not 'viewer'.
    // They may still remove themselves: a passenger may always leave.
    render_([{ user_id: 100, role: 'viewer' }], 'viewer', [100]);

    // The chip reflects passenger status, and there is no role picker.
    expect(screen.getByText('passenger')).toBeInTheDocument();
    expect(screen.queryByLabelText('Role for Me')).not.toBeInTheDocument();
    // A passenger may remove themselves even without manage rights.
    expect(screen.getByRole('button', { name: /remove me/i })).toBeInTheDocument();
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

  it('sets the all-friends role to viewer, and back to off', async () => {
    h.setTripShareAllFriends.mockResolvedValue(undefined);
    render_([{ user_id: 100, role: 'owner' }]);

    await userEvent.click(screen.getByLabelText('All friends'));
    await userEvent.click(await screen.findByRole('option', { name: 'Viewer' }));
    await waitFor(() => expect(h.setTripShareAllFriends).toHaveBeenCalledWith(7, 'viewer'));

    await userEvent.click(screen.getByLabelText('All friends'));
    await userEvent.click(await screen.findByRole('option', { name: 'Off' }));
    await waitFor(() => expect(h.setTripShareAllFriends).toHaveBeenCalledWith(7, null));
  });

  it('reflects the current all-friends role as selected', () => {
    render_([{ user_id: 100, role: 'owner' }], 'owner', [], { shareAllFriendsRole: 'viewer' });
    // The MUI Select renders its current value as the combobox's text.
    expect(screen.getByLabelText('All friends')).toHaveTextContent('Viewer');
  });

  it('notifies the friends just added when closing with the box checked', async () => {
    h.addTripMember.mockResolvedValue(undefined);
    h.notifyTripShares.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render_([{ user_id: 100, role: 'owner' }], 'owner', [], { onClose });

    await userEvent.click(screen.getByLabelText('Friend'));
    await userEvent.click(await screen.findByRole('option', { name: 'Carol' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    await waitFor(() => expect(h.addTripMember).toHaveBeenCalled());

    // The notify checkbox now appears, checked by default.
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    await waitFor(() =>
      expect(h.notifyTripShares).toHaveBeenCalledWith(7, { user_ids: [3], emails: [] }),
    );
    expect(onClose).toHaveBeenCalled();
  });

  it('invites by email and notifies that address on close', async () => {
    h.shareTripByEmail.mockResolvedValue(undefined);
    h.notifyTripShares.mockResolvedValue(undefined);
    render_([{ user_id: 100, role: 'owner' }]);

    await userEvent.type(screen.getByLabelText('Email'), 'x@y.com');
    await userEvent.click(screen.getByRole('button', { name: /^invite$/i }));
    await waitFor(() =>
      expect(h.shareTripByEmail).toHaveBeenCalledWith(7, { email: 'x@y.com', role: 'viewer' }),
    );

    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    await waitFor(() =>
      expect(h.notifyTripShares).toHaveBeenCalledWith(7, {
        user_ids: [],
        emails: ['x@y.com'],
      }),
    );
  });

  it('labels a pending friend in the picker with "(invited)"', async () => {
    h.candidates = [
      { user: user({ id: 2, username: 'bob', name: 'Bob' }), pending: false },
      { user: user({ id: 4, username: 'dora', name: 'Dora' }), pending: true },
    ];
    render_([{ user_id: 100, role: 'owner' }]);
    await userEvent.click(screen.getByLabelText('Friend'));
    expect(await screen.findByRole('option', { name: /Dora \(invited\)/ })).toBeInTheDocument();
  });

  it('invites by email with the editor role when selected', async () => {
    h.shareTripByEmail.mockResolvedValue(undefined);
    render_([{ user_id: 100, role: 'owner' }]);

    await userEvent.click(screen.getByLabelText('Email role'));
    await userEvent.click(await screen.findByRole('option', { name: 'Editor' }));
    await userEvent.type(screen.getByLabelText('Email'), 'z@y.com');
    await userEvent.click(screen.getByRole('button', { name: /^invite$/i }));

    await waitFor(() =>
      expect(h.shareTripByEmail).toHaveBeenCalledWith(7, { email: 'z@y.com', role: 'editor' }),
    );
  });

  it('does not notify when the notify box is unchecked on close', async () => {
    h.addTripMember.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render_([{ user_id: 100, role: 'owner' }], 'owner', [], { onClose });

    await userEvent.click(screen.getByLabelText('Friend'));
    await userEvent.click(await screen.findByRole('option', { name: 'Carol' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    await waitFor(() => expect(h.addTripMember).toHaveBeenCalled());

    // Uncheck the default-checked notify box, then close.
    await userEvent.click(screen.getByRole('checkbox', { name: /notify the people/i }));
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(h.notifyTripShares).not.toHaveBeenCalled();
  });
});
