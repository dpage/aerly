import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

import type { PlanPart, Trip } from '../api/types';

// Stub the MUI date picker: its date-fns adapter trips vitest's ESM resolution,
// and these tests only need a labelled control, not real date parsing.
vi.mock('@mui/x-date-pickers/DatePicker', () => ({
  DatePicker: ({ label }: { label: string }) => <input aria-label={label} readOnly />,
}));

const loadTracker = vi.fn();
const setTrackerWindow = vi.fn().mockResolvedValue(undefined);
const listTrips = vi.fn();

const state = {
  loadTracker,
  setTrackerWindow,
  listTrips,
  trackerParts: [] as PlanPart[],
  trackerTag: '',
  trackerWindow: {} as { from?: string; to?: string },
  trackerLoading: false,
  trips: [] as Trip[],
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

// PlanMapView is covered by its own test; capture the parts it receives.
const planMapSpy = vi.fn();
vi.mock('../components/PlanMapView', () => ({
  default: (props: { parts: PlanPart[]; initialSelectedPartId?: number | null }) => {
    planMapSpy(props);
    return <div data-testid="plan-map-view" />;
  },
}));

import Tracker from './Tracker';

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'Trip',
    destination: '',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function renderTracker(initial = '/tracker') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Tracker />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  state.trackerParts = [];
  state.trackerTag = '';
  state.trackerWindow = {};
  state.trackerLoading = false;
  state.trips = [];
});

describe('Tracker page', () => {
  it('loads on mount with a default From/To window and renders the controls', async () => {
    renderTracker();
    await waitFor(() => expect(loadTracker).toHaveBeenCalled());
    const w = loadTracker.mock.calls[0][0].window as { from: string; to: string };
    expect(w.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(w.to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(screen.getByLabelText('Tag')).toBeInTheDocument();
    expect(screen.getByLabelText('From')).toBeInTheDocument();
    expect(screen.getByLabelText('To')).toBeInTheDocument();
  });

  it('forwards the parts and ?part= selection to PlanMapView', () => {
    state.trackerParts = [{ id: 5 } as PlanPart];
    renderTracker('/tracker?part=5');
    const props = planMapSpy.mock.calls.at(-1)![0] as {
      parts: PlanPart[];
      initialSelectedPartId?: number | null;
    };
    expect(props.parts).toHaveLength(1);
    expect(props.initialSelectedPartId).toBe(5);
  });

  it('changing the tag seeds the window from the tag span and reloads', async () => {
    const user = userEvent.setup();
    const now = Date.now();
    const dayMs = 24 * 60 * 60 * 1000;
    state.trips = [
      trip({
        id: 1,
        tags: ['pgconf'],
        starts_on: new Date(now - 3 * dayMs).toISOString().slice(0, 10),
        ends_on: new Date(now + 4 * dayMs).toISOString().slice(0, 10),
      }),
    ];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByText('pgconf'));
    const call = loadTracker.mock.calls.find((c) => c[0]?.tag === 'pgconf');
    expect(call).toBeTruthy();
    const w = call![0].window as { from?: string; to?: string };
    expect(w.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(w.to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });
});
