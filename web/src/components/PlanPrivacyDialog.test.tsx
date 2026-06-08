import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Friendship, Plan, TripMember, User } from '../api/types';

const h = vi.hoisted(() => ({
  setPlanVisibility: vi.fn(),
  addPlanPassenger: vi.fn(),
  removePlanPassenger: vi.fn(),
  setPlanShareAllFriends: vi.fn(),
  sharePlanByEmail: vi.fn(),
  notifyPlanShares: vi.fn(),
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
      setPlanVisibility: h.setPlanVisibility,
      addPlanPassenger: h.addPlanPassenger,
      removePlanPassenger: h.removePlanPassenger,
      setPlanShareAllFriends: h.setPlanShareAllFriends,
      sharePlanByEmail: h.sharePlanByEmail,
      notifyPlanShares: h.notifyPlanShares,
      setError: h.setError,
      openHelp: h.openHelp,
    }),
}));

// useFriendCandidates derives from the mocked store state (users + friendships);
// re-export the real implementation so the passenger/share pickers see friends.
vi.mock('../state/friendUsers', async () => {
  const actual = await vi.importActual<typeof import('../state/friendUsers')>(
    '../state/friendUsers',
  );
  return actual;
});

import PlanPrivacyDialog from './PlanPrivacyDialog';

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

