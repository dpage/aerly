import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import App from './App';
import 'maplibre-gl/dist/maplibre-gl.css';

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('missing #root element');

createRoot(rootEl).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
