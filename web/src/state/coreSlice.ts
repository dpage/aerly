import type { StateCreator } from 'zustand';

import { api, ApiError } from '../api/client';
import type {
  AutoShare,
  AutoShareRole,
  Capabilities,
  Friendship,
  InviteUserInput,
  Notifications,
  PaperSize,
  UpdateUserInput,
  User,
} from '../api/types';
import { errorMessage } from './helpers';
import { clearOfflineCaches } from '../offline-cache';
import type { StoreState } from './store';

type AuthStatus = 'loading' | 'anonymous' | 'authenticated';

const SHOW_ALL_KEY = 'ft.show_all';

/** The signed-out state the store resets to after any logout flow. */
function anonymousReset(): Partial<StoreState> {
  return {
    me: null,
    auth: 'anonymous',
    users: [],
    autoShares: [],
    capabilities: {
      resolver_available: false,
      poll_interval_sec: 60,
      email_ingest_enabled: false,
      attachments_enabled: false,
      explore_enabled: true,
    },
    notifications: { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 },
    notice: null,
  };
}

/** Coerce a notifications payload to complete, finite numeric counts. The badge
 * sums the three counts, so a single missing or non-numeric field would poison
 * the sum into `NaN` and surface as a literal "NaN" badge. A backend older than
 * a given count field omits it entirely, which happens during the version-skew
 * window of a deploy (or against a stale dev binary), so default every count to
 * 0 — a count we don't yet understand reads as "nothing to show". */
function normaliseNotifications(n: Partial<Notifications> | null | undefined): Notifications {
  const count = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
  return {
    friend_requests_pending: count(n?.friend_requests_pending),
    unread_alerts: count(n?.unread_alerts),
    unread_shares: count(n?.unread_shares),
  };
}

/** The core slice: auth/me/capabilities, the user + friendship caches, and the
 * notification badge. The trip-planning slices (trips, plans, tracker, …) own
 * the redesigned domain state; this slice holds the cross-cutting session
 * bits every page needs. */
export interface CoreSlice {
  auth: AuthStatus;
  me: User | null;
  capabilities: Capabilities;
  users: User[];
  friendships: Friendship[];
  /** The caller's "always share with" defaults: people every new trip they
   * create is automatically shared with. Loaded at startup, kept in sync by
   * the dialog's mutators. */
  autoShares: AutoShare[];
  /** Superuser-only: when true, the SSE stream includes every event regardless
   * of visibility. Persisted to localStorage so it survives reloads.
   * Non-superusers see the flag stay false; the server ignores show_all for
   * them in any case. */
  showAll: boolean;
  /** Friends'-trips superuser diagnostic toggles ("All friends' trips" / "All
   * trips"). Lifted out of the TripList page so they survive opening a trip and
   * tapping Back — the page unmounts on navigation, which would otherwise reset
   * them to their defaults. In-memory only: a reload clears them, matching their
   * throwaway diagnostic nature. */
  friendsShowAllFriends: boolean;
  friendsShowAllTrips: boolean;
  error: string | null;
  notifications: Notifications;
  notice: { message: string; severity: 'success' | 'info' } | null;

  init: () => Promise<void>;
  refreshAll: () => Promise<void>;
  refreshUsers: () => Promise<void>;
  refreshFriendships: () => Promise<void>;

  inviteUser: (input: InviteUserInput) => Promise<void>;
  updateUser: (id: number, patch: UpdateUserInput) => Promise<void>;
  deleteUser: (id: number) => Promise<void>;

  setHomeAddress: (address: string) => Promise<void>;
  /** Pin the exact home coordinates, or clear the pin by passing null. */
  setHomeCoords: (coords: { lat: number; lon: number } | null) => Promise<void>;
  setPaperSize: (paperSize: PaperSize) => Promise<void>;
  /** Update the feature-hiding preferences (hide_explore / hide_maps). */
  setHiddenFeatures: (patch: { hide_explore?: boolean; hide_maps?: boolean }) => Promise<void>;
  refreshAutoShares: () => Promise<void>;
  setAutoShare: (userId: number, role: AutoShareRole) => Promise<void>;
  removeAutoShare: (userId: number) => Promise<void>;
  logout: () => Promise<void>;
  /** Sign out of every session (this device and all others). */
  logoutAll: () => Promise<void>;
  setShowAll: (v: boolean) => Promise<void>;
  setFriendsShowAllFriends: (v: boolean) => void;
  setFriendsShowAllTrips: (v: boolean) => void;
  setError: (msg: string | null) => void;
  refreshNotifications: () => Promise<void>;
  applyNotificationsUpdate: (n: Notifications) => void;
  setNotice: (n: { message: string; severity: 'success' | 'info' } | null) => void;
}

function loadShowAll(): boolean {
  try {
    return window.localStorage.getItem(SHOW_ALL_KEY) === '1';
  } catch {
    // SSR / privacy modes that throw on localStorage access — treat as off.
    return false;
  }
}

