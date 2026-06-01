import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import maplibregl, {
  type ExpressionSpecification,
  type LngLatBoundsLike,
  type Map as MlMap,
  type MapGeoJSONFeature,
  type StyleSpecification,
} from 'maplibre-gl';
import { Box, Collapse, List, ListItemButton, ListItemText, Typography } from '@mui/material';

import type { PlanPart, PlanType } from '../api/types';
import { greatCircle, toMultiLine } from '../lib/great-circle';
import { planTypeColor } from '../lib/plan-marker';
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

const POINTS = 'pmv-points';
const LEGS = 'pmv-legs';
const TRACK = 'pmv-track';

const PLAN_TYPES: PlanType[] = ['flight', 'train', 'hotel', 'ground', 'dining', 'excursion'];

// Data-driven colour for a feature, keyed off its `type` property — the same
// per-type palette the pins use (web/src/lib/plan-marker.ts). Built as a plain
// array and cast: the `match` tuple shape doesn't survive a spread in TS.
const typeColor = [
  'match',
  ['get', 'type'],
  ...PLAN_TYPES.flatMap((t) => [t, planTypeColor(t)]),
  '#6b7280',
] as unknown as ExpressionSpecification;

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

  // Mappable parts only (need at least one coordinate), time-ordered.
  const ordered = useMemo(() => {
    return parts
      .filter((p) => !p.dismissed_at && hasCoord(p))
      .slice()
      .sort((a, b) =>
        (a.effective_at ?? a.starts_at).localeCompare(b.effective_at ?? b.starts_at),
      );
  }, [parts]);

  const selected = ordered.find((p) => p.id === selectedId) ?? null;

  // Keep selection valid as the parts change (e.g. SSE refresh removes a part).
  useEffect(() => {
    if (selectedId != null && !ordered.some((p) => p.id === selectedId)) setSelectedId(null);
  }, [ordered, selectedId]);

  // --- map init (once) ------------------------------------------------------
  useEffect(() => {
    if (!containerRef.current) return;
    const map = new maplibregl.Map({
      container: containerRef.current,
      style: STYLE,
      center: [5, 50],
      zoom: 3,
    });
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    mapRef.current = map;
    map.once('load', () => {
      for (const id of [LEGS, TRACK, POINTS]) {
        map.addSource(id, { type: 'geojson', data: emptyFC() });
      }
      map.addLayer({
        id: LEGS,
        type: 'line',
        source: LEGS,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: {
          'line-color': typeColor,
          'line-width': ['case', ['get', 'selected'], 4, 2],
          'line-opacity': ['case', ['get', 'selected'], 1, 0.45],
        },
      });
      map.addLayer({
        id: TRACK,
        type: 'line',
        source: TRACK,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: { 'line-color': '#d97706', 'line-width': 3 },
      });
      map.addLayer({
        id: POINTS,
        type: 'circle',
        source: POINTS,
        paint: {
          'circle-color': typeColor,
          'circle-radius': ['case', ['get', 'selected'], 9, 6],
          'circle-stroke-width': 2,
          'circle-stroke-color': '#fff',
          'circle-opacity': ['case', ['get', 'dim'], 0.4, 1],
        },
      });
      readyRef.current = true;
      // Select on feature click (both layers); pointer cursor on hover.
      for (const layer of [POINTS, LEGS]) {
        map.on('click', layer, (e) => {
          const f = e.features?.[0] as MapGeoJSONFeature | undefined;
          const pid = f?.properties?.partId;
          if (typeof pid === 'number') setSelectedId((cur) => (cur === pid ? null : pid));
        });
        map.on('mouseenter', layer, () => {
          map.getCanvas().style.cursor = 'pointer';
        });
        map.on('mouseleave', layer, () => {
          map.getCanvas().style.cursor = '';
        });
      }
      // Trigger the first data sync now that the sources exist.
      syncRef.current?.();
    });
    return () => {
      readyRef.current = false;
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
    const pointsSrc = map.getSource(POINTS) as maplibregl.GeoJSONSource | undefined;
    const legsSrc = map.getSource(LEGS) as maplibregl.GeoJSONSource | undefined;
    const trackSrc = map.getSource(TRACK) as maplibregl.GeoJSONSource | undefined;
    if (!pointsSrc || !legsSrc || !trackSrc) return;
    const anySel = selectedId != null;
    pointsSrc.setData(pointsFC(ordered, selectedId, anySel));
    legsSrc.setData(legsFC(ordered, selectedId));
    trackSrc.setData(trackFC(selected));
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
            width: 10,
            height: 10,
            borderRadius: '50%',
            bgcolor: planTypeColor(part.type),
            flex: 'none',
            mr: 1.5,
          }}
        />
        <ListItemText
          primary={partTitle(part)}
          secondary={[planTypeLabel(part.type), fmtPartTimeRange(part)].filter(Boolean).join(' · ')}
        />
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

function pointsFC(parts: PlanPart[], selectedId: number | null, anySel: boolean): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const p of parts) {
    const sel = p.id === selectedId;
    const props = { partId: p.id, type: p.type, selected: sel, dim: anySel && !sel };
    if (p.start_lat != null && p.start_lon != null) {
      features.push(pt([p.start_lon, p.start_lat], props));
    }
    if (p.end_lat != null && p.end_lon != null) {
      features.push(pt([p.end_lon, p.end_lat], props));
    }
  }
  return { type: 'FeatureCollection', features };
}

function legsFC(parts: PlanPart[], selectedId: number | null): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const p of parts) {
    if (!isTransferType(p.type) || !hasBothEnds(p)) continue;
    const arc = toMultiLine(greatCircle(p.start_lat!, p.start_lon!, p.end_lat!, p.end_lon!));
    if (arc.length === 0) continue;
    features.push({
      type: 'Feature',
      properties: { partId: p.id, type: p.type, selected: p.id === selectedId },
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

function pt(coordinates: [number, number], properties: Record<string, unknown>): GeoJSON.Feature {
  return { type: 'Feature', properties, geometry: { type: 'Point', coordinates } };
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
