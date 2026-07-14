import { type MouseEvent, useEffect, useMemo, useState } from 'react';
import { errorMessage } from '../state/helpers';
import {
  Box,
  Button,
  Card,
  Checkbox,
  Chip,
  Collapse,
  Divider,
  Drawer,
  FormControlLabel,
  IconButton,
  Link,
  Stack,
  Switch,
  Tooltip,
  Typography,
} from '@mui/material';
import LocationOffIcon from '@mui/icons-material/LocationOff';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import PlaceIcon from '@mui/icons-material/Place';
import TravelExploreIcon from '@mui/icons-material/TravelExplore';
import IosShareIcon from '@mui/icons-material/IosShare';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import CloseIcon from '@mui/icons-material/Close';

import { api } from '../api/client';
import { useStore } from '../state/store';
import type { ExternalEvent, Plan, PlanPart, Trip } from '../api/types';
import PlanTypeIcon from '../components/PlanTypeIcon';
import PlanPrivacyDialog from '../components/PlanPrivacyDialog';
import PlanEditDialog from '../components/PlanEditDialog';
import PlanNotificationsDialog from '../components/PlanNotificationsDialog';
import MovePlanDialog from '../components/MovePlanDialog';
import AddToTripDialog from '../components/AddToTripDialog';
import ExplorePanel from '../components/ExplorePanel';
import { useShowExternalPlans } from '../lib/showExternalPlans';
import { useFeedsChangedCount } from '../lib/feedsBus';
import {
  buildExternalDays,
  buildTimeline,
  fmtPartPlaces,
  fmtPartTimeRange,
  fmtTimeOfDay,
  hotelNights,
  planTypeLabel,
} from '../lib/trip-format';
import { fmtGate } from '../lib/gate';
import { formatCost } from '../lib/format';
import { isUnlocated } from '../lib/geo';
import { buildPlanShareText, canShareNatively, sharePlan } from '../lib/share';

// Accent palette used to visually tie a plan's parts together (PRD §6.2). A
// plan's parts all share the same accent stripe and connector, so a return
// flight's two legs read as one booking even days apart. Colours are assigned
// by stable order of plan id so the same plan keeps its colour across renders.
// Seven distinct accents rotated by plan order (see accentFor). They must read
// as live, saturated colours on BOTH the light and dark themes; a too-dark
// entry (the old brown #5d4037) looked muted/disabled on the dark theme, so the
// warm-earth slot is now a vivid indigo instead.
const ACCENTS = ['#1f5fa8', '#d97706', '#2e7d32', '#7b1fa2', '#c2185b', '#00838f', '#4f46e5'];

function accentFor(planIds: number[], planId: number): string {
  const idx = planIds.indexOf(planId);
  return ACCENTS[(idx < 0 ? 0 : idx) % ACCENTS.length];
}

// External (feed) events use a separate, cooler palette so they read as
// reference items distinct from the viewer's own bookings, with each feed
// getting its own stable colour (assigned by first-seen order of feed id).
const EXTERNAL_ACCENTS = ['#546e7a', '#6a7b8a', '#4e6b7d', '#705c6b', '#5b6e5e'];

function externalAccentFor(feedIds: number[], feedId: number): string {
  const idx = feedIds.indexOf(feedId);
  return EXTERNAL_ACCENTS[(idx < 0 ? 0 : idx) % EXTERNAL_ACCENTS.length];
}

// Flights, trains and ground transport carry multi-leg bookings, so only they
// can be linked into (or split out of) one multi-part plan (#12).
function isLinkableType(type: string): boolean {
  return type === 'flight' || type === 'train' || type === 'ground';
}

// earliestStart returns the smallest part start instant (ms) of a plan, used to
// pick the primary (earliest) plan when linking. A plan with no parts sorts last.
function earliestStart(plan: Plan): number {
  return Math.min(...plan.parts.map((p) => Date.parse(p.starts_at)));
}

/** Default trip detail view (spec §11, PRD §6.2): a day-grouped vertical list
 * of plan parts sorted by `effective_at`, with sticky local-day headers, the
 * right MUI icon per type, local-time ranges, parts of one plan visually tied
 * together, multi-night hotels as a band, and superseded parts greyed. */
