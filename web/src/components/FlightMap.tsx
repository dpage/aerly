import { useEffect, useMemo, useRef } from 'react';
import maplibregl, {
  type LngLatBoundsLike,
  type Map as MlMap,
  type StyleSpecification,
} from 'maplibre-gl';
import { Box } from '@mui/material';

import { useStore } from '../state/store';
import { greatCircle, toMultiLine } from '../lib/great-circle';
import { fmtRelative } from '../lib/format';
import type { Flight, Position } from '../api/types';

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

export default function FlightMap() {
  const flights = useStore((s) => s.flights);
  const selectedFlightId = useStore((s) => s.selectedFlightId);
  const selectFlight = useStore((s) => s.selectFlight);

  const containerRef = useRef<HTMLElement | null>(null);
  const mapRef = useRef<MlMap | null>(null);
  const markersRef = useRef<Map<number, maplibregl.Marker>>(new Map());
  // Hover popups, one per marker. Created lazily on first mouseenter; removed
  // alongside the marker when a flight goes away.
  const popupsRef = useRef<Map<number, maplibregl.Popup>>(new Map());

  const flownFC = useMemo(() => buildFlown(flights, selectedFlightId), [flights, selectedFlightId]);
  const remainingFC = useMemo(
    () => buildRemaining(flights, selectedFlightId),
    [flights, selectedFlightId],
  );
  const completedFC = useMemo(
    () => buildCompleted(flights, selectedFlightId),
    [flights, selectedFlightId],
  );

  // Initialise the MapLibre instance once. Two line sources: the FLOWN track
  // (origin → recorded positions → current, drawn solid) and the REMAINING
  // route (current → destination, drawn dashed). For flights that haven't
  // started yet, only the remaining line is drawn — from origin → destination.
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
      map.addSource('completed', { type: 'geojson', data: emptyFC() });
      map.addSource('flown', { type: 'geojson', data: emptyFC() });
      map.addSource('remaining', { type: 'geojson', data: emptyFC() });
      // Completed routes underneath everything else: a muted grey great-
      // circle for flights that have arrived (or been cancelled). Selection
      // gives it the same orange the live layers use, so picking an arrived
      // flight still highlights its route on the map.
      map.addLayer({
        id: 'completed-line',
        type: 'line',
        source: 'completed',
        paint: {
          'line-color': ['case', ['get', 'selected'], '#d97706', '#9ca3af'],
          'line-width': ['case', ['get', 'selected'], 2.5, 2],
          'line-opacity': 0.7,
        },
      });
      map.addLayer({
        id: 'remaining-line',
        type: 'line',
        source: 'remaining',
        paint: {
          'line-color': ['case', ['get', 'selected'], '#d97706', '#1f5fa8'],
          'line-width': ['case', ['get', 'selected'], 2.5, 2],
          'line-dasharray': [2, 1.5],
          'line-opacity': 0.7,
        },
      });
      map.addLayer({
        id: 'flown-line',
        type: 'line',
        source: 'flown',
        paint: {
          'line-color': ['case', ['get', 'selected'], '#d97706', '#1f5fa8'],
          'line-width': ['case', ['get', 'selected'], 3.5, 2.5],
          'line-opacity': 0.95,
        },
      });
    });
    mapRef.current = map;
    return () => {
      markersRef.current.forEach((m) => m.remove());
      markersRef.current.clear();
      popupsRef.current.forEach((p) => p.remove());
      popupsRef.current.clear();
      map.remove();
      mapRef.current = null;
    };
  }, []);

  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const apply = () => {
      (map.getSource('flown') as maplibregl.GeoJSONSource | undefined)?.setData(flownFC);
      (map.getSource('remaining') as maplibregl.GeoJSONSource | undefined)?.setData(remainingFC);
      (map.getSource('completed') as maplibregl.GeoJSONSource | undefined)?.setData(completedFC);
    };
    if (map.isStyleLoaded()) apply();
    else map.once('load', apply);
  }, [flownFC, remainingFC, completedFC]);

  // Auto-fit the map when the set of renderable flights changes — keeps newly
  // added flights from being off-screen. Skipped if the user has a flight
  // selected (the next effect handles that case).
  const fittedIdsRef = useRef<string>('');
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    if (selectedFlightId != null) return;
    const renderable = flights.filter((f) => hasGeometry(f));
    const idsKey = renderable
      .map((f) => f.id)
      .sort((a, b) => a - b)
      .join(',');
    if (idsKey === fittedIdsRef.current) return;
    fittedIdsRef.current = idsKey;
    const bounds = allFlightsBounds(renderable);
    if (!bounds) return;
    const fit = () => map.fitBounds(bounds, { padding: 80, maxZoom: 6, duration: 600 });
    if (map.isStyleLoaded()) fit();
    else map.once('load', fit);
  }, [flights, selectedFlightId]);

  // Sync plane markers with the latest_position on each flight. Cancelled
  // flights drop their icon entirely (the grey great-circle conveys it);
  // Arrived flights keep a faded grey icon at the last known position so the
  // map still shows where the flight ended up.
  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const live = new Set<number>();
    for (const f of flights) {
      const pos = f.latest_position;
      if (!pos || f.status === 'Cancelled') continue;
      live.add(f.id);
      let marker = markersRef.current.get(f.id);
      const el = marker?.getElement() ?? buildMarkerEl();
      const heading = pos.heading_deg ?? 0;
      const arrived = f.status === 'Arrived';
      stylePlane(
        el,
        f.id === selectedFlightId,
        pos.is_estimated,
        arrived,
        f.ident + (pos.is_estimated ? ' (estimated)' : ''),
      );
      el.onclick = (e) => {
        e.stopPropagation();
        selectFlight(f.id === selectedFlightId ? null : f.id);
      };
      // (Re-)wire hover handlers each render so the popup HTML stays in sync
      // with the latest position when the user is currently hovering.
      const popupHtml = buildPopupHtml(f.ident, pos);
      el.onmouseenter = () => {
        let popup = popupsRef.current.get(f.id);
        if (!popup) {
          popup = new maplibregl.Popup({
            closeButton: false,
            closeOnClick: false,
            offset: 16,
            className: 'plane-tooltip',
          });
          popupsRef.current.set(f.id, popup);
        }
        popup.setLngLat([pos.lon, pos.lat]).setHTML(popupHtml).addTo(map);
      };
      el.onmouseleave = () => {
        popupsRef.current.get(f.id)?.remove();
      };
      // If the popup is currently open, refresh its content so live updates
      // are reflected without requiring the user to move the mouse.
      const openPopup = popupsRef.current.get(f.id);
      if (openPopup?.isOpen?.()) {
        openPopup.setLngLat([pos.lon, pos.lat]).setHTML(popupHtml);
      }
      if (!marker) {
        marker = new maplibregl.Marker({
          element: el,
          rotation: heading,
          rotationAlignment: 'map',
        })
          .setLngLat([pos.lon, pos.lat])
          .addTo(map);
        markersRef.current.set(f.id, marker);
      } else {
        marker.setLngLat([pos.lon, pos.lat]);
        marker.setRotation(heading);
      }
    }
    for (const [id, marker] of markersRef.current) {
      if (!live.has(id)) {
        marker.remove();
        markersRef.current.delete(id);
        popupsRef.current.get(id)?.remove();
        popupsRef.current.delete(id);
      }
    }
  }, [flights, selectedFlightId, selectFlight]);

  useEffect(() => {
    const map = mapRef.current;
    if (!map || selectedFlightId == null) return;
    const f = flights.find((x) => x.id === selectedFlightId);
    if (!f) return;
    const bounds = flightBounds(f);
    if (bounds) map.fitBounds(bounds, { padding: 80, maxZoom: 7, duration: 600 });
  }, [selectedFlightId, flights]);

  return <Box ref={containerRef} sx={{ position: 'absolute', inset: 0 }} />;
}

