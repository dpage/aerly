import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render } from '@testing-library/react';

import type { Plan, PlanPart, Trip } from '../api/types';

const state = {
  currentTrip: null as (Trip & { plans: Plan[] }) | null,
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

// PlanMapView is exercised by its own test; here we only assert TripMap forwards
// the trip's parts to it.
const planMapSpy = vi.fn();
vi.mock('../components/PlanMapView', () => ({
  default: (props: { parts: PlanPart[] }) => {
    planMapSpy(props);
    return null;
  },
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
    start_address: '',
    end_address: '',
    status: 'planned',
    effective_at: '2026-10-12T09:00:00Z',
    ...over,
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

beforeEach(() => {
  vi.clearAllMocks();
  state.currentTrip = null;
});

describe('TripMap', () => {
  it('forwards the trip\'s flattened parts to PlanMapView', () => {
    state.currentTrip = tripWith([plan([part({ id: 1 }), part({ id: 2 })])]);
    render(<TripMap />);
    expect(planMapSpy).toHaveBeenCalled();
    const props = planMapSpy.mock.calls.at(-1)![0] as { parts: PlanPart[] };
    expect(props.parts.map((p) => p.id)).toEqual([1, 2]);
  });

  it('passes an empty list when there is no current trip', () => {
    render(<TripMap />);
    const props = planMapSpy.mock.calls.at(-1)![0] as { parts: PlanPart[] };
    expect(props.parts).toEqual([]);
  });
});
