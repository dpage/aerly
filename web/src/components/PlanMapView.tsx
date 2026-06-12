import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import maplibregl, {
  type LngLatBoundsLike,
  type Map as MlMap,
  type MapGeoJSONFeature,
  type StyleSpecification,
} from 'maplibre-gl';
import {
  Alert,
  Avatar,
  AvatarGroup,
  Box,
  Button,
  Chip,
  Collapse,
  List,
  ListItemButton,
  ListItemText,
  Slider,
  Stack,
  Tooltip,
  Typography,
  useMediaQuery,
} from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';

import type { PlanPart } from '../api/types';
import { unlocatedCount } from '../lib/geo';
import {
  fmtScrubTime,
  parseMs,
  planePlacement,
  planePlacementAt,
  planeWindows,
  trackFC,
  tracksFC,
} from '../lib/flight-track';
import { greatCircle, toMultiLine } from '../lib/great-circle';
import { userInitial, userName } from '../lib/format';
import { buildMarkerPopup, buildPinEl, planTypeColor } from '../lib/plan-marker';
import { personColor } from '../lib/person-color';
import { fmtPartPlaces, fmtPartTimeRange, isTransferType, planTypeLabel } from '../lib/trip-format';
import FlightDetailCard from './FlightDetailCard';
import PartDetailBlock from './PartDetailBlock';
import BottomSheet, { sheetHeightPx, type SheetSnap } from './BottomSheet';

const STYLE: StyleSpecification = {
  version: 8,
  glyphs: 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf',
  sources: {
    osm: {
      type: 'raster',
      tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
      tileSize: 256,
      maxzoom: 19,
      attribution: '&copy; OpenStreetMap contributors',
    },
  },
  layers: [{ id: 'osm', type: 'raster', source: 'osm' }],
};

const LEGS = 'pmv-legs';
const TRACK = 'pmv-track';
// Neutral grey for a ground transfer's crow-flight connector (not an actual
// route), distinct from the type-coloured flight/train arcs.
const GROUND_LEG_COLOR = '#9e9e9e';

interface Props {
  /** All mappable parts to plot (those with ≥1 coordinate are shown). */
  parts: PlanPart[];
  loading?: boolean;
  /** Controls rendered above the list (the global map's date + tag pickers). */
  controls?: ReactNode;
  /** Pre-select a part on mount (preserves the tracker's ?part= deep-link). */
  initialSelectedPartId?: number | null;
}

/** Shared map + list view for both the trip Map tab and the global Tracker
 * (PRD §6.5/§11). Plots every mappable part as a coloured pin (and a great-circle
 * path for transfers), with a time-ordered list beside it. Selecting an item —
 * from the list OR the map — highlights it in both, fits the map to its whole
 * path (transfers) or point (venues), and expands the row to its detail (the
 * full flight card for flights, address/operator/etc. for everything else). */
