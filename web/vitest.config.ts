import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// Separate from vite.config.ts to keep the build pipeline untouched.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      // vite-plugin-pwa's virtual module only exists during a real build.
      // Point it at a tiny stub so anything importing src/pwa.ts resolves
      // under vitest; pwa.test.tsx overrides this per-test with vi.mock.
      'virtual:pwa-register/react': fileURLToPath(
        new URL('./src/test/pwa-register-stub.ts', import.meta.url),
      ),
    },
  },
  // Define the build-commit constant so version.ts compiles under tests too
  // (empty here, matching an unstamped dev build).
  define: {
    __APP_COMMIT__: JSON.stringify(process.env.COMMIT ?? ''),
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    // userEvent.type fires one input event per keystroke; with v8 coverage
    // instrumentation each handler is meaningfully slower on CI hardware,
    // pushing the multi-input dialog tests well past the default 5s (the
    // heaviest were taking ~13-19s, and the full parallel run adds contention
    // on top). 30s gives the slow case headroom on CI hardware without masking
    // genuinely stuck tests. The very heaviest forms set their fields via
    // fireEvent.change instead of simulated typing to stay fast and deterministic.
    testTimeout: 30000,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      all: true,
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/api/types.ts',
        'src/vite-env.d.ts',
        'src/**/*.d.ts',
        'src/test/**',
        'src/**/*.test.{ts,tsx}',
        '**/*.config.*',
        // The service worker shell can only run in a ServiceWorkerGlobalScope
        // (it references `self` and imports Workbox), so it can't be unit-tested
        // here; its logic lives in src/swLogic.ts, which is covered.
        'src/sw.ts',
      ],
      thresholds: {
        perFile: true,
        statements: 90,
        branches: 90,
        functions: 90,
        lines: 90,
      },
    },
  },
});
