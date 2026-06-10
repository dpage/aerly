import { useEffect, useState } from 'react';
import { errorMessage } from '../state/helpers';
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
} from '@mui/material';

import { useStore } from '../state/store';
import type { Trip } from '../api/types';
import TagInput from './TagInput';

interface Props {
  open: boolean;
  trip: Trip;
  onClose: () => void;
  /** Called after the trip is deleted, so the caller can navigate away. */
  onDeleted: () => void;
}

/** Edit a trip's name, destination, dates and tags — and delete it. Owners and
 * editors can edit; only the owner sees Delete. */
export default function TripEditDialog({ open, trip, onClose, onDeleted }: Props) {
  const updateTrip = useStore((s) => s.updateTrip);
  const deleteTrip = useStore((s) => s.deleteTrip);
  const setTripTags = useStore((s) => s.setTripTags);
  const setError = useStore((s) => s.setError);

  const [name, setName] = useState(trip.name);
  const [destination, setDestination] = useState(trip.destination);
  const [startsOn, setStartsOn] = useState(trip.starts_on ?? '');
  const [endsOn, setEndsOn] = useState(trip.ends_on ?? '');
  const [tags, setTags] = useState<string[]>(trip.tags);
  const [busy, setBusy] = useState(false);

  // Re-sync from the trip when the dialog (re)opens.
  useEffect(() => {
    if (!open) return;
    setName(trip.name);
    setDestination(trip.destination);
    setStartsOn(trip.starts_on ?? '');
    setEndsOn(trip.ends_on ?? '');
    setTags(trip.tags);
    setBusy(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sync only on (re)open
  }, [open, trip.id]);

  const reportError = (err: unknown) => setError(errorMessage(err));

  const datesValid = !startsOn || !endsOn || startsOn <= endsOn;

  const save = async () => {
    if (!name.trim() || !datesValid) return;
    setBusy(true);
    try {
      await updateTrip(trip.id, {
        name: name.trim(),
        destination: destination.trim(),
        starts_on: startsOn || undefined,
        ends_on: endsOn || undefined,
      });
      if (JSON.stringify(tags) !== JSON.stringify(trip.tags)) {
        await setTripTags(trip.id, tags);
      }
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!window.confirm(`Delete the trip "${trip.name}" and everything in it?`)) return;
    setBusy(true);
    try {
      await deleteTrip(trip.id);
      onClose();
      onDeleted();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const canDelete = trip.my_role === 'owner';

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>Edit trip</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          <TextField
            label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
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
              slotProps={{ inputLabel: { shrink: true } }}
              error={!datesValid}
              fullWidth
            />
            <TextField
              label="Ends"
              type="date"
              value={endsOn}
              onChange={(e) => setEndsOn(e.target.value)}
              slotProps={{ inputLabel: { shrink: true } }}
              error={!datesValid}
              helperText={!datesValid ? 'End is before start' : undefined}
              fullWidth
            />
          </Stack>
          <TagInput
            value={tags}
            onChange={setTags}
            helperText="Tags group trips so people find each other — they never grant access."
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        {canDelete && (
          <Button color="error" onClick={() => void remove()} disabled={busy} sx={{ mr: 'auto' }}>
            Delete
          </Button>
        )}
        <Button onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button
          variant="contained"
          onClick={() => void save()}
          disabled={busy || !name.trim() || !datesValid}
        >
          Save
        </Button>
      </DialogActions>
    </Dialog>
  );
}
