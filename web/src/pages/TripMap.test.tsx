import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

import type { Plan, PlanPart, Trip } from '../api/types';
import maplibreMock, { FakeMap, FakeMarker, resetMaplibreMock } from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

const state = {
  currentTrip: null as (Trip & { plans: Plan[] }) | null,
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

import TripMap from './TripMap';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T09:00:00Z',
    start_tz: 'UTC',
    end_tz: 'UTC',
    start_label: 'LHR',
    end_label: 'LIS',
    status: 'planned',
    effective_at: '2026-10-12T09:00:00Z',
    ...over,
  };
}

function plan(parts: PlanPart[]): Plan {
  return {
    id: 1,
    trip_id: 1,
    type: 'flight',
    title: 'Outbound',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

function tripWith(plans: Plan[]): Trip & { plans: Plan[] } {
  return {
    id: 1,
    name: 'Lisbon',
    destination: 'Lisbon',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    plans,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  resetMaplibreMock();
  state.currentTrip = null;
});

describe('TripMap', () => {
  it('shows an empty state when the trip has no mappable plans', () => {
    state.currentTrip = tripWith([plan([part({ start_lat: undefined, start_lon: undefined, end_lat: undefined, end_lon: undefined })])]);
    render(<TripMap />);
    expect(screen.getByText(/No mappable plans/i)).toBeInTheDocument();
  });

  it('creates the map and a markers + legs source, plotting geocoded parts', () => {
    state.currentTrip = tripWith([
      plan([
        part({
          id: 1,
          start_lat: 51.47,
          start_lon: -0.45,
          end_lat: 38.77,
          end_lon: -9.13,
        }),
      ]),
    ]);
    render(<TripMap />);
    const map = FakeMap.instances[0];
    expect(map).toBeTruthy();
    expect(map.sources.has('legs')).toBe(true);
    // One marker per endpoint (start + end) of the single part.
    expect(FakeMarker.instances).toHaveLength(2);
    // A great-circle leg feature for the part with both ends geocoded.
    const legs = map.getSource('legs')!.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(legs.features.length).toBeGreaterThan(0);
    // Two endpoints → bounds → fitBounds called.
    expect(map.fitBounds).toHaveBeenCalled();
  });

  it('plots a single geocoded endpoint without a leg line', () => {
    state.currentTrip = tripWith([
      plan([
        part({
          id: 1,
          type: 'dining',
          start_lat: 38.71,
          start_lon: -9.14,
          end_lat: undefined,
          end_lon: undefined,
        }),
      ]),
    ]);
    render(<TripMap />);
    const map = FakeMap.instances[0];
    expect(FakeMarker.instances).toHaveLength(1);
    const legs = map.getSource('legs')!.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(legs.features).toHaveLength(0);
  });

  it('cleans up the map and markers on unmount', () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, start_lat: 51, start_lon: 0, end_lat: 40, end_lon: -73 })]),
    ]);
    const { unmount } = render(<TripMap />);
    const map = FakeMap.instances[0];
    const markers = [...FakeMarker.instances];
    unmount();
    expect(map.remove).toHaveBeenCalled();
    markers.forEach((m) => expect(m.remove).toHaveBeenCalled());
  });
});
