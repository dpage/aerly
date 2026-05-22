import { create } from 'zustand';

import { api, ApiError } from '../api/client';
import type {
  Capabilities,
  CreateFlightInput,
  Flight,
  InviteUserInput,
  UpdateFlightInput,
  UpdateUserInput,
  User,
} from '../api/types';

type AuthStatus = 'loading' | 'anonymous' | 'authenticated';

const SHOW_ALL_KEY = 'ft.show_all';
const SHOW_OLD_KEY = 'ft.show_old';

interface AppState {
  auth: AuthStatus;
  me: User | null;
  capabilities: Capabilities;
  flights: Flight[];
  users: User[];
  selectedFlightId: number | null;
  /** Wall-clock time (ms since epoch) of the most recent flight.updated event. */
  lastUpdateAt: number | null;
  /** Superuser-only: when true, list and SSE include every flight regardless
   * of visibility. Persisted to localStorage so it survives reloads.
   * Non-superusers see the flag stay false; the server ignores show_all
   * for them in any case. */
  showAll: boolean;
  /** When true, the flight list includes flights whose effective arrival
   * is more than 24 hours in the past. SSE delivery is not gated by age;
   * the client's render-time filter (useVisibleFlights) handles ageing.
   * Available to every signed-in user; persisted to localStorage so it
   * survives reloads. */
  showOld: boolean;
  error: string | null;

  init: () => Promise<void>;
  refreshAll: () => Promise<void>;
  refreshFlights: () => Promise<void>;
  refreshUsers: () => Promise<void>;

  createFlight: (input: CreateFlightInput) => Promise<void>;
  updateFlight: (id: number, patch: UpdateFlightInput) => Promise<void>;
  deleteFlight: (id: number) => Promise<void>;
  addPassenger: (flightId: number, userId: number) => Promise<void>;
  removePassenger: (flightId: number, userId: number) => Promise<void>;
  addShare: (flightId: number, userId: number) => Promise<void>;
  removeShare: (flightId: number, userId: number) => Promise<void>;

  inviteUser: (input: InviteUserInput) => Promise<void>;
  updateUser: (id: number, patch: UpdateUserInput) => Promise<void>;
  deleteUser: (id: number) => Promise<void>;

