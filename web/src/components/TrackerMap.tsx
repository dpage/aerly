import { useEffect, useRef } from 'react';
import maplibregl, {
  type LngLatBoundsLike,
  type Map as MlMap,
  type StyleSpecification,
} from 'maplibre-gl';
import { Box } from '@mui/material';

import type { PlanPart, TrackerMarker, TrackerPart } from '../api/types';
import { greatCircle, toMultiLine } from '../lib/great-circle';
import { buildMarkerPopup, buildPinEl } from '../lib/plan-marker';

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

interface TrackerMapProps {
  /** Parts to plot — only those with a `latest_position` get a marker. */
  parts: TrackerPart[];
  /** In-window non-flight venues (hotels, taxis…) to overlay as static pins.
   * Shown only in the convergence view, not the single-flight focus. */
  markers?: TrackerMarker[];
  /** When set, the map focuses (fits to) the single part with this id. */
  focusedPartId?: number | null;
  /** The focused part's full detail (with its flown track), loaded for the
   * single-flight view. When present and a flight, the map draws the flown
   * track polyline + the planned great-circle. */
  focusedPart?: PlanPart | null;
}

const TRACK_SOURCE = 'focus-track';
const GC_SOURCE = 'focus-gc';

function emptyFeatureCollection(): GeoJSON.FeatureCollection {
  return { type: 'FeatureCollection', features: [] };
}

/** MultiLineString feature collection from a list of [lon,lat] line segments. */
function multiLineCollection(lines: [number, number][][]): GeoJSON.FeatureCollection {
  return {
    type: 'FeatureCollection',
    features: lines.length
      ? [
          {
            type: 'Feature',
            properties: {},
            geometry: { type: 'MultiLineString', coordinates: lines },
          },
        ]
      : [],
  };
}

/** Convergence map for the tracker (PRD §6.5). Plots the latest known position
 * of every in-window trackable part as a labelled marker — "who's on their way"
 * as a single live map, no ranking. When `focusedPartId` is set the view fits
 * to that one part (the single-flight focus opened from a timeline card).
 *
 * Uses the standard MapLibre lifecycle (init once, sync markers on data
 * change, fit bounds) over the lighter `TrackerPart` shape, which only carries
 * a latest position. */
