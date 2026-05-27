import { StrictMode, useMemo } from 'react';
import { createRoot } from 'react-dom/client';
import { CssBaseline, ThemeProvider } from '@mui/material';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';
import { AdapterDateFns } from '@mui/x-date-pickers/AdapterDateFnsV3';

import App from './App';
import { createAppTheme, useThemeMode } from './theme';
import 'maplibre-gl/dist/maplibre-gl.css';

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('missing #root element');

// eslint-disable-next-line react-refresh/only-export-components -- bootstrap entry, not HMR-relevant
function Root() {
  const { mode } = useThemeMode();
  const theme = useMemo(() => createAppTheme(mode), [mode]);
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <LocalizationProvider dateAdapter={AdapterDateFns}>
        <App />
      </LocalizationProvider>
    </ThemeProvider>
  );
}

createRoot(rootEl).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
