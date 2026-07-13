import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

import type { Trip } from '../api/types';

const h = vi.hoisted(() => ({
  state: {
    currentTrip: null as Trip | null,
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) => sel({ currentTrip: h.state.currentTrip }),
}));

vi.mock('../components/ExplorePanel', () => ({
  default: ({ tripId, initialPlace }: { tripId: number; initialPlace?: string }) => (
    <div data-testid="explore-panel" data-trip-id={tripId}>
      {initialPlace}
    </div>
  ),
}));

import TripExplore from './TripExplore';

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 7,
    name: 'Lisbon',
    destination: 'Florence',
    my_role: 'owner',
    members: [],
    tags: [],
    plans: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  } as Trip;
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.currentTrip = null;
});

describe('TripExplore', () => {
  it('anchors the panel to the trip destination', () => {
    h.state.currentTrip = trip({ destination: 'Florence' });
    render(<TripExplore />);
    const panel = screen.getByTestId('explore-panel');
    expect(panel).toHaveTextContent('Florence');
    expect(panel).toHaveAttribute('data-trip-id', '7');
  });

  it('renders nothing when there is no current trip', () => {
    h.state.currentTrip = null;
    const { container } = render(<TripExplore />);
    expect(container).toBeEmptyDOMElement();
  });
});
