import { useEffect, useState } from 'react';
import {
  Autocomplete,
  Box,
  Button,
  IconButton,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';

import { api } from '../api/client';
import { useStore } from '../state/store';
import { errorMessage } from '../state/helpers';
import { notifyFeedsChanged } from '../lib/feedsBus';
import type { TripFeed } from '../api/types';

// The IANA zone list from the runtime (modern browsers/Node 18+). Empty on the
// rare runtime without Intl.supportedValuesOf — the selector stays usable
// because it's free-solo, so a zone can still be typed.
function loadTimezones(): string[] {
  try {
    const fn = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] })
      .supportedValuesOf;
    return fn ? fn('timeZone') : [];
  } catch {
    return [];
  }
}
const TIMEZONES = loadTimezones();

/** A free-solo IANA timezone picker. Free-solo so a zone can be typed even when
 * the runtime exposes no list, and so an arbitrary valid zone is accepted. */
function TimezoneSelect({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}) {
  return (
    <Autocomplete
      freeSolo
      options={TIMEZONES}
      inputValue={value}
      onInputChange={(_e, v) => onChange(v)}
      disabled={disabled}
      size="small"
      fullWidth
      renderInput={(params) => (
        <TextField
          {...params}
          label="Timezone (optional)"
          helperText="Set this for feeds that publish times without a timezone"
        />
      )}
    />
  );
}

/** Manage a trip's iCal feed subscriptions ("external plans"): add a feed URL,
 * edit a feed's URL/name/timezone, or remove it. Each action hits the API
 * immediately (independent of the trip's Save button), since feeds have their
 * own endpoints and the server refreshes a new/changed feed synchronously.
 * Sharing is inherited wholesale from the trip — there's nothing per-feed to
 * configure. */
export default function TripFeedsEditor({ tripId }: { tripId: number }) {
  const setError = useStore((s) => s.setError);
  const [feeds, setFeeds] = useState<TripFeed[]>([]);
  const [newUrl, setNewUrl] = useState('');
  const [newName, setNewName] = useState('');
  const [newTz, setNewTz] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    void api
      .listTripFeeds(tripId)
      .then((f) => {
        if (live) setFeeds(f);
      })
      .catch((err) => setError(errorMessage(err)));
    return () => {
      live = false;
    };
  }, [tripId, setError]);

  const add = async () => {
    const url = newUrl.trim();
    if (!url) return;
    setBusy(true);
    try {
      const feed = await api.addTripFeed(
        tripId,
        url,
        newName.trim() || undefined,
        newTz.trim() || undefined,
      );
      setFeeds((prev) => [...prev, feed]);
      setNewUrl('');
      setNewName('');
      setNewTz('');
      notifyFeedsChanged();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: number) => {
    setBusy(true);
    try {
      await api.deleteTripFeed(tripId, id);
      setFeeds((prev) => prev.filter((f) => f.id !== id));
      notifyFeedsChanged();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const saved = (updated: TripFeed) => {
    setFeeds((prev) => prev.map((f) => (f.id === updated.id ? updated : f)));
    notifyFeedsChanged();
  };

  return (
    <Box>
      <Typography variant="subtitle2">Calendar feeds</Typography>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
        Subscribe to an iCal feed (e.g. a conference schedule). Its events appear as read-only
        “external plans” when “Show external plans” is on, and are shared with everyone on the trip.
      </Typography>

      <Stack spacing={1.5}>
        {feeds.map((feed) => (
          <FeedRow
            key={feed.id}
            tripId={tripId}
            feed={feed}
            busy={busy}
            onSaved={saved}
            onRemove={() => void remove(feed.id)}
          />
        ))}

        <Stack direction="row" spacing={1} alignItems="flex-start">
          <Stack spacing={1} sx={{ flexGrow: 1, minWidth: 0 }}>
            <TextField
              label="Feed URL"
              placeholder="https://example.com/schedule.ics"
              value={newUrl}
              onChange={(e) => setNewUrl(e.target.value)}
              size="small"
              fullWidth
            />
            <TextField
              label="Name (optional)"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              size="small"
              fullWidth
            />
            <TimezoneSelect value={newTz} onChange={setNewTz} disabled={busy} />
          </Stack>
          <Button
            variant="outlined"
            size="small"
            onClick={() => void add()}
            disabled={busy || !newUrl.trim()}
            sx={{ mt: 0.5 }}
          >
            Add
          </Button>
        </Stack>
      </Stack>
    </Box>
  );
}

/** One existing feed: editable URL, name and timezone with a Save (enabled only
 * when changed) and a Delete. Surfaces the last fetch error when the feed is
 * unhealthy. */
function FeedRow({
  tripId,
  feed,
  busy,
  onSaved,
  onRemove,
}: {
  tripId: number;
  feed: TripFeed;
  busy: boolean;
  onSaved: (f: TripFeed) => void;
  onRemove: () => void;
}) {
  const setError = useStore((s) => s.setError);
  const [url, setUrl] = useState(feed.url);
  const [name, setName] = useState(feed.name);
  const [tz, setTz] = useState(feed.timezone);
  const [saving, setSaving] = useState(false);

  const dirty = url.trim() !== feed.url || name.trim() !== feed.name || tz.trim() !== feed.timezone;

  const save = async () => {
    if (!url.trim() || !dirty) return;
    setSaving(true);
    try {
      const updated = await api.updateTripFeed(
        tripId,
        feed.id,
        url.trim(),
        name.trim() || undefined,
        tz.trim() || undefined,
      );
      onSaved(updated);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Stack direction="row" spacing={1} alignItems="flex-start">
      <Stack spacing={1} sx={{ flexGrow: 1, minWidth: 0 }}>
        <TextField
          label="Feed URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          size="small"
          fullWidth
          error={Boolean(feed.last_error)}
          helperText={feed.last_error ? `Last fetch failed: ${feed.last_error}` : undefined}
          slotProps={{
            input: feed.last_error
              ? {
                  endAdornment: (
                    <Tooltip title={feed.last_error}>
                      <ErrorOutlineIcon color="error" fontSize="small" />
                    </Tooltip>
                  ),
                }
              : undefined,
          }}
        />
        <TextField
          label="Name (optional)"
          value={name}
          onChange={(e) => setName(e.target.value)}
          size="small"
          fullWidth
        />
        <TimezoneSelect value={tz} onChange={setTz} disabled={saving || busy} />
      </Stack>
      <Stack sx={{ mt: 0.5 }}>
        {dirty && (
          <Button size="small" onClick={() => void save()} disabled={saving || busy || !url.trim()}>
            Save
          </Button>
        )}
        <Tooltip title="Remove feed">
          <span>
            <IconButton
              size="small"
              color="error"
              onClick={onRemove}
              disabled={busy || saving}
              aria-label="Remove feed"
            >
              <DeleteOutlineIcon fontSize="small" />
            </IconButton>
          </span>
        </Tooltip>
      </Stack>
    </Stack>
  );
}