export default function TripTimeline() {
  const currentTrip = useStore((s) => s.currentTrip);
  const linkPlans = useStore((s) => s.linkPlans);
  const setError = useStore((s) => s.setError);
  // Explore hides when the user opts out or the server has no POI resolver.
  const exploreEnabled = useStore((s) => s.capabilities?.explore_enabled ?? true);
  const hideExplore = useStore((s) => s.me?.hide_explore ?? false) || !exploreEnabled;
  const plans = useMemo(() => currentTrip?.plans ?? [], [currentTrip]);
  const tripId = currentTrip?.id;

  // External (iCal feed) events for this trip, plus how many feeds the trip has
  // (so the toggle is offered whenever a feed exists, even before its events
  // have loaded or if it currently has none). Re-fetched when feeds change in
  // the Edit dialog. Best-effort: a failure just means no external tiles, never
  // a broken timeline.
  const [showExternal, setShowExternal] = useShowExternalPlans();
  const [externalEvents, setExternalEvents] = useState<ExternalEvent[]>([]);
  const [feedCount, setFeedCount] = useState(0);
  const feedsChanged = useFeedsChangedCount();
  useEffect(() => {
    if (tripId == null) {
      setExternalEvents([]);
      setFeedCount(0);
      return;
    }
    let live = true;
    // Fetch events and the feed list independently, so one failing doesn't
    // discard the other (a flaky feed-list call shouldn't hide loaded events).
    void api
      .getTripExternalEvents(tripId)
      .then((events) => {
        if (live) setExternalEvents(events);
      })
      .catch(() => {
        if (live) setExternalEvents([]);
      });
    void api
      .listTripFeeds(tripId)
      .then((feeds) => {
        if (live) setFeedCount(feeds.length);
      })
      .catch(() => {
        if (live) setFeedCount(0);
      });
    return () => {
      live = false;
    };
  }, [tripId, feedsChanged]);

  const days = useMemo(() => buildTimeline(plans), [plans]);
  // Stable feed ordering for per-feed accent colours (first appearance).
  const feedIds = useMemo(() => {
    const seen: number[] = [];
    for (const e of externalEvents) if (!seen.includes(e.feed_id)) seen.push(e.feed_id);
    return seen;
  }, [externalEvents]);
  // Merge plan days and (when shown) external-event days by local day key, so a
  // conference's sessions interleave with the bookings on the same day.
  const mergedDays = useMemo(() => {
    interface MergedDay {
      dayKey: string;
      label: string;
      parts: (typeof days)[number]['parts'];
      externals: ExternalEvent[];
    }
    const byKey = new Map<string, MergedDay>();
    for (const d of days) {
      byKey.set(d.dayKey, { dayKey: d.dayKey, label: d.label, parts: d.parts, externals: [] });
    }
    if (showExternal) {
      for (const d of buildExternalDays(externalEvents)) {
        const existing = byKey.get(d.dayKey);
        if (existing) existing.externals = d.events;
        else
          byKey.set(d.dayKey, { dayKey: d.dayKey, label: d.label, parts: [], externals: d.events });
      }
    }
    return [...byKey.values()].sort((a, b) =>
      a.dayKey < b.dayKey ? -1 : a.dayKey > b.dayKey ? 1 : 0,
    );
  }, [days, externalEvents, showExternal]);
  // Stable plan ordering for accent assignment (first appearance on timeline).
  const planIds = useMemo(() => {
    const seen: number[] = [];
    for (const d of days) {
      for (const { plan } of d.parts) if (!seen.includes(plan.id)) seen.push(plan.id);
    }
    return seen;
  }, [days]);

  // Which plan ids have more than one distinct part — only those get the
  // "part of a multi-part booking" connector treatment. Count distinct part
  // ids (not timeline tiles), so a single hotel stay shown as two check-in /
  // check-out tiles isn't mistaken for a multi-part booking.
  const multiPartPlanIds = useMemo(() => {
    const partsByPlan = new Map<number, Set<number>>();
    for (const d of days)
      for (const { plan, part } of d.parts) {
        let set = partsByPlan.get(plan.id);
        if (!set) partsByPlan.set(plan.id, (set = new Set()));
        set.add(part.id);
      }
    return new Set([...partsByPlan].filter(([, s]) => s.size > 1).map(([id]) => id));
  }, [days]);

  // Tiles expand in place (multiple at once) rather than opening a modal, so a
  // whole day can be unfolded side by side. Keyed by the tile key.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const [addOpen, setAddOpen] = useState(false);

  // The hotel tile whose "Explore nearby" button was clicked, if any. Drives a
  // drawer hosting ExplorePanel anchored to that hotel's coordinates; cleared
  // (rather than merely hidden) on close so the panel unmounts and doesn't
  // keep polling the POI API in the background.
  const [explorePart, setExplorePart] = useState<PlanPart | null>(null);

  // "Link bookings" mode: editors tick 2+ same-type flight/train plans that are
  // really one booking, then confirm to fold them into one multi-part plan (#12).
  const canEdit = currentTrip?.my_role === 'owner' || currentTrip?.my_role === 'editor';
  const [linkMode, setLinkMode] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<number>>(() => new Set());
  const [linking, setLinking] = useState(false);

  const linkableCount = useMemo(() => plans.filter((p) => isLinkableType(p.type)).length, [plans]);

  // Resolve the selection against the plans actually on the timeline now, so a
  // stale id left over from a reload can't enable a bad link.
  const selectedPlans = useMemo(() => plans.filter((p) => selected.has(p.id)), [plans, selected]);
  // Linking needs 2+ selected plans that all share one type.
  const selectedTypes = useMemo(() => new Set(selectedPlans.map((p) => p.type)), [selectedPlans]);
  const canLink = selectedPlans.length >= 2 && selectedTypes.size === 1;

  const toggleSelect = (planId: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(planId)) next.delete(planId);
      else next.add(planId);
      return next;
    });

  const cancelLink = () => {
    setLinkMode(false);
    setSelected(new Set());
  };

  const handleLink = async () => {
    // The earliest-starting selected plan is the primary the rest fold into.
    // The confirm button is gated on canLink, so there are always 2+ here.
    const chosen = [...selectedPlans].sort((a, b) => earliestStart(a) - earliestStart(b));
    const primary = chosen[0].id;
    const absorb = chosen.slice(1).map((p) => p.id);
    setLinking(true);
    try {
      await linkPlans(primary, absorb);
      cancelLink();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLinking(false);
    }
  };

  if (!currentTrip) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary">Loading…</Typography>
      </Box>
    );
  }

  // The toggle is offered whenever the trip has a feed configured OR has events
  // to show — so it's discoverable as soon as a feed is added (even before its
  // events load), yet never noise on trips with no feeds. Off by default (the
  // stored preference). When it's on but the feed has produced no events yet, a
  // hint explains the wait.
  const externalToggle =
    feedCount > 0 || externalEvents.length > 0 ? (
      <Box sx={{ mb: 1 }}>
        <FormControlLabel
          sx={{ ml: 0 }}
          control={
            <Switch
              checked={showExternal}
              onChange={(e) => setShowExternal(e.target.checked)}
              size="small"
            />
          }
          label="Show external plans"
        />
        {showExternal && externalEvents.length === 0 && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
            No events from your calendar feeds yet — they refresh periodically.
          </Typography>
        )}
      </Box>
    ) : null;

  if (mergedDays.length === 0) {
    return (
      <Box sx={{ p: 3, maxWidth: 760, mx: 'auto' }}>
        {externalToggle}
        <Typography color="text.secondary">
          Nothing on this trip yet. Use{' '}
          <Link
            component="button"
            type="button"
            onClick={() => setAddOpen(true)}
            sx={{ verticalAlign: 'baseline', fontWeight: 600 }}
          >
            New plan
          </Link>{' '}
          to add a flight, hotel, or other plan.
        </Typography>
        {addOpen && (
          <AddToTripDialog
            open={addOpen}
            tripId={currentTrip.id}
            onClose={() => setAddOpen(false)}
          />
        )}
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: 760, mx: 'auto' }}>
      {externalToggle}
      {canEdit && linkableCount >= 2 && (
        <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
          {!linkMode ? (
            <Button size="small" variant="outlined" onClick={() => setLinkMode(true)}>
              Link bookings
            </Button>
          ) : (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
                Select 2 or more flights, trains or transfers that are one booking.
              </Typography>
              <Button
                size="small"
                variant="contained"
                onClick={() => void handleLink()}
                disabled={!canLink || linking}
              >
                Link{selectedPlans.length > 0 ? ` ${selectedPlans.length}` : ''}
              </Button>
              <Button size="small" onClick={cancelLink} disabled={linking}>
                Cancel
              </Button>
            </>
          )}
        </Stack>
      )}
      {mergedDays.map((day) => (
        <Box key={day.dayKey} sx={{ mb: 2 }}>
          <Typography
            variant="subtitle2"
            color="text.secondary"
            sx={{
              position: 'sticky',
              top: 0,
              zIndex: 1,
              py: 0.75,
              bgcolor: 'background.default',
              borderBottom: 1,
              borderColor: 'divider',
            }}
          >
            {day.label}
          </Typography>
          <Stack spacing={1.5} sx={{ mt: 1.5 }}>
            {day.parts.map(({ part, plan, edge }) => {
              const key = `${part.id}${edge ? `-${edge}` : ''}`;
              return (
                <PartCard
                  key={key}
                  part={part}
                  plan={plan}
                  trip={currentTrip}
                  edge={edge}
                  accent={accentFor(planIds, plan.id)}
                  multiPart={multiPartPlanIds.has(plan.id)}
                  expanded={expanded.has(key)}
                  onToggle={() => toggle(key)}
                  onExplore={hideExplore ? undefined : () => setExplorePart(part)}
                  linkMode={linkMode}
                  selectable={isLinkableType(plan.type)}
                  selected={selected.has(plan.id)}
                  onSelect={() => toggleSelect(plan.id)}
                />
              );
            })}
            {day.externals.map((ev) => (
              <ExternalEventCard
                key={`ext-${ev.id}`}
                event={ev}
                accent={externalAccentFor(feedIds, ev.feed_id)}
              />
            ))}
          </Stack>
        </Box>
      ))}

      <Drawer
        anchor="right"
        open={explorePart != null}
        onClose={() => setExplorePart(null)}
        slotProps={{ paper: { sx: { width: { xs: '100%', sm: 420 }, maxWidth: '100%' } } }}
      >
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            pt: 'env(safe-area-inset-top)',
          }}
        >
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              p: 1.5,
              borderBottom: 1,
              borderColor: 'divider',
            }}
          >
            <Typography variant="subtitle1" sx={{ flexGrow: 1, fontWeight: 600 }}>
              Explore nearby
            </Typography>
            <Tooltip title="Close">
              <IconButton onClick={() => setExplorePart(null)} aria-label="Close">
                <CloseIcon />
              </IconButton>
            </Tooltip>
          </Box>
          <Box sx={{ p: 2, overflowY: 'auto', flexGrow: 1 }}>
            {/* Only rendered while the drawer holds a part, so closing it
                (which clears explorePart) unmounts ExplorePanel rather than
                leaving it fetching in the background. */}
            {explorePart && explorePart.start_lat != null && explorePart.start_lon != null && (
              <ExplorePanel
                tripId={currentTrip.id}
                initialCenter={{
                  lat: explorePart.start_lat,
                  lon: explorePart.start_lon,
                  label: explorePart.start_label,
                }}
              />
            )}
          </Box>
        </Box>
      </Drawer>
    </Box>
  );
}