export default function PlanMapView({ parts, loading, controls, initialSelectedPartId }: Props) {
  const [selectedId, setSelectedId] = useState<number | null>(initialSelectedPartId ?? null);
  const containerRef = useRef<HTMLElement | null>(null);

  const theme = useTheme();
  // Below md the list moves into a bottom sheet over a full-bleed map; at md+
  // the side-by-side desktop layout is untouched.
  const mobile = useMediaQuery(theme.breakpoints.down('md'));
  const [snap, setSnap] = useState<SheetSnap>('peek');
  // Inputs for the fit padding, in a ref so a snap change alone never re-runs
  // the fit effect (fitKeyRef gates on intent).
  const sheetSnapRef = useRef<{ mobile: boolean; snap: SheetSnap }>({
    mobile: false,
    snap: 'peek',
  });
  sheetSnapRef.current = { mobile, snap };

  const mapRef = useRef<MlMap | null>(null);
  const readyRef = useRef(false);
  // One teardrop pin per geocoded endpoint, keyed by part + role + coordinate.
  const markersRef = useRef<Map<string, maplibregl.Marker>>(new Map());
  // The active flight's plane icon — at its live position when airborne, else
  // parked at the origin (not departed) or destination (arrived) and oriented
  // along its route. Only flights inside their visibility window get one, and a
  // booking's connecting legs hand the single icon off between them so only one
  // plane is ever shown per journey. Keyed by part id.
  const planesRef = useRef<Map<number, maplibregl.Marker>>(new Map());

  // Mappable parts only (need at least one coordinate), time-ordered.
  const ordered = useMemo(() => {
    return parts
      .filter((p) => !p.dismissed_at && hasCoord(p))
      .slice()
      .sort((a, b) => (a.effective_at ?? a.starts_at).localeCompare(b.effective_at ?? b.starts_at));
  }, [parts]);

  const strandedCount = useMemo(() => unlocatedCount(parts), [parts]);

  const selected = ordered.find((p) => p.id === selectedId) ?? null;

  // A minute tick so the plane visibility windows (2h before departure … 2h
  // after arrival) are re-evaluated against wall-clock time even when no SSE
  // refresh arrives — planes appear/disappear and hand off at the boundary.
  const [minuteTick, setMinuteTick] = useState(0);
  useEffect(() => {
    const id = window.setInterval(() => setMinuteTick((t) => t + 1), 60_000);
    return () => window.clearInterval(id);
  }, []);

  // Time scrubber (issue: map time slider). null = parked on the live edge —
  // the map shows where flights *are now*; a number = an instant the user has
  // scrubbed back to, where the map shows where they *were* then. Replays the
  // flown tracks rather than fetching anything (every position is already
  // loaded with the part).
  const [scrubMs, setScrubMs] = useState<number | null>(null);

  // The slider's domain (recomputed each minute so the live edge keeps up with
  // wall-clock time): far left = the earliest plotted instant (the trip/window
  // start), far right = the earlier of *now* or the latest plotted instant. A
  // still-running trip therefore pins the right edge to "now" (the live view);
  // a wholly past one pins it to the trip's end.
  const timeDomain = useMemo(() => {
    let lo: number | null = null;
    let hi: number | null = null;
    let hasFlight = false;
    for (const p of ordered) {
      if (p.type === 'flight') hasFlight = true;
      const s = parseMs(p.starts_at) ?? parseMs(p.effective_at);
      if (s != null) lo = lo == null ? s : Math.min(lo, s);
      const e = parseMs(p.ends_at) ?? parseMs(p.effective_at) ?? s;
      if (e != null) hi = hi == null ? e : Math.max(hi, e);
    }
    const now = Date.now();
    const end = hi != null ? Math.min(now, hi) : now;
    // The right edge tracks the live "now" while the trip is still running.
    const inProgress = hi != null && now < hi;
    // Only worth a slider once there's a past to look back over, and only when
    // something actually moves (flights) — venues/hotels are static.
    const show = hasFlight && lo != null && end > lo;
    return { start: lo ?? now, end, inProgress, show };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- minuteTick advances `now`
  }, [ordered, minuteTick]);

  // The instant currently shown: the clamped scrub time, else the live edge.
  const scrubbing = scrubMs != null;
  const valueMs = scrubbing
    ? Math.min(Math.max(scrubMs, timeDomain.start), timeDomain.end)
    : timeDomain.end;
  // Genuinely "live" (vs. scrubbed back, or pinned to a past trip's end).
  const liveEdge = !scrubbing && timeDomain.inProgress;

  // Switching trip/tag (the parts — and so the domain's start — change) drops
  // back to the live edge rather than stranding the slider at an out-of-range
  // instant from the previous dataset.
  const prevStartRef = useRef(timeDomain.start);
  useEffect(() => {
    if (prevStartRef.current !== timeDomain.start) {
      prevStartRef.current = timeDomain.start;
      // Functional no-op when not scrubbing so React bails out (no spurious
      // re-render / act warning on every parts change).
      setScrubMs((cur) => (cur == null ? cur : null));
    }
  }, [timeDomain.start]);

  // Keep selection valid as the parts change (e.g. SSE refresh removes a part).
  useEffect(() => {
    if (selectedId != null && !ordered.some((p) => p.id === selectedId)) setSelectedId(null);
  }, [ordered, selectedId]);

  // --- map init (once) ------------------------------------------------------
  useEffect(() => {
    if (!containerRef.current) return;
    const markers = markersRef.current;
    const map = new maplibregl.Map({
      container: containerRef.current,
      style: STYLE,
      center: [5, 50],
      zoom: 3,
      // Re-home the OSM attribution to the top: the time slider owns the bottom
      // edge, where the default bottom-right attribution (its ⓘ + credit) would
      // otherwise poke out from under the slider.
      attributionControl: false,
    });
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    map.addControl(new maplibregl.AttributionControl({ compact: true }), 'top-left');
    mapRef.current = map;
    map.once('load', () => {
      for (const id of [LEGS, TRACK]) {
        map.addSource(id, { type: 'geojson', data: emptyFC() });
      }
      // The planned great-circle per transfer, coloured by type, the selected
      // one drawn solid + wide. (Pins are DOM markers, synced separately.)
      map.addLayer({
        id: LEGS,
        type: 'line',
        source: LEGS,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['case', ['get', 'selected'], 4, 2],
          'line-opacity': ['case', ['get', 'selected'], 1, 0.45],
        },
      });
      // The selected flight's flown track over the planned arc.
      map.addLayer({
        id: TRACK,
        type: 'line',
        source: TRACK,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: { 'line-color': '#d97706', 'line-width': 3 },
      });
      readyRef.current = true;
      // Clicking a leg selects its part; pointer cursor on hover.
      map.on('click', LEGS, (e) => {
        const f = e.features?.[0] as MapGeoJSONFeature | undefined;
        const pid = f?.properties?.partId;
        if (typeof pid === 'number') setSelectedId((cur) => (cur === pid ? null : pid));
      });
      map.on('mouseenter', LEGS, () => {
        map.getCanvas().style.cursor = 'pointer';
      });
      map.on('mouseleave', LEGS, () => {
        map.getCanvas().style.cursor = '';
      });
      // Trigger the first data sync now that the sources exist.
      syncRef.current?.();
    });
    const planes = planesRef.current;
    return () => {
      readyRef.current = false;
      markers.forEach((m) => m.remove());
      markers.clear();
      planes.forEach((m) => m.remove());
      planes.clear();
      map.remove();
      mapRef.current = null;
    };
  }, []);

  // --- sync sources from parts + selection -----------------------------------
  // Held in a ref so the load handler can run the first sync immediately.
  const syncRef = useRef<() => void>();
  syncRef.current = () => {
    const map = mapRef.current;
    if (!map || !readyRef.current) return;
    const legsSrc = map.getSource(LEGS) as maplibregl.GeoJSONSource | undefined;
    const trackSrc = map.getSource(TRACK) as maplibregl.GeoJSONSource | undefined;
    if (!legsSrc || !trackSrc) return;
    legsSrc.setData(legsFC(ordered, selectedId));

    // The instant to render at: the scrubbed time, else the real "now" (so the
    // live path is byte-for-byte the original behaviour).
    const at = scrubbing ? valueMs : Date.now();
    const windows = planeWindows(ordered);

    // Flown trails. Live: only the selected flight's full trail (as before).
    // Scrubbed back: the trail (clipped to the scrubbed instant) under *every*
    // flight active at that moment, so replaying the past shows where each
    // plane had been — not just the one selected in the list.
    if (scrubbing) {
      const active = ordered.filter((p) => {
        const win = windows.get(p.id);
        return win != null && at >= win.start && at < win.end;
      });
      trackSrc.setData(tracksFC(active, at));
    } else {
      trackSrc.setData(trackFC(selected, null));
    }

    // Sync the teardrop pins (one per geocoded endpoint). Reuse existing
    // markers, drop stale ones, and dim the unselected when something's picked.
    const anySel = selectedId != null;
    const live = new Set<string>();
    for (const p of ordered) {
      for (const ep of endpoints(p)) {
        const key = `${p.id}:${ep.role}:${ep.lon},${ep.lat}`;
        live.add(key);
        let marker = markersRef.current.get(key);
        if (!marker) {
          const el = buildPinEl(p.type, personColor(p.trip_owner_id));
          el.dataset.partId = String(p.id);
          el.dataset.role = ep.role;
          el.addEventListener('click', () => setSelectedId((cur) => (cur === p.id ? null : p.id)));
          marker = new maplibregl.Marker({ element: el, anchor: 'bottom' })
            .setLngLat([ep.lon, ep.lat])
            .setPopup(
              new maplibregl.Popup({ offset: 30, closeButton: false }).setDOMContent(
                buildMarkerPopup({
                  title: partTitle(p),
                  type: p.type,
                  location: ep.label,
                  iso: ep.iso,
                  tz: ep.tz,
                  owner: p.owner ? userName(p.owner) : undefined,
                  passengers: (p.passengers ?? []).map((u) => ({
                    name: userName(u),
                    avatarUrl: u.avatar_url || undefined,
                  })),
                }),
              ),
            )
            .addTo(map);
          markersRef.current.set(key, marker);
        }
        const el = marker.getElement();
        el.style.opacity = anySel && p.id !== selectedId ? '0.4' : '1';
        el.style.zIndex = p.id === selectedId ? '1' : '0';
      }
    }
    for (const [key, marker] of markersRef.current) {
      if (!live.has(key)) {
        marker.remove();
        markersRef.current.delete(key);
      }
    }

    // Sync plane icons: one per active flight, rotated to its heading (or its
    // route bearing when parked) and dimmed when the position is dead-reckoned.
    // Only flights inside their visibility window get an icon, and a booking's
    // connecting legs hand the single icon off between them (planeWindows), so
    // only one plane is ever shown per journey.
    const livePlanes = new Set<number>();
    for (const p of ordered) {
      const win = windows.get(p.id);
      if (!win || at < win.start || at >= win.end) continue;
      // Live: the latest reported fix. Scrubbed back: where it was at `at`,
      // interpolated along the flown track.
      const place = scrubbing ? planePlacementAt(p, at) : planePlacement(p);
      if (!place) continue;
      livePlanes.add(p.id);
      let plane = planesRef.current.get(p.id);
      if (!plane) {
        const el = buildPlaneEl(personColor(p.trip_owner_id) ?? planTypeColor('flight'));
        el.dataset.partId = String(p.id);
        el.dataset.role = 'plane';
        el.addEventListener('click', () => setSelectedId((cur) => (cur === p.id ? null : p.id)));
        plane = new maplibregl.Marker({ element: el, rotationAlignment: 'map' })
          .setLngLat([place.lon, place.lat])
          .addTo(map);
        planesRef.current.set(p.id, plane);
      } else {
        plane.setLngLat([place.lon, place.lat]);
      }
      plane.setRotation(place.heading);
      const el = plane.getElement();
      el.dataset.estimated = place.estimated ? '1' : '0';
      const base = place.estimated ? 0.6 : 1;
      el.style.opacity = String(anySel && p.id !== selectedId ? 0.4 : base);
      el.style.zIndex = p.id === selectedId ? '2' : '1';
    }
    for (const [id, plane] of planesRef.current) {
      if (!livePlanes.has(id)) {
        plane.remove();
        planesRef.current.delete(id);
      }
    }
  };

  useEffect(() => {
    syncRef.current?.();
  }, [ordered, selectedId, minuteTick, scrubMs]);

  // Fit the map: to the selected item's path/point, else to everything. Each
  // fit runs once per change of *intent* — picking a different item, or the set
  // of plotted points changing while nothing's selected. A live data refresh
  // (an active flight's position/track updating, an SSE poll) must NOT re-fit,
  // or it would keep yanking the map back from a zoom the user set by hand.
  const fitKeyRef = useRef('');
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const run = () => {
      // On mobile the bottom sheet covers the lower part of the map: pad fits
      // so the focus lands in the visible strip above it (clamped to half the
      // pane so fitBounds always has room; at the full snap the sheet covers
      // most of the pane anyway, so the clamp deliberately favours a sane fit
      // over exact visibility — the user is reading the list at that point).
      const paneH = containerRef.current?.clientHeight ?? 0;
      const { mobile: isMobile, snap: curSnap } = sheetSnapRef.current;
      const sheetPad = isMobile
        ? Math.min(sheetHeightPx(curSnap, paneH), Math.round(paneH * 0.5))
        : 0;
      const boundsPad = sheetPad > 0 ? { top: 80, left: 80, right: 80, bottom: 80 + sheetPad } : 80;
      // flyTo/easeTo `padding` would persist on the camera and skew or break every
      // later fitBounds (which adds persistent padding on top of its own); `offset`
      // is per-animation only, so the camera stays stateless. Shifting the target
      // up by half the sheet height centres it in the visible strip above the sheet.
      const flyOffset =
        sheetPad > 0 ? { offset: [0, -Math.round(sheetPad / 2)] as [number, number] } : {};
      if (selected) {
        // Re-fit only when the selection changes, not when the selected part's
        // own data refreshes (which would otherwise reset a manual zoom).
        const key = `sel:${selected.id}`;
        if (key === fitKeyRef.current) return;
        fitKeyRef.current = key;
        const pts = partCoords(selected);
        if (pts.length === 1) {
          map.flyTo({ center: pts[0], zoom: 11, duration: 600, ...flyOffset });
        } else if (pts.length > 1) {
          const b = boundsOf(pts);
          if (b) map.fitBounds(b, { padding: boundsPad, maxZoom: 9, duration: 600 });
        }
        return;
      }
      // Nothing selected: frame all points, but only when the set changes.
      const all = ordered.flatMap(partCoords);
      const key = all.map((c) => `${c[0]},${c[1]}`).join(';');
      if (key === fitKeyRef.current) return;
      fitKeyRef.current = key;
      const b = boundsOf(all);
      if (all.length === 1) map.flyTo({ center: all[0], zoom: 9, duration: 600, ...flyOffset });
      else if (b) map.fitBounds(b, { padding: boundsPad, maxZoom: 9, duration: 600 });
    };
    if (map.isStyleLoaded() && readyRef.current) run();
    else map.once('idle', run);
  }, [ordered, selected, selectedId]);

  const timeSlider = timeDomain.show ? (
    <TimeSlider
      start={timeDomain.start}
      end={timeDomain.end}
      value={valueMs}
      liveEdge={liveEdge}
      inProgress={timeDomain.inProgress}
      onScrub={(ms) =>
        // Snapping to the right edge of a running trip re-locks the live
        // view (null); anywhere else is a fixed past instant.
        setScrubMs(timeDomain.inProgress && ms >= timeDomain.end - SCRUB_STEP_MS ? null : ms)
      }
      onReset={() => setScrubMs(null)}
    />
  ) : null;

  const listContent = (
    <>
      {controls && <Box sx={{ p: 2, pb: 1 }}>{controls}</Box>}
      {strandedCount > 0 && (
        <Box sx={{ px: 2, pt: 1 }}>
          <Alert severity="warning" sx={{ py: 0 }}>
            {strandedCount} location{strandedCount === 1 ? '' : 's'} couldn&apos;t be placed on the
            map — open the item to fix its address.
          </Alert>
        </Box>
      )}
      {ordered.length === 0 ? (
        <Box sx={{ p: 2 }}>
          <Typography variant="body2" color="text.secondary">
            {loading ? 'Loading…' : 'No mappable plans in view.'}
          </Typography>
        </Box>
      ) : (
        <List dense disablePadding>
          {ordered.map((p) => (
            <PartRow
              key={p.id}
              part={p}
              selected={p.id === selectedId}
              onToggle={() => setSelectedId((cur) => (cur === p.id ? null : p.id))}
            />
          ))}
        </List>
      )}
    </>
  );

  return (
    <Box sx={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'row' }}>
      {/* The map pane is the first child in both layouts so React keeps the
          element (and the MapLibre instance bound to it) across a breakpoint
          flip; only the sibling sidebar / overlay sheet comes and goes. */}
      <Box sx={{ position: 'relative', flexGrow: 1, minWidth: 0, minHeight: 240 }}>
        <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} data-testid="plan-map" />
        {!mobile && timeSlider}
        {mobile && (
          <BottomSheet
            snap={snap}
            onSnapChange={setSnap}
            above={timeSlider}
            header={
              <SheetPeekHeader selected={selected} count={ordered.length} loading={loading} />
            }
          >
            {listContent}
          </BottomSheet>
        )}
      </Box>
      {!mobile && (
        <Box
          sx={{
            width: 320,
            borderLeft: 1,
            borderColor: 'divider',
            overflowY: 'auto',
          }}
        >
          {listContent}
        </Box>
      )}
    </Box>
  );
}

