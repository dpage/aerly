import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { VitePWA } from 'vite-plugin-pwa';
import mockApiPlugin from './mock/vite-plugin-mock-api';

// Vite dev server proxies /api, /auth, /healthz to the Go server on :8080.
// In production the Go binary serves the built SPA directly, so the proxy is
// only used during `npm run dev`.
export default defineConfig({
  plugins: [
    react(),
    // MOCK=1 npm run dev serves the UI from in-memory fixtures (no Go backend):
    // the plugin must come before the /api proxy so its middleware answers first.
    // Only registered when MOCK=1, so a normal `npm run dev` is untouched.
    ...(process.env.MOCK === '1' ? [mockApiPlugin()] : []),
    // Progressive Web App: precaches the built shell so Aerly installs to the
    // home screen on iOS and Android and opens offline, and runtime-caches the
    // itinerary API + map tiles so trips stay readable with no connection.
    //
    // `registerType: 'prompt'` keeps us in control of activation: the existing
    // "A new version is available — Refresh" snackbar (src/App.tsx) drives the
    // update instead of a surprise auto-reload. injectRegister is off because we
    // register through the React hook in src/pwa.ts.
    VitePWA({
      registerType: 'prompt',
      injectRegister: false,
      // Don't run the service worker under `npm run dev`; it caches aggressively
      // and fights Vite's HMR. It's exercised only in the production build.
      devOptions: { enabled: false },
      manifest: {
        name: 'Aerly',
        short_name: 'Aerly',
        description: "Track your friends' flights on a live world map.",
        // Dark field matches the brand mark (designed to read on dark) and the
        // dark theme's page background; shown as the splash while loading.
        theme_color: '#0d1117',
        background_color: '#0d1117',
        display: 'standalone',
        orientation: 'any',
        scope: '/',
        start_url: '/',
        icons: [
          { src: '/pwa-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/pwa-512.png', sizes: '512x512', type: 'image/png' },
          {
            src: '/pwa-maskable-512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        // Keep the Workbox runtime inlined into a single sw.js so there's one
        // service-worker file to embed and cache-bust (see internal/handlers/spa.go).
        inlineWorkboxRuntime: true,
        cleanupOutdatedCaches: true,
        // Take control of the page as soon as the worker activates (including
        // its first install), so API responses are cached during that first
        // session too. Without this the worker doesn't intercept fetches until
        // the next load, so a trip opened right after install is never cached
        // and isn't readable offline. skipWaiting stays off (the update prompt
        // drives activation), which clientsClaim doesn't affect.
        clientsClaim: true,
        // Client-side routes fall back to the precached index.html when offline,
        // EXCEPT the server APIs — those must hit the network (or their runtime
        // cache), never the SPA shell.
        navigateFallback: 'index.html',
        navigateFallbackDenylist: [/^\/api/, /^\/auth/, /^\/healthz/],
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff2,webmanifest}'],
        runtimeCaching: [
          {
            // Itinerary + account data: serve fresh when online, fall back to the
            // last response offline. The live SSE stream (/api/events) is excluded
            // so the service worker never buffers and stalls it.
            urlPattern: ({ url, request, sameOrigin }) =>
              sameOrigin &&
              request.method === 'GET' &&
              url.pathname.startsWith('/api/') &&
              url.pathname !== '/api/events',
            handler: 'NetworkFirst',
            options: {
              cacheName: 'aerly-api',
              networkTimeoutSeconds: 5,
              expiration: { maxEntries: 200, maxAgeSeconds: 7 * 24 * 60 * 60 },
              cacheableResponse: { statuses: [200] },
            },
          },
          {
            // OpenStreetMap raster tiles — cache-first so recently viewed maps
            // render offline; bounded so the cache can't grow without limit.
            urlPattern: ({ url }) => url.hostname === 'tile.openstreetmap.org',
            handler: 'CacheFirst',
            options: {
              cacheName: 'aerly-map-tiles',
              expiration: { maxEntries: 500, maxAgeSeconds: 30 * 24 * 60 * 60 },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
          {
            // MapLibre glyph/sprite assets for the base style.
            urlPattern: ({ url }) => url.hostname === 'demotiles.maplibre.org',
            handler: 'CacheFirst',
            options: {
              cacheName: 'aerly-map-assets',
              expiration: { maxEntries: 100, maxAgeSeconds: 30 * 24 * 60 * 60 },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
        ],
      },
    }),
  ],
  // Bake the build commit into the bundle so the running UI knows which version
  // it is and can prompt a refresh when the server reports a newer one. The
  // Makefile passes COMMIT (the same value stamped into the Go binary); it's
  // empty under `npm run dev`, where the update check stays dormant.
  define: {
    __APP_COMMIT__: JSON.stringify(process.env.COMMIT ?? ''),
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // sourcemap: false in production builds. Rollup buffers the entire
    // source map in memory as it emits each chunk, and for our bundle
    // (~1.5MB minified with maplibre-gl + MUI + React in it) the source
    // maps reach several MB of JSON — enough to OOM-kill `vite build` on
    // small VPS boxes during the "rendering chunks" step.
    //
    // Dev still has full sourcemaps automatically (Vite generates them
    // on the fly when serving via `npm run dev`).
    sourcemap: false,
    // Skip the "computing gzip size" build-log line for the same reason —
    // it gzips the whole bundle in memory just to print one number.
    // Assets are served gzip-compressed at runtime regardless.
    reportCompressedSize: false,
  },
});
