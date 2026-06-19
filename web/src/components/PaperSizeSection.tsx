import { FormControl, FormControlLabel, Radio, RadioGroup, Stack, Typography } from '@mui/material';

import type { PaperSize } from '../api/types';
import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';

/** The signed-in user's preferred page size for the downloadable PDF itinerary
 * (Trip → Download PDF), shown as a Preferences tab. A4 is the default; US
 * Letter suits North American printers. Saves immediately on change; on failure
 * it surfaces the error (the canonical value from `me` re-renders the choice). */
export default function PaperSizeSection() {
  const me = useStore((s) => s.me);
  const setPaperSize = useStore((s) => s.setPaperSize);
  const setError = useStore((s) => s.setError);
  // Legacy/absent values fall back to A4, matching the server default.
  const value: PaperSize = me?.paper_size === 'letter' ? 'letter' : 'a4';

  const onChange = async (next: PaperSize) => {
    if (next === value) return;
    try {
      await setPaperSize(next);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <Stack spacing={2}>
      <Typography variant="body2" color="text.secondary">
        Page size for the PDF itinerary you can download from a trip. A4 suits most of the world; US
        Letter suits North American printers.
      </Typography>
      <FormControl>
        <RadioGroup
          aria-label="PDF paper size"
          value={value}
          onChange={(e) => void onChange(e.target.value as PaperSize)}
        >
          <FormControlLabel value="a4" control={<Radio />} label="A4" />
          <FormControlLabel value="letter" control={<Radio />} label="US Letter" />
        </RadioGroup>
      </FormControl>
    </Stack>
  );
}
