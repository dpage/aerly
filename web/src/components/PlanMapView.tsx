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
  Collapse,
  List,
  ListItemButton,
  ListItemText,
  Tooltip,
  Typography,
} from '@mui/material';

import type { PlanPart } from '../api/types';
import { unlocatedCount } from '../lib/geo';
import { greatCircle, toMultiLine } from '../lib/great-circle';
import { userInitial, userName } from '../lib/format';
import { buildMarkerPopup, buildPinEl, planTypeColor } from '../lib/plan-marker';
import { personColor } from '../lib/person-color';
import {
  fmtPartPlaces,
  fmtPartTimeRange,
  isTransferType,
  planTypeLabel,
} from '../lib/trip-format';
import FlightDetailCard from './FlightDetailCard';
import PartDetailBlock from './PartDetailBlock';

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
  const mapRef = useRef<MlMap | null>(null);
  const readyRef = useRef(false);
  // One teardrop pin per geocoded endpoint, keyed by part + role + coordinate.
  const markersRef = useRef<Map<string, maplibregl.Marker>>(new Map());
  // One plane icon per flight part — at its live position when airborne, else
  // parked at the origin (not departed) or destination (arrived). Keyed by part id.
  const planesRef = useRef<Map<number, maplibregl.Marker>>(new Map());

  // Mappable parts only (need at least one coordinate), time-ordered.
  const ordered = useMemo(() => {
    return parts
      .filter((p) => !p.dismissed_at && hasCoord(p))
      .slice()
      .sort((a, b) =>
        (a.effective_at ?? a.starts_at).localeCompare(b.effective_at ?? b.starts_at),
      );
  }, [parts]);

  const strandedCount = useMemo(() => unlocatedCount(parts), [parts]);

  const selected = ordered.find((p) => p.id === selectedId) ?? null;

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
    });
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
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
    trackSrc.setData(trackFC(selected));

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
          el.addEventListener('click', () =>
            setSelectedId((cur) => (cur === p.id ? null : p.id)),
          );
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

    // Sync plane icons: one per flight part, rotated to its heading and dimmed
    // when the position is dead-reckoned. Parked at the origin/destination when
    // the flight hasn't departed / has landed (matches the old pure tracker).
    const livePlanes = new Set<number>();
    for (const p of ordered) {
      const place = planePlacement(p);
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
  }, [ordered, selectedId]);

  // Fit the map: to the selected item's path/point, else to everything.
  const fitKeyRef = useRef('');
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const run = () => {
      if (selected) {
        const pts = partCoords(selected);
        if (pts.length === 1) {
          map.flyTo({ center: pts[0], zoom: 11, duration: 600 });
        } else if (pts.length > 1) {
          const b = boundsOf(pts);
          if (b) map.fitBounds(b, { padding: 80, maxZoom: 9, duration: 600 });
        }
        return;
      }
      // Nothing selected: frame all points, but only when the set changes.
      const all = ordered.flatMap(partCoords);
      const key = all.map((c) => `${c[0]},${c[1]}`).join(';');
      if (key === fitKeyRef.current) return;
      fitKeyRef.current = key;
      const b = boundsOf(all);
      if (all.length === 1) map.flyTo({ center: all[0], zoom: 9, duration: 600 });
      else if (b) map.fitBounds(b, { padding: 80, maxZoom: 9, duration: 600 });
    };
    if (map.isStyleLoaded() && readyRef.current) run();
    else map.once('idle', run);
  }, [ordered, selected, selectedId]);

  return (
    <Box
      sx={{
        position: 'absolute',
        inset: 0,
        display: 'flex',
        flexDirection: { xs: 'column', md: 'row' },
      }}
    >
      <Box sx={{ position: 'relative', flexGrow: 1, minHeight: 240 }}>
        <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} data-testid="plan-map" />
      </Box>
      <Box
        sx={{
          width: { xs: '100%', md: 320 },
          borderLeft: { md: 1 },
          borderTop: { xs: 1, md: 0 },
          borderColor: 'divider',
          overflowY: 'auto',
        }}
      >
        {controls && <Box sx={{ p: 2, pb: 1 }}>{controls}</Box>}
        {strandedCount > 0 && (
          <Box sx={{ px: 2, pt: 1 }}>
            <Alert severity="warning" sx={{ py: 0 }}>
              {strandedCount} location{strandedCount === 1 ? '' : 's'} couldn&apos;t
              be placed on the map — open the item to fix its address.
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
      </Box>
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
          secondary={[
            planTypeLabel(part.type),
            part.supplier_name,
            fmtPartTimeRange(part),
            part.owner ? `Added by ${userName(part.owner)}` : '',
          ]
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

// --- helpers ----------------------------------------------------------------

/** A human title for a row: a flight's ident, else its place line, else type. */
function partTitle(part: PlanPart): string {
  if (part.type === 'flight' && part.flight?.ident) return part.flight.ident;
  return fmtPartPlaces(part.type, part.start_label, part.end_label) || planTypeLabel(part.type);
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
  return (
    p.start_lat != null && p.start_lon != null && p.end_lat != null && p.end_lon != null
  );
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

/** Where to draw a flight's plane icon, and how. Live position when we have one
 * (estimated/dead-reckoned included); else parked at the origin before
 * departure or the destination once arrived. null for non-flight parts or a
 * flight with no usable coordinate. */
interface PlanePlacement {
  lon: number;
  lat: number;
  heading: number;
  estimated: boolean;
}

function planePlacement(p: PlanPart): PlanePlacement | null {
  if (p.type !== 'flight') return null;
  const hasStart = p.start_lat != null && p.start_lon != null;
  const hasEnd = p.end_lat != null && p.end_lon != null;
  // Landed: park at the destination, regardless of the last tracked fix.
  if (p.flight?.flight_status === 'Arrived' && hasEnd) {
    return { lon: p.end_lon!, lat: p.end_lat!, heading: 0, estimated: false };
  }
  // Airborne (or dead-reckoned through an ADS-B gap): the live position.
  const pos = p.flight?.latest_position;
  if (pos) {
    return { lon: pos.lon, lat: pos.lat, heading: pos.heading_deg ?? 0, estimated: pos.is_estimated };
  }
  // Not departed yet: park at the origin.
  if (hasStart) return { lon: p.start_lon!, lat: p.start_lat!, heading: 0, estimated: false };
  if (hasEnd) return { lon: p.end_lon!, lat: p.end_lat!, heading: 0, estimated: false };
  return null;
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

function trackFC(selected: PlanPart | null): GeoJSON.FeatureCollection {
  const track = selected?.flight?.track ?? [];
  if (track.length < 2) return emptyFC();
  return {
    type: 'FeatureCollection',
    features: [
      {
        type: 'Feature',
        properties: {},
        geometry: { type: 'LineString', coordinates: track.map((t) => [t.lon, t.lat]) },
      },
    ],
  };
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