export default function TrackerMap({ parts, markers = [], focusedPartId, focusedPart }: TrackerMapProps) {
  const containerRef = useRef<HTMLElement | null>(null);
  const mapRef = useRef<MlMap | null>(null);
  const markersRef = useRef<Map<number, maplibregl.Marker>>(new Map());
  // Venue overlay markers, keyed by part id + coordinate (a taxi has two).
  const venueRef = useRef<Map<string, maplibregl.Marker>>(new Map());

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
    // Sources + layers for the single-flight focus: the planned great-circle
    // (dashed) under the flown track (solid). Added once the style is ready.
    map.once('load', () => {
      map.addSource(GC_SOURCE, { type: 'geojson', data: emptyFeatureCollection() });
      map.addSource(TRACK_SOURCE, { type: 'geojson', data: emptyFeatureCollection() });
      map.addLayer({
        id: GC_SOURCE,
        type: 'line',
        source: GC_SOURCE,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: { 'line-color': '#94a3b8', 'line-width': 2, 'line-dasharray': [2, 2] },
      });
      map.addLayer({
        id: TRACK_SOURCE,
        type: 'line',
        source: TRACK_SOURCE,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: { 'line-color': '#d97706', 'line-width': 3 },
      });
    });
    return () => {
      markersRef.current.forEach((m) => m.remove());
      markersRef.current.clear();
      venueRef.current.forEach((m) => m.remove());
      venueRef.current.clear();
      map.remove();
      mapRef.current = null;
    };
  }, []);

  // Sync the venue overlay (hotels, taxis, …). Only in the convergence view —
  // the single-flight focus is about the one flight's route.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const show = focusedPartId == null ? markers : [];
    const live = new Set<string>();
    for (const m of show) {
      const key = `${m.plan_part_id}:${m.lat},${m.lon}`;
      live.add(key);
      let marker = venueRef.current.get(key);
      if (!marker) {
        const el = buildPinEl(m.type);
        el.title = m.label;
        marker = new maplibregl.Marker({ element: el, anchor: 'bottom' })
          .setLngLat([m.lon, m.lat])
          .setPopup(
            new maplibregl.Popup({ offset: 30, closeButton: false }).setDOMContent(
              buildMarkerPopup({ title: m.label, type: m.type, iso: m.when, tz: m.tz }),
            ),
          )
          .addTo(map);
        venueRef.current.set(key, marker);
      } else {
        marker.setLngLat([m.lon, m.lat]);
      }
    }
    for (const [key, marker] of venueRef.current) {
      if (!live.has(key)) {
        marker.remove();
        venueRef.current.delete(key);
      }
    }
  }, [markers, focusedPartId]);

  // Sync one marker per plotted part. Markers carry a text label (the ident, or
  // the title as a fallback) so a cluster of friends heading to the same place
  // reads at a glance.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const live = new Set<number>();
    const plotted = focusedPartId != null ? parts.filter((p) => p.plan_part_id === focusedPartId) : parts;

    for (const p of plotted) {
      const pos = p.latest_position;
      if (!pos) continue;
      live.add(p.plan_part_id);
      const focused = focusedPartId === p.plan_part_id;
      let marker = markersRef.current.get(p.plan_part_id);
      const label = p.ident || p.title || `#${p.plan_part_id}`;
      const el = marker?.getElement() ?? buildMarkerEl();
      styleMarker(el, label, focused, pos.is_estimated);
      if (!marker) {
        marker = new maplibregl.Marker({ element: el }).setLngLat([pos.lon, pos.lat]).addTo(map);
        markersRef.current.set(p.plan_part_id, marker);
      } else {
        marker.setLngLat([pos.lon, pos.lat]);
      }
    }
    for (const [id, marker] of markersRef.current) {
      if (!live.has(id)) {
        marker.remove();
        markersRef.current.delete(id);
      }
    }
  }, [parts, focusedPartId]);

  // Draw the focused flight's flown track (from its positions) and planned
  // great-circle (origin → dest). Cleared when not focused / not a flight.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const apply = () => {
      const trackSrc = map.getSource(TRACK_SOURCE) as maplibregl.GeoJSONSource | undefined;
      const gcSrc = map.getSource(GC_SOURCE) as maplibregl.GeoJSONSource | undefined;
      if (!trackSrc || !gcSrc) return;

      const flight = focusedPart?.flight;
      if (focusedPartId == null || !flight) {
        trackSrc.setData(emptyFeatureCollection());
        gcSrc.setData(emptyFeatureCollection());
        return;
      }

      // Flown track: the recorded positions, oldest → newest.
      const trackPts: [number, number][] = (flight.track ?? []).map((p) => [p.lon, p.lat]);
      trackSrc.setData(
        trackPts.length > 1
          ? multiLineCollection([trackPts])
          : emptyFeatureCollection(),
      );

      // Planned great-circle from the part's start/end coordinates.
      const { start_lat, start_lon, end_lat, end_lon } = focusedPart;
      if (
        start_lat != null &&
        start_lon != null &&
        end_lat != null &&
        end_lon != null
      ) {
        const arc = greatCircle(start_lat, start_lon, end_lat, end_lon);
        gcSrc.setData(multiLineCollection(toMultiLine(arc)));
      } else {
        gcSrc.setData(emptyFeatureCollection());
      }
    };
    if (map.isStyleLoaded() && map.getSource(TRACK_SOURCE)) apply();
    else map.once('load', apply);
  }, [focusedPart, focusedPartId]);

  // Fit the map: to the single focused part, or to the whole in-window cluster.
  const fittedRef = useRef<string>('');
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const plotted = (
      focusedPartId != null ? parts.filter((p) => p.plan_part_id === focusedPartId) : parts
    ).filter((p) => p.latest_position);
    const pts: [number, number][] = plotted.map((p) => [
      p.latest_position!.lon,
      p.latest_position!.lat,
    ]);
    // Include the venue overlay so the convergence view frames them too.
    if (focusedPartId == null) {
      for (const m of markers) pts.push([m.lon, m.lat]);
    }

    // In the single-flight focus, widen the fit to the whole route: the part's
    // start/end coordinates plus every flown-track point, so the polyline +
    // great-circle are fully in view rather than zoomed to the marker alone.
    if (focusedPartId != null && focusedPart) {
      const { start_lat, start_lon, end_lat, end_lon } = focusedPart;
      if (start_lat != null && start_lon != null) pts.push([start_lon, start_lat]);
      if (end_lat != null && end_lon != null) pts.push([end_lon, end_lat]);
      for (const p of focusedPart.flight?.track ?? []) pts.push([p.lon, p.lat]);
    }

    const key =
      (focusedPartId ?? 'all') + ':' + pts.map((p) => `${p[0]},${p[1]}`).join(';');
    if (key === fittedRef.current) return;
    fittedRef.current = key;

    if (focusedPartId != null && pts.length === 1) {
      const fly = () => map.flyTo({ center: pts[0], zoom: 5, duration: 600 });
      if (map.isStyleLoaded()) fly();
      else map.once('load', fly);
      return;
    }
    const bounds = boundsFor(pts);
    if (!bounds) return;
    const fit = () => map.fitBounds(bounds, { padding: 80, maxZoom: 6, duration: 600 });
    if (map.isStyleLoaded()) fit();
    else map.once('load', fit);
  }, [parts, markers, focusedPartId, focusedPart]);

  return <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} data-testid="tracker-map" />;
}

function buildMarkerEl(): HTMLElement {
  const el = document.createElement('div');
  el.style.display = 'flex';
  el.style.alignItems = 'center';
  el.style.gap = '4px';
  el.style.cursor = 'default';
  el.innerHTML = `
    <svg class="tm-icon" viewBox="0 0 24 24" width="22" height="22" fill="currentColor"
         style="filter: drop-shadow(0 1px 2px rgba(0,0,0,0.4))">
      <path d="M12 2 L13.2 11 L22 15 L22 17 L13.2 14.5 L13 20 L16 22 L16 23 L12 22 L8 23 L8 22 L11 20 L10.8 14.5 L2 17 L2 15 L10.8 11 Z"/>
    </svg>
    <span class="tm-label" style="font:600 11px/1 system-ui,-apple-system,sans-serif;
      background:rgba(255,255,255,0.9);color:#111;padding:2px 5px;border-radius:4px;
      white-space:nowrap;box-shadow:0 1px 2px rgba(0,0,0,0.3)"></span>`;
  return el;
}

function styleMarker(el: HTMLElement, label: string, focused: boolean, estimated: boolean): void {
  el.style.color = focused ? '#d97706' : '#1f5fa8';
  el.style.opacity = estimated ? '0.7' : '1';
  el.title = label + (estimated ? ' (estimated)' : '');
  const span = el.querySelector('.tm-label');
  if (span) span.textContent = label;
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
