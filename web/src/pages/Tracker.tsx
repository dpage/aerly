import { useEffect } from 'react';
import { Box, Typography } from '@mui/material';

import { useStore } from '../state/store';

/** Tracker convergence view (spec §7/§11). Wave 0b stub: wires the data flow by
 * loading the tracker on mount. Wave 1C/2 builds the convergence map, the
 * single-part focus panel, and the window sliders. */
export default function Tracker() {
  const loadTracker = useStore((s) => s.loadTracker);
  const parts = useStore((s) => s.trackerParts);

  useEffect(() => {
    void loadTracker();
  }, [loadTracker]);

  return (
    <Box sx={{ p: 3, maxWidth: 720, mx: 'auto' }}>
      <Typography variant="h5" gutterBottom>
        Tracker
      </Typography>
      <Typography color="text.secondary">
        Convergence view coming soon{parts.length ? ` (${parts.length} in flight)` : ''}.
      </Typography>
    </Box>
  );
}