function PartRow({
  part,
  selected,
  onToggle,
}: {
  part: PlanPart;
  selected: boolean;
  onToggle: () => void;
}) {
  return (
    <Box>
      <ListItemButton selected={selected} onClick={onToggle} data-testid={`plan-row-${part.id}`}>
        <Box
          component="span"
          sx={{
            width: 12,
            height: 12,
            borderRadius: '50%',
            bgcolor: planTypeColor(part.type),
            // Type colour fill + a person-coloured ring, mirroring the map pins
            // (issue #13). No ring when the trip owner is unknown.
            border: personColor(part.trip_owner_id)
              ? `2px solid ${personColor(part.trip_owner_id)}`
              : 'none',
            boxSizing: 'border-box',
            flex: 'none',
            mr: 1.5,
          }}
        />
        <ListItemText
          primary={partTitle(part)}
          secondary={[partSummary(part), part.owner ? `Added by ${userName(part.owner)}` : '']
            .filter(Boolean)
            .join(' · ')}
        />
        {part.passengers && part.passengers.length > 0 && (
          <AvatarGroup
            max={4}
            sx={{ ml: 1, '& .MuiAvatar-root': { width: 24, height: 24, fontSize: 12 } }}
          >
            {part.passengers.map((u) => (
              <Tooltip key={u.id} title={userName(u)}>
                <Avatar src={u.avatar_url || undefined} alt={userName(u)}>
                  {userInitial(u)}
                </Avatar>
              </Tooltip>
            ))}
          </AvatarGroup>
        )}
      </ListItemButton>
      <Collapse in={selected} unmountOnExit>
        <Box sx={{ px: 2, py: 1.5, bgcolor: 'action.hover' }}>
          {part.type === 'flight' && part.flight ? (
            <FlightDetailCard flight={part.flight} startTz={part.start_tz} endTz={part.end_tz} />
          ) : (
            <PartDetailBlock part={part} />
          )}
        </Box>
      </Collapse>
    </Box>
  );
}

