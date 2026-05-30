import { useEffect } from 'react';
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import { Box, Button, Tab, Tabs, Typography } from '@mui/material';

import { useStore } from '../state/store';

/** Trip detail layout (spec §11). Holds the Timeline / Map sub-tabs and loads
 * the trip into the store on mount; the active tab renders via the nested
 * route `<Outlet>`. Wave 0b wires loading + tab navigation; the tab bodies are
 * placeholders fleshed out in Wave 1F. */
export default function TripDetail() {
  const params = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const tripId = Number(params.id);

  const currentTrip = useStore((s) => s.currentTrip);
  const loadTrip = useStore((s) => s.loadTrip);
  const clearCurrentTrip = useStore((s) => s.clearCurrentTrip);

  useEffect(() => {
    if (!Number.isFinite(tripId)) return;
    void loadTrip(tripId);
    return () => clearCurrentTrip();
  }, [tripId, loadTrip, clearCurrentTrip]);

  const onMap = location.pathname.endsWith('/map');
  const tab = onMap ? 'map' : 'timeline';
  const title = currentTrip?.id === tripId ? currentTrip.name : `Trip #${tripId}`;

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <Box sx={{ px: 3, pt: 2, display: 'flex', alignItems: 'center', gap: 2 }}>
        <Button size="small" onClick={() => navigate('/')}>
          ← Trips
        </Button>
        <Typography variant="h5" sx={{ flexGrow: 1 }}>
          {title}
        </Typography>
      </Box>
      <Tabs
        value={tab}
        onChange={(_e, v) => navigate(v === 'map' ? `/trips/${tripId}/map` : `/trips/${tripId}`)}
        sx={{ px: 3, borderBottom: 1, borderColor: 'divider' }}
      >
        <Tab label="Timeline" value="timeline" />
        <Tab label="Map" value="map" />
      </Tabs>
      <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>
        <Outlet />
      </Box>
    </Box>
  );
}