  logout: () => Promise<void>;
  selectFlight: (id: number | null) => void;
  setShowAll: (v: boolean) => Promise<void>;
  setShowOld: (v: boolean) => Promise<void>;
  applyFlightUpdate: (f: Flight) => void;
  /** Drop a flight from local state in response to a flight.deleted SSE
   * event. Idempotent: no-op if the id isn't present (we may have already
   * removed it locally via deleteFlight()). */
  applyFlightDelete: (id: number) => void;
  setError: (msg: string | null) => void;
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

function loadShowOld(): boolean {
  try {
    return window.localStorage.getItem(SHOW_OLD_KEY) === '1';
  } catch {
    // SSR / privacy modes that throw on localStorage access — treat as off.
    return false;
  }
}

function persistShowOld(v: boolean): void {
  try {
    if (v) window.localStorage.setItem(SHOW_OLD_KEY, '1');
    else window.localStorage.removeItem(SHOW_OLD_KEY);
  } catch {
    // ignore — best effort
  }
}

export const useStore = create<AppState>((set, get) => ({
  auth: 'loading',
  me: null,
  capabilities: { resolver_available: false, poll_interval_sec: 60 },
  flights: [],
  users: [],
  selectedFlightId: null,
  lastUpdateAt: null,
  showAll: loadShowAll(),
  showOld: loadShowOld(),
  error: null,

  async init() {
    try {
      const [me, capabilities] = await Promise.all([api.getMe(), api.getConfig()]);
      set({ me, capabilities, auth: 'authenticated' });
      await get().refreshAll();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        set({ me: null, auth: 'anonymous' });
      } else {
        set({ error: errorMessage(err), auth: 'anonymous' });
      }
    }
  },

  async refreshAll() {
    await Promise.all([get().refreshFlights(), get().refreshUsers()]);
  },

  async refreshFlights() {
    try {
      const flights = await api.listFlights({
        showAll: get().showAll,
        showOld: get().showOld,
      });
      set({ flights });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async refreshUsers() {
    try {
      const users = await api.listUsers();
      set({ users });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async createFlight(input) {
    const flight = await api.createFlight(input);
    // The server publishes a flight.updated SSE event before it returns
    // the HTTP response, so the SSE listener may have already inserted
    // this flight by the time we get here. Upsert by id instead of
    // appending blindly to avoid showing the same flight twice.
    get().applyFlightUpdate(flight);
  },
  async updateFlight(id, patch) {
    const updated = await api.updateFlight(id, patch);
    set((s) => ({ flights: s.flights.map((f) => (f.id === id ? updated : f)) }));
  },
  async deleteFlight(id) {
    await api.deleteFlight(id);
    set((s) => ({
      flights: s.flights.filter((f) => f.id !== id),
      selectedFlightId: s.selectedFlightId === id ? null : s.selectedFlightId,
    }));
  },
  async addPassenger(flightId, userId) {
    await api.addPassenger(flightId, userId);
    const updated = await api.getFlight(flightId);
    set((s) => ({ flights: s.flights.map((f) => (f.id === flightId ? updated : f)) }));
  },
  async removePassenger(flightId, userId) {
    await api.removePassenger(flightId, userId);
    const updated = await api.getFlight(flightId);
    set((s) => ({ flights: s.flights.map((f) => (f.id === flightId ? updated : f)) }));
  },
  async addShare(flightId, userId) {
    await api.addShare(flightId, userId);
    const updated = await api.getFlight(flightId);
    set((s) => ({ flights: s.flights.map((f) => (f.id === flightId ? updated : f)) }));
  },
  async removeShare(flightId, userId) {
    await api.removeShare(flightId, userId);
    const updated = await api.getFlight(flightId);
    set((s) => ({ flights: s.flights.map((f) => (f.id === flightId ? updated : f)) }));
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

  async logout() {
    await api.logout();
    set({
      me: null,
      auth: 'anonymous',
      flights: [],
      users: [],
      selectedFlightId: null,
      capabilities: { resolver_available: false, poll_interval_sec: 60 },
      lastUpdateAt: null,
    });
  },

  selectFlight(id) {
    set({ selectedFlightId: id });
  },

  async setShowAll(v) {
    persistShowAll(v);
    set({ showAll: v });
    // Refetch flights to immediately reflect the new visibility scope; the
    // SSE connection is re-established by App.tsx because showAll is in its
    // useEffect dependency list.
    await get().refreshFlights();
  },

  async setShowOld(v) {
    persistShowOld(v);
    set({ showOld: v });
    // Refetch flights to immediately reflect the new age scope. Unlike
    // setShowAll, no SSE reconnect is needed — the hub doesn't filter by
    // age; the client's render-time filter (useVisibleFlights, Task 5)
    // handles flights that age out while the page is open.
    await get().refreshFlights();
  },

  applyFlightUpdate(f) {
    set((s) => {
      const idx = s.flights.findIndex((x) => x.id === f.id);
      const flights =
        idx === -1
          ? [...s.flights, f].sort(byScheduledOut)
          : (() => {
              const next = s.flights.slice();
              next[idx] = f;
              return next;
            })();
      return { flights, lastUpdateAt: Date.now() };
    });
  },

  applyFlightDelete(id) {
    set((s) => ({
      flights: s.flights.filter((f) => f.id !== id),
      selectedFlightId: s.selectedFlightId === id ? null : s.selectedFlightId,
      lastUpdateAt: Date.now(),
    }));
  },

  setError(msg) {
    set({ error: msg });
  },
}));

function byScheduledOut(a: Flight, b: Flight) {
  return a.scheduled_out.localeCompare(b.scheduled_out);
}

function byLogin(a: User, b: User) {
  return a.github_login.toLowerCase().localeCompare(b.github_login.toLowerCase());
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
