import { describe, it, expect, beforeEach, vi } from 'vitest';

import { ApiError } from '../api/client';
import type { User } from '../api/types';

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return {
    ApiError: actual.ApiError,
    api: {
      isAuthError: actual.api.isAuthError,
      getMe: vi.fn(),
      getConfig: vi.fn(),
      listUsers: vi.fn(),
      listFriends: vi.fn(),
      listMyAutoShares: vi.fn(),
      setMyAutoShare: vi.fn(),
      removeMyAutoShare: vi.fn(),
      inviteUser: vi.fn(),
      updateUser: vi.fn(),
      deleteUser: vi.fn(),
      updateMe: vi.fn(),
      getNotifications: vi.fn(),
      logout: vi.fn(),
    },
  };
});

import { api } from '../api/client';
import { useStore } from './store';

const mockApi = api as unknown as Record<string, ReturnType<typeof vi.fn>>;

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'octocat',
    name: 'Octo',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    ...over,
  };
}

const initialState = useStore.getState();

beforeEach(() => {
  vi.clearAllMocks();
  // Provide safe defaults for API methods called by refreshAll so tests that
  // don't care about a particular slice don't need to set them up explicitly.
  mockApi.listFriends.mockResolvedValue([]);
  mockApi.listUsers.mockResolvedValue([]);
  mockApi.listMyAutoShares.mockResolvedValue([]);
  // restore action references in case a previous test mutated them
  useStore.setState({ ...initialState }, false);
  useStore.setState(
    {
      auth: 'loading',
      me: null,
      capabilities: { resolver_available: false },
      users: [],
      friendships: [],
      error: null,
    },
    false,
  );
});

describe('init', () => {
  it('authenticates and loads data on success', async () => {
    mockApi.getMe.mockResolvedValue(user());
    mockApi.getConfig.mockResolvedValue({ resolver_available: true });
    mockApi.listUsers.mockResolvedValue([user()]);
    await useStore.getState().init();
    const s = useStore.getState();
    expect(s.auth).toBe('authenticated');
    expect(s.me?.id).toBe(1);
    expect(s.capabilities.resolver_available).toBe(true);
    expect(s.users).toHaveLength(1);
  });

  it('goes anonymous on 401 ApiError', async () => {
    mockApi.getMe.mockRejectedValue(new ApiError(401, 'unauth'));
    mockApi.getConfig.mockResolvedValue({ resolver_available: false });
    await useStore.getState().init();
    const s = useStore.getState();
    expect(s.auth).toBe('anonymous');
    expect(s.me).toBeNull();
    expect(s.error).toBeNull();
  });

  it('sets error and goes anonymous on other error', async () => {
    mockApi.getMe.mockRejectedValue(new Error('network down'));
    mockApi.getConfig.mockResolvedValue({ resolver_available: false });
    await useStore.getState().init();
    const s = useStore.getState();
    expect(s.auth).toBe('anonymous');
    expect(s.error).toBe('network down');
  });
});

describe('refreshUsers error branches', () => {
  it('refreshUsers sets error on failure (non-Error -> String())', async () => {
    mockApi.listUsers.mockRejectedValue('strerr');
    await useStore.getState().refreshUsers();
    expect(useStore.getState().error).toBe('strerr');
  });

  it('refreshAll refreshes users and friendships', async () => {
    mockApi.listUsers.mockResolvedValue([user()]);
    mockApi.listFriends.mockResolvedValue([
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 },
    ]);
    await useStore.getState().refreshAll();
    expect(useStore.getState().users).toHaveLength(1);
    expect(useStore.getState().friendships).toHaveLength(1);
  });
});

describe('auto-shares', () => {
  it('refreshAutoShares loads the defaults', async () => {
    mockApi.listMyAutoShares.mockResolvedValue([{ user_id: 2, role: 'viewer' }]);
    await useStore.getState().refreshAutoShares();
    expect(useStore.getState().autoShares).toEqual([{ user_id: 2, role: 'viewer' }]);
  });

  it('refreshAutoShares sets error on failure', async () => {
    mockApi.listMyAutoShares.mockRejectedValue(new Error('boom'));
    await useStore.getState().refreshAutoShares();
    expect(useStore.getState().error).toBe('boom');
  });

  it('setAutoShare stores the server-returned list', async () => {
    mockApi.setMyAutoShare.mockResolvedValue([{ user_id: 3, role: 'editor' }]);
    await useStore.getState().setAutoShare(3, 'editor');
    expect(mockApi.setMyAutoShare).toHaveBeenCalledWith(3, 'editor');
    expect(useStore.getState().autoShares).toEqual([{ user_id: 3, role: 'editor' }]);
  });

  it('removeAutoShare drops the entry locally', async () => {
    useStore.setState({
      autoShares: [
        { user_id: 2, role: 'viewer' },
        { user_id: 3, role: 'editor' },
      ],
    });
    mockApi.removeMyAutoShare.mockResolvedValue(undefined);
    await useStore.getState().removeAutoShare(2);
    expect(mockApi.removeMyAutoShare).toHaveBeenCalledWith(2);
    expect(useStore.getState().autoShares).toEqual([{ user_id: 3, role: 'editor' }]);
  });
});

