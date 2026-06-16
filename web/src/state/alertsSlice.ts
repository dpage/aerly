import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type {
  AlertPrefs,
  FlightAlert,
  NotificationItem,
  UpdateAlertPrefsInput,
} from '../api/types';
import { errorMessage, reloadCurrent } from './helpers';
import type { StoreState } from './store';

/** State + actions for per-user alert preferences and per-plan alert opt-in
 * (spec §9).
 *
 * Wave 0b: typed fetch/set stubs. Wave 2B builds the alert-prefs UI and the
 * per-plan opt-in toggle on top of these. */
export interface AlertsSlice {
  alertPrefs: AlertPrefs | null;
  alerts: NotificationItem[];

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
  deleteAlert: (item: NotificationItem) => Promise<void>;
  clearAlerts: () => Promise<void>;
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
      // The REST endpoint returns the generic NotificationItem inbox shape,
      // which is exactly the stored list type now.
      const alerts = await api.getAlerts();
      set({ alerts });
    } catch {
      // Non-fatal: SSE / next reload recovers the inbox.
    }
  },

  applyIncomingAlert(alert) {
    // The SSE alert.created payload is still the flight-only FlightAlert; map
    // it into the generic NotificationItem shape the inbox list now stores.
    const item: NotificationItem = {
      id: alert.id,
      // The alert.created SSE event only ever carries flight_alerts rows
      // (flight changes and reminders), so it's always the 'flight' source.
      source: 'flight',
      kind: alert.kind,
      trip_id: alert.trip_id,
      plan_id: alert.plan_id,
      plan_part_id: alert.plan_part_id,
      message: alert.message,
      created_at: alert.created_at,
      read_at: alert.read_at,
    };
    set((s) => {
      // SSE can redeliver on reconnect — dedupe by (kind, id) so the inbox
      // and the unread badge don't double-count the same alert. Flight-alert ids
      // and share-notification ids come from separate DB sequences and can
      // collide, so keying on id alone would wrongly suppress a flight alert
      // whose id happens to match an already-stored share notification.
      if (s.alerts.some((a) => a.kind === item.kind && a.id === item.id)) return {};
      return {
        alerts: [item, ...s.alerts].slice(0, 50),
        // Only bump the unread badge for an actually-unread alert.
        notifications: item.read_at
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
    const prevUnreadShares = get().notifications.unread_shares;
    const now = new Date().toISOString();
    set((s) => ({
      notifications: { ...s.notifications, unread_alerts: 0, unread_shares: 0 },
      alerts: s.alerts.map((a) => (a.read_at ? a : { ...a, read_at: now })),
    }));
    try {
      await api.markAlertsRead();
    } catch (err) {
      set((s) => ({
        alerts: prevAlerts,
        notifications: {
          ...s.notifications,
          unread_alerts: prevUnread,
          unread_shares: prevUnreadShares,
        },
        error: errorMessage(err),
      }));
    }
  },

  async deleteAlert(item) {
    // Snapshot for rollback — the optimistic removal keeps the dialog and badge
    // in sync, but must reconcile with the server if the delete fails.
    const prevAlerts = get().alerts;
    const prevNotifications = get().notifications;
    set((s) => {
      // Only decrement the badge for an unread item, and only the counter that
      // backs this item's source (flight alerts vs share notifications).
      const wasUnread = !item.read_at;
      const notifications =
        wasUnread && item.source === 'flight'
          ? { ...s.notifications, unread_alerts: Math.max(0, s.notifications.unread_alerts - 1) }
          : wasUnread && item.source === 'notification'
            ? { ...s.notifications, unread_shares: Math.max(0, s.notifications.unread_shares - 1) }
            : s.notifications;
      return {
        alerts: s.alerts.filter((a) => !(a.source === item.source && a.id === item.id)),
        notifications,
      };
    });
    try {
      await api.deleteAlert(item.source, item.id);
    } catch (err) {
      set({ alerts: prevAlerts, notifications: prevNotifications, error: errorMessage(err) });
    }
  },

  async clearAlerts() {
    const prevAlerts = get().alerts;
    const prevNotifications = get().notifications;
    set((s) => ({
      alerts: [],
      notifications: { ...s.notifications, unread_alerts: 0, unread_shares: 0 },
    }));
    try {
      await api.clearAlerts();
    } catch (err) {
      set({ alerts: prevAlerts, notifications: prevNotifications, error: errorMessage(err) });
    }
  },
});
