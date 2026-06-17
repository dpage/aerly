import { useEffect, useState } from 'react';
import { Stack, TextField, Typography } from '@mui/material';

import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';

/** The signed-in user's home address as a Preferences tab. Used as context when
 * adding plans from text (so "taxi from home to LHR" resolves "home"), and only
 * ever visible to the user. Auto-saves the trimmed value on blur; on failure it
 * surfaces the error and restores the canonical value from `me`. */
export default function HomeAddressSection() {
  const me = useStore((s) => s.me);
  const setHomeAddress = useStore((s) => s.setHomeAddress);
  const setError = useStore((s) => s.setError);
  const canonical = me?.home_address ?? '';
  const [value, setValue] = useState(canonical);

  useEffect(() => {
    setValue(canonical);
  }, [canonical]);

  const onBlur = async () => {
    const trimmed = value.trim();
    if (trimmed === canonical) return;
    try {
      await setHomeAddress(trimmed);
    } catch (err) {
      setError(errorMessage(err));
      setValue(canonical);
    }
  };

  return (
    <Stack spacing={2}>
      <Typography variant="body2" color="text.secondary">
        Used as context when adding plans from text — so a confirmation like "taxi from home to the
        airport" knows where home is.
      </Typography>
      <TextField
        label="Home address"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={() => void onBlur()}
        fullWidth
        multiline
        minRows={2}
        placeholder="e.g. 12 Acacia Avenue, Reading, RG1 1AA"
      />
    </Stack>
  );
}