/** The bottom sheet's always-visible summary row: the selected plan's
 * one-liner, else how many plans the sheet holds. */
function SheetPeekHeader({
  selected,
  count,
  loading,
}: {
  selected: PlanPart | null;
  count: number;
  loading?: boolean;
}) {
  return (
    <Box sx={{ px: 2, pb: 1 }} data-testid="sheet-peek">
      {selected ? (
        <>
          <Typography variant="body2" noWrap sx={{ fontWeight: 600 }}>
            {partTitle(selected)}
          </Typography>
          <Typography variant="caption" color="text.secondary" noWrap component="div">
            {partSummary(selected)}
          </Typography>
        </>
      ) : (
        <Typography variant="body2" color="text.secondary">
          {loading && count === 0
            ? 'Loading…'
            : count === 0
              ? 'No plans'
              : `${count} plan${count === 1 ? '' : 's'}`}
        </Typography>
      )}
    </Box>
  );
}

/** Scrubber granularity: one minute is fine enough to track a plane yet keeps
 * the live re-lock snap (the right edge) a comfortable target. */
const SCRUB_STEP_MS = 60_000;

/** The bottom-of-map time scrubber. Drag left to replay where flights were in
 * the past; the right edge is the live view (a running trip) or the trip's end
 * (a past one), and a still-running trip re-locks to live when dragged back to
 * that edge. */
