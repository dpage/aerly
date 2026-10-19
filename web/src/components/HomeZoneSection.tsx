import { useRef, useState } from 'react';
import { Button, Stack, Typography } from '@mui/material';

import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';
import TimezoneSelect from './TimezoneSelect';

function isZone(tz: string): boolean {
  try {
    new Intl.DateTimeFormat('en-GB', { timeZone: tz });
    return true;
  } catch {
    return false;
  }
}

/** The signed-in user's home time zone (Preferences → Itinerary). It drives the
 * 'Your time' line and the zone used in emails and notifications. Saves
 * when a zone is picked, Enter is pressed or the field loses focus; on
 * failure it surfaces the error and the stored value from `me` is shown again. */
export default function HomeZoneSection() {
  const me = useStore((s) => s.me);
  const setHomeTz = useStore((s) => s.setHomeTz);
  const setError = useStore((s) => s.setError);
  const saved = me?.home_tz ?? '';
  // The picker reports every keystroke, so keep the text being typed locally and
  // only save on commit.
  const [draft, setDraft] = useState<string | null>(null);
  const value = draft ?? saved;
  const lastSent = useRef<string | null>(null);
  const device = Intl.DateTimeFormat().resolvedOptions().timeZone;

  const save = async (next: string) => {
    try {
      await setHomeTz(next);
    } catch (err) {
      setError(errorMessage(err));
    }
    // Keep anything typed whilst the request was in flight.
    setDraft((d) => (d === next ? null : d));
  };

  const onCommit = (text: string) => {
    const next = text.trim();
    if (next !== '' && !isZone(next)) {
      setDraft(null);
      setError(`'${next}' is not a time zone`);
      return;
    }
    // Enter and the blur that follows it both commit; save once.
    if (next === saved || next === lastSent.current) return;
    lastSent.current = next;
    void save(next).finally(() => {
      lastSent.current = null;
    });
  };

  return (
    <Stack spacing={2}>
      <Typography variant="h6" component="h3">
        Home time zone
      </Typography>
      <Typography variant="body2" color="text.secondary">
        Emails, notifications and the 'Your time' line use this zone. Leave it blank to use this
        device's zone on the web and to show one time in emails.
      </Typography>
      <TimezoneSelect value={value} onChange={setDraft} onCommit={onCommit} label="Home time zone" />
      {saved === '' && device && (
        <Button size="small" sx={{ alignSelf: 'flex-start' }} onClick={() => onCommit(device)}>
          {`Use this device's zone (${device})`}
        </Button>
      )}
    </Stack>
  );
}
