import { useRef, useState } from 'react';
import { Button, Stack, Typography } from '@mui/material';

import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';
import TimezoneSelect from './TimezoneSelect';
import { canonicalZone } from '../lib/zones';

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
    const typed = text.trim();
    // Saved as the zone list spells it, since the server's zone names are
    // case-sensitive ('europe/london' is saved as 'Europe/London').
    const next = typed === '' ? '' : canonicalZone(typed);
    if (next === null) {
      setDraft(null);
      setError(`'${typed}' is not a time zone`);
      return;
    }
    // Enter and the blur that follows it both commit; save once.
    if (next === saved) {
      setDraft(null);
      return;
    }
    if (next === lastSent.current) return;
    lastSent.current = next;
    setDraft(next);
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
        Emails, notifications and the 'Your time' line use this zone. It starts as this device's
        zone; clear it to show a single time in emails.
      </Typography>
      <TimezoneSelect
        value={value}
        onChange={setDraft}
        onCommit={onCommit}
        label="Home time zone"
      />
      {saved === '' && device && (
        <Button size="small" sx={{ alignSelf: 'flex-start' }} onClick={() => onCommit(device)}>
          {`Use this device's zone (${device})`}
        </Button>
      )}
    </Stack>
  );
}