/** A read-only timeline tile for an external (iCal feed) event. Visually
 * distinct from booking tiles: a calendar icon and a cooler per-feed accent, and
 * a chip naming its source feed. Titles (and locations) can be long, so on a
 * narrow screen they're truncated by default; tapping the tile toggles them to
 * wrap in full. The description always shows in full. */
function ExternalEventCard({ event, accent }: { event: ExternalEvent; accent: string }) {
  const [expanded, setExpanded] = useState(false);
  const when = event.all_day
    ? 'All day'
    : fmtTimeOfDay(event.starts_at, event.start_tz) +
      (event.ends_at ? ` – ${fmtTimeOfDay(event.ends_at, event.start_tz)}` : '');
  return (
    <Card
      variant="outlined"
      onClick={() => setExpanded((v) => !v)}
      sx={{ position: 'relative', borderLeft: `4px solid ${accent}`, cursor: 'pointer' }}
      data-testid={`external-event-${event.id}`}
    >
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ p: 1.5, '&:hover': { bgcolor: 'action.hover' } }}
        alignItems="flex-start"
      >
        <CalendarMonthIcon sx={{ color: accent, mt: 0.25 }} />
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Typography variant="subtitle2" sx={{ fontWeight: 600 }} noWrap={!expanded}>
              {event.title || 'Event'}
            </Typography>
            {event.feed_name && (
              <Chip
                label={event.feed_name}
                size="small"
                variant="outlined"
                sx={{ height: 18, fontSize: 10, borderColor: accent, color: accent, flexShrink: 0 }}
              />
            )}
          </Stack>
          {event.location && (
            <Typography
              variant="body2"
              color="text.secondary"
              noWrap={!expanded}
              sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}
            >
              <PlaceIcon sx={{ fontSize: 14, flexShrink: 0 }} /> {event.location}
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
            {when}
          </Typography>
          {event.description && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: 'block', mt: 0.5, whiteSpace: 'pre-wrap' }}
            >
              {event.description}
            </Typography>
          )}
        </Box>
      </Stack>
    </Card>
  );
}