function plan(over: Partial<Plan> = {}): Plan {
  return {
    id: 42,
    trip_id: 7,
    type: 'flight',
    title: 'BA123',
    confirmation_ref: '',
    notes: '',
    source: '',
    share_all_friends: false,
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

const members: TripMember[] = [
  { user_id: 100, role: 'owner' },
  { user_id: 2, role: 'editor' },
  { user_id: 3, role: 'viewer' },
];

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

function render_(p: Plan = plan()) {
  return render(<PlanPrivacyDialog open plan={p} members={members} onClose={() => {}} />);
}

describe('PlanPrivacyDialog', () => {
  it('opens the sharing help via "How sharing works"', async () => {
    render_();
    await userEvent.click(screen.getByRole('button', { name: /how sharing works/i }));
    expect(h.openHelp).toHaveBeenCalledWith('sharing');
  });

  it('defaults to "everyone" and saves it with an empty user list', async () => {
    h.setPlanVisibility.mockResolvedValue(undefined);
    render_();
    expect(screen.getByRole('radio', { name: /everyone on the trip/i })).toBeChecked();
    // No member multi-select while everyone is selected.
    expect(screen.queryByLabelText('Only visible to')).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /save visibility/i }));
    await waitFor(() =>
      expect(h.setPlanVisibility).toHaveBeenCalledWith(42, { mode: 'everyone', user_ids: [] }),
    );
  });

  it('reveals the member picker for "only visible to" and saves the selection', async () => {
    h.setPlanVisibility.mockResolvedValue(undefined);
    render_();

    await userEvent.click(screen.getByRole('radio', { name: /only visible to/i }));
    await userEvent.click(screen.getByLabelText('Only visible to'));
    await userEvent.click(await screen.findByRole('option', { name: /Bob/ }));
    // Close the listbox before clicking Save.
    await userEvent.keyboard('{Escape}');

    await userEvent.click(screen.getByRole('button', { name: /save visibility/i }));
    await waitFor(() =>
      expect(h.setPlanVisibility).toHaveBeenCalledWith(42, {
        mode: 'only_visible_to',
        user_ids: [2],
      }),
    );
  });

  it('pre-populates the scope from existing hidden_from visibility', async () => {
    h.setPlanVisibility.mockResolvedValue(undefined);
    render_(plan({ visibility: { mode: 'hidden_from', user_ids: [3] } }));

    expect(screen.getByRole('radio', { name: /hidden from/i })).toBeChecked();
    await userEvent.click(screen.getByRole('button', { name: /save visibility/i }));
    await waitFor(() =>
      expect(h.setPlanVisibility).toHaveBeenCalledWith(42, {
        mode: 'hidden_from',
        user_ids: [3],
      }),
    );
  });

  it('shows the passenger auto-grant copy', () => {
    render_();
    expect(
      screen.getByText(/grants them viewer access to the whole trip/i),
    ).toBeInTheDocument();
  });

  it('adds a passenger', async () => {
    h.addPlanPassenger.mockResolvedValue(undefined);
    render_();

    await userEvent.click(screen.getByLabelText('Add passenger'));
    await userEvent.click(await screen.findByRole('option', { name: 'Carol' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));

    await waitFor(() => expect(h.addPlanPassenger).toHaveBeenCalledWith(42, 3));
  });

  it('removes a passenger', async () => {
    h.removePlanPassenger.mockResolvedValue(undefined);
    render_(plan({ passenger_ids: [2] }));

    await userEvent.click(screen.getByLabelText(/remove passenger bob/i));
    await waitFor(() => expect(h.removePlanPassenger).toHaveBeenCalledWith(42, 2));
  });

  it('excludes current passengers from the add picker', async () => {
    render_(plan({ passenger_ids: [2] }));
    await userEvent.click(screen.getByLabelText('Add passenger'));
    expect(await screen.findByRole('option', { name: 'Carol' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Bob' })).not.toBeInTheDocument();
  });

  it('surfaces visibility save errors via setError', async () => {
    h.setPlanVisibility.mockRejectedValue(new Error('vis boom'));
    render_();
    await userEvent.click(screen.getByRole('button', { name: /save visibility/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('vis boom'));
  });

  it('surfaces passenger add errors via setError', async () => {
    h.addPlanPassenger.mockRejectedValue('pax boom');
    render_();
    await userEvent.click(screen.getByLabelText('Add passenger'));
    await userEvent.click(await screen.findByRole('option', { name: 'Bob' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('pax boom'));
  });

  it('keeps an unsaved visibility selection across a same-plan refetch', async () => {
    const { rerender } = render(
      <PlanPrivacyDialog open plan={plan()} members={members} onClose={() => {}} />,
    );
    // User switches to "only visible to" but has not saved yet.
    await userEvent.click(screen.getByRole('radio', { name: /only visible to/i }));
    expect(screen.getByRole('radio', { name: /only visible to/i })).toBeChecked();

    // A passenger-add (or live SSE) refetch hands down a fresh plan object with
    // the same id and same visibility but a new passenger_ids array reference.
    rerender(
      <PlanPrivacyDialog open plan={plan({ passenger_ids: [3] })} members={members} onClose={() => {}} />,
    );

    // The in-progress, unsaved selection must survive — not snap back to "everyone".
    expect(screen.getByRole('radio', { name: /only visible to/i })).toBeChecked();
  });

  it('renders nothing actionable when closed', () => {
    render(<PlanPrivacyDialog open={false} plan={plan()} members={members} onClose={() => {}} />);
    expect(screen.queryByRole('button', { name: /save visibility/i })).not.toBeInTheDocument();
  });

  it('falls back to a "User #id" label and initial for unknown passenger ids', () => {
    // Passenger 999 is not in the users index → label() returns "User #999"
    // and the avatar uses the label's first character.
    render_(plan({ passenger_ids: [999] }));
    expect(screen.getByText('User #999')).toBeInTheDocument();
    expect(screen.getByLabelText(/remove passenger user #999/i)).toBeInTheDocument();
  });

  it('disables the add picker with a hint when there are no friends to add', () => {
    h.state.friendships = [];
    render_();
    expect(screen.getByText(/No friends left to add\./i)).toBeInTheDocument();
    expect(screen.getByLabelText('Add passenger')).toHaveAttribute('aria-disabled', 'true');
  });

  it('surfaces passenger remove errors via setError', async () => {
    h.removePlanPassenger.mockRejectedValue(new Error('rm pax boom'));
    render_(plan({ passenger_ids: [2] }));
    await userEvent.click(screen.getByLabelText(/remove passenger bob/i));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('rm pax boom'));
  });

  it('toggles "Share with all friends" on', async () => {
    h.setPlanShareAllFriends.mockResolvedValue(undefined);
    render_();
    await userEvent.click(screen.getByRole('checkbox', { name: /share with all friends/i }));
    await waitFor(() => expect(h.setPlanShareAllFriends).toHaveBeenCalledWith(42, true));
  });

  it('renders the share-all switch checked when the plan already shares', () => {
    render_(plan({ share_all_friends: true }));
    expect(screen.getByRole('checkbox', { name: /share with all friends/i })).toBeChecked();
  });

  it('notifies the people just added on close', async () => {
    h.addPlanPassenger.mockResolvedValue(undefined);
    h.notifyPlanShares.mockResolvedValue(undefined);
    render_();

    await userEvent.click(screen.getByLabelText('Add passenger'));
    await userEvent.click(await screen.findByRole('option', { name: 'Carol' }));
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    await waitFor(() => expect(h.addPlanPassenger).toHaveBeenCalledWith(42, 3));

    // The notify checkbox appears once someone's been added; close to fire it.
    await userEvent.click(screen.getByRole('button', { name: /^close$/i }));
    await waitFor(() =>
      expect(h.notifyPlanShares).toHaveBeenCalledWith(42, { user_ids: [3], emails: [] }),
    );
  });

  it('invites by email and notifies that address on close', async () => {
    h.sharePlanByEmail.mockResolvedValue(undefined);
    h.notifyPlanShares.mockResolvedValue(undefined);
    render_();

    await userEvent.type(screen.getByLabelText('Email'), 'x@y.com');
    await userEvent.click(screen.getByRole('button', { name: /^invite$/i }));
    await waitFor(() => expect(h.sharePlanByEmail).toHaveBeenCalledWith(42, 'x@y.com'));

    await userEvent.click(screen.getByRole('button', { name: /^close$/i }));
    await waitFor(() =>
      expect(h.notifyPlanShares).toHaveBeenCalledWith(42, { user_ids: [], emails: ['x@y.com'] }),
    );
  });
});