describe('setShowAll', () => {
  // Node 25 ships a built-in localStorage global that shadows jsdom's working
  // implementation with a methodless stub; vitest @ Node 22 in CI doesn't hit
  // this. Install a fresh in-memory localStorage on window before each test so
  // the suite is portable across both.
  let store: Record<string, string>;

  beforeEach(() => {
    store = {};
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: (k: string) => (k in store ? store[k] : null),
        setItem: (k: string, v: string) => {
          store[k] = String(v);
        },
        removeItem: (k: string) => {
          delete store[k];
        },
        clear: () => {
          store = {};
        },
        key: (i: number) => Object.keys(store)[i] ?? null,
        get length() {
          return Object.keys(store).length;
        },
      },
    });
  });

  it('persists true to localStorage and flips the flag', async () => {
    await useStore.getState().setShowAll(true);
    expect(window.localStorage.getItem('ft.show_all')).toBe('1');
    expect(useStore.getState().showAll).toBe(true);
  });

  it('persists false by removing the localStorage entry', async () => {
    window.localStorage.setItem('ft.show_all', '1');
    await useStore.getState().setShowAll(false);
    expect(window.localStorage.getItem('ft.show_all')).toBeNull();
    expect(useStore.getState().showAll).toBe(false);
  });

  // Some privacy modes / SSR shims throw on every localStorage access. The
  // persist helper swallows that so flipping the toggle still updates state.
  it('swallows localStorage errors and still updates state', async () => {
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: {
        getItem: () => {
          throw new Error('blocked');
        },
        setItem: () => {
          throw new Error('blocked');
        },
        removeItem: () => {
          throw new Error('blocked');
        },
      },
    });
    await useStore.getState().setShowAll(true);
    expect(useStore.getState().showAll).toBe(true);
  });
});

