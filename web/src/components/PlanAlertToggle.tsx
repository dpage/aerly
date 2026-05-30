import { useEffect, useState } from 'react';
import { FormControlLabel, Switch } from '@mui/material';

import { useStore } from '../state/store';
import type { Plan } from '../api/types';

interface Props {
  /** The plan to toggle alerts for. The opted-in state is read from
   * `plan.alert_opted_in` (a per-viewer DTO field) rather than a prop. */
  plan: Plan;
  /** Notified after a successful opt-in/opt-out so the parent can refresh its
   * own copy of the plan if it wants to. */
  onChange?: (optedIn: boolean) => void;
}

/** Per-plan "Notify me of changes" opt-in (spec §6.8). Trip viewers don't get
 * a plan's alerts by default; this toggle opts them in or out. Owners/editors
 * already receive alerts via their own prefs, so this is surfaced for viewers. */
export default function PlanAlertToggle({ plan, onChange }: Props) {
  const optInPlanAlerts = useStore((s) => s.optInPlanAlerts);
  const optOutPlanAlerts = useStore((s) => s.optOutPlanAlerts);
  const setError = useStore((s) => s.setError);

  const [on, setOn] = useState(plan.alert_opted_in);
  const [busy, setBusy] = useState(false);

  // Keep the local switch in sync when the plan's opted-in state changes
  // underneath us (e.g. a live trip refetch after a plan.updated event).
  useEffect(() => {
    setOn(plan.alert_opted_in);
  }, [plan.alert_opted_in]);

  const handleToggle = async (next: boolean) => {
    setBusy(true);
    // Optimistic flip; revert on failure.
    setOn(next);
    try {
      if (next) await optInPlanAlerts(plan.id);
      else await optOutPlanAlerts(plan.id);
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
