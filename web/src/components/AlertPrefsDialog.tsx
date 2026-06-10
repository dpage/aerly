import { useEffect, useState } from 'react';
import { errorMessage } from '../state/helpers';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  FormGroup,
  Stack,
  Switch,
  TextField,
  Typography,
} from '@mui/material';

import { useStore } from '../state/store';

interface Props {
  open: boolean;
  onClose: () => void;
}

/** Per-user alert preferences (spec §6.8): pick delivery channels (in-app /
 * email) and a minimum delay threshold below which flight changes are
 * suppressed. Loads current prefs on open and saves the patch on demand. */
export default function AlertPrefsDialog({ open, onClose }: Props) {
  const alertPrefs = useStore((s) => s.alertPrefs);
  const loadAlertPrefs = useStore((s) => s.loadAlertPrefs);
  const updateAlertPrefs = useStore((s) => s.updateAlertPrefs);
  const setError = useStore((s) => s.setError);

  const [inApp, setInApp] = useState(true);
  const [email, setEmail] = useState(false);
  const [minDelay, setMinDelay] = useState('15');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    void loadAlertPrefs();
  }, [open, loadAlertPrefs]);

  // Mirror the loaded prefs into editable local state once they arrive.
  useEffect(() => {
    if (!alertPrefs) return;
    setInApp(alertPrefs.in_app);
    setEmail(alertPrefs.email);
    setMinDelay(String(alertPrefs.min_delay_min));
  }, [alertPrefs]);

  const reportError = (err: unknown) => setError(errorMessage(err));

  const handleSave = async () => {
    const parsed = Number.parseInt(minDelay, 10);
    const min_delay_min = Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
    setBusy(true);
    try {
      await updateAlertPrefs({ in_app: inApp, email, min_delay_min });
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Alert preferences</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
          <Box>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              How would you like to be notified?
            </Typography>
            <FormGroup>
              <FormControlLabel
                control={<Switch checked={inApp} onChange={(e) => setInApp(e.target.checked)} />}
                label="In-app"
              />
              <FormControlLabel
                control={<Switch checked={email} onChange={(e) => setEmail(e.target.checked)} />}
                label="Email"
              />
            </FormGroup>
          </Box>
          <TextField
            label="Ignore delays shorter than"
            type="number"
            size="small"
            value={minDelay}
            onChange={(e) => setMinDelay(e.target.value)}
            slotProps={{ htmlInput: { min: 0, 'aria-label': 'Minimum delay in minutes' } }}
            helperText="Minutes. Flight changes below this delay won't alert you."
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" onClick={() => void handleSave()} disabled={busy}>
          Save
        </Button>
      </DialogActions>
    </Dialog>
  );
}
