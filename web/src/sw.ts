/// <reference lib="webworker" />
//
// Aerly service worker (injectManifest strategy). We own this source — rather
// than letting vite-plugin-pwa generate it — so we can add Web Push handlers
// alongside the offline caching. The caching routes below are a faithful port
// of the previous generateSW `workbox.runtimeCaching` config; the push and
// notificationclick handlers are the new part. All non-trivial logic lives in
// ./swLogic so it can be unit-tested; this file is excluded from coverage as it
// can only run in a real service-worker context.

import { CacheableResponsePlugin } from 'workbox-cacheable-response';
import { clientsClaim } from 'workbox-core';
import { ExpirationPlugin } from 'workbox-expiration';
import { cleanupOutdatedCaches, createHandlerBoundToURL, precacheAndRoute } from 'workbox-precaching';
import { NavigationRoute, registerRoute } from 'workbox-routing';
import { CacheFirst, NetworkFirst } from 'workbox-strategies';

import {
  focusOrOpen,
  hasFocusedClient,
  parsePushPayload,
  toNotification,
  urlForNotification,
  type FocusableClient,
} from './swLogic';

declare let self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<{ url: string; revision: string | null }>;
};

// Precache the built shell (vite-plugin-pwa injects the manifest here), drop
// caches from superseded builds, and take control of open pages on activation
// — matching the previous generateSW `cleanupOutdatedCaches` + `clientsClaim`.
precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();
clientsClaim();

// The update prompt (src/pwa.ts) drives activation: when the user taps
// "Refresh", pwa-register posts SKIP_WAITING to the waiting worker. generateSW
// wired this for us; under injectManifest we handle it ourselves.
self.addEventListener('message', (event) => {
  if ((event.data as { type?: string } | undefined)?.type === 'SKIP_WAITING') {
    void self.skipWaiting();
  }
});

// Itinerary + account data: fresh when online, last response when offline. The
// live SSE stream is excluded so the worker never buffers and stalls it.
registerRoute(
  ({ url, request, sameOrigin }) =>
    sameOrigin &&
    request.method === 'GET' &&
    url.pathname.startsWith('/api/') &&
    url.pathname !== '/api/events',
  new NetworkFirst({
    cacheName: 'aerly-api',
    networkTimeoutSeconds: 5,
    plugins: [
      new ExpirationPlugin({ maxEntries: 200, maxAgeSeconds: 7 * 24 * 60 * 60 }),
      new CacheableResponsePlugin({ statuses: [200] }),
    ],
  }),
);

// OpenStreetMap raster tiles — cache-first so recently viewed maps render
// offline; bounded so the cache can't grow without limit.
registerRoute(
  ({ url }) => url.hostname === 'tile.openstreetmap.org',
  new CacheFirst({
    cacheName: 'aerly-map-tiles',
    plugins: [
      new ExpirationPlugin({ maxEntries: 500, maxAgeSeconds: 30 * 24 * 60 * 60 }),
      new CacheableResponsePlugin({ statuses: [0, 200] }),
    ],
  }),
);

// MapLibre glyph/sprite assets for the base style.
registerRoute(
  ({ url }) => url.hostname === 'demotiles.maplibre.org',
  new CacheFirst({
    cacheName: 'aerly-map-assets',
    plugins: [
      new ExpirationPlugin({ maxEntries: 100, maxAgeSeconds: 30 * 24 * 60 * 60 }),
      new CacheableResponsePlugin({ statuses: [0, 200] }),
    ],
  }),
);

// Client-side routes fall back to the precached index.html when offline, EXCEPT
// the server APIs — those must hit the network (or their runtime cache).
registerRoute(
  new NavigationRoute(createHandlerBoundToURL('index.html'), {
    denylist: [/^\/api/, /^\/auth/, /^\/healthz/],
  }),
);

// Web Push: render the payload as an OS notification, unless the app is already
// focused (it's been updated live over SSE).
self.addEventListener('push', (event) => {
  const payload = parsePushPayload(event.data?.text());
  event.waitUntil(
    (async () => {
      const clients = (await self.clients.matchAll({
        type: 'window',
        includeUncontrolled: true,
      })) as unknown as FocusableClient[];
      if (hasFocusedClient(clients)) return;
      const { title, options } = toNotification(payload);
      await self.registration.showNotification(title, options);
    })(),
  );
});

// Clicking a notification focuses an open window (routing it to the deep link)
// or opens a new one.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = urlForNotification(event.notification.data);
  event.waitUntil(
    (async () => {
      const clients = (await self.clients.matchAll({
        type: 'window',
        includeUncontrolled: true,
      })) as unknown as FocusableClient[];
      await focusOrOpen(url, clients, (u) => self.clients.openWindow(u) as Promise<unknown>);
    })(),
  );
});
