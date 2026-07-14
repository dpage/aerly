import { Button, Dialog, DialogActions, DialogContent, DialogTitle, Stack } from '@mui/material';

import type { Plan } from '../api/types';
import PlanAlertToggle from './PlanAlertToggle';
import PlanReminderOverride from './PlanReminderOverride';

interface Props {
  open: boolean;
  plan: Plan;
  /** Viewers get the per-plan "Notify me of changes" opt-in; owners/editors
   * already receive a plan's alerts via their own prefs, so it's hidden for
   * them. The reminder override is shown to everyone. */
  isViewer: boolean;
  onClose: () => void;
}

/** Per-plan personal notification settings, moved off the timeline tile into a
 * dialog (opened from the tile's "Notifications" button) to keep the tile tidy.
 * These are the current user's own preferences, so the dialog is available to
 * viewers as well as owners/editors — unlike the owner-only Edit dialog. */
export default function PlanNotificationsDialog({ open, plan, isViewer, onClose }: Props) {
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>Notifications</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 0.5 }} alignItems="flex-start">
          {isViewer && <PlanAlertToggle plan={plan} />}
          <PlanReminderOverride plan={plan} />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
