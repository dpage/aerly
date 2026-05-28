import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

import {
  createAppTheme,
  loadPreference,
  resolveMode,
  setThemePreference,
  THEME_STORAGE_KEY,
  useThemeMode,
} from './theme';

describe('createAppTheme', () => {
  it('builds a light palette with the configured colours', () => {
    const theme = createAppTheme('light');
    expect(theme.palette.mode).toBe('light');
    expect(theme.palette.primary.main).toBe('#1f5fa8');
    expect(theme.palette.secondary.main).toBe('#d97706');
    expect(theme.palette.background.default).toBe('#f5f6fa');
    expect(theme.shape.borderRadius).toBe(8);
    expect(theme.typography.fontFamily).toContain('system-ui');
  });

  it('builds a dark palette with a dark default background', () => {
    const theme = createAppTheme('dark');
    expect(theme.palette.mode).toBe('dark');
    expect(theme.palette.primary.main).toBe('#60a5fa');
    expect(theme.palette.background.default).toBe('#0d1117');
    expect(theme.palette.background.paper).toBe('#161b22');
  });
});

describe('resolveMode', () => {
  it('honours explicit light/dark preferences regardless of system', () => {
    expect(resolveMode('light', true)).toBe('light');
    expect(resolveMode('light', false)).toBe('light');
    expect(resolveMode('dark', true)).toBe('dark');
    expect(resolveMode('dark', false)).toBe('dark');
  });
  it('follows system preference when set to system', () => {
    expect(resolveMode('system', true)).toBe('dark');
    expect(resolveMode('system', false)).toBe('light');
  });
});

describe('loadPreference', () => {
  beforeEach(() => {
    localStorage.clear();
  });
  it('defaults to system when no value is stored', () => {
    expect(loadPreference()).toBe('system');
  });
  it('returns the stored value when it is recognised', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'dark');
    expect(loadPreference()).toBe('dark');
  });
  it('falls back to system when the stored value is unrecognised', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'sepia');
    expect(loadPreference()).toBe('system');
  });
});

describe('useThemeMode', () => {
  let mqlListeners: Array<(e: MediaQueryListEvent) => void>;
  let prefersDark: boolean;

  beforeEach(() => {
    localStorage.clear();
    // Force the module-level cache to re-read from localStorage on the next
    // hook invocation by setting then resetting through the public setter.
    setThemePreference('system');
    localStorage.clear();
    mqlListeners = [];
    prefersDark = false;
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      matches: query.includes('dark') ? prefersDark : false,
      media: query,
      onchange: null,
      addListener: vi.fn((l: (e: MediaQueryListEvent) => void) => mqlListeners.push(l)),
      removeListener: vi.fn(),
      addEventListener: vi.fn((_: string, l: (e: MediaQueryListEvent) => void) =>
        mqlListeners.push(l),
      ),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));
  });

  afterEach(() => {
    setThemePreference('system');
    localStorage.clear();
  });

  it('defaults to system + light mode when no preference is stored', () => {
    const { result } = renderHook(() => useThemeMode());
    expect(result.current.preference).toBe('system');
    expect(result.current.mode).toBe('light');
  });

  it('resolves to dark when system prefers dark', () => {
    prefersDark = true;
    const { result } = renderHook(() => useThemeMode());
    expect(result.current.mode).toBe('dark');
  });

  it('persists the chosen preference and updates the mode', () => {
    const { result } = renderHook(() => useThemeMode());
    act(() => result.current.setPreference('dark'));
    expect(result.current.preference).toBe('dark');
    expect(result.current.mode).toBe('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
  });

  it('keeps separate hook instances in sync via the module-level subscription', () => {
    const a = renderHook(() => useThemeMode());
    const b = renderHook(() => useThemeMode());
    act(() => a.result.current.setPreference('dark'));
    expect(b.result.current.preference).toBe('dark');
    expect(b.result.current.mode).toBe('dark');
  });

  it('reacts to system theme changes when preference is system', () => {
    const { result } = renderHook(() => useThemeMode());
    expect(result.current.mode).toBe('light');
    act(() => {
      for (const l of mqlListeners) l({ matches: true } as MediaQueryListEvent);
    });
    expect(result.current.mode).toBe('dark');
  });
});