function TimeSlider({
  start,
  end,
  value,
  liveEdge,
  inProgress,
  onScrub,
  onReset,
}: {
  start: number;
  end: number;
  value: number;
  liveEdge: boolean;
  inProgress: boolean;
  onScrub: (ms: number) => void;
  onReset: () => void;
}) {
  return (
    <Box
      data-testid="time-slider"
      sx={{
        position: 'absolute',
        left: 0,
        right: 0,
        bottom: 0,
        zIndex: 2,
        px: 2,
        py: 0.75,
        // Theme-aware translucent panel so the time stays legible in dark mode
        // too (a hardcoded white panel left light text invisible).
        bgcolor: (theme) => alpha(theme.palette.background.paper, 0.92),
        color: 'text.primary',
        borderTop: 1,
        borderColor: 'divider',
      }}
    >
      {/* Status + time on their own row, the slider full-width below. Keeping
          the slider on a separate row of constant width means dragging off the
          live edge (chip → caption, reset button appearing) can't resize it —
          which otherwise made the thumb jump/flash — and the slider's ends can
          never overlap the LIVE label. */}
      <Stack direction="row" spacing={1} alignItems="center" sx={{ height: 24, mb: 0.25 }}>
        {/* Fixed-width status cell so the time doesn't shift as the badge
            (LIVE) swaps for the "Positions at" caption. */}
        <Box sx={{ width: 92, flex: 'none', display: 'flex', alignItems: 'center' }}>
          {liveEdge ? (
            <Chip
              label="● LIVE"
              size="small"
              color="error"
              data-testid="time-slider-live"
              sx={{ height: 18, fontSize: 11, fontWeight: 700, '& .MuiChip-label': { px: 0.75 } }}
            />
          ) : (
            <Typography variant="caption" color="text.secondary">
              Positions at
            </Typography>
          )}
        </Box>
        <Typography
          variant="body2"
          data-testid="time-slider-time"
          sx={{ fontVariantNumeric: 'tabular-nums' }}
        >
          {fmtScrubTime(value)}
        </Typography>
        {!liveEdge && (
          <Button
            size="small"
            onClick={onReset}
            // Trim vertical padding so the button fits the fixed 24px status
            // row; otherwise its intrinsic ~30px height grows the panel when it
            // appears on scrubbing off the live edge.
            sx={{ ml: 'auto', flex: 'none', minHeight: 0, py: 0 }}
          >
            {inProgress ? 'Live' : 'Latest'}
          </Button>
        )}
      </Stack>
      <Slider
        size="small"
        min={start}
        max={end}
        step={SCRUB_STEP_MS}
        value={value}
        onChange={(_, v) => onScrub(v as number)}
        aria-label="Scrub flight positions back in time"
        sx={{ display: 'block', width: '100%', py: 0.5 }}
      />
    </Box>
  );
}

