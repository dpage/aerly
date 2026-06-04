import { useEffect, useState } from 'react';
import { MenuItem, Stack, TextField } from '@mui/material';

import { useStore } from '../state/store';
import type { Plan } from '../api/types';

type Mode = 'inherit' | 'on' | 'off';

interface Props {
  plan: Plan;
}

/** Per-plan reminder override (#11). "Use trip setting" clears the override;
 * "Remind me" / "Don't remind me" write an explicit plan_reminder_optin row,
 * which takes priority over the trip-level opt-in. */
export default function PlanReminderOverride({ plan }: Props) {
  const setPlanReminder = useStore((s) => s.setPlanReminder);
  const clearPlanReminder = useStore((s) => s.clearPlanReminder);
  const setError = useStore((s) => s.setError);

  const [mode, setMode] = useState<Mode>(plan.reminder_override);
  const [lead, setLead] = useState(String(plan.reminder_lead_hours || 24));
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setMode(plan.reminder_override);
    setLead(String(plan.reminder_lead_hours || 24));
  }, [plan.reminder_override, plan.reminder_lead_hours]);

  const leadNum = () => {
    const n = Number.parseInt(lead, 10);
    return Number.isFinite(n) && n > 0 ? n : 24;
  };

  const apply = async (next: Mode, leadHours: number) => {
    setBusy(true);
    setMode(next);
    try {
      if (next === 'inherit') await clearPlanReminder(plan.id);
      else await setPlanReminder(plan.id, next === 'on', leadHours);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Stack direction="row" spacing={2} alignItems="center">
      <TextField
        select
        size="small"
        label="Reminder"
        value={mode}
        disabled={busy}
        onChange={(e) => void apply(e.target.value as Mode, leadNum())}
        sx={{ minWidth: 180 }}
      >
        <MenuItem value="inherit">Use trip setting</MenuItem>
        <MenuItem value="on">Remind me</MenuItem>
        <MenuItem value="off">Don&apos;t remind me</MenuItem>
      </TextField>
      {mode === 'on' && (
        <TextField
          label="Hours before"
          type="number"
          size="small"
          value={lead}
          disabled={busy}
          onChange={(e) => setLead(e.target.value)}
          onBlur={() => void apply('on', leadNum())}
          slotProps={{ htmlInput: { min: 1, 'aria-label': 'Reminder lead time in hours' } }}
          sx={{ width: 130 }}
        />
      )}
    </Stack>
  );
}
