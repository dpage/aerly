import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Vite dev server proxies /api, /auth, /healthz to the Go server on :8080.
// In production the Go binary serves the built SPA directly, so the proxy is
// only used during `npm run dev`.
export default defineConfig({
  plugins: [react()],
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
