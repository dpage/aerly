import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Box,
  Button,
  Divider,
  FormControl,
  InputLabel,
  MenuItem,
  Popover,
  Select,
  Stack,
  Typography,
  useMediaQuery,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { DatePicker } from '@mui/x-date-pickers/DatePicker';
import { format, parseISO } from 'date-fns';

import { useStore } from '../state/store';
import type { TrackerWindow } from '../state/trackerSlice';
import { tripSpan } from '../lib/trip-format';
import PlanMapView from '../components/PlanMapView';

const DAY_MS = 24 * 60 * 60 * 1000;
const ymd = (d: Date): string => format(d, 'yyyy-MM-dd');
const toDate = (s?: string): Date | null => (s ? parseISO(s) : null);
/** True for a real, finite Date. MUI's DatePicker emits an `Invalid Date`
 *  (truthy) while the user is mid-edit, and date-fns `format` throws on it. */
const isValidDate = (d: Date | null): d is Date => d != null && !Number.isNaN(d.getTime());

/** A short day label for the pill ("5 Jun"), or null for an unset/invalid bound. */
const fmtWinDay = (s?: string): string | null => {
  const d = toDate(s);
  return isValidDate(d) ? format(d, 'd MMM') : null;
};

/** Global tracker (PRD §6.5): the unified map+list view over every mappable part
 * in a date window, optionally scoped to a tag. Identical to the trip Map tab
 * except for the From/To date pickers + tag selector. `?part=` deep-links a
 * pre-selected part. */
export default function Tracker() {
  const [searchParams] = useSearchParams();
  const focusedPartId = useMemo(() => {
    const raw = searchParams.get('part');
    if (raw == null) return null;
    const n = Number(raw);
    return Number.isFinite(n) ? n : null;
  }, [searchParams]);

  const loadTracker = useStore((s) => s.loadTracker);
  const setTrackerWindow = useStore((s) => s.setTrackerWindow);
  const parts = useStore((s) => s.trackerParts);
  const tag = useStore((s) => s.trackerTag);
  const win = useStore((s) => s.trackerWindow);
  const loading = useStore((s) => s.trackerLoading);
  const trips = useStore((s) => s.trips);
  const listTrips = useStore((s) => s.listTrips);

  const theme = useTheme();
  // Below md the heading/controls row goes; a pill floats over the map instead.
  const mobile = useMediaQuery(theme.breakpoints.down('md'));
  const [pillAnchor, setPillAnchor] = useState<HTMLElement | null>(null);

  // A breakpoint flip unmounts the pill; drop the anchor so flipping back
  // doesn't re-open the popover against a detached element.
  useEffect(() => {
    if (!mobile) setPillAnchor(null);
  }, [mobile]);

  // Initial load: default the window to now−7d … now+30d when none is persisted.
  useEffect(() => {
    const w: TrackerWindow =
      win.from || win.to
        ? win
        : {
            from: ymd(new Date(Date.now() - 7 * DAY_MS)),
            to: ymd(new Date(Date.now() + 30 * DAY_MS)),
          };
    void loadTracker({ window: w });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mount only
  }, []);

  // The trip list backs the tag options and the tag-derived default span.
  useEffect(() => {
    if (trips.length === 0) void listTrips();
  }, [trips.length, listTrips]);

  const tagOptions = useMemo(() => {
    const set = new Set<string>();
    for (const t of trips) for (const label of t.tags) set.add(label);
    return [...set].sort();
  }, [trips]);

  // Default window spanning the tagged trips the viewer can see (§6.6), padded a
  // day each side. Used when switching tags.
  const tagWindow = (label: string): TrackerWindow | null => {
    if (!label) return null;
    let lo: number | null = null;
    let hi: number | null = null;
    for (const t of trips) {
      if (!t.tags.includes(label)) continue;
      const span = tripSpan(t);
      if (span.start != null) lo = lo == null ? span.start : Math.min(lo, span.start);
      if (span.end != null) hi = hi == null ? span.end : Math.max(hi, span.end);
    }
    if (lo == null && hi == null) return null;
    return {
      from: lo != null ? ymd(new Date(lo - DAY_MS)) : undefined,
      to: hi != null ? ymd(new Date(hi + DAY_MS)) : undefined,
    };
  };

  const onTagChange = (label: string) => {
    // Seed the window from the tag's span (so a past-trip tag isn't clipped),
    // else keep the current window.
    void loadTracker({ tag: label, window: tagWindow(label) ?? win });
  };

  const windowLabel = [fmtWinDay(win.from), fmtWinDay(win.to)].filter(Boolean).join(' – ');
  const pillLabel = [tag || 'Everyone', windowLabel].filter(Boolean).join(' · ');

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {!mobile && (
        <>
          <Box sx={{ px: 3, pt: 2, pb: 1 }}>
            <Typography variant="h5" sx={{ mb: 1.5 }}>
              Tracker
            </Typography>
            <TrackerControls
              direction="row"
              tag={tag}
              tagOptions={tagOptions}
              onTagChange={onTagChange}
              win={win}
              setTrackerWindow={setTrackerWindow}
            />
          </Box>
          <Divider />
        </>
      )}

      <Box sx={{ position: 'relative', flexGrow: 1, minHeight: 0 }}>
        <PlanMapView parts={parts} loading={loading} initialSelectedPartId={focusedPartId} />
        {mobile && (
          <>
            <Button
              data-testid="tracker-filter-pill"
              aria-label={`Filters: ${pillLabel}`}
              aria-haspopup="true"
              aria-expanded={pillAnchor != null}
              onClick={(e) => setPillAnchor(e.currentTarget)}
              sx={{
                // Floats clear of the map's top-left attribution ⓘ and
                // top-right zoom controls; the bottom sheet (zIndex 3) may
                // ride over it at full height.
                position: 'absolute',
                top: 8,
                left: 48,
                right: 48,
                zIndex: 2,
                minHeight: 44,
                px: 2,
                borderRadius: 99,
                bgcolor: 'background.paper',
                color: 'text.primary',
                boxShadow: 3,
                textTransform: 'none',
                justifyContent: 'flex-start',
                overflow: 'hidden',
                '&:hover': { bgcolor: 'background.paper' },
              }}
            >
              <Typography variant="body2" noWrap>
                {pillLabel}
              </Typography>
            </Button>
            <Popover
              open={pillAnchor != null}
              anchorEl={pillAnchor}
              onClose={() => setPillAnchor(null)}
              anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
              transformOrigin={{ vertical: 'top', horizontal: 'center' }}
            >
              <Box sx={{ p: 2, pt: 2.5, width: 280 }}>
                <TrackerControls
                  direction="column"
                  tag={tag}
                  tagOptions={tagOptions}
                  onTagChange={onTagChange}
                  win={win}
                  setTrackerWindow={setTrackerWindow}
                />
              </Box>
            </Popover>
          </>
        )}
      </Box>
    </Box>
  );
}

