import { useState } from 'react';
import { FormControlLabel, Switch } from '@mui/material';

import { useStore } from '../state/store';

interface Props {
  planId: number;
  /** Whether the viewer is currently opted in to this plan's change alerts. */
  optedIn: boolean;
  /** Notified after a successful opt-in/opt-out so the parent can update its
   * own copy of the flag (the store doesn't track per-plan opt-in state). */
  onChange?: (optedIn: boolean) => void;
}

/** Per-plan "Notify me of changes" opt-in (spec §6.8). Trip viewers don't get
 * a plan's alerts by default; this toggle opts them in or out. Owners/editors
 * already receive alerts via their own prefs, so this is surfaced for viewers. */
export default function PlanAlertToggle({ planId, optedIn, onChange }: Props) {
  const optInPlanAlerts = useStore((s) => s.optInPlanAlerts);
  const optOutPlanAlerts = useStore((s) => s.optOutPlanAlerts);
  const setError = useStore((s) => s.setError);

  const [on, setOn] = useState(optedIn);
  const [busy, setBusy] = useState(false);

  const handleToggle = async (next: boolean) => {
    setBusy(true);
    // Optimistic flip; revert on failure.
    setOn(next);
    try {
      if (next) await optInPlanAlerts(planId);
      else await optOutPlanAlerts(planId);
      onChange?.(next);
    } catch (err) {
      setOn(!next);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <FormControlLabel
      control={
        <Switch
          checked={on}
          disabled={busy}
          onChange={(e) => void handleToggle(e.target.checked)}
          inputProps={{ 'aria-label': 'Notify me of changes' }}
        />
      }
      label="Notify me of changes"
    />
  );
}
