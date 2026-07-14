import { Checkbox, FormControlLabel, FormGroup, Stack, Typography } from '@mui/material';

import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';

/** Lets the signed-in user hide features they don't use, to declutter the UI.
 * Everything is shown by default; ticking a box hides that feature. Saves
 * immediately; on failure the canonical value from `me` re-renders the box. */
export default function FeaturesSection() {
  const me = useStore((s) => s.me);
  const setHiddenFeatures = useStore((s) => s.setHiddenFeatures);
  const setError = useStore((s) => s.setError);

  const hideExplore = me?.hide_explore ?? false;
  const hideMaps = me?.hide_maps ?? false;

  const save = async (patch: { hide_explore?: boolean; hide_maps?: boolean }) => {
    try {
      await setHiddenFeatures(patch);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <Stack spacing={2}>
      <Typography variant="body2" color="text.secondary">
        Hide features you don&rsquo;t use to keep the interface tidy. Everything is shown unless you
        hide it here, and you can turn anything back on at any time.
      </Typography>
      <FormGroup>
        <FormControlLabel
          control={
            <Checkbox
              checked={hideExplore}
              onChange={(e) => void save({ hide_explore: e.target.checked })}
            />
          }
          label="Hide Explore"
        />
        <Typography variant="caption" color="text.secondary" sx={{ ml: 4, mt: -0.5, mb: 1 }}>
          Removes the Explore tab and the &ldquo;Explore nearby&rdquo; button on accommodation.
        </Typography>
        <FormControlLabel
          control={
            <Checkbox
              checked={hideMaps}
              onChange={(e) => void save({ hide_maps: e.target.checked })}
            />
          }
          label="Hide maps"
        />
        <Typography variant="caption" color="text.secondary" sx={{ ml: 4, mt: -0.5 }}>
          Removes the trip Map tab and the Tracker. The map inside Explore is unaffected.
        </Typography>
      </FormGroup>
    </Stack>
  );
}
