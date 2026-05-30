import { Box, Typography } from '@mui/material';

/** Secondary trip detail tab: the trip's parts on a MapLibre map (spec §11).
 * Wave 0b placeholder; Wave 1F reuses `FlightMap`/MapLibre here. */
export default function TripMap() {
  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h6" gutterBottom>
        Map
      </Typography>
      <Typography color="text.secondary">Trip map coming soon.</Typography>
    </Box>
  );
}
