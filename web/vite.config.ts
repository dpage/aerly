import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Vite dev server proxies /api, /auth, /healthz to the Go server on :8080.
// In production the Go binary serves the built SPA directly, so the proxy is
// only used during `npm run dev`.
export default defineConfig({
  plugins: [react()],
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
