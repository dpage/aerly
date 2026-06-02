import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Avatar,
  Box,
  Button,
  Card,
  CardActionArea,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import PlaceIcon from '@mui/icons-material/Place';

import { useStore } from '../state/store';
import type { Trip } from '../api/types';
import { userInitial, userName } from '../lib/format';
import { classifyTrip, fmtTripDates, tripSpan, type TripBucket } from '../lib/trip-format';

/** Which slice of the viewer's trips a TripList shows:
 *  - 'mine'    → trips the viewer owns (the home view, with a "New trip" action);
 *  - 'friends' → trips a friend has shared with the viewer (no create action). */
export type TripScope = 'mine' | 'friends';

/** Trip list — the redesign's home view (spec §11, PRD §6.1). Loads the
 * viewer's trips, filters to the requested `scope`, and groups them into
 * Upcoming / Happening now / Past by each trip's effective span vs now. The
 * 'mine' scope offers a "New trip" primary action; 'friends' is read-only. */
export default function TripList({ scope = 'mine' }: { scope?: TripScope }) {
  const trips = useStore((s) => s.trips);
  const loading = useStore((s) => s.tripsLoading);
  const listTrips = useStore((s) => s.listTrips);

  useEffect(() => {
    void listTrips();
  }, [listTrips]);

  // The viewer owns a trip iff their role on it is 'owner'; everything else
  // (editor / viewer) is a trip a friend shared with them.
  const scoped = useMemo(
    () => trips.filter((t) => (scope === 'mine' ? t.my_role === 'owner' : t.my_role !== 'owner')),
    [trips, scope],
  );
  const groups = useMemo(() => groupTrips(scoped), [scoped]);
  const [createOpen, setCreateOpen] = useState(false);

  const mine = scope === 'mine';

  return (
    <Box sx={{ p: 3, maxWidth: 760, mx: 'auto' }}>
      <Stack direction="row" alignItems="center" sx={{ mb: 2 }}>
        <Typography variant="h5" sx={{ flexGrow: 1 }}>
          {mine ? 'Your trips' : "Friends' trips"}
        </Typography>
        {mine && (
          <Button variant="contained" startIcon={<AddIcon />} onClick={() => setCreateOpen(true)}>
            New trip
          </Button>
        )}
      </Stack>

      {loading && scoped.length === 0 ? (
        <Box sx={{ display: 'grid', placeItems: 'center', py: 6 }}>
          <CircularProgress />
        </Box>
      ) : scoped.length === 0 ? (
        <Typography color="text.secondary">
          {mine ? (
            <>
              No trips yet. Click <strong>New trip</strong> to start planning your first one.
            </>
          ) : (
            "No trips have been shared with you yet. When a friend adds you to one of their trips, it'll appear here."
          )}
        </Typography>
      ) : (
        <Stack spacing={3}>
          {GROUP_ORDER.map(({ bucket, label }) =>
            groups[bucket].length > 0 ? (
              <TripGroup key={bucket} label={label} trips={groups[bucket]} />
            ) : null,
          )}
        </Stack>
      )}

      {mine && <NewTripDialog open={createOpen} onClose={() => setCreateOpen(false)} />}
    </Box>
  );
}

const GROUP_ORDER: Array<{ bucket: TripBucket; label: string }> = [
  { bucket: 'now', label: 'Happening now' },
  { bucket: 'upcoming', label: 'Upcoming' },
  { bucket: 'past', label: 'Past' },
];

function groupTrips(trips: Trip[]): Record<TripBucket, Trip[]> {
  const now = Date.now();
  const out: Record<TripBucket, Trip[]> = { upcoming: [], now: [], past: [] };
  for (const trip of trips) {
    out[classifyTrip(tripSpan(trip), now)].push(trip);
  }
  // Soonest-first within Upcoming/Now; most-recent-first for Past.
  const key = (t: Trip) => tripSpan(t).start ?? tripSpan(t).end ?? Infinity;
  out.now.sort((a, b) => key(a) - key(b));
  out.upcoming.sort((a, b) => key(a) - key(b));
  out.past.sort((a, b) => key(b) - key(a));
  return out;
}

