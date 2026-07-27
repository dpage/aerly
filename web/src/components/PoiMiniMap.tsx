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

// One colour per theme, so every sub-category within it (e.g. all of Food &
// drink's cafes/bars/pubs/etc.) shares a hue and pins stay visually coherent.
// Written as a direct object literal (rather than derived from THEMES via
// Object.fromEntries + an `as` assertion) so `tsc` itself enforces that every
// one of the 27 sub-categories has a colour: dropping a key here is a compile
// error against `Record<PoiCategory, string>`, not a silent `undefined`.
const CATEGORY_COLOUR: Record<PoiCategory, string> = {
  // Sights & landmarks — #1565c0
  attractions: '#1565c0',
  viewpoints: '#1565c0',
  monuments_heritage: '#1565c0',
  // Food & drink — #e65100
  restaurants: '#e65100',
  cafes: '#e65100',
  bars: '#e65100',
  pubs: '#e65100',
  street_food: '#e65100',
  // Live music & nightlife — #ad1457
  nightclubs: '#ad1457',
  live_venues: '#ad1457',
  cinemas: '#ad1457',
  // Culture — #6a1b9a
  museums: '#6a1b9a',
  galleries: '#6a1b9a',
  theatres: '#6a1b9a',
  // Outdoors & nature — #2e7d32
  parks_gardens: '#2e7d32',
  natural_features: '#2e7d32',
  beaches: '#2e7d32',
  // Shopping — #f9a825
  markets: '#f9a825',
  malls: '#f9a825',
  speciality_shops: '#f9a825',
  // Sport & leisure — #0277bd
  sports_centres: '#0277bd',
  swimming: '#0277bd',
  stadiums: '#0277bd',
  // Family — #c2185b
  zoos_aquariums: '#c2185b',
  theme_parks: '#c2185b',
  playgrounds: '#c2185b',
  // Worship — #00695c
  worship: '#00695c',
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
    map.addControl(
      new maplibregl.AttributionControl({
        compact: true,
        customAttribution: 'Powered by <a href="https://www.geoapify.com/">Geoapify</a>',
      })
    );
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
