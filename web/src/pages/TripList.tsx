import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import {
  Avatar,
  Box,
  Button,
  Card,
  CardActionArea,
  Chip,
  CircularProgress,
  Collapse,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  FormGroup,
  IconButton,
  InputAdornment,
  ListItemIcon,
  Menu,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import ClearIcon from '@mui/icons-material/Clear';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import FileUploadIcon from '@mui/icons-material/FileUpload';
import MoreVertIcon from '@mui/icons-material/MoreVert';
import PictureAsPdfIcon from '@mui/icons-material/PictureAsPdfOutlined';
import PlaceIcon from '@mui/icons-material/Place';
import SearchIcon from '@mui/icons-material/Search';
import UnfoldLessIcon from '@mui/icons-material/UnfoldLess';
import UnfoldMoreIcon from '@mui/icons-material/UnfoldMore';

import { useStore } from '../state/store';
import { useOnlineStatus } from '../pwa';
import { api } from '../api/client';
import type { Trip } from '../api/types';
import { userInitial, userName } from '../lib/format';
import { classifyTrip, fmtTripDates, tripSpan, type TripBucket } from '../lib/trip-format';

/** Which slice of the viewer's trips a TripList shows:
 *  - 'mine'    → trips the viewer owns (the home view, with a "New trip" action);
 *  - 'friends' → trips a friend has shared with the viewer (no create action). */
export type TripScope = 'mine' | 'friends';

/** Trip list — the redesign's home view (spec §11, PRD §6.1). Loads the
 * viewer's trips, filters to the requested `scope`, and groups them into
 * Upcoming / Happening now / Past by each trip's effective span vs now. The
 * 'mine' scope offers a "New trip" primary action; 'friends' is read-only. */
export default function TripList({ scope = 'mine' }: { scope?: TripScope }) {
  const navigate = useNavigate();
  const trips = useStore((s) => s.trips);
  const loading = useStore((s) => s.tripsLoading);
  const listTrips = useStore((s) => s.listTrips);
  const setError = useStore((s) => s.setError);
  const me = useStore((s) => s.me);
  // Creating/importing trips writes to the server, so disable both while offline.
  const online = useOnlineStatus();

  useEffect(() => {
    void listTrips();
  }, [listTrips]);

  // Import a TripIt or Kayak .ics as its own trip(s): the backend creates (or
  // reuses, on re-import) the trip(s) from the export and commits their plans,
  // then we refresh the list. A single-trip import opens the trip; a multi-trip
  // Kayak feed stays on the list.
  const [importing, setImporting] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  // Secondary actions (Import .ics, Download PDF) live in an overflow (⋮) menu
  // beside the primary "New trip" button, mirroring the Trip header.
  const [actionsAnchor, setActionsAnchor] = useState<HTMLElement | null>(null);
  const closeActions = () => setActionsAnchor(null);
  // Trip ids of the past trips currently on screen — the expanded years.
  // PastTripGroup owns the fold state, so it reports its visible ids up here for
  // the "Download PDF" action (the upcoming/now groups are always visible).
  const [visiblePastIds, setVisiblePastIds] = useState<number[]>([]);
  const onImportFile = async (file?: File) => {
    // Guard the handler too, not just the button: the connection can drop after
    // the file picker is already open.
    if (!online || !file || importing) return;
    setImporting(true);
    try {
      const res = await api.importTrip(file);
      await listTrips();
      // A Kayak feed imports several trips at once; stay on the refreshed list
      // rather than guessing which one to open. A single import opens its trip.
      if ((res.trips?.length ?? 1) > 1) return;
      navigate(`/trips/${res.trip.id}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not import that .ics.');
    } finally {
      setImporting(false);
      if (fileRef.current) fileRef.current.value = '';
    }
  };

  const mine = scope === 'mine';
  const isSuper = !mine && !!me?.is_superuser;

  // Superuser-only diagnostic toggles on the Friends' trips tab. Held in the
  // store (not local state) so they survive opening a trip and tapping Back —
  // the page unmounts on navigation, which would otherwise reset them.
  const showAllFriends = useStore((s) => s.friendsShowAllFriends);
  const showAllTrips = useStore((s) => s.friendsShowAllTrips);
  const setShowAllFriends = useStore((s) => s.setFriendsShowAllFriends);
  const setShowAllTrips = useStore((s) => s.setFriendsShowAllTrips);
  // "All trips" subsumes "all friends' trips".
  const include: 'friends' | 'all' | undefined = !isSuper
    ? undefined
    : showAllTrips
      ? 'all'
      : showAllFriends
        ? 'friends'
        : undefined;

  // When a diagnostic scope is active, fetch that broader set separately (it
  // isn't the viewer's own owner/member list, so it doesn't belong in the store).
  const [diagTrips, setDiagTrips] = useState<Trip[] | null>(null);
  const [diagLoading, setDiagLoading] = useState(false);
  useEffect(() => {
    if (!include) {
      setDiagTrips(null);
      return;
    }
    let cancelled = false;
    setDiagLoading(true);
    api
      .listTrips(include)
      .then((t) => !cancelled && setDiagTrips(t))
      .catch(() => !cancelled && setDiagTrips([]))
      .finally(() => !cancelled && setDiagLoading(false));
    return () => {
      cancelled = true;
    };
  }, [include]);

  // "My trips" holds the trips the viewer is part of: ones they own AND ones
  // they're a passenger on (travelling on a friend's trip — issue #19). The
  // Friends tab holds the rest that's shared with them: trips shared as a
  // viewer/editor where they aren't travelling. Diagnostic scopes (superuser,
  // Friends tab only) keep their prior non-owned filter unchanged.
  const scoped = useMemo(() => {
    if (include) return (diagTrips ?? []).filter((t) => t.my_role !== 'owner');
    return trips.filter((t) =>
      mine
        ? t.my_role === 'owner' || t.viewer_is_passenger
        : t.my_role !== 'owner' && !t.viewer_is_passenger,
    );
  }, [include, diagTrips, trips, mine]);
  const groups = useMemo(() => groupTrips(scoped), [scoped]);
  const [createOpen, setCreateOpen] = useState(false);
  const busy = include ? diagLoading : loading;

  const [filter, setFilter] = useState('');
  const filterRef = useRef<HTMLInputElement>(null);

  // Activate on "/" (vi/less style) when not already in a text field. Only when
  // the filter bar is actually rendered (i.e. there are trips), so we don't
  // swallow "/" on an empty list where there's nothing to focus.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/') return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable)
        return;
      if (!filterRef.current) return;
      e.preventDefault();
      filterRef.current.focus();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, []);

  const filterNorm = filter.trim().toLowerCase();

  // When filter is active, flatten all trips into a single filtered list. Run
  // the matches through the same bucketing/sort as the grouped view and flatten
  // in BUCKET_ORDER so the flat list keeps a meaningful chronological order
  // (Happening now → Upcoming soonest-first → Past most-recent-first) rather
  // than inheriting the store's arbitrary updated_at order.
  const filteredTrips = useMemo(() => {
    if (!filterNorm) return null;
    const matched = scoped.filter((t) => tripMatchesFilter(t, filterNorm));
    const g = groupTrips(matched);
    return BUCKET_ORDER.flatMap(({ bucket }) => g[bucket]);
  }, [scoped, filterNorm]);

  // The trips a "Download PDF" should cover: exactly the tiles on screen. With a
  // filter active that's every match; otherwise it's Happening now + Upcoming
  // (always shown) plus the past trips from the years the user has expanded
  // (reported by PastTripGroup, intersected with the current past trips so a
  // stale id from a since-removed trip can't sneak in). Order follows the list.
  const exportTripIds = useMemo(() => {
    if (filteredTrips !== null) return filteredTrips.map((t) => t.id);
    const ids = [...groups.now, ...groups.upcoming].map((t) => t.id);
    const pastIds = new Set(groups.past.map((t) => t.id));
    for (const id of visiblePastIds) if (pastIds.has(id)) ids.push(id);
    return ids;
  }, [filteredTrips, groups, visiblePastIds]);

  const onDownloadPdf = async () => {
    if (!online || exportTripIds.length === 0) return;
    try {
      await api.exportTripsPdf(exportTripIds);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not download the PDF.');
    }
  };

  return (
    <Box sx={{ px: 3, pt: 2, pb: 3, maxWidth: 760, mx: 'auto' }}>
      <Stack direction="row" alignItems="center" sx={{ mb: 2 }}>
        <Typography variant="h5" sx={{ flexGrow: 1 }}>
          {mine ? 'Your trips' : "Friends' trips"}
        </Typography>
        {mine && (
          // Pull the actions 12px into the right padding so the kebab lines up
          // with the Trip header's overflow button (which sits at px:1.5).
          <Stack direction="row" spacing={1} alignItems="center" sx={{ mr: -1.5 }}>
            <Button
              variant="contained"
              size="small"
              startIcon={<AddIcon />}
              onClick={() => setCreateOpen(true)}
              disabled={!online}
            >
              New trip
            </Button>
            <Tooltip title="More actions">
              <IconButton
                size="small"
                aria-label="More actions"
                onClick={(e) => setActionsAnchor(e.currentTarget)}
              >
                <MoreVertIcon />
              </IconButton>
            </Tooltip>
            <Menu
              anchorEl={actionsAnchor}
              open={actionsAnchor !== null}
              onClose={closeActions}
              anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
              transformOrigin={{ vertical: 'top', horizontal: 'right' }}
            >
              <MenuItem
                onClick={() => {
                  closeActions();
                  fileRef.current?.click();
                }}
                disabled={importing || !online}
              >
                <ListItemIcon>
                  <FileUploadIcon fontSize="small" />
                </ListItemIcon>
                Import .ics
              </MenuItem>
              <MenuItem
                onClick={() => {
                  closeActions();
                  void onDownloadPdf();
                }}
                disabled={!online || exportTripIds.length === 0}
              >
                <ListItemIcon>
                  <PictureAsPdfIcon fontSize="small" />
                </ListItemIcon>
                Download PDF
              </MenuItem>
            </Menu>
            <input
              ref={fileRef}
              type="file"
              hidden
              accept=".ics,text/calendar"
              onChange={(e) => void onImportFile(e.target.files?.[0])}
            />
          </Stack>
        )}
      </Stack>

      {isSuper && (
        <FormGroup row sx={{ mb: 2, gap: 2 }}>
          <FormControlLabel
            control={
              <Switch
                size="small"
                checked={showAllFriends || showAllTrips}
                disabled={showAllTrips}
                onChange={(e) => setShowAllFriends(e.target.checked)}
              />
            }
            label="All friends' trips"
          />
          <FormControlLabel
            control={
              <Switch
                size="small"
                checked={showAllTrips}
                onChange={(e) => setShowAllTrips(e.target.checked)}
              />
            }
            label="All trips (incl. non-friends)"
          />
        </FormGroup>
      )}

      {scoped.length > 0 && (
        <TextField
          inputRef={filterRef}
          size="small"
          placeholder='Filter trips… (press "/" to focus)'
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          onKeyDown={(e) => e.key === 'Escape' && setFilter('')}
          fullWidth
          sx={{ mb: 2 }}
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon fontSize="small" color={filterNorm ? 'primary' : 'disabled'} />
                </InputAdornment>
              ),
              endAdornment: filter ? (
                <InputAdornment position="end">
                  <Tooltip title="Clear filter">
                    <IconButton
                      size="small"
                      onClick={() => setFilter('')}
                      edge="end"
                      aria-label="Clear filter"
                    >
                      <ClearIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </InputAdornment>
              ) : undefined,
            },
          }}
        />
      )}

      {busy && scoped.length === 0 ? (
        <Box sx={{ display: 'grid', placeItems: 'center', py: 6 }}>
          <CircularProgress />
        </Box>
      ) : scoped.length === 0 ? (
        <Typography color="text.secondary">
          {mine ? (
            <>
              No trips yet. Click <strong>New trip</strong> to start planning your first one.
            </>
          ) : include ? (
            'No trips match this view.'
          ) : (
            "No trips have been shared with you yet. When a friend adds you to one of their trips, it'll appear here."
          )}
        </Typography>
      ) : filteredTrips !== null ? (
        // Filter active: flat list, no folding
        <Box>
          <Typography variant="overline" color="text.secondary" sx={{ mb: 1, display: 'block' }}>
            {filteredTrips.length === 0
              ? 'No matching trips'
              : `${filteredTrips.length} trip${filteredTrips.length === 1 ? '' : 's'} matched`}
          </Typography>
          <Stack spacing={1.5}>
            {filteredTrips.map((trip) => (
              <TripCard key={trip.id} trip={trip} />
            ))}
          </Stack>
        </Box>
      ) : (
        <Stack spacing={3}>
          {BUCKET_ORDER.map(({ bucket, label }) =>
            bucket === 'past' ? (
              groups.past.length > 0 ? (
                <PastTripGroup key="past" trips={groups.past} onVisibleChange={setVisiblePastIds} />
              ) : null
            ) : groups[bucket].length > 0 ? (
              <TripGroup key={bucket} label={label} trips={groups[bucket]} />
            ) : null,
          )}
        </Stack>
      )}

      {mine && (
        <NewTripDialog open={createOpen} onClose={() => setCreateOpen(false)} online={online} />
      )}
    </Box>
  );
}

const BUCKET_ORDER: Array<{ bucket: TripBucket; label: string }> = [
  { bucket: 'now', label: 'Happening now' },
  { bucket: 'upcoming', label: 'Upcoming' },
  { bucket: 'past', label: 'Past' },
];

/** Does a trip contain `q` (lowercase) in any of its searchable text fields? */
function tripMatchesFilter(trip: Trip, q: string): boolean {
  const fields = [
    trip.name,
    trip.destination,
    trip.starts_on,
    trip.ends_on,
    trip.effective_start,
    trip.effective_end,
    ...(trip.tags ?? []),
  ];
  return fields.some((f) => f && f.toLowerCase().includes(q));
}

/** Group past trips by calendar year, most-recent year first. */
function groupPastByYear(trips: Trip[]): Array<{ year: number; trips: Trip[] }> {
  const map = new Map<number, Trip[]>();
  for (const trip of trips) {
    // Prefer starts_on / effective_start for year bucketing; fall back to ends_on.
    const dateStr = trip.starts_on ?? trip.effective_start ?? trip.ends_on ?? trip.effective_end;
    const year = dateStr ? new Date(dateStr).getUTCFullYear() : new Date().getUTCFullYear();
    if (!map.has(year)) map.set(year, []);
    map.get(year)!.push(trip);
  }
  return [...map.entries()].sort(([a], [b]) => b - a).map(([year, trips]) => ({ year, trips }));
}

/** Past trips grouped by year with per-year and global fold/unfold controls.
 * Reports the ids of the trips in its expanded years via `onVisibleChange` so
 * the page's "Download PDF" action can cover exactly the tiles on screen. */
function PastTripGroup({
  trips,
  onVisibleChange,
}: {
  trips: Trip[];
  onVisibleChange?: (ids: number[]) => void;
}) {
  const yearGroups = useMemo(() => groupPastByYear(trips), [trips]);

  // Start with only the most-recent year expanded.
  const [collapsedYears, setCollapsedYears] = useState<Set<number>>(
    () => new Set(yearGroups.slice(1).map((g) => g.year)),
  );

  // Report the visible (expanded-year) trip ids up whenever the fold state or
  // the year groups change, so the parent can build the PDF export's id list.
  useEffect(() => {
    if (!onVisibleChange) return;
    const ids: number[] = [];
    for (const g of yearGroups) {
      if (!collapsedYears.has(g.year)) for (const t of g.trips) ids.push(t.id);
    }
    onVisibleChange(ids);
  }, [yearGroups, collapsedYears, onVisibleChange]);

  // Keep the initial collapsed state up to date when the year list changes
  // (e.g. a trip is added to a new year) without blowing away manual toggles.
  const prevYearsRef = useRef<number[]>([]);
  useEffect(() => {
    const prev = new Set(prevYearsRef.current);
    const next = yearGroups.map((g) => g.year);
    const newYears = next.filter((y) => !prev.has(y));
    if (newYears.length > 0) {
      setCollapsedYears((c) => {
        const s = new Set(c);
        // Collapse newly-appeared years that aren't the most recent.
        const mostRecent = next[0];
        for (const y of newYears) if (y !== mostRecent) s.add(y);
        return s;
      });
    }
    prevYearsRef.current = next;
  }, [yearGroups]);

  const allExpanded = collapsedYears.size === 0;

  const toggleYear = useCallback((year: number) => {
    setCollapsedYears((c) => {
      const s = new Set(c);
      if (s.has(year)) {
        s.delete(year);
      } else {
        s.add(year);
      }
      return s;
    });
  }, []);

  const collapseAll = useCallback(() => {
    setCollapsedYears(new Set(yearGroups.map((g) => g.year)));
  }, [yearGroups]);

  const expandAll = useCallback(() => {
    setCollapsedYears(new Set());
  }, []);

  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
        <Typography variant="overline" color="text.secondary">
          Past
        </Typography>
        <Chip
          label={trips.length}
          size="small"
          sx={{ height: 18, fontSize: '0.7rem', '& .MuiChip-label': { px: 0.75 } }}
        />
        <Box sx={{ flex: 1 }} />
        {yearGroups.length > 1 && (
          <Tooltip title={allExpanded ? 'Collapse all years' : 'Expand all years'}>
            <IconButton
              size="small"
              onClick={allExpanded ? collapseAll : expandAll}
              aria-label={allExpanded ? 'Collapse all years' : 'Expand all years'}
            >
              {allExpanded ? (
                <UnfoldLessIcon fontSize="small" />
              ) : (
                <UnfoldMoreIcon fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
        )}
      </Stack>

      <Stack spacing={1.5}>
        {yearGroups.map(({ year, trips: yearTrips }) => {
          const isCollapsed = collapsedYears.has(year);
          return (
            <Box key={year}>
              <Stack
                direction="row"
                alignItems="center"
                spacing={0.5}
                sx={{
                  cursor: 'pointer',
                  borderRadius: 1,
                  px: 0.5,
                  py: 0.25,
                  mb: isCollapsed ? 0 : 1,
                  // Hover only on hover-capable devices: on touch, :hover sticks
                  // after a tap and would leave this header greyed until you
                  // tapped elsewhere.
                  '@media (hover: hover)': { '&:hover': { bgcolor: 'action.hover' } },
                  userSelect: 'none',
                }}
                onClick={() => toggleYear(year)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggleYear(year);
                  }
                }}
                role="button"
                tabIndex={0}
                aria-expanded={!isCollapsed}
              >
                {isCollapsed ? (
                  <ExpandMoreIcon fontSize="small" color="action" />
                ) : (
                  <ExpandLessIcon fontSize="small" color="action" />
                )}
                <Typography variant="caption" fontWeight={600} color="text.secondary">
                  {year}
                </Typography>
                <Chip
                  label={yearTrips.length}
                  size="small"
                  sx={{ height: 16, fontSize: '0.65rem', '& .MuiChip-label': { px: 0.6 } }}
                />
              </Stack>

              <Collapse in={!isCollapsed} unmountOnExit>
                <Stack spacing={1.5}>
                  {yearTrips.map((trip) => (
                    <TripCard key={trip.id} trip={trip} />
                  ))}
                </Stack>
              </Collapse>
            </Box>
          );
        })}
      </Stack>
    </Box>
  );
}

function groupTrips(trips: Trip[]): Record<TripBucket, Trip[]> {
  const now = Date.now();
  // Compute each trip's span once (tripSpan scans the trip's parts), then reuse
  // it for both classification and sorting rather than recomputing per compare.
  const spans = new Map<number, ReturnType<typeof tripSpan>>();
  for (const trip of trips) spans.set(trip.id, tripSpan(trip));

  const out: Record<TripBucket, Trip[]> = { upcoming: [], now: [], past: [] };
  for (const trip of trips) {
    out[classifyTrip(spans.get(trip.id)!, now)].push(trip);
  }
  // Soonest-first within Upcoming/Now; most-recent-first for Past.
  const key = (t: Trip) => {
    const sp = spans.get(t.id)!;
    return sp.start ?? sp.end ?? Infinity;
  };
  out.now.sort((a, b) => key(a) - key(b));
  out.upcoming.sort((a, b) => key(a) - key(b));
  out.past.sort((a, b) => key(b) - key(a));
  return out;
}

function TripGroup({ label, trips }: { label: string; trips: Trip[] }) {
  return (
    <Box>
      <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
        <Typography variant="overline" color="text.secondary">
          {label}
        </Typography>
        <Chip
          label={trips.length}
          size="small"
          sx={{ height: 18, fontSize: '0.7rem', '& .MuiChip-label': { px: 0.75 } }}
        />
      </Stack>
      <Stack spacing={1.5}>
        {trips.map((trip) => (
          <TripCard key={trip.id} trip={trip} />
        ))}
      </Stack>
    </Box>
  );
}

function TripCard({ trip }: { trip: Trip }) {
  const navigate = useNavigate();
  const location = useLocation();
  const users = useStore((s) => s.users);
  const me = useStore((s) => s.me);

  const usersById = useMemo(() => new Map(users.map((u) => [u.id, u])), [users]);
  // Show whose trip it is — just the owner — on trips shared with the viewer.
  // (No avatar on the viewer's own trips; editors/viewers aren't shown here.)
  const ownerMember = trip.members.find((m) => m.role === 'owner');
  const owner =
    ownerMember && ownerMember.user_id !== me?.id ? usersById.get(ownerMember.user_id) : undefined;

  // Badge trips the viewer is travelling on but doesn't own, so owned and
  // passenger trips are distinguishable at a glance under "My trips" (#19).
  const showPassengerChip = trip.viewer_is_passenger && trip.my_role !== 'owner';

  const flag = flagUrl(trip.country_code);

  return (
    <Card variant="outlined" sx={{ position: 'relative', overflow: 'hidden' }}>
      {flag && (
        <Box
          component="img"
          src={flag.src}
          alt=""
          aria-hidden
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).style.display = 'none';
          }}
          sx={{
            position: 'absolute',
            top: 0,
            right: 0,
            height: '100%',
            width: '45%',
            objectFit: 'cover',
            opacity: 0.5,
            pointerEvents: 'none',
            // Fade in from the middle of the card towards the right edge.
            maskImage: 'linear-gradient(to right, transparent 0%, rgba(0,0,0,1) 75%)',
            WebkitMaskImage: 'linear-gradient(to right, transparent 0%, rgba(0,0,0,1) 75%)',
          }}
        />
      )}
      <CardActionArea
        // Remember which list we opened the trip from (home vs Friends' trips)
        // so the trip's Back button returns there rather than always going home.
        onClick={() => navigate(`/trips/${trip.id}`, { state: { from: location.pathname } })}
        sx={{ p: 2, position: 'relative' }}
      >
        <Stack direction="row" alignItems="flex-start" spacing={1}>
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }} noWrap>
              {trip.name}
            </Typography>
            {trip.destination && (
              <Stack
                direction="row"
                alignItems="center"
                spacing={0.5}
                sx={{ color: 'text.secondary' }}
              >
                <PlaceIcon fontSize="inherit" />
                <Typography variant="body2" color="text.secondary" noWrap>
                  {trip.destination}
                </Typography>
              </Stack>
            )}
            <Typography variant="caption" color="text.secondary">
              {fmtTripDates(trip)}
            </Typography>
          </Box>
          {(showPassengerChip || owner) && (
            <Stack alignItems="flex-end" spacing={0.5} sx={{ flex: 'none' }}>
              {showPassengerChip && (
                <Chip
                  label="Passenger"
                  size="small"
                  color="info"
                  variant="outlined"
                  sx={{ height: 20, '& .MuiChip-label': { px: 1, fontSize: 11 } }}
                />
              )}
              {owner && (
                <Tooltip title={`Owner: ${userName(owner)}`}>
                  <Avatar src={owner.avatar_url} sx={{ width: 26, height: 26, fontSize: 12 }}>
                    {userInitial(owner)}
                  </Avatar>
                </Tooltip>
              )}
            </Stack>
          )}
        </Stack>
      </CardActionArea>
    </Card>
  );
}

/** Local flag SVG path for a trip's country, or null when there's no usable
 * country code ("" while underived, "zz" = derived-but-unknown). The SVGs are
 * served same-origin from /flags (copied out of flag-icons at build time by
 * scripts/copy-flags.mjs), keyed on lowercase ISO 3166-1 alpha-2. SVG scales
 * cleanly, so no 1x/2x variants are needed. */
function flagUrl(code?: string): { src: string } | null {
  if (!code || code === 'zz' || !/^[a-z]{2}$/.test(code)) return null;
  return { src: `/flags/${code}.svg` };
}

function NewTripDialog({
  open,
  onClose,
  online,
}: {
  open: boolean;
  onClose: () => void;
  online: boolean;
}) {
  const navigate = useNavigate();
  const createTrip = useStore((s) => s.createTrip);
  const [name, setName] = useState('');
  const [destination, setDestination] = useState('');
  const [startsOn, setStartsOn] = useState('');
  const [endsOn, setEndsOn] = useState('');
  const [busy, setBusy] = useState(false);

  // Reset the fields whenever the dialog opens, so a cancelled draft doesn't
  // leak into the next open.
  useEffect(() => {
    if (open) {
      setName('');
      setDestination('');
      setStartsOn('');
      setEndsOn('');
      setBusy(false);
    }
  }, [open]);

  const submit = async () => {
    if (!name.trim() || busy) return;
    setBusy(true);
    const trip = await createTrip({
      name: name.trim(),
      destination: destination.trim() || undefined,
      starts_on: startsOn || undefined,
      ends_on: endsOn || undefined,
    });
    setBusy(false);
    if (trip) {
      onClose();
      navigate(`/trips/${trip.id}`);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>New trip</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          <TextField
            label="Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            required
            fullWidth
          />
          <TextField
            label="Destination"
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
            fullWidth
          />
          <Stack direction="row" spacing={2}>
            <TextField
              label="Starts"
              type="date"
              value={startsOn}
              onChange={(e) => setStartsOn(e.target.value)}
              InputLabelProps={{ shrink: true }}
              fullWidth
            />
            <TextField
              label="Ends"
              type="date"
              value={endsOn}
              onChange={(e) => setEndsOn(e.target.value)}
              InputLabelProps={{ shrink: true }}
              fullWidth
            />
          </Stack>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button
          variant="contained"
          onClick={() => void submit()}
          disabled={!online || !name.trim() || busy}
        >
          Create
        </Button>
      </DialogActions>
    </Dialog>
  );
}