function persistShowAll(v: boolean): void {
  try {
    if (v) window.localStorage.setItem(SHOW_ALL_KEY, '1');
    else window.localStorage.removeItem(SHOW_ALL_KEY);
  } catch {
    // ignore — best effort
  }
}

export const createCoreSlice: StateCreator<StoreState, [], [], CoreSlice> = (set, get) => ({
  auth: 'loading',
  me: null,
  capabilities: {
    resolver_available: false,
    poll_interval_sec: 60,
    email_ingest_enabled: false,
    attachments_enabled: false,
  },
  users: [],
  friendships: [],
  autoShares: [],
  showAll: loadShowAll(),
  friendsShowAllFriends: false,
  friendsShowAllTrips: false,
  error: null,
  notifications: { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 },
  notice: null,

  async init() {
    try {
      const [me, capabilities] = await Promise.all([api.getMe(), api.getConfig()]);
      set({ me, capabilities, auth: 'authenticated' });
      await Promise.all([get().refreshAll(), get().refreshNotifications(), get().loadAlerts()]);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        set({ me: null, auth: 'anonymous' });
      } else {
        set({ error: errorMessage(err), auth: 'anonymous' });
      }
    }
  },

  async refreshAll() {
    await Promise.all([
      get().refreshUsers(),
      get().refreshFriendships(),
      get().refreshAutoShares(),
    ]);
  },

  async refreshUsers() {
    try {
      const users = await api.listUsers();
      set({ users });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async refreshFriendships() {
    try {
      const friendships = await api.listFriends();
      set({ friendships });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async inviteUser(input) {
    const user = await api.inviteUser(input);
    set((s) => ({ users: [...s.users, user].sort(byLogin) }));
  },
  async updateUser(id, patch) {
    const updated = await api.updateUser(id, patch);
    set((s) => ({
      users: s.users.map((u) => (u.id === id ? updated : u)),
      me: s.me?.id === id ? updated : s.me,
    }));
  },
  async deleteUser(id) {
    await api.deleteUser(id);
    set((s) => ({ users: s.users.filter((u) => u.id !== id) }));
  },

  async setHomeAddress(address) {
    const updated = await api.updateMe({ home_address: address });
    set((s) => ({
      me: updated,
      users: s.users.map((u) => (u.id === updated.id ? updated : u)),
    }));
  },

  async setHomeCoords(coords) {
    const updated = await api.updateMe({
      home_coords: coords ? { lat: coords.lat, lon: coords.lon } : { lat: null, lon: null },
    });
    set((s) => ({
      me: updated,
      users: s.users.map((u) => (u.id === updated.id ? updated : u)),
    }));
  },

  async setPaperSize(paperSize) {
    const updated = await api.updateMe({ paper_size: paperSize });
    set((s) => ({
      me: updated,
      users: s.users.map((u) => (u.id === updated.id ? updated : u)),
    }));
  },

  async setHiddenFeatures(patch) {
    const updated = await api.updateMe(patch);
    set((s) => ({
      me: updated,
      users: s.users.map((u) => (u.id === updated.id ? updated : u)),
    }));
  },

  async refreshAutoShares() {
    try {
      const autoShares = await api.listMyAutoShares();
      set({ autoShares });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },
  async setAutoShare(userId, role) {
    const autoShares = await api.setMyAutoShare(userId, role);
    set({ autoShares });
  },
  async removeAutoShare(userId) {
    await api.removeMyAutoShare(userId);
    set((s) => ({ autoShares: s.autoShares.filter((a) => a.user_id !== userId) }));
  },

  async logout() {
    await api.logout();
    // Drop cached account data so it can't be read offline after sign-out.
    await clearOfflineCaches();
    set(anonymousReset());
  },

  async logoutAll() {
    await api.logoutAll();
    await clearOfflineCaches();
    set(anonymousReset());
  },

  async setShowAll(v) {
    persistShowAll(v);
    set({ showAll: v });
    // The SSE connection is re-established by App.tsx because showAll is in its
    // useEffect dependency list, so the new visibility scope takes effect on
    // the event stream immediately.
  },

  setFriendsShowAllFriends(v) {
    set({ friendsShowAllFriends: v });
  },

  setFriendsShowAllTrips(v) {
    set({ friendsShowAllTrips: v });
  },

  setError(msg) {
    set({ error: msg });
  },

  async refreshNotifications() {
    try {
      const n = await api.getNotifications();
      set({ notifications: normaliseNotifications(n) });
    } catch {
      // Non-fatal: stale badge is fine; SSE / next reload will recover.
    }
  },

  applyNotificationsUpdate(n) {
    set({ notifications: normaliseNotifications(n) });
  },

  setNotice(n) {
    set({ notice: n });
  },
});

function byLogin(a: User, b: User) {
  return a.username.toLowerCase().localeCompare(b.username.toLowerCase());
}