function buildMarkerEl(): HTMLElement {
  const el = document.createElement('div');
  el.style.width = '32px';
  el.style.height = '32px';
  el.style.display = 'grid';
  el.style.placeItems = 'center';
  el.style.cursor = 'pointer';
  el.innerHTML = `
    <svg viewBox="0 0 24 24" width="28" height="28" fill="currentColor"
         stroke="currentColor" stroke-width="0"
         style="filter: drop-shadow(0 1px 2px rgba(0,0,0,0.4))">
      <path d="M12 2 L13.2 11 L22 15 L22 17 L13.2 14.5 L13 20 L16 22 L16 23 L12 22 L8 23 L8 22 L11 20 L10.8 14.5 L2 17 L2 15 L10.8 11 Z"/>
    </svg>`;
  return el;
}

function stylePlane(
  el: HTMLElement,
  selected: boolean,
  estimated: boolean,
  arrived: boolean,
  title: string,
) {
  // Selection always wins for the outline colour; otherwise blue for active,
  // grey for arrived (matches the completed-route line). Arrived flights also
  // get a permanent low opacity so they fade into the background.
  if (selected) {
    el.style.color = '#d97706';
  } else if (arrived) {
    el.style.color = '#9ca3af';
  } else {
    el.style.color = '#1f5fa8';
  }
  let opacity = 1;
  if (arrived) opacity = 0.7;
  if (estimated) opacity = 0.6;
  el.style.opacity = String(opacity);
  const svg = el.querySelector('svg');
  const path = svg?.querySelector('path');
  if (svg && path) {
    if (estimated) {
      path.setAttribute('fill', 'rgba(255,255,255,0.85)');
      path.setAttribute('stroke-width', '1.2');
      path.setAttribute('stroke-dasharray', '2 1.5');
    } else {
      path.setAttribute('fill', 'currentColor');
      path.setAttribute('stroke-width', '0');
      path.removeAttribute('stroke-dasharray');
    }
  }
  el.title = title;
}

