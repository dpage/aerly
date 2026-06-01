import { useEffect, useMemo, useRef } from 'react';
import maplibregl, {
  type LngLatBoundsLike,
  type Map as MlMap,
  type StyleSpecification,
} from 'maplibre-gl';
import {
  Box,
  List,
  ListItemButton,
  ListItemText,
  Typography,
} from '@mui/material';

import { useStore } from '../state/store';
import type { Plan, PlanPart, PlanType } from '../api/types';
import { greatCircle, toMultiLine } from '../lib/great-circle';
import { fmtTimeOfDay, planTypeLabel } from '../lib/trip-format';
import { buildMarkerPopup, buildPinEl, planTypeColor } from '../lib/plan-marker';

// Standard OSM raster style for the trip map (spec §11: MapLibre).
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

/** A geocoded endpoint extracted from a part, for plotting on the map. */
interface MapPoint {
  lat: number;
  lon: number;
  /** Plan title, shown bold in the marker popover. */
  title: string;
  type: PlanType;
  /** Place name at this endpoint (start_label / end_label). */
  location: string;
  /** Instant + tz of this endpoint, for the popover's local date/time. */
  iso: string;
  tz?: string;
}

/** Secondary trip detail tab (spec §11): the trip's geocoded parts on a
 * MapLibre map. Each part with start/end coordinates contributes a point (and,
 * when both ends are known, a great-circle leg). */
export default function TripMap() {
  const currentTrip = useStore((s) => s.currentTrip);
  const plans = useMemo(() => currentTrip?.plans ?? [], [currentTrip]);

  const points = useMemo(() => collectPoints(plans), [plans]);
  const legsFC = useMemo(() => buildLegs(plans), [plans]);
  const events = useMemo(() => collectEvents(plans), [plans]);

  const containerRef = useRef<HTMLElement | null>(null);
  const mapRef = useRef<MlMap | null>(null);
  const markersRef = useRef<maplibregl.Marker[]>([]);

  useEffect(() => {
    if (!containerRef.current) return;
    const map = new maplibregl.Map({
      container: containerRef.current,
      style: STYLE,
      center: [5, 50],
      zoom: 3,
    });
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    map.on('load', () => {
      map.addSource('legs', { type: 'geojson', data: emptyFC() });
      map.addLayer({
        id: 'legs-line',
        type: 'line',
        source: 'legs',
        paint: { 'line-color': '#1f5fa8', 'line-width': 2, 'line-opacity': 0.7 },
      });
    });
    mapRef.current = map;
    return () => {
      markersRef.current.forEach((m) => m.remove());
      markersRef.current = [];
      map.remove();
      mapRef.current = null;
    };
  }, []);

  // Sync leg lines.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const apply = () => {
      (map.getSource('legs') as maplibregl.GeoJSONSource | undefined)?.setData(legsFC);
    };
    if (map.isStyleLoaded()) apply();
    else map.once('load', apply);
  }, [legsFC]);

  // Sync point markers and fit the map to all points.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    markersRef.current.forEach((m) => m.remove());
    markersRef.current = [];
    for (const p of points) {
      const el = buildPinEl(p.type);
      el.title = p.location || p.title;
      const marker = new maplibregl.Marker({ element: el, anchor: 'bottom' })
        .setLngLat([p.lon, p.lat])
        .setPopup(
          new maplibregl.Popup({ offset: 30, closeButton: false }).setDOMContent(
            buildMarkerPopup({ title: p.title, type: p.type, location: p.location, iso: p.iso, tz: p.tz }),
          ),
        )
        .addTo(map);
      markersRef.current.push(marker);
    }
    const bounds = boundsFor(points.map((p) => [p.lon, p.lat] as [number, number]));
    if (bounds) {
      const fit = () => map.fitBounds(bounds, { padding: 80, maxZoom: 9, duration: 600 });
      if (map.isStyleLoaded()) fit();
      else map.once('load', fit);
    }
  }, [points]);

  if (currentTrip && points.length === 0) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary">
          No mappable plans yet. Plans with a location appear here once added.
        </Typography>
      </Box>
    );
  }

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
        <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} />
      </Box>
      <Box
        sx={{
          width: { xs: '100%', md: 280 },
          borderLeft: { md: 1 },
          borderTop: { xs: 1, md: 0 },
          borderColor: 'divider',
          overflowY: 'auto',
        }}
      >
        <TripEventList
          events={events}
          onFocus={(lat, lon) =>
            mapRef.current?.flyTo({ center: [lon, lat], zoom: 12, duration: 600 })
          }
        />
      </Box>
    </Box>
  );
}