// --- helpers ----------------------------------------------------------------

/** A human title for a row: a flight's ident, else its place line, else type. */
function partTitle(part: PlanPart): string {
  if (part.type === 'flight' && part.flight?.ident) return part.flight.ident;
  return fmtPartPlaces(part.type, part.start_label, part.end_label) || planTypeLabel(part.type);
}

/** The '·'-joined secondary line for a part: type · supplier · time range. */
function partSummary(part: PlanPart): string {
  return [planTypeLabel(part.type), part.supplier_name, fmtPartTimeRange(part)]
    .filter(Boolean)
    .join(' · ');
}

function hasCoord(p: PlanPart): boolean {
  return (p.start_lat != null && p.start_lon != null) || (p.end_lat != null && p.end_lon != null);
}

/** Every plotted coordinate of a part (start, end, and a selected flight's
 * flown-track points), for fitting the map. */
function partCoords(p: PlanPart): [number, number][] {
  const out: [number, number][] = [];
  if (p.start_lat != null && p.start_lon != null) out.push([p.start_lon, p.start_lat]);
  if (p.end_lat != null && p.end_lon != null) out.push([p.end_lon, p.end_lat]);
  for (const t of p.flight?.track ?? []) out.push([t.lon, t.lat]);
  return out;
}

function hasBothEnds(p: PlanPart): boolean {
  return p.start_lat != null && p.start_lon != null && p.end_lat != null && p.end_lon != null;
}

