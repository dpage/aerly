import { useEffect } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import {
  Box,
  CircularProgress,
  List,
  ListItemButton,
  ListItemText,
  Typography,
} from '@mui/material';

import { useStore } from '../state/store';

/** Trip list — the redesign's home view (spec §11). Wave 0b stub: it wires the
 * data flow by calling the `listTrips` store action on mount and rendering a
 * flat list. Wave 1F adds the Upcoming / Happening now / Past grouping. */
export default function TripList() {
  const trips = useStore((s) => s.trips);
  const loading = useStore((s) => s.tripsLoading);
  const listTrips = useStore((s) => s.listTrips);

  useEffect(() => {
    void listTrips();
  }, [listTrips]);

  return (
    <Box sx={{ p: 3, maxWidth: 720, mx: 'auto' }}>
      <Typography variant="h5" gutterBottom>
        Your trips
      </Typography>
      {loading && trips.length === 0 ? (
        <Box sx={{ display: 'grid', placeItems: 'center', py: 6 }}>
          <CircularProgress />
        </Box>
      ) : trips.length === 0 ? (
        <Typography color="text.secondary">
          No trips yet. Trip grouping and the “New trip” action are coming soon.
        </Typography>
      ) : (
        <List>
          {trips.map((trip) => (
            <ListItemButton key={trip.id} component={RouterLink} to={`/trips/${trip.id}`}>
              <ListItemText primary={trip.name} secondary={trip.destination || undefined} />
            </ListItemButton>
          ))}
        </List>
      )}
    </Box>
  );
}