// buildPopupHtml renders the hover tooltip body for a plane marker. Telemetry
// fields render as "Label: value" rows; ones with no value are skipped
// entirely (no em-dash placeholders) so dead-reckoned positions — which lack
// altitude / speed / heading — produce a compact popup. The "estimated" /
// "Xs ago" lines appear as muted footnotes when applicable. Styles are
// inlined because the marker DOM lives outside the React/MUI tree.
function buildPopupHtml(ident: string, pos: Position): string {
  const rows: Array<[string, string]> = [['Flight', ident]];
  if (pos.altitude_ft != null) {
    rows.push(['Altitude', `${pos.altitude_ft.toLocaleString()} ft`]);
  }
  if (pos.groundspeed_kt != null) {
    rows.push(['Speed', `${pos.groundspeed_kt} kt`]);
  }
  if (pos.heading_deg != null) {
    rows.push(['Heading', `${pos.heading_deg}°`]);
  }
  const body = rows
    .map(
      ([k, v]) =>
        `<div style="display:flex;gap:8px"><span style="color:#6b7280;min-width:56px">${escapeHtml(k)}</span><span>${escapeHtml(v)}</span></div>`,
    )
    .join('');
  const footnotes: string[] = [];
  if (pos.is_estimated) {
    footnotes.push(
      `<div style="color:#6b7280;font-style:italic;font-size:11px">dead-reckoned position</div>`,
    );
  }
  const ageSec = Math.max(0, Math.floor((Date.now() - new Date(pos.ts).getTime()) / 1000));
  if (ageSec >= 60) {
    footnotes.push(
      `<div style="color:#d97706;font-size:11px">fix: ${fmtRelative(ageSec)} ago</div>`,
    );
  }
  const footer =
    footnotes.length > 0
      ? `<div style="margin-top:4px;padding-top:4px;border-top:1px solid #e5e7eb">${footnotes.join('')}</div>`
      : '';
  return `<div style="font:12px/1.4 system-ui,-apple-system,sans-serif;min-width:120px">${body}${footer}</div>`;
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function emptyFC(): GeoJSON.FeatureCollection {
  return { type: 'FeatureCollection', features: [] };
}

// buildFlown returns one feature per flight: a MultiLineString joining
// origin → first-tracked-point (via great-circle samples) → subsequent
// tracked points linearly → latest_position. Flights that haven't produced
// any position fix yet are skipped — their pre-departure route is rendered
// by buildRemaining instead.
function buildFlown(flights: Flight[], selectedId: number | null): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const f of flights) {
    const track = f.track ?? [];
    const latest = f.latest_position;
    if (track.length === 0 && !latest) continue;

    const haveOrigin = f.origin_lat != null && f.origin_lon != null;
    const segments: [number, number][][] = [];
    let current: [number, number][] = [];

    const pushPoint = (lon: number, lat: number) => {
      const last = current[current.length - 1];
      if (last && Math.abs(lon - last[0]) > 180) {
        if (current.length > 1) segments.push(current);
        current = [];
      }
      current.push([lon, lat]);
    };

    // Stitch origin → first sample with a great-circle so the line follows
    // the planned route until ADS-B kicks in.
    const firstSample = track[0] ?? latest;
    if (haveOrigin && firstSample) {
      const gc = greatCircle(f.origin_lat!, f.origin_lon!, firstSample.lat, firstSample.lon);
      const parts = toMultiLine(gc);
      for (const part of parts) {
        for (const [lon, lat] of part) pushPoint(lon, lat);
        if (current.length > 1) {
          segments.push(current);
          current = [];
        }
      }
    }

    // Then the recorded positions as straight segments — they're close
    // enough in time (~1 min apart) that linear interpolation is fine.
    for (const p of track) {
      pushPoint(p.lon, p.lat);
    }
    // Latest_position may not yet be in track[] (poller writes then queries
    // — they're consistent, but be defensive).
    if (latest && (track.length === 0 || track[track.length - 1].ts !== latest.ts)) {
      pushPoint(latest.lon, latest.lat);
    }
    if (current.length > 1) segments.push(current);

    if (segments.length === 0) continue;
    features.push({
      type: 'Feature',
      properties: { id: f.id, selected: f.id === selectedId },
      geometry:
        segments.length === 1
          ? { type: 'LineString', coordinates: segments[0] }
          : { type: 'MultiLineString', coordinates: segments },
    });
  }
  return { type: 'FeatureCollection', features };
}

