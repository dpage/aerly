import { useEffect, useState } from 'react';
import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
} from '@mui/material';

import { useStore } from '../state/store';

/** Set the signed-in user's home address. It's used as context when adding
 * plans from text (so "taxi from home to LHR" resolves "home"), and is only
 * ever visible to the user. */
export default function HomeAddressDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const me = useStore((s) => s.me);
  const setHomeAddress = useStore((s) => s.setHomeAddress);
  const setError = useStore((s) => s.setError);
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) setValue(me?.home_address ?? '');
  }, [open, me?.home_address]);

  const save = async () => {
    setBusy(true);
    try {
      await setHomeAddress(value.trim());
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>Home address</DialogTitle>
      <DialogContent>
        <DialogContentText sx={{ mb: 2 }}>
          Used as context when adding plans from text — so a confirmation like “taxi from home to
          the airport” knows where home is.
        </DialogContentText>
        <TextField
          label="Home address"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          fullWidth
          multiline
          minRows={2}
          placeholder="e.g. 12 Acacia Avenue, Reading, RG1 1AA"
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" onClick={() => void save()} disabled={busy}>
          Save
        </Button>
      </DialogActions>
    </Dialog>
  );
}
