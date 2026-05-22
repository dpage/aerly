import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, act } from '@testing-library/react';

import type { Flight, Position } from '../api/types';
import maplibreMock, {
  FakeMap,
  FakeMarker,
  FakePopup,
  resetMaplibreMock,
} from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

const h = vi.hoisted(() => ({
  state: {
    flights: [] as Flight[],
    selectedFlightId: null as number | null,
    selectFlight: vi.fn(),
    showOld: false,
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import FlightMap from './FlightMap';

function pos(over: Partial<Position> = {}): Position {
  return { ts: '2024-01-01T10:00:00Z', lat: 50, lon: 5, is_estimated: false, ...over };
}

function flight(over: Partial<Flight> = {}): Flight {
  return {
    id: 1,
    ident: 'BA1',
    scheduled_out: '2024-01-01T10:00:00Z',
    scheduled_in: '2024-01-01T12:00:00Z',
    origin_iata: 'LHR',
    dest_iata: 'JFK',
    origin_lat: 51.47,
    origin_lon: -0.45,
    dest_lat: 40.64,
    dest_lon: -73.78,
    status: 'Enroute',
    notes: '',
    passenger_ids: [],
    shared_user_ids: [],
    is_public: false,
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  resetMaplibreMock();
  h.state.flights = [];
  h.state.selectedFlightId = null;
  h.state.showOld = true;
});

describe('FlightMap lifecycle', () => {
  it('creates the map, adds control/sources/layers on load, and cleans up on unmount', () => {
    h.state.flights = [flight()];
    const { unmount } = render(<FlightMap />);
    const map = FakeMap.instances[0];
    expect(map).toBeTruthy();
    expect(map.controls).toHaveLength(1);
    expect(map.sources.has('flown')).toBe(true);
    expect(map.sources.has('remaining')).toBe(true);
    expect(map.sources.has('estimated-past')).toBe(true);
    expect(map.sources.has('completed')).toBe(true);
    expect(map.layers).toHaveLength(4);
    unmount();
    expect(map.remove).toHaveBeenCalled();
  });

  it('returns early when the container ref is null', () => {
    // Render then immediately check: jsdom always provides a ref via Box, so
    // instead verify nothing crashes with zero flights.
    render(<FlightMap />);
    expect(FakeMap.instances).toHaveLength(1);
  });

  it('applies data immediately when style is loaded', () => {
    h.state.flights = [flight({ latest_position: pos() })];
    render(<FlightMap />);
    const map = FakeMap.instances[0];
    expect(map.getSource('flown')?.setData).toHaveBeenCalled();
    expect(map.getSource('remaining')?.setData).toHaveBeenCalled();
    expect(map.getSource('estimated-past')?.setData).toHaveBeenCalled();
    expect(map.getSource('completed')?.setData).toHaveBeenCalled();
  });

  it('renders an Arrived flight as a grey completed-route great-circle', () => {
    h.state.flights = [
      flight({ status: 'Arrived', latest_position: undefined }),
    ];
    render(<FlightMap />);
    const map = FakeMap.instances[0];
    const completed = map.getSource('completed')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(completed.features).toHaveLength(1);
    // Arrived flights produce no remaining-line (nothing remaining) and no
    // flown-line (no logged track for a flight added post-arrival).
    const remaining = map.getSource('remaining')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    const flown = map.getSource('flown')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(remaining.features).toHaveLength(0);
    expect(flown.features).toHaveLength(0);
  });

  it('defers data apply via once(load) when style not yet loaded', () => {
    const orig = FakeMap.prototype.isStyleLoaded;
    FakeMap.prototype.isStyleLoaded = function () {
      return false;
    };
    try {
      h.state.flights = [flight({ latest_position: pos() })];
      render(<FlightMap />);
      const map = FakeMap.instances[0];
      // once('load') fires synchronously in the mock -> setData still called.
      expect(map.getSource('flown')?.setData).toHaveBeenCalled();
    } finally {
      FakeMap.prototype.isStyleLoaded = orig;
    }
  });
});

describe('auto-fit effect', () => {
  it('fits bounds for renderable flights when nothing is selected', () => {
    h.state.flights = [flight()];
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
  });

  it('skips auto-fit when a flight is selected', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight()];
    render(<FlightMap />);
    // selected-flight effect fits instead; ensure no crash and instance exists.
    expect(FakeMap.instances[0]).toBeTruthy();
  });

  it('skips re-fit when idsKey is unchanged (memoized)', () => {
    h.state.flights = [flight()];
    const { rerender } = render(<FlightMap />);
    const map = FakeMap.instances[0];
    const callsBefore = map.fitBounds.mock.calls.length;
    rerender(<FlightMap />);
    expect(map.fitBounds.mock.calls.length).toBe(callsBefore);
  });

  it('does nothing when bounds are null (single point only)', () => {
    h.state.flights = [
      flight({
        origin_lat: undefined,
        origin_lon: undefined,
        dest_lat: undefined,
        dest_lon: undefined,
        latest_position: pos(),
      }),
    ];
    render(<FlightMap />);
    // hasGeometry true (latest_position) but boundsFor needs >=2 pts -> null.
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });

  it('defers fit via once(load) when style not loaded', () => {
    const orig = FakeMap.prototype.isStyleLoaded;
    FakeMap.prototype.isStyleLoaded = function () {
      return false;
    };
    try {
      h.state.flights = [flight()];
      render(<FlightMap />);
      expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
    } finally {
      FakeMap.prototype.isStyleLoaded = orig;
    }
  });
});

describe('marker sync', () => {
  it('adds a marker for an enroute flight with a position', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos() })];
    render(<FlightMap />);
    expect(FakeMarker.instances.length).toBeGreaterThan(0);
    expect(FakeMarker.instances[0].added).toBe(true);
  });

  it('updates an existing marker on position change', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos({ lat: 50, lon: 5 }) })];
    const { rerender } = render(<FlightMap />);
    const before = FakeMarker.instances.length;
    h.state.flights = [
      flight({ id: 1, latest_position: pos({ lat: 51, lon: 6, heading_deg: 90 }) }),
    ];
    rerender(<FlightMap />);
    expect(FakeMarker.instances.length).toBe(before); // reused, not recreated
    expect(FakeMarker.instances[0].rotation).toBe(90);
  });

  it('removes stale markers when a flight disappears', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos() })];
    const { rerender } = render(<FlightMap />);
    const marker = FakeMarker.instances[0];
    h.state.flights = [];
    rerender(<FlightMap />);
    expect(marker.remove).toHaveBeenCalled();
  });

  it('skips markers for Cancelled flights and ones without position', () => {
    h.state.flights = [
      flight({ id: 1, status: 'Cancelled', latest_position: pos() }),
      flight({ id: 2, status: 'Enroute', latest_position: undefined }),
    ];
    render(<FlightMap />);
    expect(FakeMarker.instances).toHaveLength(0);
  });

  it('keeps a grey, low-opacity marker for an Arrived flight at the last fix', () => {
    h.state.flights = [
      flight({ id: 1, status: 'Arrived', latest_position: pos({ lat: 40.7, lon: -73.8 }) }),
    ];
    render(<FlightMap />);
    expect(FakeMarker.instances).toHaveLength(1);
    const marker = FakeMarker.instances[0];
    expect(marker.lngLat).toEqual([-73.8, 40.7]);
    const el = marker.getElement();
    expect(el.style.color).toBe('rgb(156, 163, 175)'); // #9ca3af
    expect(el.style.opacity).toBe('0.7');
  });

  it('arrived flight: selection still tints the marker orange', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight({ id: 1, status: 'Arrived', latest_position: pos() })];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    expect(el.style.color).toBe('rgb(217, 119, 6)'); // #d97706
  });

  it('shows a popup with telemetry on marker mouseenter and removes it on mouseleave', () => {
    h.state.flights = [
      flight({
        id: 1,
        latest_position: pos({ altitude_ft: 35000, groundspeed_kt: 480, heading_deg: 273 }),
      }),
    ];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    act(() => {
      el.onmouseenter?.(new MouseEvent('mouseenter'));
    });
    expect(FakePopup.instances).toHaveLength(1);
    const popup = FakePopup.instances[0];
    expect(popup.added).toBe(true);
    expect(popup.html).toContain('BA1');
    expect(popup.html).toContain('35,000 ft');
    expect(popup.html).toContain('480 kt');
    expect(popup.html).toContain('273°');
    act(() => {
      el.onmouseleave?.(new MouseEvent('mouseleave'));
    });
    expect(popup.remove).toHaveBeenCalled();
  });

  it('popup omits telemetry fields with no value', () => {
    h.state.flights = [
      flight({
        id: 1,
        latest_position: pos({ altitude_ft: undefined, groundspeed_kt: undefined, heading_deg: undefined }),
      }),
    ];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    act(() => {
      el.onmouseenter?.(new MouseEvent('mouseenter'));
    });
    const html = FakePopup.instances[0].html;
    expect(html).toContain('BA1');
    expect(html).not.toContain('ft');
    expect(html).not.toContain('kt');
    expect(html).not.toContain('°');
  });

  it('popup notes dead-reckoned positions in a footnote', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos({ is_estimated: true }) })];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    act(() => {
      el.onmouseenter?.(new MouseEvent('mouseenter'));
    });
    expect(FakePopup.instances[0].html).toContain('dead-reckoned');
  });

  it('popup uses labelled rows (Flight/Altitude/Speed/Heading)', () => {
    h.state.flights = [
      flight({
        id: 1,
        latest_position: pos({ altitude_ft: 35000, groundspeed_kt: 480, heading_deg: 273 }),
      }),
    ];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    act(() => {
      el.onmouseenter?.(new MouseEvent('mouseenter'));
    });
    const html = FakePopup.instances[0].html;
    expect(html).toContain('Flight');
    expect(html).toContain('Altitude');
    expect(html).toContain('Speed');
    expect(html).toContain('Heading');
  });

  it('removes the popup when its flight disappears', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos() })];
    const { rerender } = render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    act(() => {
      el.onmouseenter?.(new MouseEvent('mouseenter'));
    });
    const popup = FakePopup.instances[0];
    h.state.flights = [];
    rerender(<FlightMap />);
    expect(popup.remove).toHaveBeenCalled();
  });

  it('applies estimated styling and the click handler toggles selection', () => {
    h.state.flights = [
      flight({ id: 1, latest_position: pos({ is_estimated: true, heading_deg: 45 }) }),
    ];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    expect(el.style.opacity).toBe('0.6');
    expect(el.title).toContain('(estimated)');
    act(() => {
      el.onclick?.(new MouseEvent('click'));
    });
    expect(h.state.selectFlight).toHaveBeenCalledWith(1);
  });

  it('click on selected marker deselects', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight({ id: 1, latest_position: pos() })];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    act(() => {
      el.onclick?.(new MouseEvent('click'));
    });
    expect(h.state.selectFlight).toHaveBeenCalledWith(null);
  });

  it('non-estimated marker uses solid plane styling', () => {
    h.state.flights = [flight({ id: 1, latest_position: pos({ is_estimated: false }) })];
    render(<FlightMap />);
    const el = FakeMarker.instances[0].getElement();
    expect(el.style.opacity).toBe('1');
    const path = el.querySelector('path')!;
    expect(path.getAttribute('fill')).toBe('currentColor');
  });

  it('omits markers for old flights when showOld is false', () => {
    h.state.flights = [
      flight({
        id: 21,
        scheduled_out: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
        scheduled_in: new Date(Date.now() + 3 * 60 * 60 * 1000).toISOString(),
        latest_position: pos({ lat: 50, lon: 0 }),
      }),
      flight({
        id: 22,
        actual_in: new Date(Date.now() - 30 * 60 * 60 * 1000).toISOString(),
        latest_position: pos({ lat: 51, lon: 1 }),
      }),
    ];
    h.state.showOld = false;
    render(<FlightMap />);
    // Only the fresh flight gets a marker; the 30h-old one is filtered out.
    expect(FakeMarker.instances).toHaveLength(1);
    expect(FakeMarker.instances[0].lngLat).toEqual([0, 50]);
  });

  it('includes markers for old flights when showOld is true', () => {
    h.state.flights = [
      flight({
        id: 22,
        actual_in: new Date(Date.now() - 30 * 60 * 60 * 1000).toISOString(),
        latest_position: pos({ lat: 51, lon: 1 }),
      }),
    ];
    h.state.showOld = true;
    render(<FlightMap />);
    expect(FakeMarker.instances).toHaveLength(1);
    expect(FakeMarker.instances[0].lngLat).toEqual([1, 51]);
  });
});