/** A geocoded endpoint of a part, for plotting a pin + its tooltip. */
interface Endpoint {
  role: 'start' | 'end';
  lat: number;
  lon: number;
  label: string;
  iso: string;
  tz?: string;
}

function endpoints(p: PlanPart): Endpoint[] {
  const start: Endpoint | null =
    p.start_lat != null && p.start_lon != null
      ? {
          role: 'start',
          lat: p.start_lat,
          lon: p.start_lon,
          label: p.start_label,
          iso: p.starts_at,
          tz: p.start_tz,
        }
      : null;
  const end: Endpoint | null =
    p.end_lat != null && p.end_lon != null
      ? {
          role: 'end',
          lat: p.end_lat,
          lon: p.end_lon,
          label: p.end_label,
          iso: p.ends_at ?? p.starts_at,
          tz: p.end_tz || p.start_tz,
        }
      : null;
  return [start, end].filter((e): e is Endpoint => e != null);
}

/** A north-pointing plane glyph in `color` (the owner's person colour, else the
 * flight type colour); the marker is rotated to the heading and dimmed by the
 * caller when the position is estimated. */
function buildPlaneEl(color: string): HTMLElement {
  const el = document.createElement('div');
  el.style.cursor = 'pointer';
  el.style.color = color;
  el.style.lineHeight = '0';
  el.innerHTML = `
    <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor"
         style="filter: drop-shadow(0 1px 2px rgba(0,0,0,0.4))">
      <path d="M12 2 L13.2 11 L22 15 L22 17 L13.2 14.5 L13 20 L16 22 L16 23 L12 22 L8 23 L8 22 L11 20 L10.8 14.5 L2 17 L2 15 L10.8 11 Z"/>
    </svg>`;
  return el;
}