/** A clickable event in the trip-map side list. */
interface TripEvent {
  key: string;
  type: PlanType;
  title: string;
  iso: string;
  tz?: string;
  lat?: number;
  lon?: number;
}

/** One row per non-dismissed part, chronological. Tied to the map: clicking a
 * row with coordinates pans the map to it, so "where's dinner?" is one tap. */
function collectEvents(plans: Plan[]): TripEvent[] {
  const out: TripEvent[] = [];
  for (const plan of plans) {
    const title = plan.title || planTypeLabel(plan.type);
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      const lat = part.start_lat ?? part.end_lat ?? undefined;
      const lon = part.start_lon ?? part.end_lon ?? undefined;
      out.push({
        key: `p${part.id}`,
        type: part.type,
        title,
        iso: part.effective_at ?? part.starts_at,
        tz: part.start_tz,
        lat: lat ?? undefined,
        lon: lon ?? undefined,
      });
    }
  }
  return out.sort((a, b) => a.iso.localeCompare(b.iso));
}

function TripEventList({
  events,
  onFocus,
}: {
  events: TripEvent[];
  onFocus: (lat: number, lon: number) => void;
}) {
  if (events.length === 0) return null;
  return (
    <List dense disablePadding>
      {events.map((e) => {
        const secondary = [planTypeLabel(e.type), fmtTimeOfDay(e.iso, e.tz)]
          .filter(Boolean)
          .join(' · ');
        const hasCoords = e.lat != null && e.lon != null;
        return (
          <ListItemButton
            key={e.key}
            disabled={!hasCoords}
            onClick={() => hasCoords && onFocus(e.lat!, e.lon!)}
          >
            <Box
              component="span"
              sx={{
                width: 10,
                height: 10,
                borderRadius: '50%',
                bgcolor: planTypeColor(e.type),
                flex: 'none',
                mr: 1.5,
              }}
            />
            <ListItemText primary={e.title} secondary={secondary || undefined} />
          </ListItemButton>
        );
      })}
    </List>
  );
}

function collectPoints(plans: Plan[]): MapPoint[] {
  const points: MapPoint[] = [];
  for (const plan of plans) {
    const title = plan.title || planTypeLabel(plan.type);
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      if (part.start_lat != null && part.start_lon != null) {
        points.push({
          lat: part.start_lat,
          lon: part.start_lon,
          title,
          type: part.type,
          location: part.start_label || plan.title,
          iso: part.starts_at,
          tz: part.start_tz,
        });
      }
      if (part.end_lat != null && part.end_lon != null) {
        points.push({
          lat: part.end_lat,
          lon: part.end_lon,
          title,
          type: part.type,
          location: part.end_label || plan.title,
          iso: part.ends_at ?? part.starts_at,
          tz: part.end_tz || part.start_tz,
        });
      }
    }
  }
  return points;
}

function buildLegs(plans: Plan[]): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const plan of plans) {
    for (const part of plan.parts) {
      if (part.dismissed_at) continue;
      if (!hasBothEnds(part)) continue;
      const gc = greatCircle(part.start_lat!, part.start_lon!, part.end_lat!, part.end_lon!);
      const parts = toMultiLine(gc);
      if (parts.length === 0) continue;
      features.push({
        type: 'Feature',
        properties: { id: part.id },
        geometry:
          parts.length === 1
            ? { type: 'LineString', coordinates: parts[0] }
            : { type: 'MultiLineString', coordinates: parts },
      });
    }
  }
  return { type: 'FeatureCollection', features };
}

function hasBothEnds(part: PlanPart): boolean {
  return (
    part.start_lat != null &&
    part.start_lon != null &&
    part.end_lat != null &&
    part.end_lon != null
  );
}

function emptyFC(): GeoJSON.FeatureCollection {
  return { type: 'FeatureCollection', features: [] };
}

function boundsFor(pts: [number, number][]): LngLatBoundsLike | null {
  if (pts.length < 2) return null;
  let west = pts[0][0],
    east = pts[0][0],
    south = pts[0][1],
    north = pts[0][1];
  for (const [lon, lat] of pts) {
    west = Math.min(west, lon);
    east = Math.max(east, lon);
    south = Math.min(south, lat);
    north = Math.max(north, lat);
  }
  return [
    [west, south],
    [east, north],
  ];
}