function TripGroup({ label, trips }: { label: string; trips: Trip[] }) {
  return (
    <Box>
      <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
        {label}
      </Typography>
      <Stack spacing={1.5}>
        {trips.map((trip) => (
          <TripCard key={trip.id} trip={trip} />
        ))}
      </Stack>
    </Box>
  );
}

function TripCard({ trip }: { trip: Trip }) {
  const navigate = useNavigate();
  const users = useStore((s) => s.users);
  const me = useStore((s) => s.me);

  const usersById = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  // Show whose trip it is — just the owner — on trips shared with the viewer.
  // (No avatar on the viewer's own trips; editors/viewers aren't shown here.)
  const ownerMember = trip.members.find((m) => m.role === 'owner');
  const owner =
    ownerMember && ownerMember.user_id !== me?.id
      ? usersById.get(ownerMember.user_id)
      : undefined;

  return (
    <Card variant="outlined">
      <CardActionArea onClick={() => navigate(`/trips/${trip.id}`)} sx={{ p: 2 }}>
        <Stack direction="row" alignItems="flex-start" spacing={1}>
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }} noWrap>
              {trip.name}
            </Typography>
            {trip.destination && (
              <Stack direction="row" alignItems="center" spacing={0.5} sx={{ color: 'text.secondary' }}>
                <PlaceIcon fontSize="inherit" />
                <Typography variant="body2" color="text.secondary" noWrap>
                  {trip.destination}
                </Typography>
              </Stack>
            )}
            <Typography variant="caption" color="text.secondary">
              {fmtTripDates(trip)}
            </Typography>
          </Box>
          {owner && (
            <Tooltip title={`Owner: ${userName(owner)}`}>
              <Avatar src={owner.avatar_url} sx={{ width: 26, height: 26, fontSize: 12 }}>
                {userInitial(owner)}
              </Avatar>
            </Tooltip>
          )}
        </Stack>
      </CardActionArea>
    </Card>
  );
}

function NewTripDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const createTrip = useStore((s) => s.createTrip);
  const [name, setName] = useState('');
  const [destination, setDestination] = useState('');
  const [startsOn, setStartsOn] = useState('');
  const [endsOn, setEndsOn] = useState('');
  const [busy, setBusy] = useState(false);

  // Reset the fields whenever the dialog opens, so a cancelled draft doesn't
  // leak into the next open.
  useEffect(() => {
    if (open) {
      setName('');
      setDestination('');
      setStartsOn('');
      setEndsOn('');
      setBusy(false);
    }
  }, [open]);

  const submit = async () => {
    if (!name.trim() || busy) return;
    setBusy(true);
    const trip = await createTrip({
      name: name.trim(),
      destination: destination.trim() || undefined,
      starts_on: startsOn || undefined,
      ends_on: endsOn || undefined,
    });
    setBusy(false);
    if (trip) {
      onClose();
      navigate(`/trips/${trip.id}`);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>New trip</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          <TextField
            label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            required
            fullWidth
          />
          <TextField
            label="Destination"
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
            fullWidth
          />
          <Stack direction="row" spacing={2}>
            <TextField
              label="Starts"
              type="date"
              value={startsOn}
              onChange={(e) => setStartsOn(e.target.value)}
              InputLabelProps={{ shrink: true }}
              fullWidth
            />
            <TextField
              label="Ends"
              type="date"
              value={endsOn}
              onChange={(e) => setEndsOn(e.target.value)}
              InputLabelProps={{ shrink: true }}
              fullWidth
            />
          </Stack>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button variant="contained" onClick={() => void submit()} disabled={!name.trim() || busy}>
          Create
        </Button>
      </DialogActions>
    </Dialog>
  );
}
