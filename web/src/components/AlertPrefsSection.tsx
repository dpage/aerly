import { useEffect, useState } from 'react';
import { Box, FormControlLabel, FormGroup, Stack, Switch, TextField, Typography } from '@mui/material';

import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';

/** Per-user alert preferences (spec §6.8) as a Preferences tab: delivery
 * channels (in-app / email) and a minimum delay threshold below which flight
 * changes are suppressed. Auto-saves — toggles persist immediately, the
 * threshold persists on blur. On failure it surfaces the error and reloads the
 * canonical prefs so the controls never sit showing an unsaved edit. */
export default function AlertPrefsSection() {
  const alertPrefs = useStore((s) => s.alertPrefs);
  const loadAlertPrefs = useStore((s) => s.loadAlertPrefs);
  const updateAlertPrefs = useStore((s) => s.updateAlertPrefs);
  const setError = useStore((s) => s.setError);

  const [inApp, setInApp] = useState(true);
  const [email, setEmail] = useState(false);
  const [minDelay, setMinDelay] = useState('15');

  useEffect(() => {
    void loadAlertPrefs();
  }, [loadAlertPrefs]);

  useEffect(() => {
    if (!alertPrefs) return;
    setInApp(alertPrefs.in_app);
    setEmail(alertPrefs.email);
    setMinDelay(String(alertPrefs.min_delay_min));
  }, [alertPrefs]);

  const parseDelay = (s: string) => {
    const n = Number.parseInt(s, 10);
    return Number.isFinite(n) && n >= 0 ? n : 0;
  };

  const persist = async (patch: { in_app: boolean; email: boolean; min_delay_min: number }) => {
    try {
      await updateAlertPrefs(patch);
    } catch (err) {
      setError(errorMessage(err));
      void loadAlertPrefs();
    }
  };

  const onToggleInApp = (checked: boolean) => {
    setInApp(checked);
    void persist({ in_app: checked, email, min_delay_min: parseDelay(minDelay) });
  };
  const onToggleEmail = (checked: boolean) => {
    setEmail(checked);
    void persist({ in_app: inApp, email: checked, min_delay_min: parseDelay(minDelay) });
  };
  const onBlurDelay = () => {
    const parsed = parseDelay(minDelay);
    setMinDelay(String(parsed));
    void persist({ in_app: inApp, email, min_delay_min: parsed });
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
    </Stack>
  );
}