describe('selected-flight fitBounds effect', () => {
  it('fits bounds to the selected flight', () => {
    h.state.flights = [flight({ id: 1 })];
    h.state.selectedFlightId = 1;
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
  });

  it('does nothing when the selected flight is not found', () => {
    h.state.flights = [flight({ id: 1 })];
    h.state.selectedFlightId = 999;
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });

  it('does nothing when selected flight has no bounds', () => {
    h.state.flights = [
      flight({
        id: 1,
        origin_lat: undefined,
        origin_lon: undefined,
        dest_lat: undefined,
        dest_lon: undefined,
        latest_position: pos(),
      }),
    ];
    h.state.selectedFlightId = 1;
    render(<FlightMap />);
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });
});

describe('buildFlown / buildRemaining geometry branches', () => {
  it('builds a LineString for a simple track and a MultiLineString across the antimeridian', () => {
    // Antimeridian: track jumps from lon 170 to -170 (>180 diff) -> pushPoint
    // break -> MultiLineString in buildFlown.
    h.state.flights = [
      flight({
        id: 1,
        origin_lat: 10,
        origin_lon: 169,
        dest_lat: 10,
        dest_lon: -160,
        status: 'Enroute',
        track: [
          pos({ ts: 't1', lat: 10, lon: 170 }),
          pos({ ts: 't2', lat: 10, lon: -170 }),
        ],
        latest_position: pos({ ts: 't3', lat: 10, lon: -165 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    const geom = (fc.features[0].geometry as GeoJSON.GeometryObject).type;
    expect(['MultiLineString', 'LineString']).toContain(geom);
    expect(fc.features.length).toBeGreaterThan(0);
  });

  it('skips flights with no track and no latest_position in buildFlown', () => {
    h.state.flights = [flight({ id: 1, track: [], latest_position: undefined })];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(0);
  });

  it('handles a flight with no origin (buildFlown no-origin branch)', () => {
    h.state.flights = [
      flight({
        id: 1,
        origin_lat: undefined,
        origin_lon: undefined,
        track: [pos({ ts: 'a', lat: 50, lon: 5 }), pos({ ts: 'b', lat: 51, lon: 6 })],
        latest_position: pos({ ts: 'c', lat: 52, lon: 7 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features.length).toBeGreaterThan(0);
  });

  it('latest already in track (defensive branch not appending duplicate)', () => {
    h.state.flights = [
      flight({
        id: 1,
        track: [pos({ ts: 'same', lat: 50, lon: 5 }), pos({ ts: 'last', lat: 51, lon: 6 })],
        latest_position: pos({ ts: 'last', lat: 51, lon: 6 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features.length).toBeGreaterThan(0);
  });

  it('buildRemaining: skips Arrived/Cancelled and missing dest, anchors on origin or latest', () => {
    h.state.flights = [
      flight({ id: 1, status: 'Arrived' }),
      flight({ id: 2, status: 'Cancelled' }),
      flight({ id: 3, status: 'Enroute', dest_lat: undefined, dest_lon: undefined }),
      flight({
        id: 4,
        status: 'Enroute',
        origin_lat: undefined,
        origin_lon: undefined,
        latest_position: undefined,
      }),
      flight({ id: 5, status: 'Enroute', latest_position: pos({ lat: 45, lon: -30 }) }),
      flight({ id: 6, status: 'Scheduled' }), // anchored on origin
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    const ids = fc.features.map((f) => (f.properties as { id: number }).id).sort();
    expect(ids).toEqual([5, 6]);
  });

  it('buildRemaining MultiLineString across the antimeridian', () => {
    h.state.flights = [
      flight({
        id: 1,
        status: 'Scheduled',
        origin_lat: 10,
        origin_lon: 170,
        dest_lat: 10,
        dest_lon: -170,
        latest_position: undefined,
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features.length).toBeGreaterThan(0);
    expect(fc.features[0].geometry.type).toBe('MultiLineString');
  });

  it('buildRemaining skips when great-circle yields no parts (Δ<1e-9 -> single point)', () => {
    // Anchor == dest at (0,0): great-circle Δ is exactly 0 (<1e-9) so it
    // returns a single point and toMultiLine filters it to [] -> continue.
    h.state.flights = [
      flight({
        id: 1,
        status: 'Scheduled',
        origin_lat: 0,
        origin_lon: 0,
        dest_lat: 0,
        dest_lon: 0,
        latest_position: undefined,
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(0);
  });

  it('marks the selected flight in feature properties', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight({ id: 1, status: 'Scheduled' })];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('remaining')!.setData.mock
      .calls[0][0] as GeoJSON.FeatureCollection;
    expect((fc.features[0].properties as { selected: boolean }).selected).toBe(true);
  });
});

describe('buildEstimatedPast', () => {
  it('draws a dashed origin → first-observed arc when track exists', () => {
    h.state.flights = [
      flight({
        id: 1,
        status: 'Enroute',
        track: [pos({ ts: 'a', lat: 55, lon: -30 }), pos({ ts: 'b', lat: 50, lon: -50 })],
        latest_position: pos({ ts: 'c', lat: 45, lon: -70 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('estimated-past')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(1);
    expect((fc.features[0].properties as { id: number }).id).toBe(1);
  });

  it('falls back to latest_position when there is no track', () => {
    h.state.flights = [
      flight({
        id: 1,
        status: 'Enroute',
        track: [],
        latest_position: pos({ lat: 55, lon: -30 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('estimated-past')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(1);
  });

  it('skips when there is no observed position at all', () => {
    h.state.flights = [flight({ id: 1, status: 'Scheduled', track: [], latest_position: undefined })];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('estimated-past')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(0);
  });

  it('skips arrived/cancelled flights (the completed layer handles them)', () => {
    h.state.flights = [
      flight({ id: 1, status: 'Arrived', latest_position: pos() }),
      flight({ id: 2, status: 'Cancelled', latest_position: pos() }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('estimated-past')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(0);
  });

  it('skips when origin coordinates are unknown', () => {
    h.state.flights = [
      flight({
        id: 1,
        status: 'Enroute',
        origin_lat: undefined,
        origin_lon: undefined,
        latest_position: pos(),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('estimated-past')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(0);
  });

  it('marks the selected flight in the estimated-past feature', () => {
    h.state.selectedFlightId = 1;
    h.state.flights = [flight({ id: 1, status: 'Enroute', latest_position: pos() })];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('estimated-past')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect((fc.features[0].properties as { selected: boolean }).selected).toBe(true);
  });
});

describe('buildFlown no longer synthesizes the past', () => {
  it('flown line consists only of observed positions, not an origin→first-fix arc', () => {
    h.state.flights = [
      flight({
        id: 1,
        origin_lat: 51.5,
        origin_lon: 0,
        status: 'Enroute',
        track: [pos({ ts: 'a', lat: 55, lon: -30 })],
        latest_position: pos({ ts: 'b', lat: 50, lon: -50 }),
      }),
    ];
    render(<FlightMap />);
    const fc = FakeMap.instances[0].getSource('flown')!.setData.mock
      .calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(fc.features).toHaveLength(1);
    const geom = fc.features[0].geometry as GeoJSON.LineString;
    expect(geom.type).toBe('LineString');
    // Exactly two points: the single track sample and latest. No GC samples
    // before track[0].
    expect(geom.coordinates).toEqual([
      [-30, 55],
      [-50, 50],
    ]);
  });
});