interface PartCardProps {
  part: PlanPart;
  plan: Plan;
  trip: Trip;
  /** Set for the two tiles of a multi-night hotel stay (see buildTimeline). */
  edge?: 'check-in' | 'check-out';
  accent: string;
  multiPart: boolean;
  expanded: boolean;
  onToggle: () => void;
  /** Opens the Explore-nearby drawer anchored to this part. Only shown for a
   * hotel part that has coordinates (see the render guard below). */
  onExplore?: () => void;
  /** Link-selection mode: the card selects its plan instead of expanding. */
  linkMode: boolean;
  /** Whether this plan's type (flight/train) can be linked. */
  selectable: boolean;
  selected: boolean;
  onSelect: () => void;
}

/** A timeline tile. Tapping the header expands it in place (PRD §6.2 tap-through
 * to the whole plan) to reveal the address, type-specific detail, notes and the
 * per-plan actions — owners/editors get Edit / Sharing / Delete
 * (§6.4), viewers get the "Notify me of changes" opt-in (§6.8). */
function PartCard({
  part,
  plan,
  trip,
  edge,
  accent,
  multiPart,
  expanded,
  onToggle,
  onExplore,
  linkMode,
  selectable,
  selected,
  onSelect,
}: PartCardProps) {
  const deletePlan = useStore((s) => s.deletePlan);
  const setError = useStore((s) => s.setError);
  const setNotice = useStore((s) => s.setNotice);
  const [privacyOpen, setPrivacyOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);
  const [notifOpen, setNotifOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const canEdit = trip.my_role === 'owner' || trip.my_role === 'editor';
  const isViewer = trip.my_role === 'viewer';

  // A cancelled part stays on the timeline, greyed out, until it's tidied
  // away (PRD §6.2/§6.9). On a rebooking the OLD part is the one stamped
  // `status='cancelled'` and marked superseded — the NEW part carries
  // `supersedes_id` and stays full-colour. So we key the greying purely on
  // `status === 'cancelled'`, which also correctly greys a plain cancellation.
  // Dismissed parts are already dropped by buildTimeline().
  const greyed = part.status === 'cancelled';
  const places = fmtPartPlaces(part.type, part.start_label, part.end_label);
  const addr = fmtPartPlaces(part.type, part.start_address, part.end_address);
  const details = partDetailLines(part);
  // A multi-night hotel stay renders as two tiles (check-in and check-out); the
  // "N nights" chip on both ties them as one booking and shows the stay length,
  // the hotel equivalent of a flight's "multi-part" badge. `edge` is only set on
  // those hotel-band tiles.
  const nights = edge ? hotelNights(part) : 0;
  const nightsLabel = nights > 0 ? `${nights} night${nights === 1 ? '' : 's'}` : '';

  const handleDelete = async () => {
    if (!window.confirm(`Delete "${plan.title || planTypeLabel(plan.type)}" from this trip?`))
      return;
    setBusy(true);
    try {
      await deletePlan(plan.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  // In an installed PWA we open the native share sheet; in a browser tab we copy
  // the plan to the clipboard. The flag drives the icon/tooltip and is stable
  // for the session, so computing it per render is cheap.
  const nativeShare = canShareNatively();

  const handleShare = async () => {
    try {
      const outcome = await sharePlan(buildPlanShareText(plan, part), plan.title || planTypeLabel(part.type));
      if (outcome === 'copied') setNotice({ message: 'Plan copied to clipboard', severity: 'success' });
    } catch {
      setError('Could not share this plan');
    }
  };

  // Clicks inside the expanded body shouldn't fold the card back up.
  const stop = (e: MouseEvent) => e.stopPropagation();

  // In link mode a click selects (eligible plans) rather than expanding; an
  // ineligible plan (e.g. a hotel) is inert and dimmed.
  const cardClick = linkMode ? (selectable ? onSelect : undefined) : onToggle;
  const dimmed = linkMode && !selectable;

  return (
    <Card
      variant="outlined"
      onClick={cardClick}
      sx={{
        position: 'relative',
        opacity: greyed ? 0.55 : dimmed ? 0.5 : 1,
        borderLeft: `4px solid ${accent}`,
        ...(selected ? { outline: `2px solid ${accent}`, outlineOffset: -2 } : {}),
      }}
      data-testid={`part-card-${part.id}${edge ? `-${edge}` : ''}`}
    >
      <Stack
        direction="row"
        spacing={1.5}
        sx={{
          p: 1.5,
          cursor: linkMode && !selectable ? 'default' : 'pointer',
          '&:hover': { bgcolor: 'action.hover' },
        }}
        alignItems="flex-start"
      >
        {linkMode && selectable && (
          <Checkbox
            checked={selected}
            onClick={stop}
            onChange={onSelect}
            size="small"
            sx={{ p: 0.5, mt: -0.25 }}
            inputProps={{ 'aria-label': `Select ${plan.title || planTypeLabel(part.type)}` }}
          />
        )}
        <PlanTypeIcon type={part.type} sx={{ color: accent, mt: 0.25 }} />
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Typography variant="subtitle2" sx={{ fontWeight: 600 }} noWrap>
              {plan.title || planTypeLabel(part.type)}
            </Typography>
            {multiPart && (
              <Chip
                label="multi-part"
                size="small"
                variant="outlined"
                sx={{ height: 18, fontSize: 10, borderColor: accent, color: accent }}
              />
            )}
            {nightsLabel && (
              <Chip
                label={nightsLabel}
                size="small"
                variant="outlined"
                sx={{ height: 18, fontSize: 10, borderColor: accent, color: accent }}
              />
            )}
            {part.status === 'confirmed' && (
              <Chip
                label="confirmed"
                size="small"
                color="success"
                variant="outlined"
                sx={{ height: 18, fontSize: 10 }}
              />
            )}
            {greyed && (
              <Chip
                label="cancelled"
                size="small"
                color="warning"
                variant="outlined"
                sx={{ height: 18, fontSize: 10 }}
              />
            )}
          </Stack>

          {places && places !== (plan.title || planTypeLabel(part.type)) && (
            <Typography variant="body2" color="text.secondary" noWrap>
              {places}
            </Typography>
          )}
          {/* The street address is at-a-glance info (e.g. to read to a cab
              driver), so it lives on the collapsed tile rather than behind a
              tap. Shown only when it says something the place label doesn't —
              an airport's address echoes its label and would just be noise.
              Wraps fully so a long address stays readable. */}
          {addr && addr !== places && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              {addr}
            </Typography>
          )}
          {isUnlocated(part) && (
            <Tooltip title="Address couldn't be located, so it's not shown on the map">
              <Chip
                size="small"
                color="warning"
                variant="outlined"
                icon={<LocationOffIcon sx={{ fontSize: 14 }} />}
                label="Not on map"
                sx={{ height: 18, fontSize: 10, mt: 0.25 }}
              />
            </Tooltip>
          )}

          <Typography variant="caption" color="text.secondary">
            {edge === 'check-in'
              ? `Check in · ${fmtTimeOfDay(part.starts_at, part.start_tz)}`
              : edge === 'check-out'
                ? `Check out · ${fmtTimeOfDay(part.ends_at ?? part.starts_at, part.end_tz || part.start_tz)}`
                : fmtPartTimeRange(part)}
          </Typography>

          {part.type === 'flight' && part.flight?.ident && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Flight: {part.flight.ident}
            </Typography>
          )}

          {plan.ticket_number && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Ticket: {plan.ticket_number}
            </Typography>
          )}

          {plan.supplier_name && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Supplier: {plan.supplier_name}
            </Typography>
          )}

          {part.type === 'flight' && part.flight && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Departure gate: {fmtGate(part.flight.origin_terminal, part.flight.origin_gate)}
            </Typography>
          )}

          {/* Belt is only known close to arrival, so show it only when published
              (unlike the departure gate, which is known well ahead). */}
          {part.type === 'flight' && part.flight?.dest_baggage_belt && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Baggage belt: {part.flight.dest_baggage_belt}
            </Typography>
          )}
        </Box>

        {/* Right-hand tile actions, stacked vertically so Share (on every tile)
            sits above Explore (hotels with coordinates) when both are present. */}
        <Stack
          direction="column"
          spacing={0.5}
          alignItems="center"
          sx={{ flexShrink: 0, alignSelf: 'center' }}
        >
          <Tooltip title={nativeShare ? 'Share' : 'Copy to clipboard'}>
            <IconButton
              size="small"
              aria-label={nativeShare ? 'Share plan' : 'Copy plan to clipboard'}
              onClick={(e) => {
                e.stopPropagation();
                void handleShare();
              }}
              sx={{ color: accent }}
            >
              {nativeShare ? (
                <IosShareIcon fontSize="small" />
              ) : (
                <ContentCopyIcon fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
          {part.type === 'hotel' && part.start_lat != null && part.start_lon != null && onExplore && (
            <Tooltip title="Explore nearby">
              <IconButton
                size="small"
                aria-label="Explore nearby"
                onClick={(e) => {
                  e.stopPropagation();
                  onExplore();
                }}
                sx={{ color: accent }}
              >
                <TravelExploreIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
        </Stack>
      </Stack>

      <Collapse in={expanded} unmountOnExit>
        <Box onClick={stop} sx={{ px: 1.5, pb: 1.5, pl: 5.5 }}>
          <Divider sx={{ mb: 1 }} />
          {plan.confirmation_ref && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Ref: {plan.confirmation_ref}
            </Typography>
          )}
          {formatCost(plan.cost_amount, plan.cost_currency) && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Cost: {formatCost(plan.cost_amount, plan.cost_currency)}
            </Typography>
          )}
          {details.map((line, i) => (
            <Typography key={i} variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              {line}
            </Typography>
          ))}
          <PlanContact plan={plan} onClickStop={stop} />
          {plan.notes && (
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{ mt: 1, whiteSpace: 'pre-wrap' }}
            >
              {plan.notes}
            </Typography>
          )}
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap sx={{ mt: 1.5 }}>
            <Button size="small" onClick={() => setNotifOpen(true)}>
              Notifications
            </Button>
            {canEdit && (
              <>
                <Button size="small" onClick={() => setEditOpen(true)}>
                  Edit
                </Button>
                <Button size="small" onClick={() => setPrivacyOpen(true)}>
                  Sharing
                </Button>
                <Button size="small" onClick={() => setMoveOpen(true)}>
                  Move
                </Button>
                <Button
                  size="small"
                  color="error"
                  onClick={() => void handleDelete()}
                  disabled={busy}
                >
                  Delete
                </Button>
              </>
            )}
          </Stack>
        </Box>
      </Collapse>

      {canEdit && privacyOpen && (
        <PlanPrivacyDialog
          open={privacyOpen}
          plan={plan}
          members={trip.members}
          onClose={() => setPrivacyOpen(false)}
        />
      )}
      {canEdit && editOpen && (
        <PlanEditDialog open={editOpen} plan={plan} onClose={() => setEditOpen(false)} />
      )}
      {canEdit && moveOpen && (
        <MovePlanDialog open={moveOpen} plan={plan} onClose={() => setMoveOpen(false)} />
      )}
      {notifOpen && (
        <PlanNotificationsDialog
          open={notifOpen}
          plan={plan}
          isViewer={isViewer}
          onClose={() => setNotifOpen(false)}
        />
      )}
    </Card>
  );
}

