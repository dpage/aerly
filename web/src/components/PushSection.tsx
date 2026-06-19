import { useEffect } from 'react';
import { Alert, Box, FormControlLabel, FormGroup, Stack, Switch, Typography } from '@mui/material';

import { useStore } from '../state/store';

/** Notifications (Web Push) preferences as a Preferences tab: a master
 * per-device enable switch that drives the browser permission prompt and the
 * push subscription, plus per-kind toggles once enabled. Degrades gracefully to
 * an explanatory message on browsers without push, and on iOS Safari that
 * hasn't been added to the Home Screen (where Web Push isn't available yet). */
export default function PushSection() {
  const supported = useStore((s) => s.pushSupported);
  const permission = useStore((s) => s.pushPermission);
  const iosHint = useStore((s) => s.pushIosHint);
  const subscribed = useStore((s) => s.pushSubscribed);
  const prefs = useStore((s) => s.pushPrefs);
  const busy = useStore((s) => s.pushBusy);
  const lastError = useStore((s) => s.pushLastError);
  const loadPushState = useStore((s) => s.loadPushState);
  const enablePush = useStore((s) => s.enablePush);
  const disablePush = useStore((s) => s.disablePush);
  const setPushKind = useStore((s) => s.setPushKind);

  useEffect(() => {
    void loadPushState();
  }, [loadPushState]);

  if (!supported) {
    return <Alert severity="info">This browser doesn&apos;t support push notifications.</Alert>;
  }
  if (iosHint) {
    return (
      <Alert severity="info">
        To get push notifications on iPhone or iPad, add Aerly to your Home Screen first (Share → Add
        to Home Screen), then open it from there.
      </Alert>
    );
  }

  const onToggleMaster = (checked: boolean) => {
    if (checked) void enablePush();
    else void disablePush();
  };

  const denied = permission === 'denied' || lastError === 'denied';

  return (
    <Stack spacing={3}>
      <Box>
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          Push notifications
        </Typography>
        <FormGroup>
          <FormControlLabel
            control={
              <Switch
                checked={subscribed}
                disabled={busy}
                onChange={(e) => onToggleMaster(e.target.checked)}
              />
            }
            label="Enable push on this device"
          />
        </FormGroup>
        {denied && (
          <Alert severity="warning" sx={{ mt: 1 }}>
            Notifications are blocked. Allow them for Aerly in your browser&apos;s site settings, then
            try again.
          </Alert>
        )}
        {lastError === 'disabled' && (
          <Alert severity="info" sx={{ mt: 1 }}>
            Push isn&apos;t configured on this server.
          </Alert>
        )}
        {lastError === 'error' && (
          <Alert severity="error" sx={{ mt: 1 }}>
            Couldn&apos;t enable push. Please try again.
          </Alert>
        )}
      </Box>

      {subscribed && prefs && (
        <Box>
          <Typography variant="subtitle2" sx={{ mb: 1 }}>
            What to push
          </Typography>
          <FormGroup>
            <FormControlLabel
              control={
                <Switch
                  checked={prefs.alert}
                  onChange={(e) => void setPushKind('alert', e.target.checked)}
                />
              }
              label="Flight alerts"
            />
            <FormControlLabel
              control={
                <Switch
                  checked={prefs.share}
                  onChange={(e) => void setPushKind('share', e.target.checked)}
                />
              }
              label="Trip shares"
            />
          </FormGroup>
        </Box>
      )}
    </Stack>
  );
}
