import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type { AlertPrefs, FlightAlert, UpdateAlertPrefsInput } from '../api/types';
import { errorMessage, reloadCurrent } from './helpers';
import type { StoreState } from './store';

/** State + actions for per-user alert preferences and per-plan alert opt-in
 * (spec §9).
 *
 * Wave 0b: typed fetch/set stubs. Wave 2B builds the alert-prefs UI and the
 * per-plan opt-in toggle on top of these. */
export interface AlertsSlice {
  alertPrefs: AlertPrefs | null;
  alerts: FlightAlert[];

  loadAlertPrefs: () => Promise<void>;
  updateAlertPrefs: (patch: UpdateAlertPrefsInput) => Promise<void>;
  optInPlanAlerts: (planId: number) => Promise<void>;
  optOutPlanAlerts: (planId: number) => Promise<void>;

  // Upcoming-plan reminders (#11).
  setTripReminder: (tripId: number, leadHours: number) => Promise<void>;
  clearTripReminder: (tripId: number) => Promise<void>;
  setPlanReminder: (planId: number, enabled: boolean, leadHours: number) => Promise<void>;
  clearPlanReminder: (planId: number) => Promise<void>;

  loadAlerts: () => Promise<void>;
  applyIncomingAlert: (alert: FlightAlert) => void;
  markAlertsRead: () => Promise<void>;
}

export const createAlertsSlice: StateCreator<StoreState, [], [], AlertsSlice> = (set, get) => ({
  alertPrefs: null,
  alerts: [],

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

  async setTripReminder(tripId, leadHours) {
    await api.setTripReminder(tripId, leadHours);
    await reloadCurrent(get);
  },

  async clearTripReminder(tripId) {
    await api.clearTripReminder(tripId);
    await reloadCurrent(get);
  },

  async setPlanReminder(planId, enabled, leadHours) {
    await api.setPlanReminder(planId, enabled, leadHours);
    await reloadCurrent(get);
  },

  async clearPlanReminder(planId) {
    await api.clearPlanReminder(planId);
    await reloadCurrent(get);
  },

  async loadAlerts() {
    try {
      // The REST endpoint now returns the generic NotificationItem shape; cast
      // to FlightAlert[] so existing state/SSE/component consumers are unaffected
      // until the inbox UI is updated in Task 15.
      const alerts = (await api.getAlerts()) as unknown as FlightAlert[];
      set({ alerts });
    } catch {
      // Non-fatal: SSE / next reload recovers the inbox.
    }
  },

  applyIncomingAlert(alert) {
    set((s) => {
      // SSE can redeliver on reconnect — dedupe by id so the inbox and the
      // unread badge don't double-count the same alert.
      if (s.alerts.some((a) => a.id === alert.id)) return {};
      return {
        alerts: [alert, ...s.alerts].slice(0, 50),
        // Only bump the unread badge for an actually-unread alert.
        notifications: alert.read_at
          ? s.notifications
          : { ...s.notifications, unread_alerts: s.notifications.unread_alerts + 1 },
      };
    });
  },

  async markAlertsRead() {
    // Snapshot so we can roll the optimistic update back if the server call
    // fails — otherwise the badge reads "all read" while the server still has
    // them unread, and they'd only reconcile on the next reload.
    const prevAlerts = get().alerts;
    const prevUnread = get().notifications.unread_alerts;
    const now = new Date().toISOString();
    set((s) => ({
      notifications: { ...s.notifications, unread_alerts: 0 },
      alerts: s.alerts.map((a) => (a.read_at ? a : { ...a, read_at: now })),
    }));
    try {
      await api.markAlertsRead();
    } catch (err) {
      set((s) => ({
        alerts: prevAlerts,
        notifications: { ...s.notifications, unread_alerts: prevUnread },
        error: errorMessage(err),
      }));
    }
  },
});