// buildCompleted returns one feature per Arrived / Cancelled flight that
// has origin + destination coords: a single great-circle from origin to
// destination, rendered grey by the completed-line layer. Indicates "this
// flight is done" without showing a live plane marker.
function buildCompleted(flights: Flight[], selectedId: number | null): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const f of flights) {
    if (f.status !== 'Arrived' && f.status !== 'Cancelled') continue;
    if (f.origin_lat == null || f.origin_lon == null) continue;
    if (f.dest_lat == null || f.dest_lon == null) continue;
    const gc = greatCircle(f.origin_lat, f.origin_lon, f.dest_lat, f.dest_lon);
    const parts = toMultiLine(gc);
    if (parts.length === 0) continue;
    features.push({
      type: 'Feature',
      properties: { id: f.id, selected: f.id === selectedId },
      geometry:
        parts.length === 1
          ? { type: 'LineString', coordinates: parts[0] }
          : { type: 'MultiLineString', coordinates: parts },
    });
  }
  return { type: 'FeatureCollection', features };
}

// buildRemaining returns one feature per flight: a (multi-)line from the
// "current" anchor (latest_position when known, otherwise origin) to the
// destination, drawn as a dashed great-circle. Skipped once a flight is
// Arrived or Cancelled — there is nothing remaining.
function buildRemaining(flights: Flight[], selectedId: number | null): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const f of flights) {
    if (f.status === 'Arrived' || f.status === 'Cancelled') continue;
    if (f.dest_lat == null || f.dest_lon == null) continue;

    let anchorLat: number;
    let anchorLon: number;
    if (f.latest_position) {
      anchorLat = f.latest_position.lat;
      anchorLon = f.latest_position.lon;
    } else if (f.origin_lat != null && f.origin_lon != null) {
      anchorLat = f.origin_lat;
      anchorLon = f.origin_lon;
    } else {
      continue;
    }

    const gc = greatCircle(anchorLat, anchorLon, f.dest_lat, f.dest_lon);
    const parts = toMultiLine(gc);
    if (parts.length === 0) continue;

    features.push({
      type: 'Feature',
      properties: { id: f.id, selected: f.id === selectedId },
      geometry:
        parts.length === 1
          ? { type: 'LineString', coordinates: parts[0] }
          : { type: 'MultiLineString', coordinates: parts },
    });
  }
  return { type: 'FeatureCollection', features };
}

function flightBounds(f: Flight): LngLatBoundsLike | null {
  return boundsFor(flightPoints(f));
}

function allFlightsBounds(flights: Flight[]): LngLatBoundsLike | null {
  const pts: [number, number][] = [];
  for (const f of flights) pts.push(...flightPoints(f));
  return boundsFor(pts);
}

function flightPoints(f: Flight): [number, number][] {
  const pts: [number, number][] = [];
  if (f.origin_lon != null && f.origin_lat != null) pts.push([f.origin_lon, f.origin_lat]);
  if (f.dest_lon != null && f.dest_lat != null) pts.push([f.dest_lon, f.dest_lat]);
  if (f.latest_position) pts.push([f.latest_position.lon, f.latest_position.lat]);
  return pts;
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

function hasGeometry(f: Flight): boolean {
  return (
    (f.origin_lat != null && f.origin_lon != null && f.dest_lat != null && f.dest_lon != null) ||
    f.latest_position != null
  );
}

// hasGeometry already covers the arrived-with-coords case (origin + dest
// known), so auto-fit picks them up; allFlightsBounds covers their points.
// No change needed there.
