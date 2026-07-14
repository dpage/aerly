import { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  MenuItem,
  TextField,
} from '@mui/material';

import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';
import { useOnlineStatus } from '../pwa';
import type { Plan } from '../api/types';

interface Props {
  open: boolean;
  plan: Plan;
  onClose: () => void;
}

/** Move a plan (and its parts) to another trip the viewer can edit (PRD §6.3).
 * Its own dialog, opened from the timeline tile's Move button; owner/editor
 * only, gated by the caller. Moving needs the server, so the dialog is
 * read-only offline. */
export default function MovePlanDialog({ open, plan, onClose }: Props) {
  const trips = useStore((s) => s.trips);
  const listTrips = useStore((s) => s.listTrips);
  const movePlan = useStore((s) => s.movePlan);
  const setError = useStore((s) => s.setError);
  const setNotice = useStore((s) => s.setNotice);
  const online = useOnlineStatus();

  const [target, setTarget] = useState<number | ''>('');
  const [busy, setBusy] = useState(false);

  // Reset the picker and refresh the trip list each time the dialog opens, so
  // the move targets reflect what the viewer can edit right now. The fetch needs
  // the server, so skip it (and its error churn) while offline.
  useEffect(() => {
    if (!open) return;
    setTarget('');
    if (online) void listTrips();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reset only on (re)open / plan switch
  }, [open, plan.id]);

  // A plan can only move to another trip the viewer can edit (spec §5.2).
  const moveTargets = useMemo(
    () =>
      trips.filter(
        (t) => t.id !== plan.trip_id && (t.my_role === 'owner' || t.my_role === 'editor'),
      ),
    [trips, plan.trip_id],
  );

  // Takes the destination id directly (the Move button only enables once a real
  // trip is picked), so there's no empty-target guard to reason about here. The
  // chosen id always names a current move target, hence the non-null lookup.
  const handleMove = async (toTripId: number) => {
    setBusy(true);
    try {
      const dest = moveTargets.find((t) => t.id === toTripId)!;
      await movePlan(plan.id, toTripId);
      setNotice({ message: `Moved to ${dest.name}`, severity: 'success' });
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Move plan</DialogTitle>
      <DialogContent>
        {!online ? (
          <Alert severity="info">You&apos;re offline — reconnect to move this plan.</Alert>
        ) : moveTargets.length === 0 ? (
          <DialogContentText>
            You don&apos;t have another trip to move this to. Create a trip, or ask to be made an
            editor on one, then try again.
          </DialogContentText>
        ) : (
          <>
            <DialogContentText sx={{ mb: 2 }}>
              Takes &ldquo;{plan.title || 'this plan'}&rdquo; and its parts to another trip you can
              edit.
            </DialogContentText>
            <TextField
              select
              fullWidth
              label="Move to another trip"
              value={target === '' ? '' : String(target)}
              onChange={(e) => setTarget(e.target.value === '' ? '' : Number(e.target.value))}
              slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
            >
              <MenuItem value="" disabled>
                Choose a trip…
              </MenuItem>
              {moveTargets.map((t) => (
                <MenuItem key={t.id} value={String(t.id)}>
                  {t.name}
                </MenuItem>
              ))}
            </TextField>
          </>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{online && moveTargets.length > 0 ? 'Cancel' : 'Close'}</Button>
        {online && moveTargets.length > 0 && (
          <Button
            variant="contained"
            // Disabled until a trip is picked, so target is a number here.
            onClick={() => void handleMove(target as number)}
            disabled={busy || target === ''}
          >
            Move
          </Button>
        )}
      </DialogActions>
    </Dialog>
  );
}
