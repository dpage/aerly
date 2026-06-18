import { useEffect, useMemo, useState } from 'react';
import { errorMessage } from '../state/helpers';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Paper,
  Stack,
  Tab,
  Tabs,
  Typography,
} from '@mui/material';

import { api } from '../api/client';
import type { Flight, Trip } from '../api/types';
import { useStore } from '../state/store';
import { computeStats, type Bucket, type Stats } from '../state/stats';

interface Props {
  open: boolean;
  onClose: () => void;
}

type TabKey = 'flown' | 'upcoming';

export default function StatsDialog({ open, onClose }: Props) {
  const me = useStore((s) => s.me);
  const [flights, setFlights] = useState<Flight[] | null>(null);
  const [trips, setTrips] = useState<Trip[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<TabKey>('flown');

  useEffect(() => {
    if (!open) {
      setFlights(null);
      setTrips([]);
      setError(null);
      setTab('flown');
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    // Flights are the primary data; trips only feed the "countries visited"
    // highlight, so a trips failure shouldn't sink the whole dialog — fall
    // back to an empty list and carry on.
    Promise.all([
      api.listFlights({ showOld: true }),
      api.listTrips().catch(() => [] as Trip[]),
    ])
      .then(([flightRows, tripRows]) => {
        if (!cancelled) {
          setFlights(flightRows);
          setTrips(tripRows);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errorMessage(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  const stats: Stats | null = useMemo(() => {
    if (!flights || !me) return null;
    return computeStats(flights, me.id, trips);
  }, [flights, trips, me]);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Statistics</DialogTitle>
      <DialogContent dividers>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 2 }}>
          Your travel so far, current and archived.
        </Typography>
        {loading && (
          <Box sx={{ display: 'grid', placeItems: 'center', minHeight: 200 }}>
            <CircularProgress />
          </Box>
        )}
        {error && (
          <Alert severity="error" role="alert">
            {error}
          </Alert>
        )}
        {stats && !loading && !error && (
          <>
            <Tabs
              value={tab}
              onChange={(_, v) => setTab(v as TabKey)}
              sx={{ borderBottom: 1, borderColor: 'divider' }}
            >
              <Tab label="Past" value="flown" />
              <Tab label="Upcoming" value="upcoming" />
            </Tabs>
            {/* The bucket tiles share a border with the tabs so they read as a
                single tabbed panel, distinct from the Highlights card below. */}
            <Paper
              variant="outlined"
              sx={{ p: 2, borderTop: 0, borderTopLeftRadius: 0, borderTopRightRadius: 0 }}
            >
              <BucketTiles
                bucket={tab === 'flown' ? stats.flown : stats.upcoming}
                upcoming={tab === 'upcoming'}
              />
            </Paper>
            <Typography variant="overline" sx={{ display: 'block', mt: 3, mb: 1 }}>
              Highlights
            </Typography>
            <Paper variant="outlined" sx={{ p: 2 }}>
              <HighlightTiles stats={stats} />
            </Paper>
            {stats.excluded > 0 && (
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 3 }}>
                {stats.excluded} cancelled/diverted flight{stats.excluded === 1 ? '' : 's'} not
                counted.
              </Typography>
            )}
          </>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}

function BucketTiles({ bucket, upcoming }: { bucket: Bucket; upcoming: boolean }) {
  return (
    <Stack spacing={1.5}>
      <Tile label="Flights" value={String(bucket.count)} />
      <Tile label="Distance" value={formatDistance(bucket.miles)} />
      <Tile label="Time in the air" value={formatDuration(bucket.minutes)} />
      <Tile
        label={upcoming ? 'Airports to be visited' : 'Airports visited'}
        value={String(bucket.airports)}
      />
    </Stack>
  );
}

function HighlightTiles({ stats }: { stats: Stats }) {
  const { longest, mostVisited, distinctAirlines, mostAirline, earthLaps } = stats.highlight;
  return (
    <Stack spacing={1.5}>
      <Tile label="Countries visited" value={String(stats.countries)} />
      <Tile
        label="Longest flight"
        value={
          longest
            ? `${longest.ident} · ${longest.origin} → ${longest.dest} · ${formatMiles(longest.miles)}`
            : '—'
        }
      />
      <Tile
        label="Most-visited airport"
        value={mostVisited ? `${mostVisited.iata} (${mostVisited.count} visits)` : '—'}
      />
      <Tile
        label="Most-used airline"
        value={
          mostAirline
            ? `${mostAirline.code} (${mostAirline.count} flight${mostAirline.count === 1 ? '' : 's'})`
            : '—'
        }
      />
      <Tile label="Distinct airlines" value={String(distinctAirlines)} />
      {earthLaps >= 0.1 && (
        <Tile label="Around the Earth" value={`${earthLaps.toFixed(1)}× laps`} />
      )}
    </Stack>
  );
}

function Tile({ label, value }: { label: string; value: string }) {
  return (
    <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
      <Typography variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Typography variant="body1" sx={{ fontVariantNumeric: 'tabular-nums' }}>
        {value}
      </Typography>
    </Box>
  );
}

function formatDistance(miles: number): string {
  if (miles <= 0) return '0 mi';
  const km = miles * 1.609344;
  return `${formatMiles(miles)} · ${formatKm(km)}`;
}

function formatMiles(miles: number): string {
  return `${Math.round(miles).toLocaleString()} mi`;
}

function formatKm(km: number): string {
  return `${Math.round(km).toLocaleString()} km`;
}

function formatDuration(minutes: number): string {
  if (minutes <= 0) return '0h';
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m}m`;
  if (m === 0) return `${h}h`;
  return `${h}h ${m}m`;
}
