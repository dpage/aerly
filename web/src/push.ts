// Browser-side Web Push: feature detection, the subscribe/unsubscribe dance
// with the PushManager, and the iOS install caveat. The store's pushSlice calls
// these; keeping the navigator/PushManager/Notification access here (behind
// small functions) keeps the slice and component testable.

import { api } from './api/client';

/** Whether this browser exposes the Push APIs at all. iOS only gained them in
 * 16.4, and only for installed PWAs, so support can be present yet still need
 * the home-screen install (see iosNeedsInstall). */
export function pushSupported(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    'serviceWorker' in navigator &&
    typeof window !== 'undefined' &&
    'PushManager' in window &&
    'Notification' in window
  );
}

/** True on iOS/iPadOS Safari that has NOT been added to the Home Screen, where
 * Web Push is unavailable until the user installs the PWA. We surface a hint
 * rather than a dead toggle. iPadOS reports a desktop UA, so a touch-capable
 * "Macintosh" counts as iOS too. */
export function iosNeedsInstall(): boolean {
  if (typeof navigator === 'undefined' || typeof window === 'undefined') return false;
  const ua = navigator.userAgent;
  const isIos = /iphone|ipad|ipod/i.test(ua) || (ua.includes('Macintosh') && 'ontouchend' in document);
  if (!isIos) return false;
  const standalone =
    window.matchMedia('(display-mode: standalone)').matches ||
    (navigator as { standalone?: boolean }).standalone === true;
  return !standalone;
}

/** The browser's current Notification permission, or 'unsupported'. */
export function currentPermission(): NotificationPermission | 'unsupported' {
  return pushSupported() ? Notification.permission : 'unsupported';
}

/** Decode a base64url VAPID public key into the Uint8Array that
 * pushManager.subscribe expects as applicationServerKey. */
export function urlBase64ToUint8Array(base64: string): Uint8Array {
  const padding = '='.repeat((4 - (base64.length % 4)) % 4);
  const b64 = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(b64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) out[i] = raw.charCodeAt(i);
  return out;
}

/** Whether a push subscription already exists for this device. */
export async function isSubscribed(): Promise<boolean> {
  if (!pushSupported()) return false;
  const reg = await navigator.serviceWorker.ready;
  const sub = await reg.pushManager.getSubscription();
  return sub !== null;
}

/** Why an enablePush attempt didn't subscribe, for the UI to explain. */
export type EnableFailure = 'unsupported' | 'disabled' | 'denied' | 'error';

export interface EnableResult {
  ok: boolean;
  reason?: EnableFailure;
}

/** Run the full enable flow: confirm support, fetch the server's VAPID key,
 * request notification permission, subscribe via the PushManager, and register
 * the subscription with the backend. Returns a tagged failure rather than
 * throwing so the UI can show the right message. */
export async function enablePush(): Promise<EnableResult> {
  if (!pushSupported()) return { ok: false, reason: 'unsupported' };
  try {
    const key = await api.getPushVapidKey();
    if (!key.enabled || !key.public_key) return { ok: false, reason: 'disabled' };

    const permission = await Notification.requestPermission();
    if (permission !== 'granted') return { ok: false, reason: 'denied' };

    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(key.public_key),
    });
    const json = sub.toJSON();
    if (!json.endpoint || !json.keys?.p256dh || !json.keys?.auth) {
      return { ok: false, reason: 'error' };
    }
    await api.subscribePush({
      endpoint: json.endpoint,
      keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
    });
    return { ok: true };
  } catch {
    return { ok: false, reason: 'error' };
  }
}

/** Unsubscribe this device: tell the backend to drop the subscription, then
 * cancel it in the browser. Idempotent and safe to call when not subscribed. */
export async function disablePush(): Promise<void> {
  if (!pushSupported()) return;
  const reg = await navigator.serviceWorker.ready;
  const sub = await reg.pushManager.getSubscription();
  if (!sub) return;
  await api.unsubscribePush(sub.endpoint);
  await sub.unsubscribe();
}
