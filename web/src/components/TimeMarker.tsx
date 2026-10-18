import { useState } from 'react';
import type { MouseEvent } from 'react';
import { Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Typography } from '@mui/material';
import type { PlanPart } from '../api/types';
import { useStore } from '../state/store';
import { errorMessage } from '../state/helpers';
import { needsZone, zoneGuessed } from '../lib/time';
import { endBlockOf, startBlockOf, zoneConfirmPatch } from '../lib/trip-format';
import TimezoneSelect from './TimezoneSelect';

interface Props {
  part: PlanPart;
  which: 'start' | 'end';
  /** Whether the viewer may edit the plan; a viewer sees the marker as text. */
  canEdit: boolean;
}

/** The note beside one end's time: a derived arrival's basis, a guessed zone
 * to confirm, or a missing zone to set. Tapping a zone note opens a picker and
 * confirms through the ordinary part PATCH. Renders nothing when all is known. */
export default function TimeMarker({ part, which, canEdit }: Props) {
  const updatePlanPart = useStore((s) => s.updatePlanPart);
  const setError = useStore((s) => s.setError);
  const [open, setOpen] = useState(false);
  const [tz, setTz] = useState('');
  const [busy, setBusy] = useState(false);

  const b = which === 'start' ? startBlockOf(part) : endBlockOf(part);
  if (!b) return null;
  const derived = b.kind === 'derived';
  const guessed = zoneGuessed(b);
  const missing = needsZone(b);
  if (!derived && !guessed && !missing) return null;

  const label = guessed ? (canEdit ? 'zone guessed, tap to confirm' : 'zone guessed') : 'needs a time zone';
  // The tile toggles on click, so a marker tap must not reach it.
  const stop = (e: MouseEvent) => e.stopPropagation();
  const openPicker = (e: MouseEvent) => {
    stop(e);
    setTz(b.tz);
    setOpen(true);
  };
  const confirm = async () => {
    setBusy(true);
    try {
      await updatePlanPart(part.id, zoneConfirmPatch(part, which, tz));
      setOpen(false);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Box component="span" sx={{ display: 'block' }}>
      {derived && (
        <Typography variant="caption" color="text.secondary" component="span">
          (based on departure delay)
        </Typography>
      )}
      {(guessed || missing) &&
        (canEdit ? (
          <Button
            size="small"
            variant="text"
            color="warning"
            onClick={openPicker}
            sx={{ p: 0, ml: derived ? 1 : 0, minWidth: 0, textTransform: 'none', fontSize: 12 }}
          >
            {label}
          </Button>
        ) : (
          <Typography variant="caption" color="warning.main" component="span" sx={{ ml: derived ? 1 : 0 }}>
            {label}
          </Typography>
        ))}
      <Dialog open={open} onClose={() => setOpen(false)} onClick={stop}>
        <DialogTitle>Confirm the time zone</DialogTitle>
        <DialogContent>
          <TimezoneSelect
            value={tz}
            onChange={setTz}
            helperText="The zone this time is quoted in, e.g. Asia/Tashkent."
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setOpen(false)}>Cancel</Button>
          <Button variant="contained" disabled={busy || tz === ''} onClick={confirm}>
            Confirm
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
