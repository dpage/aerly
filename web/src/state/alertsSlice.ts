import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type { AlertPrefs, UpdateAlertPrefsInput } from '../api/types';
import type { StoreState } from './store';

/** State + actions for per-user alert preferences and per-plan alert opt-in
 * (spec §9).
 *
 * Wave 0b: typed fetch/set stubs. Wave 2B builds the alert-prefs UI and the
 * per-plan opt-in toggle on top of these. */
export interface AlertsSlice {
  alertPrefs: AlertPrefs | null;

  loadAlertPrefs: () => Promise<void>;
  updateAlertPrefs: (patch: UpdateAlertPrefsInput) => Promise<void>;
  optInPlanAlerts: (planId: number) => Promise<void>;
  optOutPlanAlerts: (planId: number) => Promise<void>;
}

export const createAlertsSlice: StateCreator<StoreState, [], [], AlertsSlice> = (set, get) => ({
  alertPrefs: null,

  async loadAlertPrefs() {
    try {
      const alertPrefs = await api.getAlertPrefs();
      set({ alertPrefs });
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },

  async updateAlertPrefs(patch) {
    const alertPrefs = await api.updateAlertPrefs(patch);
    set({ alertPrefs });
  },

  async optInPlanAlerts(planId) {
    await api.optInPlanAlerts(planId);
    await reloadCurrent(get);
  },

  async optOutPlanAlerts(planId) {
    await api.optOutPlanAlerts(planId);
    await reloadCurrent(get);
  },
});

/** Reload whichever trip is currently open so a plan's per-viewer
 * `alert_opted_in` projection reflects the opt-in change on reopen. No-op when
 * no trip is open. */
async function reloadCurrent(get: () => StoreState): Promise<void> {
  const id = get().currentTrip?.id;
  if (id != null) await get().loadTrip(id);
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
