import { useEffect, useState } from 'react';
import { createTheme, type Theme } from '@mui/material';

export type ThemePreference = 'light' | 'dark' | 'system';
export type ThemeMode = 'light' | 'dark';

export const THEME_STORAGE_KEY = 'aerly:theme';

export function createAppTheme(mode: ThemeMode): Theme {
  return createTheme({
    palette: {
      mode,
      primary: { main: mode === 'dark' ? '#60a5fa' : '#1f5fa8' },
      secondary: { main: '#d97706' },
      ...(mode === 'light'
        ? { background: { default: '#f5f6fa' } }
        : { background: { default: '#0d1117', paper: '#161b22' } }),
    },
    shape: { borderRadius: 8 },
    typography: {
      fontFamily:
        'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    },
    components: {
      // Safari renders the outlined-input notch unreliably: the legend
      // sometimes stays at max-width:0.01px even when the label is
      // shrunk, so the focused border draws straight through the label.
      // Trying to coax MUI's variant matching into agreement is brittle
      // (font-metric drift, sibling-selector mismatches inside Autocomplete,
      // etc.). Skip that whole song-and-dance:
      //   - Collapse the fieldset's legend to zero width so it never
      //     opens a notch. The border is then continuous along the top.
      //   - Give the shrunk InputLabel a solid background that matches
      //     the input's container, plus a hair of horizontal padding,
      //     so the label sits ON TOP of the border and visually covers
      //     the line behind it. The "notch" effect is now painted, not
      //     measured.
      // This matches the workaround used in the pgEdge AI DBA Workbench
      // codebase, which hit the same Safari behaviour.
      MuiOutlinedInput: {
        styleOverrides: {
          notchedOutline: {
            '& legend': {
              width: 0,
            },
          },
        },
      },
      MuiInputLabel: {
        styleOverrides: {
          root: {
            '&.MuiInputLabel-shrink.MuiInputLabel-outlined': {
              backgroundColor:
                mode === 'dark' ? '#161b22' : '#ffffff',
              // In dark mode, the surrounding Dialog/Paper paints
              // background.paper plus an elevation-24 white overlay
              // (alpha 0.165) — match it so the label blends in.
              ...(mode === 'dark' && {
                backgroundImage:
                  'linear-gradient(rgba(255, 255, 255, 0.165), rgba(255, 255, 255, 0.165))',
              }),
              paddingLeft: 4,
              paddingRight: 4,
              marginLeft: -2,
            },
          },
        },
      },
    },
  });
}

export function loadPreference(): ThemePreference {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY);
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw;
  } catch {
    // Ignore storage access failures and fall back to system.
  }
  return 'system';
}

function systemPrefersDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

export function resolveMode(preference: ThemePreference, systemDark: boolean): ThemeMode {
  if (preference === 'light') return 'light';
  if (preference === 'dark') return 'dark';
  return systemDark ? 'dark' : 'light';
}

// Module-level pub/sub keeps every useThemeMode() consumer in sync — the
// ThemeProvider at the root and the picker inside the account menu both read
// the same value without needing a React context.
type Listener = (p: ThemePreference) => void;
const listeners = new Set<Listener>();
let currentPreference: ThemePreference | null = null;

function getPreference(): ThemePreference {
  if (currentPreference === null) currentPreference = loadPreference();
  return currentPreference;
}

export function setThemePreference(p: ThemePreference): void {
  currentPreference = p;
  try {
    localStorage.setItem(THEME_STORAGE_KEY, p);
  } catch {
    // Ignore persistence failures; keep runtime preference in sync.
  }
  for (const l of listeners) l(p);
}

export function useThemeMode(): {
  preference: ThemePreference;
  mode: ThemeMode;
  setPreference: (p: ThemePreference) => void;
} {
  const [preference, setPref] = useState<ThemePreference>(getPreference);
  const [systemDark, setSystemDark] = useState<boolean>(systemPrefersDark);

  useEffect(() => {
    const onChange: Listener = (p) => setPref(p);
    listeners.add(onChange);
    return () => {
      listeners.delete(onChange);
    };
  }, []);

  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, []);

  return {
    preference,
    mode: resolveMode(preference, systemDark),
    setPreference: setThemePreference,
  };
}
