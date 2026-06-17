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
  const {
    needRefresh: [needRefresh],
    updateServiceWorker,
  } = useRegisterSW({
    onRegisteredSW(_swUrl, registration) {
      registrationRef.current = registration ?? undefined;
      if (!registration) return;
      // Long-open tabs (a left-open phone) won't navigate, so poll the server
      // for a fresh worker the way version.ts polls /api/version.
      window.setInterval(() => void registration.update(), UPDATE_POLL_MS);
    },
  });

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