describe('user mutations', () => {
  it('inviteUser appends and sorts by login (case-insensitive)', async () => {
    useStore.setState({ users: [user({ id: 1, username: 'Zed' })] });
    mockApi.inviteUser.mockResolvedValue(user({ id: 2, username: 'abc' }));
    await useStore.getState().inviteUser({ username: 'abc' });
    expect(useStore.getState().users.map((u) => u.id)).toEqual([2, 1]);
  });

  it('updateUser updates the list and me when it is me', async () => {
    useStore.setState({
      users: [user({ id: 1, name: 'old' })],
      me: user({ id: 1, name: 'old' }),
    });
    mockApi.updateUser.mockResolvedValue(user({ id: 1, name: 'new' }));
    await useStore.getState().updateUser(1, { name: 'new' });
    expect(useStore.getState().users[0].name).toBe('new');
    expect(useStore.getState().me?.name).toBe('new');
  });

  it('updateUser leaves me untouched when updating someone else', async () => {
    useStore.setState({
      users: [user({ id: 2, name: 'old' })],
      me: user({ id: 1, name: 'mine' }),
    });
    mockApi.updateUser.mockResolvedValue(user({ id: 2, name: 'new' }));
    await useStore.getState().updateUser(2, { name: 'new' });
    expect(useStore.getState().me?.name).toBe('mine');
  });

  it('updateUser handles me being null', async () => {
    useStore.setState({ users: [user({ id: 2 })], me: null });
    mockApi.updateUser.mockResolvedValue(user({ id: 2, name: 'new' }));
    await useStore.getState().updateUser(2, { name: 'new' });
    expect(useStore.getState().me).toBeNull();
  });

  it('deleteUser removes the user', async () => {
    useStore.setState({ users: [user({ id: 1 }), user({ id: 2 })] });
    mockApi.deleteUser.mockResolvedValue(undefined);
    await useStore.getState().deleteUser(1);
    expect(useStore.getState().users.map((u) => u.id)).toEqual([2]);
  });

  it('setHomeAddress updates me and the users list', async () => {
    useStore.setState({
      users: [user({ id: 1, home_address: '' })],
      me: user({ id: 1, home_address: '' }),
    });
    mockApi.updateMe.mockResolvedValue(user({ id: 1, home_address: '1 Main St' }));
    await useStore.getState().setHomeAddress('1 Main St');
    expect(mockApi.updateMe).toHaveBeenCalledWith({ home_address: '1 Main St' });
    expect(useStore.getState().me?.home_address).toBe('1 Main St');
    expect(useStore.getState().users[0].home_address).toBe('1 Main St');
  });

  it('setHomeCoords pins coordinates and updates me and the users list', async () => {
    useStore.setState({
      users: [user({ id: 1 })],
      me: user({ id: 1 }),
    });
    mockApi.updateMe.mockResolvedValue(user({ id: 1, home_lat: 51.5, home_lon: -0.12 }));
    await useStore.getState().setHomeCoords({ lat: 51.5, lon: -0.12 });
    expect(mockApi.updateMe).toHaveBeenCalledWith({ home_coords: { lat: 51.5, lon: -0.12 } });
    expect(useStore.getState().me?.home_lat).toBe(51.5);
    expect(useStore.getState().users[0].home_lon).toBe(-0.12);
  });

  it('setHomeCoords(null) clears the pin via null lat/lon', async () => {
    useStore.setState({
      users: [user({ id: 1, home_lat: 51.5, home_lon: -0.12 })],
      me: user({ id: 1, home_lat: 51.5, home_lon: -0.12 }),
    });
    mockApi.updateMe.mockResolvedValue(user({ id: 1 }));
    await useStore.getState().setHomeCoords(null);
    expect(mockApi.updateMe).toHaveBeenCalledWith({ home_coords: { lat: null, lon: null } });
    expect(useStore.getState().me?.home_lat).toBeUndefined();
  });

  it('setPaperSize updates me and the users list', async () => {
    useStore.setState({
      users: [user({ id: 1, paper_size: 'a4' })],
      me: user({ id: 1, paper_size: 'a4' }),
    });
    mockApi.updateMe.mockResolvedValue(user({ id: 1, paper_size: 'letter' }));
    await useStore.getState().setPaperSize('letter');
    expect(mockApi.updateMe).toHaveBeenCalledWith({ paper_size: 'letter' });
    expect(useStore.getState().me?.paper_size).toBe('letter');
    expect(useStore.getState().users[0].paper_size).toBe('letter');
  });

  it('setHiddenFeatures updates me and the users list', async () => {
    useStore.setState({
      users: [user({ id: 1, hide_maps: false })],
      me: user({ id: 1, hide_maps: false }),
    });
    mockApi.updateMe.mockResolvedValue(user({ id: 1, hide_maps: true }));
    await useStore.getState().setHiddenFeatures({ hide_maps: true });
    expect(mockApi.updateMe).toHaveBeenCalledWith({ hide_maps: true });
    expect(useStore.getState().me?.hide_maps).toBe(true);
    expect(useStore.getState().users[0].hide_maps).toBe(true);
  });
});

describe('logout / setError', () => {
  it('logout resets the store', async () => {
    useStore.setState({
      me: user(),
      auth: 'authenticated',
      users: [user()],
      capabilities: { resolver_available: true },
    });
    mockApi.logout.mockResolvedValue(undefined);
    await useStore.getState().logout();
    const s = useStore.getState();
    expect(s.me).toBeNull();
    expect(s.auth).toBe('anonymous');
    expect(s.users).toEqual([]);
    expect(s.capabilities.resolver_available).toBe(false);
  });

  it('setError sets and clears the error', () => {
    useStore.getState().setError('oops');
    expect(useStore.getState().error).toBe('oops');
    useStore.getState().setError(null);
    expect(useStore.getState().error).toBeNull();
  });

  it('persists the Friends-trips diagnostic toggles', () => {
    useStore.getState().setFriendsShowAllFriends(true);
    expect(useStore.getState().friendsShowAllFriends).toBe(true);
    useStore.getState().setFriendsShowAllTrips(true);
    expect(useStore.getState().friendsShowAllTrips).toBe(true);
    useStore.getState().setFriendsShowAllFriends(false);
    useStore.getState().setFriendsShowAllTrips(false);
    expect(useStore.getState().friendsShowAllFriends).toBe(false);
    expect(useStore.getState().friendsShowAllTrips).toBe(false);
  });
});