/** Turn a stored website value into a safe href, or null when it can't be one.
 * A bare host (no scheme) is assumed https; an explicit http(s):// URL is kept;
 * any other scheme (javascript:, data:, …) is rejected so a persisted, shared
 * field can't become a stored script-URL injection vector. */
function normalizeWebsite(url: string | undefined | null): string | null {
  const trimmed = (url ?? '').trim();
  if (!trimmed) return null;
  const scheme = /^([a-z][a-z0-9+.-]*):/i.exec(trimmed);
  if (!scheme) return `https://${trimmed}`;
  const proto = scheme[1].toLowerCase();
  return proto === 'http' || proto === 'https' ? trimmed : null;
}

/** The supplier contact block shown in a plan's expanded body: email and phone
 * as mailto:/tel: links and the website as an open-in-new-tab link. Consistent
 * across every plan type; renders nothing when no contact detail is set. The
 * links stop click propagation so tapping them doesn't fold the card. */
function PlanContact({ plan, onClickStop }: { plan: Plan; onClickStop: (e: MouseEvent) => void }) {
  const website = normalizeWebsite(plan.website);
  if (!plan.contact_email && !plan.contact_phone && !website) return null;
  return (
    <>
      {plan.contact_email && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
          Email:{' '}
          <Link href={`mailto:${plan.contact_email}`} onClick={onClickStop}>
            {plan.contact_email}
          </Link>
        </Typography>
      )}
      {plan.contact_phone && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
          Phone:{' '}
          <Link href={`tel:${plan.contact_phone.replace(/\s+/g, '')}`} onClick={onClickStop}>
            {plan.contact_phone}
          </Link>
        </Typography>
      )}
      {website && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
          Website:{' '}
          <Link
            href={website}
            target="_blank"
            rel="noopener noreferrer"
            onClick={onClickStop}
            sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.25 }}
          >
            {plan.website.trim()} <OpenInNewIcon sx={{ fontSize: 12 }} />
          </Link>
        </Typography>
      )}
    </>
  );
}