/** The Tag + From/To window controls, laid out as the desktop header row or
 * stacked in the mobile pill's popover. */
function TrackerControls({
  direction,
  tag,
  tagOptions,
  onTagChange,
  win,
  setTrackerWindow,
}: {
  direction: 'row' | 'column';
  tag: string;
  tagOptions: string[];
  onTagChange: (label: string) => void;
  win: TrackerWindow;
  setTrackerWindow: (w: Partial<TrackerWindow>) => void;
}) {
  return (
    <Stack
      direction={direction}
      spacing={2}
      alignItems={direction === 'row' ? 'center' : 'stretch'}
    >
      <FormControl size="small" sx={{ minWidth: 200 }}>
        <InputLabel id="tracker-tag-label">Tag</InputLabel>
        <Select
          labelId="tracker-tag-label"
          label="Tag"
          value={tag}
          onChange={(e) => onTagChange(e.target.value)}
        >
          <MenuItem value="">
            <em>Everyone (untagged view)</em>
          </MenuItem>
          {tagOptions.map((label) => (
            <MenuItem key={label} value={label}>
              {label}
            </MenuItem>
          ))}
        </Select>
      </FormControl>
      <DatePicker
        label="From"
        value={toDate(win.from)}
        onChange={(d) => isValidDate(d) && setTrackerWindow({ from: ymd(d) })}
        slotProps={{ textField: { size: 'small' } }}
      />
      <DatePicker
        label="To"
        value={toDate(win.to)}
        onChange={(d) => isValidDate(d) && setTrackerWindow({ to: ymd(d) })}
        slotProps={{ textField: { size: 'small' } }}
      />
    </Stack>
  );
}
