import { useEffect, useState } from 'react';
import {
  Box,
  FormControlLabel,
  FormGroup,
  Stack,
  Switch,
  TextField,
  Typography,
} from '@mui/material';

import type { UpdateAlertPrefsInput } from '../api/types';
import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';

/** Per-user alert preferences (spec §6.8) as a Preferences tab: delivery
 * channels (in-app / email), a minimum delay threshold below which flight
 * changes are suppressed, and the check-in reminder (issue #119), which is off
 * unless asked for and rides the same channels. Auto-saves — toggles persist
 * immediately, the threshold persists on blur. On failure it surfaces the error
 * and reloads the canonical prefs so the controls never sit showing an unsaved
 * edit. */
export default function AlertPrefsSection() {
  const alertPrefs = useStore((s) => s.alertPrefs);
  const loadAlertPrefs = useStore((s) => s.loadAlertPrefs);
  const updateAlertPrefs = useStore((s) => s.updateAlertPrefs);
  const setError = useStore((s) => s.setError);

  const [inApp, setInApp] = useState(true);
  const [email, setEmail] = useState(false);
  const [minDelay, setMinDelay] = useState('15');
  const [checkin, setCheckin] = useState(false);

  useEffect(() => {
    void loadAlertPrefs();
  }, [loadAlertPrefs]);

  useEffect(() => {
    if (!alertPrefs) return;
    setInApp(alertPrefs.in_app);
    setEmail(alertPrefs.email);
    setMinDelay(String(alertPrefs.min_delay_min));
    setCheckin(alertPrefs.checkin);
  }, [alertPrefs]);

  const parseDelay = (s: string) => {
    const n = Number.parseInt(s, 10);
    return Number.isFinite(n) && n >= 0 ? n : 0;
  };

  // Each handler sends ONLY the field it changed. Sending a full snapshot let
  // two overlapping saves clobber each other: the later-completing request
  // would write back the stale copy of every other field it had captured when
  // its own control was touched, silently reverting the earlier change.
  const persist = async (patch: UpdateAlertPrefsInput) => {
    try {
      await updateAlertPrefs(patch);
    } catch (err) {
      setError(errorMessage(err));
      void loadAlertPrefs();
    }
  };

  const onToggleInApp = (checked: boolean) => {
    setInApp(checked);
    void persist({ in_app: checked });
  };
  const onToggleEmail = (checked: boolean) => {
    setEmail(checked);
    void persist({ email: checked });
  };
  const onBlurDelay = () => {
    const parsed = parseDelay(minDelay);
    setMinDelay(String(parsed));
    void persist({ min_delay_min: parsed });
  };
  const onToggleCheckin = (checked: boolean) => {
    setCheckin(checked);
    void persist({ checkin: checked });
  };

  return (
    <Stack spacing={3}>
      <Box>
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          How would you like to be notified?
        </Typography>
        <FormGroup>
          <FormControlLabel
            control={<Switch checked={inApp} onChange={(e) => onToggleInApp(e.target.checked)} />}
            label="In-app"
          />
          <FormControlLabel
            control={<Switch checked={email} onChange={(e) => onToggleEmail(e.target.checked)} />}
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
        onBlur={onBlurDelay}
        slotProps={{ htmlInput: { min: 0, 'aria-label': 'Minimum delay in minutes' } }}
        helperText="Minutes. Flight changes below this delay won't alert you."
      />
      <Box>
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          Check-in
        </Typography>
        <FormGroup>
          <FormControlLabel
            control={
              <Switch checked={checkin} onChange={(e) => onToggleCheckin(e.target.checked)} />
            }
            label="Remind me when check-in opens"
          />
        </FormGroup>
        <Typography variant="caption" color="text.secondary">
          Five minutes before online check-in opens, 24 hours ahead of each flight.
        </Typography>
      </Box>
    </Stack>
  );
}
