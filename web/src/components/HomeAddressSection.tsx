import { useEffect, useState } from 'react';
import { Alert, Box, Button, Stack, TextField, Typography } from '@mui/material';

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
  // A geocoded guess awaiting accept/reject (see applyPin). Never pinned until
  // the user confirms it: a geocoded link is a lead, not the pin they chose.
  const [pendingCoords, setPendingCoords] = useState<
    { lat: number; lon: number; label?: string } | null
  >(null);

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
    setPendingCoords(null);
    try {
      // Reads a bare pair or a coords-bearing URL locally, and sends anything
      // else that's a Maps link to the backend, which follows a short link or
      // geocodes a place-only link's text. A geocoded result waits for
      // acceptPending/rejectPending rather than being pinned straight away.
      const r = await resolveCoordsFromInput(pinText);
      if (!r) {
        setError(MAPS_NO_COORDS);
        return;
      }
      if (r.needsConfirmation) {
        setPendingCoords(r);
        return;
      }
      await setHomeCoords({ lat: r.lat, lon: r.lon });
      setPinText('');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const acceptPending = async () => {
    if (!pendingCoords) return;
    setSaving(true);
    try {
      await setHomeCoords({ lat: pendingCoords.lat, lon: pendingCoords.lon });
      setPinText('');
      setPendingCoords(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const rejectPending = () => {
    setPendingCoords(null);
    setError(MAPS_NO_COORDS);
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
        {pendingCoords && (
          // We geocoded the link's text rather than reading a coordinate from
          // it, so it's a good lead rather than the pin the user chose: it
          // waits here until they say which it is.
          <Alert
            severity="info"
            sx={{ mt: 1.5 }}
            action={
              <Stack direction="row" spacing={1}>
                <Button
                  size="small"
                  onClick={() => void acceptPending()}
                  disabled={saving}
                  aria-label="Use this location"
                >
                  Use it
                </Button>
                <Button
                  size="small"
                  onClick={rejectPending}
                  disabled={saving}
                  aria-label="Reject this location"
                >
                  No
                </Button>
              </Stack>
            }
          >
            We found{' '}
            <strong>{pendingCoords.label ?? `${pendingCoords.lat}, ${pendingCoords.lon}`}</strong>.
            Use this location?
          </Alert>
        )}
      </Box>
    </Stack>
  );
}
