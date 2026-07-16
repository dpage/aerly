import { useEffect, useState } from 'react';
import { Box, Button, Stack, TextField, Typography } from '@mui/material';

import { MAPS_NO_COORDS, resolveCoordsFromInput } from '../lib/resolve-coords';
import { errorMessage } from '../state/helpers';
import { useStore } from '../state/store';

/** The signed-in user's home address as a Preferences tab. Used as context when
 * adding plans from text (so "taxi from home to LHR" resolves "home"), and only
 * ever visible to the user. Auto-saves the trimmed value on blur; on failure it
 * surfaces the error and restores the canonical value from `me`.
 *
 * The optional exact-location pin sits below: geocoding rural addresses can land
 * a street or two off, so pinning the precise coordinates (paste a Google Maps
 * link or a "lat, lon" pair) makes every "from home" plan plot exactly and feeds
 * the "Use my home" button in the plan editor. */
export default function HomeAddressSection() {
  const me = useStore((s) => s.me);
  const setHomeAddress = useStore((s) => s.setHomeAddress);
  const setHomeCoords = useStore((s) => s.setHomeCoords);
  const setError = useStore((s) => s.setError);
  const canonical = me?.home_address ?? '';
  const [value, setValue] = useState(canonical);
  const [pinText, setPinText] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setValue(canonical);
  }, [canonical]);

  const pinned =
    me?.home_lat != null && me?.home_lon != null
      ? { lat: me.home_lat, lon: me.home_lon }
      : null;

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

  const applyPin = async () => {
    setSaving(true);
    try {
      // Reads a bare pair or a coords-bearing URL locally, and follows a short
      // link through the backend — which only yields coordinates when they're
      // actually in the link. A place-only link has no exact spot to pin.
      const coords = await resolveCoordsFromInput(pinText);
      if (!coords) {
        setError(MAPS_NO_COORDS);
        return;
      }
      await setHomeCoords(coords);
      setPinText('');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const clearPin = async () => {
    setSaving(true);
    try {
      await setHomeCoords(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
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

      <Box>
        <Typography variant="subtitle2" gutterBottom>
          Exact location (optional)
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
          Geocoding a rural address can land a street or two off. Pin the precise spot and every
          "from home" plan will plot exactly there — paste a Google Maps link or a "latitude,
          longitude" pair.
        </Typography>
        {pinned ? (
          <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 1.5 }}>
            <Typography variant="body2">
              📍 Pinned at {pinned.lat.toFixed(5)}, {pinned.lon.toFixed(5)}
            </Typography>
            <Button size="small" color="inherit" onClick={() => void clearPin()} disabled={saving}>
              Clear
            </Button>
          </Stack>
        ) : (
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            Not pinned — plans from home use the address above.
          </Typography>
        )}
        <Stack direction="row" spacing={1} alignItems="flex-start">
          <TextField
            label="Map link or coordinates"
            value={pinText}
            onChange={(e) => setPinText(e.target.value)}
            fullWidth
            size="small"
            placeholder="https://maps.google.com/… or 51.50735, -0.12776"
          />
          <Button
            variant="outlined"
            onClick={() => void applyPin()}
            disabled={saving || pinText.trim() === ''}
            sx={{ flexShrink: 0, mt: 0.25 }}
          >
            {pinned ? 'Update' : 'Pin'}
          </Button>
        </Stack>
      </Box>
    </Stack>
  );
}
