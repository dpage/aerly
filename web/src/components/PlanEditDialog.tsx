import { useEffect, useMemo, useState } from 'react';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from '@mui/material';

import type { Plan } from '../api/types';
import { useStore } from '../state/store';

interface Props {
  open: boolean;
  plan: Plan;
  onClose: () => void;
}

/** Edit a plan's title / confirmation / notes (PRD §6.4) and move it to
 * another trip the viewer can edit (PRD §6.3). Owner/editor only — gated by
 * the caller. */
export default function PlanEditDialog({ open, plan, onClose }: Props) {
  const trips = useStore((s) => s.trips);
  const listTrips = useStore((s) => s.listTrips);
  const updatePlan = useStore((s) => s.updatePlan);
  const movePlan = useStore((s) => s.movePlan);
  const setError = useStore((s) => s.setError);

  const [title, setTitle] = useState(plan.title);
  const [confRef, setConfRef] = useState(plan.confirmation_ref);
  const [notes, setNotes] = useState(plan.notes);
  const [moveTarget, setMoveTarget] = useState<number | ''>('');
  const [busy, setBusy] = useState(false);

  // Re-sync the form when the dialog (re)opens or switches plans, and refresh
  // the trip list so the move targets reflect what the viewer can edit now.
  // Not keyed on plan.* fields so an in-flight refetch can't clobber edits.
  useEffect(() => {
    if (!open) return;
    setTitle(plan.title);
    setConfRef(plan.confirmation_ref);
    setNotes(plan.notes);
    setMoveTarget('');
    void listTrips();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sync only on (re)open / plan switch
  }, [open, plan.id]);

  // A plan can only move to another trip the viewer can edit (spec §5.2).
  const moveTargets = useMemo(
    () =>
      trips.filter(
        (t) => t.id !== plan.trip_id && (t.my_role === 'owner' || t.my_role === 'editor'),
      ),
    [trips, plan.trip_id],
  );

  const reportError = (err: unknown) =>
    setError(err instanceof Error ? err.message : String(err));

  const handleSave = async () => {
    setBusy(true);
    try {
      await updatePlan(plan.id, {
        title: title.trim(),
        confirmation_ref: confRef.trim(),
        notes,
      });
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleMove = async () => {
    if (moveTarget === '') return;
    setBusy(true);
    try {
      await movePlan(plan.id, moveTarget);
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Edit plan</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          <TextField
            label="Title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            fullWidth
          />
          <TextField
            label="Confirmation ref"
            value={confRef}
            onChange={(e) => setConfRef(e.target.value)}
            fullWidth
          />
          <TextField
            label="Notes"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            fullWidth
            multiline
            minRows={2}
          />

          {moveTargets.length > 0 && (
            <Box>
              <Typography variant="subtitle2" sx={{ mb: 1 }}>
                Move to another trip
              </Typography>
              <Stack direction="row" spacing={1} alignItems="flex-start">
                <TextField
                  select
                  size="small"
                  label="Move to another trip"
                  fullWidth
                  value={moveTarget === '' ? '' : String(moveTarget)}
                  onChange={(e) =>
                    setMoveTarget(e.target.value === '' ? '' : Number(e.target.value))
                  }
                  helperText="Takes the plan and its parts to another trip you can edit."
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
                <Button
                  variant="outlined"
                  onClick={() => void handleMove()}
                  disabled={busy || moveTarget === ''}
                  sx={{ mt: 0.5 }}
                >
                  Move
                </Button>
              </Stack>
            </Box>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="contained"
          onClick={() => void handleSave()}
          disabled={busy || !title.trim()}
        >
          Save
        </Button>
      </DialogActions>
    </Dialog>
  );
}