/** Type-specific detail lines for the plan-detail dialog, so a tapped plan
 * shows what it actually is (room type, operator, reservation…) not just a
 * place and time. Each returned string renders as its own caption line. */
function partDetailLines(part: PlanPart): string[] {
  const join = (...bits: (string | undefined)[]) => bits.filter(Boolean).join(' · ');
  const out: string[] = [];
  switch (part.type) {
    case 'hotel':
      if (part.hotel) out.push(join(part.hotel.room_type, part.hotel.phone));
      break;
    case 'train':
      if (part.train) {
        out.push(join(part.train.operator, part.train.service_no, part.train.class));
        out.push(
          join(
            part.train.coach && `Coach ${part.train.coach}`,
            part.train.seat && `Seat ${part.train.seat}`,
            part.train.platform && `Platform ${part.train.platform}`,
          ),
        );
      }
      break;
    case 'ground':
      if (part.ground) out.push(join(part.ground.provider, part.ground.vehicle, part.ground.phone));
      break;
    case 'dining':
      if (part.dining)
        out.push(
          join(
            part.dining.reservation_name && `Reservation: ${part.dining.reservation_name}`,
            part.dining.phone,
          ),
        );
      break;
    case 'excursion':
      if (part.excursion) out.push(part.excursion.provider);
      break;
    case 'ice_cream':
      if (part.ice_cream) {
        const r = Math.max(0, Math.min(5, Math.round(part.ice_cream.rating)));
        out.push(
          join(r > 0 ? '★'.repeat(r) + '☆'.repeat(5 - r) : undefined, part.ice_cream.what_ordered),
        );
      }
      break;
    case 'meeting':
      if (part.meeting)
        out.push(join(part.meeting.organiser, part.meeting.location, part.meeting.platform));
      break;
    case 'event':
      if (part.event)
        out.push(
          join(
            part.event.performer,
            part.event.category,
            part.event.venue_area,
          ),
        );
      break;
    case 'flight':
      if (part.flight) out.push(join(part.flight.ident, part.flight.flight_status));
      break;
  }
  return out.filter(Boolean);
}
