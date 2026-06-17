// Test stub for vite-plugin-pwa's `virtual:pwa-register/react` module, which
// only exists during a real Vite build. Aliased in vitest.config.ts so any
// module importing src/pwa.ts resolves under tests. It reports no pending
// update; pwa.test.tsx replaces it with vi.mock to drive the update paths.

export interface RegisterSWOptions {
  onRegisteredSW?: (swUrl: string, registration?: ServiceWorkerRegistration) => void;
  onRegisterError?: (error: unknown) => void;
  onNeedRefresh?: () => void;
  onOfflineReady?: () => void;
}

export function useRegisterSW(_options: RegisterSWOptions = {}) {
  return {
    needRefresh: [false, () => {}] as [boolean, (value: boolean) => void],
    offlineReady: [false, () => {}] as [boolean, (value: boolean) => void],
    updateServiceWorker: async (_reloadPage?: boolean) => {},
  };
}
