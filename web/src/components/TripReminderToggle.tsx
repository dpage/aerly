import { useEffect, useState } from 'react';
import { FormControlLabel, Stack, Switch, TextField } from '@mui/material';

import { useStore } from '../state/store';
import type { Trip } from '../api/types';

interface Props {
  trip: Trip;
}

/** Trip-level "Email me reminders" opt-in (#11). On = a row in
 * trip_reminder_optin; the number field sets the lead time in hours. Plans can
 * override this individually (see PlanReminderOverride). */
export default function TripReminderToggle({ trip }: Props) {
  const setTripReminder = useStore((s) => s.setTripReminder);
  const clearTripReminder = useStore((s) => s.clearTripReminder);
  const setError = useStore((s) => s.setError);

  const [on, setOn] = useState(trip.reminder_opted_in);
  const [lead, setLead] = useState(String(trip.reminder_lead_hours || 24));
  const [busy, setBusy] = useState(false);

  // Resync when the trip's reminder state changes underneath us (e.g. a live
  // refetch after the PUT/DELETE reloads the trip).
  useEffect(() => {
    setOn(trip.reminder_opted_in);
    setLead(String(trip.reminder_lead_hours || 24));
  }, [trip.reminder_opted_in, trip.reminder_lead_hours]);

  const leadNum = () => {
    const n = Number.parseInt(lead, 10);
    return Number.isFinite(n) && n > 0 ? n : 24;
  };

  const apply = async (next: boolean, leadHours: number) => {
    const prevOn = on;
    setBusy(true);
    setOn(next); // optimistic; revert on failure
    try {
      if (next) await setTripReminder(trip.id, leadHours);
      else await clearTripReminder(trip.id);
    } catch (err) {
      setOn(prevOn);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Stack direction="row" spacing={2} alignItems="center">
      <FormControlLabel
        control={
          <Switch
            checked={on}
            disabled={busy}
            onChange={(e) => void apply(e.target.checked, leadNum())}
            inputProps={{ 'aria-label': 'Email me reminders for this trip' }}
          />
        }
        label="Email me reminders"
      />
      {on && (
        <TextField
          label="Hours before"
          type="number"
          size="small"
          value={lead}
          disabled={busy}
          onChange={(e) => setLead(e.target.value)}
          onBlur={() => void apply(true, leadNum())}
          slotProps={{ htmlInput: { min: 1, 'aria-label': 'Reminder lead time in hours' } }}
          sx={{ width: 130 }}
        />
      )}
    </Stack>
  );
}
