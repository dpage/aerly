import { useEffect, useRef } from 'react';
import maplibregl, {
  type Map as MlMap,
  type Marker as MlMarker,
  type LngLatBoundsLike,
} from 'maplibre-gl';
import { Box } from '@mui/material';

import type { Poi, PoiCategory } from '../api/types';
import { osmRasterStyle } from '../lib/map-style';

interface PoiMiniMapProps {
  pois: Poi[];
  /** The search centre (hotel coordinates or geocoded destination), shown as a
   * distinct anchor pin. */
  center?: { lat: number; lon: number };
  selectedId?: string;
  onSelectPoi: (id: string) => void;
}

const CATEGORY_COLOUR: Record<PoiCategory, string> = {
  sights: '#1565c0',
  museum: '#6a1b9a',
  landmark: '#ad1457',
  park: '#2e7d32',
  food: '#e65100',
};

function pinElement(poi: Poi, selected: boolean, onClick: () => void): HTMLButtonElement {
  const el = document.createElement('button');
  el.type = 'button';
  el.title = poi.name;
  el.setAttribute('aria-label', poi.name);
  const size = selected ? 20 : 13;
  Object.assign(el.style, {
    width: `${size}px`,
    height: `${size}px`,
    padding: '0',
    borderRadius: '50%',
    cursor: 'pointer',
    background: CATEGORY_COLOUR[poi.category],
    border: '2px solid #fff',
    boxShadow: selected ? '0 0 0 3px rgba(0,0,0,0.35)' : '0 1px 3px rgba(0,0,0,0.4)',
  });
  el.addEventListener('click', (e) => {
    e.stopPropagation();
    onClick();
  });
  return el;
}

function anchorElement(): HTMLDivElement {
  const el = document.createElement('div');
  el.title = 'Search centre';
  Object.assign(el.style, {
    width: '16px',
    height: '16px',
    borderRadius: '50%',
    background: 'transparent',
    border: '3px solid #d32f2f',
    boxShadow: '0 0 0 2px #fff',
  });
  return el;
}

/** A compact map of the current POI results, pinned around the search centre.
 * Clicking a pin selects its list row (where the "Add to trip" action lives);
 * the map is purely for spatial context. Tiles are OpenStreetMap, matching the
 * trip map. */
export default function PoiMiniMap({ pois, center, selectedId, onSelectPoi }: PoiMiniMapProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<MlMap | null>(null);
  const markersRef = useRef<MlMarker[]>([]);

  // Create the map once; the marker effect below (re)places pins and fits the
  // view whenever the results or centre change.
  useEffect(() => {
    // Runs once (empty deps) after mount, so the container ref is set.
    const map = new maplibregl.Map({
      container: containerRef.current as HTMLElement,
      style: osmRasterStyle,
      center: center ? [center.lon, center.lat] : [0, 0],
      zoom: 12,
      attributionControl: false,
    });
    map.addControl(new maplibregl.AttributionControl({ compact: true }));
    mapRef.current = map;
    return () => {
      map.remove();
      mapRef.current = null;
    };
    // Created once; center is only the initial placeholder — the marker effect
    // recentres via fitBounds/flyTo once results arrive.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;

    for (const m of markersRef.current) m.remove();
    markersRef.current = [];

    const pts: [number, number][] = [];
    if (center) {
      const m = new maplibregl.Marker({ element: anchorElement() })
        .setLngLat([center.lon, center.lat])
        .addTo(map);
      markersRef.current.push(m);
      pts.push([center.lon, center.lat]);
    }
    for (const poi of pois) {
      const el = pinElement(poi, poi.id === selectedId, () => onSelectPoi(poi.id));
      const m = new maplibregl.Marker({ element: el }).setLngLat([poi.lon, poi.lat]).addTo(map);
      markersRef.current.push(m);
      pts.push([poi.lon, poi.lat]);
    }

    const selected = pois.find((p) => p.id === selectedId);
    if (selected) {
      map.flyTo({ center: [selected.lon, selected.lat], zoom: 15 });
    } else if (pts.length === 1) {
      map.flyTo({ center: pts[0], zoom: 14 });
    } else if (pts.length > 1) {
      let minLng = pts[0][0];
      let minLat = pts[0][1];
      let maxLng = pts[0][0];
      let maxLat = pts[0][1];
      for (const [lng, lat] of pts) {
        minLng = Math.min(minLng, lng);
        minLat = Math.min(minLat, lat);
        maxLng = Math.max(maxLng, lng);
        maxLat = Math.max(maxLat, lat);
      }
      map.fitBounds(
        [
          [minLng, minLat],
          [maxLng, maxLat],
        ] as LngLatBoundsLike,
        { padding: 40, maxZoom: 15 },
      );
    }
  }, [pois, center, selectedId, onSelectPoi]);

  return (
    <Box
      ref={containerRef}
      data-testid="poi-mini-map"
      sx={{ height: 280, width: '100%', borderRadius: 1, overflow: 'hidden' }}
    />
  );
}
