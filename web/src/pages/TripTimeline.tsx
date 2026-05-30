import { Box, Typography } from '@mui/material';

/** Default trip detail view: a day-grouped vertical list of plan parts
 * (spec §11). Wave 0b placeholder; Wave 1F builds the real timeline with day
 * grouping, local-tz headers, linked parts, hotel bands, and greyed/dismissed
 * superseded parts. */
export default function TripTimeline() {
  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h6" gutterBottom>
        Timeline
      </Typography>
      <Typography color="text.secondary">Timeline view coming soon.</Typography>
    </Box>
  );
}