function legsFC(parts: PlanPart[], selectedId: number | null): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const p of parts) {
    if (!isTransferType(p.type) || !hasBothEnds(p)) continue;
    const arc = toMultiLine(greatCircle(p.start_lat!, p.start_lon!, p.end_lat!, p.end_lon!));
    if (arc.length === 0) continue;
    features.push({
      type: 'Feature',
      // Ground transfers get a neutral grey crow-flight line (it's an
      // as-the-crow-flies connector, not an actual driven route). Flights/trains
      // take the owner's person colour (issue #13), falling back to the type
      // colour when the trip owner is unknown.
      properties: {
        partId: p.id,
        color:
          p.type === 'ground'
            ? GROUND_LEG_COLOR
            : (personColor(p.trip_owner_id) ?? planTypeColor(p.type)),
        selected: p.id === selectedId,
      },
      geometry:
        arc.length === 1
          ? { type: 'LineString', coordinates: arc[0] }
          : { type: 'MultiLineString', coordinates: arc },
    });
  }
  return { type: 'FeatureCollection', features };
}

function emptyFC(): GeoJSON.FeatureCollection {
  return { type: 'FeatureCollection', features: [] };
}

function boundsOf(pts: [number, number][]): LngLatBoundsLike | null {
  if (pts.length === 0) return null;
  let w = pts[0][0];
  let e = pts[0][0];
  let s = pts[0][1];
  let n = pts[0][1];
  for (const [lon, lat] of pts) {
    w = Math.min(w, lon);
    e = Math.max(e, lon);
    s = Math.min(s, lat);
    n = Math.max(n, lat);
  }
  return [
    [w, s],
    [e, n],
  ];
}
