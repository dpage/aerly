import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// Pin the default locale for date/time formatting so assertions are
// deterministic regardless of the runner's locale (CI defaults to en-US,
// dev machines often en-GB — which silently flipped "12 Oct 2026" to
// "Oct 12, 2026" and reddened CI). Production still uses the user's locale:
// the app calls toLocale*(undefined, …) on purpose, and only the *default*
// (undefined locale) is overridden here — explicit locales (e.g. 'en-US' in
// tzAbbrev, 'en-CA' for sort keys) are left untouched.
const TEST_LOCALE = 'en-GB';
for (const method of ['toLocaleDateString', 'toLocaleTimeString', 'toLocaleString'] as const) {
  const original = Date.prototype[method];
  Date.prototype[method] = function (
    this: Date,
    locales?: Intl.LocalesArgument,
    options?: Intl.DateTimeFormatOptions,
  ) {
    return original.call(this, locales ?? TEST_LOCALE, options);
  };
}

// jsdom 28 removed the built-in in-memory localStorage implementation.
// Provide a simple Map-backed mock so tests can call getItem/setItem/clear/etc.
const _localStorageStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (key: string) => _localStorageStore.get(key) ?? null,
  setItem: (key: string, value: string) => { _localStorageStore.set(key, String(value)); },
  removeItem: (key: string) => { _localStorageStore.delete(key); },
  clear: () => { _localStorageStore.clear(); },
  key: (index: number) => [..._localStorageStore.keys()][index] ?? null,
  get length() { return _localStorageStore.size; },
});

// Mutable module-level flag for matchMedia.matches — flip it for narrow/wide
// layout tests via setMatchMedia().
let matchMediaMatches = false;

export function setMatchMedia(matches: boolean): void {
  matchMediaMatches = matches;
}

window.matchMedia = vi.fn().mockImplementation((query: string) => ({
  matches: matchMediaMatches,
  media: query,
  onchange: null,
  addListener: vi.fn(),
  removeListener: vi.fn(),
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  dispatchEvent: vi.fn(),
}));

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;

afterEach(() => {
  cleanup();
  setMatchMedia(false);
  _localStorageStore.clear();
});
