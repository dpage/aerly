// Service-worker registration + offline status for the installable PWA.
//
// vite-plugin-pwa generates the service worker (sw.js) and exposes the
// `virtual:pwa-register/react` hook below. We register with `prompt`
// semantics: when a new build ships, Workbox installs the new worker in the
// background and flips `needRefresh`, which App.tsx surfaces through the
// existing "A new version is available — Refresh" snackbar. Applying the
// update activates the waiting worker and reloads onto the new bundle — the
// same auto-update behaviour the browser already had, now also when installed
// to the home screen.

import { useEffect, useRef, useState } from 'react';
import { useRegisterSW } from 'virtual:pwa-register/react';

/** How often an open tab re-checks the server for a newer service worker. */
const UPDATE_POLL_MS = 5 * 60 * 1000;

export interface ServiceWorkerUpdate {
  /** True once a newer build has been fetched and is waiting to activate. */
  updateAvailable: boolean;
  /** Activate the waiting worker and reload onto the new build. */
  applyUpdate: () => void;
}

/** React hook wiring vite-plugin-pwa's registration into a small, testable
 * surface. Registers the worker on mount, polls for updates on a slow
 * interval, and reports when one is ready. */
export function useServiceWorkerUpdate(): ServiceWorkerUpdate {
  const registrationRef = useRef<ServiceWorkerRegistration | undefined>(undefined);
  const pollTimerRef = useRef<number | undefined>(undefined);
  const {
    needRefresh: [needRefresh],
    updateServiceWorker,
  } = useRegisterSW({
    onRegisteredSW(_swUrl, registration) {
      registrationRef.current = registration ?? undefined;
      // Drop any prior timer so a re-registration (HMR / remount) can't stack
      // pollers that each fire registration.update().
      if (pollTimerRef.current !== undefined) {
        window.clearInterval(pollTimerRef.current);
        pollTimerRef.current = undefined;
      }
      if (!registration) return;
      // Long-open tabs (a left-open phone) won't navigate, so poll the server
      // for a fresh worker the way version.ts polls /api/version.
      pollTimerRef.current = window.setInterval(
        () => void registration.update(),
        UPDATE_POLL_MS,
      );
    },
  });

  // Stop polling when the hook unmounts.
  useEffect(
    () => () => {
      if (pollTimerRef.current !== undefined) window.clearInterval(pollTimerRef.current);
    },
    [],
  );

  const applyUpdate = () => {
    if (needRefresh) {
      // A new worker is installed and waiting: skip waiting and reload onto it.
      void updateServiceWorker(true);
      return;
    }
    const registration = registrationRef.current;
    if (registration) {
      // The server reports a newer build but this worker hasn't fetched it yet.
      // Activating an old worker would just reload the stale precache, so nudge
      // it to check now; the prompt stays up and the next tap activates it.
      void registration.update();
      return;
    }
    // No service worker (unsupported / blocked): fall back to a plain reload.
    window.location.reload();
  };

  return { updateAvailable: needRefresh, applyUpdate };
}

/** The non-standard event Chromium fires when a site meets the install
 * criteria. Capturing it lets us defer the prompt to our own button. */
interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>;
}

/** True on iOS/iPadOS, where there's no install API — the user must use
 * Safari's Share → "Add to Home Screen". iPadOS 13+ reports a desktop UA, so we
 * also treat a touch-capable "Macintosh" as iOS. */
function isIos(): boolean {
  const ua = navigator.userAgent;
  return /iphone|ipad|ipod/i.test(ua) || (ua.includes('Macintosh') && 'ontouchend' in document);
}

/** True when already running as an installed app, so we don't offer to install
 * again. */
function isStandalone(): boolean {
  return (
    window.matchMedia('(display-mode: standalone)').matches ||
    (navigator as { standalone?: boolean }).standalone === true
  );
}

export interface InstallPrompt {
  /** Chromium's native install prompt is available (Android/desktop Chrome). */
  canInstall: boolean;
  /** iOS, not yet installed: show the manual "Add to Home Screen" hint. */
  iosHint: boolean;
  /** Trigger the native install prompt; no-op when unavailable. */
  promptInstall: () => void;
}

/** React hook backing the in-app install affordance. On Chromium it captures
 * `beforeinstallprompt` so a button can trigger the native prompt on demand; on
 * iOS (which has no install API) it signals that a manual hint should show.
 * Both are suppressed once the app is installed. */
export function useInstallPrompt(): InstallPrompt {
  const [deferred, setDeferred] = useState<BeforeInstallPromptEvent | null>(null);
  const [iosHint, setIosHint] = useState(false);

  useEffect(() => {
    if (isStandalone()) return;
    const onBeforeInstall = (e: Event) => {
      // Stop Chrome's default mini-infobar so our own button drives the prompt.
      e.preventDefault();
      setDeferred(e as BeforeInstallPromptEvent);
    };
    const onInstalled = () => {
      setDeferred(null);
      setIosHint(false);
    };
    window.addEventListener('beforeinstallprompt', onBeforeInstall);
    window.addEventListener('appinstalled', onInstalled);
    // iOS never fires beforeinstallprompt — decide the hint once on mount.
    setIosHint(isIos());
    return () => {
      window.removeEventListener('beforeinstallprompt', onBeforeInstall);
      window.removeEventListener('appinstalled', onInstalled);
    };
  }, []);

  const promptInstall = () => {
    if (!deferred) return;
    void deferred.prompt();
    // The event can only be used once; drop it so the button hides afterwards.
    setDeferred(null);
  };

  return { canInstall: deferred !== null, iosHint, promptInstall };
}

/** React hook tracking online/offline transitions so the UI can show an
 * unobtrusive "offline" notice. When the connection returns, the SSE stream
 * reconnects and the network-first caches refresh on their own. */
export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState(navigator.onLine);
  useEffect(() => {
    const goOnline = () => setOnline(true);
    const goOffline = () => setOnline(false);
    window.addEventListener('online', goOnline);
    window.addEventListener('offline', goOffline);
    return () => {
      window.removeEventListener('online', goOnline);
      window.removeEventListener('offline', goOffline);
    };
  }, []);
  return online;
}
