import type { StateCreator } from 'zustand';

import { api } from '../api/client';
import type { PushKind } from '../api/types';
import * as pushlib from '../push';
import type { StoreState } from './store';

/** State + actions for Web Push (PWA push notifications). The browser-specific
 * work lives in src/push.ts; this slice holds the UI-facing state (support,
 * permission, subscription status, per-kind prefs) and orchestrates the calls
 * the Notifications preferences section drives. */
export interface PushSlice {
  /** Whether the Push APIs exist in this browser at all. */
  pushSupported: boolean;
  /** Browser notification permission, or 'unsupported'. */
  pushPermission: NotificationPermission | 'unsupported';
  /** iOS Safari that needs the PWA installed before push works. */
  pushIosHint: boolean;
  /** Whether this device currently has a push subscription. */
  pushSubscribed: boolean;
  /** Per-kind toggles; null until loaded (only meaningful when subscribed). */
  pushPrefs: Record<PushKind, boolean> | null;
  /** True whilst an enable/disable round-trip is in flight. */
  pushBusy: boolean;
  /** Why the last enable attempt failed, for the UI to explain; null when fine. */
  pushLastError: pushlib.EnableFailure | null;

  loadPushState: () => Promise<void>;
  enablePush: () => Promise<void>;
  disablePush: () => Promise<void>;
  setPushKind: (kind: PushKind, enabled: boolean) => Promise<void>;
}

async function loadPrefs(): Promise<Record<PushKind, boolean> | null> {
  try {
    return (await api.getPushPrefs()).kinds;
  } catch {
    return null;
  }
}

export const createPushSlice: StateCreator<StoreState, [], [], PushSlice> = (set) => ({
  pushSupported: false,
  pushPermission: 'unsupported',
  pushIosHint: false,
  pushSubscribed: false,
  pushPrefs: null,
  pushBusy: false,
  pushLastError: null,

  async loadPushState() {
    const supported = pushlib.pushSupported();
    const permission = pushlib.currentPermission();
    const iosHint = pushlib.iosNeedsInstall();
    let subscribed = false;
    let prefs: Record<PushKind, boolean> | null = null;
    if (supported) {
      subscribed = await pushlib.isSubscribed();
      if (subscribed) prefs = await loadPrefs();
    }
    set({
      pushSupported: supported,
      pushPermission: permission,
      pushIosHint: iosHint,
      pushSubscribed: subscribed,
      pushPrefs: prefs,
    });
  },

  async enablePush() {
    set({ pushBusy: true, pushLastError: null });
    const res = await pushlib.enablePush();
    if (res.ok) {
      const prefs = await loadPrefs();
      set({
        pushSubscribed: true,
        pushPermission: pushlib.currentPermission(),
        pushPrefs: prefs,
        pushBusy: false,
      });
    } else {
      set({
        pushBusy: false,
        pushLastError: res.reason ?? 'error',
        pushPermission: pushlib.currentPermission(),
      });
    }
  },

  async disablePush() {
    set({ pushBusy: true });
    try {
      await pushlib.disablePush();
    } finally {
      set({ pushSubscribed: false, pushBusy: false });
    }
  },

  async setPushKind(kind, enabled) {
    const prefs = (await api.updatePushPref(kind, enabled)).kinds;
    set({ pushPrefs: prefs });
  },
});
