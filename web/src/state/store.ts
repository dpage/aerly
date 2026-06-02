import { create } from 'zustand';

import { createCoreSlice, type CoreSlice } from './coreSlice';
import { createTripsSlice, type TripsSlice } from './tripsSlice';
import { createPlansSlice, type PlansSlice } from './plansSlice';
import { createTrackerSlice, type TrackerSlice } from './trackerSlice';
import { createIngestSlice, type IngestSlice } from './ingestSlice';
import { createAlertsSlice, type AlertsSlice } from './alertsSlice';
import { createHelpSlice, type HelpSlice } from './helpSlice';

/** The full store type — the union of every domain slice. Slices reference
 * this type in their `StateCreator` so they can call across slice boundaries
 * (e.g. plansSlice → loadTrip, every slice → setError via `error`).
 *
 * Per-domain slices live in sibling files (coreSlice, tripsSlice, …) so feature
 * waves can edit their own slice without colliding on one giant store file. */
export type StoreState = CoreSlice &
  TripsSlice &
  PlansSlice &
  TrackerSlice &
  IngestSlice &
  AlertsSlice &
  HelpSlice;

export const useStore = create<StoreState>()((...a) => ({
  ...createCoreSlice(...a),
  ...createTripsSlice(...a),
  ...createPlansSlice(...a),
  ...createTrackerSlice(...a),
  ...createIngestSlice(...a),
  ...createAlertsSlice(...a),
  ...createHelpSlice(...a),
}));