describe('notifications + notice slice', () => {
  it('init also fetches notifications and writes them to state', async () => {
    mockApi.getMe.mockResolvedValue(user());
    mockApi.getConfig.mockResolvedValue({ resolver_available: false });
    mockApi.listUsers.mockResolvedValue([]);
    mockApi.getNotifications.mockResolvedValue({ friend_requests_pending: 3 });
    await useStore.getState().init();
    expect(useStore.getState().notifications.friend_requests_pending).toBe(3);
  });

  it('refreshNotifications swallows errors and leaves state untouched', async () => {
    useStore.setState({ notifications: { friend_requests_pending: 5 } });
    mockApi.getNotifications.mockRejectedValue(new Error('boom'));
    await useStore.getState().refreshNotifications();
    // Count survives the failure; no error written.
    expect(useStore.getState().notifications.friend_requests_pending).toBe(5);
    expect(useStore.getState().error).toBeNull();
  });

  it('applyNotificationsUpdate overwrites notifications state', () => {
    useStore.setState({ notifications: { friend_requests_pending: 0 } });
    useStore.getState().applyNotificationsUpdate({ friend_requests_pending: 7 });
    expect(useStore.getState().notifications.friend_requests_pending).toBe(7);
  });

  it('applyNotificationsUpdate defaults a missing count to 0 (version skew)', () => {
    useStore.setState({
      notifications: { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 },
    });
    // A backend older than the unread_shares field omits it from the payload.
    // The badge sums the three counts, so a missing field must read as 0 rather
    // than undefined, otherwise the avatar badge renders the literal "NaN".
    useStore.getState().applyNotificationsUpdate({ friend_requests_pending: 2, unread_alerts: 1 });
    const n = useStore.getState().notifications;
    expect(n.unread_shares).toBe(0);
    expect(n.friend_requests_pending + n.unread_alerts + n.unread_shares).toBe(3);
  });

  it('refreshNotifications normalises a payload missing a count to 0', async () => {
    mockApi.getNotifications.mockResolvedValue({ friend_requests_pending: 1, unread_alerts: 4 });
    await useStore.getState().refreshNotifications();
    const n = useStore.getState().notifications;
    expect(n.unread_shares).toBe(0);
    expect(n.friend_requests_pending + n.unread_alerts + n.unread_shares).toBe(5);
  });

  it('setNotice sets and clears the notice', () => {
    useStore.getState().setNotice({ message: 'hi', severity: 'success' });
    expect(useStore.getState().notice).toEqual({ message: 'hi', severity: 'success' });
    useStore.getState().setNotice(null);
    expect(useStore.getState().notice).toBeNull();
  });

  it('logout resets notifications and notice', async () => {
    useStore.setState({
      me: user(),
      auth: 'authenticated',
      notifications: { friend_requests_pending: 4 },
      notice: { message: 'x', severity: 'info' },
    });
    mockApi.logout.mockResolvedValue(undefined);
    await useStore.getState().logout();
    const s = useStore.getState();
    expect(s.notifications.friend_requests_pending).toBe(0);
    expect(s.notice).toBeNull();
  });
});

describe('friendships slice', () => {
  beforeEach(() => {
    mockApi.listFriends.mockReset();
  });

  it('starts empty and refreshFriendships loads from the API', async () => {
    const fixtures = [
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 },
      {
        user_low: 1,
        user_high: 3,
        friend_id: 3,
        status: 'pending',
        requested_by: 1,
        direction: 'outgoing',
      },
    ];
    mockApi.listFriends.mockResolvedValueOnce(fixtures);

    expect(useStore.getState().friendships).toEqual([]);
    await useStore.getState().refreshFriendships();
    expect(useStore.getState().friendships).toEqual(fixtures);
  });

  it('refreshAll() also refreshes friendships', async () => {
    mockApi.listUsers.mockResolvedValue([]);
    mockApi.listFriends.mockResolvedValue([
      { user_low: 1, user_high: 2, friend_id: 2, status: 'accepted', requested_by: 1 },
    ]);

    await useStore.getState().refreshAll();
    expect(mockApi.listFriends).toHaveBeenCalled();
    expect(useStore.getState().friendships).toHaveLength(1);
  });
});
