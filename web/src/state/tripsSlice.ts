import type { StateCreator } from 'zustand';

import { api, ApiError } from '../api/client';
import type {
  AddTripMemberInput,
  CreateTripInput,
  Plan,
  TagSuggestion,
  Trip,
  UpdateTripInput,
} from '../api/types';
import { errorMessage } from './helpers';
import type { StoreState } from './store';

/** State + actions owning the trip list and the currently-open trip.
 *
 * Wave 0b: actions are typed and wired to the client (fetch + set), but kept
 * deliberately thin. Wave 1F (trip list + timeline) fleshes out grouping,
 * optimistic updates, and SSE reconciliation. Leaving bodies as straight
 * "fetch then set" keeps the data flow demonstrable without pre-empting those
 * decisions. */
export interface TripsSlice {
  trips: Trip[];
  /** The trip open in the detail view, with its plans loaded. */
  currentTrip: (Trip & { plans: Plan[] }) | null;
  tripsLoading: boolean;
  tagSuggestions: TagSuggestion[];

  listTrips: () => Promise<void>;
  loadTrip: (id: number) => Promise<void>;
  clearCurrentTrip: () => void;
  createTrip: (input: CreateTripInput) => Promise<Trip | undefined>;
  updateTrip: (id: number, patch: UpdateTripInput) => Promise<void>;
  deleteTrip: (id: number) => Promise<void>;
  addTripMember: (tripId: number, input: AddTripMemberInput) => Promise<void>;
  removeTripMember: (tripId: number, userId: number) => Promise<void>;
  setTripTags: (tripId: number, labels: string[]) => Promise<void>;
  suggestTags: (q: string) => Promise<void>;
}

export const createTripsSlice: StateCreator<StoreState, [], [], TripsSlice> = (set, get) => ({
  trips: [],
  currentTrip: null,
  tripsLoading: false,
  tagSuggestions: [],

  async listTrips() {
    set({ tripsLoading: true });
    try {
      const trips = await api.listTrips();
      set({ trips, tripsLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), tripsLoading: false });
    }
  },

  async loadTrip(id) {
    try {
      const currentTrip = await api.getTrip(id);
      set({ currentTrip });
    } catch (err) {
      // A 404 means the trip is gone — typically because it was just deleted
      // and a live event (or this navigation) raced the deletion. Clear it
      // silently rather than alarming the user with a "not found".
      if (err instanceof ApiError && err.status === 404) {
        set((s) => (s.currentTrip?.id === id ? { currentTrip: null } : {}));
        return;
      }
      set({ error: errorMessage(err) });
    }
  },

  clearCurrentTrip() {
    set({ currentTrip: null });
  },

  async createTrip(input) {
    try {
      const trip = await api.createTrip(input);
      set((s) => ({ trips: [...s.trips, trip] }));
      return trip;
    } catch (err) {
      set({ error: errorMessage(err) });
      return undefined;
    }
  },

  async updateTrip(id, patch) {
    const updated = await api.updateTrip(id, patch);
    set((s) => ({
      trips: s.trips.map((t) => (t.id === id ? updated : t)),
      currentTrip:
        s.currentTrip?.id === id ? { ...updated, plans: s.currentTrip.plans } : s.currentTrip,
    }));
  },

  async deleteTrip(id) {
    await api.deleteTrip(id);
    set((s) => ({
      trips: s.trips.filter((t) => t.id !== id),
      currentTrip: s.currentTrip?.id === id ? null : s.currentTrip,
    }));
  },

  async addTripMember(tripId, input) {
    const updated = await api.addTripMember(tripId, input);
    set((s) => ({ trips: s.trips.map((t) => (t.id === tripId ? updated : t)) }));
  },

  async removeTripMember(tripId, userId) {
    await api.removeTripMember(tripId, userId);
    // TODO(1F): reconcile membership in-place; for now refetch the trip.
    await get().loadTrip(tripId);
  },

  async setTripTags(tripId, labels) {
    const updated = await api.setTripTags(tripId, labels);
    set((s) => ({ trips: s.trips.map((t) => (t.id === tripId ? updated : t)) }));
  },

  async suggestTags(q) {
    try {
      const tagSuggestions = await api.suggestTags(q);
      set({ tagSuggestions });
    } catch {
      // Non-fatal: autocomplete is best-effort.
    }
  },
});

