import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

import type { Trip } from '../api/types';
import { setMatchMedia } from '../test/setup';

const h = vi.hoisted(() => ({
  loadTrip: vi.fn(),
  clearCurrentTrip: vi.fn(),
  plansOutsideTripDates: vi.fn(() => false),
  state: { currentTrip: null as Trip | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      currentTrip: h.state.currentTrip,
      loadTrip: h.loadTrip,
      clearCurrentTrip: h.clearCurrentTrip,
    }),
}));

vi.mock('../lib/trip-format', () => ({
  plansOutsideTripDates: (...args: unknown[]) => h.plansOutsideTripDates(...args),
  fmtTripDates: (t: { starts_on?: string; ends_on?: string }) =>
    `${t.starts_on ?? ''} – ${t.ends_on ?? ''}`,
}));

// Stub child dialogs: reflect their open prop so we can assert toggles, and
// expose close / delete affordances so the parent's onClose/onDeleted
// callbacks can be exercised.
function stubDialog(testid: string) {
  return ({
    open,
    onClose,
    onDeleted,
  }: {
    open: boolean;
    onClose?: () => void;
    onDeleted?: () => void;
  }) =>
    open ? (
      <div data-testid={testid}>
        <button type="button" onClick={() => onClose?.()}>
          close-{testid}
        </button>
        {onDeleted && (
          <button type="button" onClick={() => onDeleted()}>
            delete-{testid}
          </button>
        )}
      </div>
    ) : null;
}
vi.mock('../components/TripEditDialog', () => ({ default: stubDialog('edit-dialog') }));
vi.mock('../components/TripMembersDialog', () => ({ default: stubDialog('members-dialog') }));
vi.mock('../components/CalendarSubscribeDialog', () => ({ default: stubDialog('subscribe-dialog') }));
vi.mock('../components/AddToTripDialog', () => ({ default: stubDialog('add-plan-dialog') }));

import TripDetail from './TripDetail';

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 7,
    name: 'Lisbon',
    destination: 'Portugal',
    my_role: 'owner',
    members: [],
    tags: [],
    plans: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  } as Trip;
}

function renderDetail(path = '/trips/7') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/trips/:id" element={<TripDetail />}>
          <Route index element={<div data-testid="outlet">timeline body</div>} />
          <Route path="map" element={<div data-testid="outlet">map body</div>} />
        </Route>
        <Route path="/" element={<div data-testid="trips-list">trips list</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.currentTrip = null;
  h.plansOutsideTripDates.mockReturnValue(false);
});

describe('TripDetail', () => {
  it('loads the trip on mount and clears it on unmount', async () => {
    const { unmount } = renderDetail();
    await waitFor(() => expect(h.loadTrip).toHaveBeenCalledWith(7));
    unmount();
    expect(h.clearCurrentTrip).toHaveBeenCalled();
  });

  it('shows a placeholder title before the trip loads', () => {
    renderDetail();
    expect(screen.getByText('Trip #7')).toBeInTheDocument();
  });

  it('does not load when the id is not a finite number', () => {
    renderDetail('/trips/not-a-number');
    expect(h.loadTrip).not.toHaveBeenCalled();
    expect(screen.getByText('Trip #NaN')).toBeInTheDocument();
  });

  it('renders the loaded trip name and the Edit/Share/Subscribe actions for an owner', () => {
    h.state.currentTrip = trip();
    renderDetail();
    expect(screen.getByRole('heading', { name: 'Lisbon' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /share/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /subscribe/i })).toBeInTheDocument();
  });

  it('shows destination and from/to dates beside the trip name', () => {
    h.state.currentTrip = trip({
      destination: 'Portugal',
      starts_on: '2026-10-01',
      ends_on: '2026-10-05',
    });
    renderDetail();
    expect(screen.getByRole('heading', { name: 'Lisbon' })).toBeInTheDocument();
    // Destination + date span render as a single secondary line; the "·"
    // separator only appears when both parts are present.
    expect(screen.getByText(/Portugal ·/)).toBeInTheDocument();
  });

  it('shows only the destination beside the name when the trip has no dates', () => {
    h.state.currentTrip = trip({ destination: 'Portugal' });
    renderDetail();
    // No date span → no separator, just the destination.
    expect(screen.getByText('Portugal')).toBeInTheDocument();
  });

  it('hides Edit for viewers', () => {
    h.state.currentTrip = trip({ my_role: 'viewer' });
    renderDetail();
    expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument();
    // Share is still available to everyone with the trip loaded.
    expect(screen.getByRole('button', { name: /share/i })).toBeInTheDocument();
  });

  it('shows New plan for an owner and opens the add-plan dialog', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /new plan/i }));
    expect(screen.getByTestId('add-plan-dialog')).toBeInTheDocument();
  });

  it('hides New plan for viewers', () => {
    h.state.currentTrip = trip({ my_role: 'viewer' });
    renderDetail();
    expect(screen.queryByRole('button', { name: /new plan/i })).not.toBeInTheDocument();
  });

  it('closes the add-plan dialog via its onClose', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /new plan/i }));
    await userEvent.click(screen.getByRole('button', { name: 'close-add-plan-dialog' }));
    expect(screen.queryByTestId('add-plan-dialog')).not.toBeInTheDocument();
  });

  it('opens the edit dialog', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /edit/i }));
    expect(screen.getByTestId('edit-dialog')).toBeInTheDocument();
  });

  it('opens the members (share) dialog', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /share/i }));
    expect(screen.getByTestId('members-dialog')).toBeInTheDocument();
  });

  it('opens the subscribe dialog', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /subscribe/i }));
    expect(screen.getByTestId('subscribe-dialog')).toBeInTheDocument();
  });

  it('closes the edit dialog via its onClose', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /^edit$/i }));
    expect(screen.getByTestId('edit-dialog')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'close-edit-dialog' }));
    expect(screen.queryByTestId('edit-dialog')).not.toBeInTheDocument();
  });

  it('navigates home when the edit dialog reports a deletion', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /^edit$/i }));
    await userEvent.click(screen.getByRole('button', { name: 'delete-edit-dialog' }));
    expect(screen.getByTestId('trips-list')).toBeInTheDocument();
  });

  it('closes the members dialog via its onClose', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /share/i }));
    await userEvent.click(screen.getByRole('button', { name: 'close-members-dialog' }));
    expect(screen.queryByTestId('members-dialog')).not.toBeInTheDocument();
  });

  it('closes the subscribe dialog via its onClose', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /subscribe/i }));
    await userEvent.click(screen.getByRole('button', { name: 'close-subscribe-dialog' }));
    expect(screen.queryByTestId('subscribe-dialog')).not.toBeInTheDocument();
  });

  it('navigates back to the trips list', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /trips/i }));
    expect(screen.getByTestId('trips-list')).toBeInTheDocument();
  });

  it('renders the timeline outlet by default and switches to map', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    expect(screen.getByTestId('outlet')).toHaveTextContent('timeline body');
    await userEvent.click(screen.getByRole('tab', { name: 'Map' }));
    await waitFor(() => expect(screen.getByTestId('outlet')).toHaveTextContent('map body'));
  });

  it('switches back to timeline from the map tab', async () => {
    h.state.currentTrip = trip();
    renderDetail('/trips/7/map');
    expect(screen.getByTestId('outlet')).toHaveTextContent('map body');
    await userEvent.click(screen.getByRole('tab', { name: 'Timeline' }));
    await waitFor(() => expect(screen.getByTestId('outlet')).toHaveTextContent('timeline body'));
  });

  it('warns about out-of-range plans with an Edit hint for editors', () => {
    h.state.currentTrip = trip();
    h.plansOutsideTripDates.mockReturnValue(true);
    renderDetail();
    expect(screen.getByText(/check the dates with Edit/i)).toBeInTheDocument();
  });

  it('warns about out-of-range plans without the Edit hint for viewers', () => {
    h.state.currentTrip = trip({ my_role: 'viewer' });
    h.plansOutsideTripDates.mockReturnValue(true);
    renderDetail();
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent(/Some plans fall outside this trip's dates\.$/);
    expect(screen.queryByText(/check the dates with Edit/i)).not.toBeInTheDocument();
  });

  it('does not warn when no plans are out of range', () => {
    h.state.currentTrip = trip();
    h.plansOutsideTripDates.mockReturnValue(false);
    renderDetail();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('treats a different loaded trip id as not-yet-loaded', () => {
    h.state.currentTrip = trip({ id: 99 });
    renderDetail('/trips/7');
    // currentTrip.id (99) !== route id (7) → placeholder title, no Edit/Share.
    expect(screen.getByText('Trip #7')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /share/i })).not.toBeInTheDocument();
  });
});

describe('TripDetail (narrow / mobile)', () => {
  beforeEach(() => setMatchMedia(true));

  it('keeps the trip name and dates visible (not squeezed out by the actions)', () => {
    h.state.currentTrip = trip({
      destination: 'Portugal',
      starts_on: '2026-10-01',
      ends_on: '2026-10-05',
    });
    renderDetail();
    expect(screen.getByRole('heading', { name: 'Lisbon' })).toBeInTheDocument();
    expect(screen.getByText(/Portugal ·/)).toBeInTheDocument();
  });

  it('keeps New plan as a primary action and folds Edit/Share/Subscribe into a menu', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    // New plan stays a first-class button.
    expect(screen.getByRole('button', { name: /new plan/i })).toBeInTheDocument();
    // The secondary actions are not loose buttons any more.
    expect(screen.queryByRole('button', { name: /^edit$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^share$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^subscribe$/i })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /more actions/i }));
    expect(screen.getByRole('menuitem', { name: /edit/i })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /share/i })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /subscribe/i })).toBeInTheDocument();
  });

  it('opens the edit dialog from the overflow menu', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /edit/i }));
    expect(screen.getByTestId('edit-dialog')).toBeInTheDocument();
  });

  it('opens the share dialog from the overflow menu', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /share/i }));
    expect(screen.getByTestId('members-dialog')).toBeInTheDocument();
  });

  it('opens the subscribe dialog from the overflow menu', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }));
    await userEvent.click(screen.getByRole('menuitem', { name: /subscribe/i }));
    expect(screen.getByTestId('subscribe-dialog')).toBeInTheDocument();
  });

  it('omits Edit from the overflow menu for viewers but keeps Share/Subscribe', async () => {
    h.state.currentTrip = trip({ my_role: 'viewer' });
    renderDetail();
    expect(screen.queryByRole('button', { name: /new plan/i })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /more actions/i }));
    expect(screen.queryByRole('menuitem', { name: /edit/i })).not.toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /share/i })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /subscribe/i })).toBeInTheDocument();
  });

  it('navigates back to the trips list via the back button', async () => {
    h.state.currentTrip = trip();
    renderDetail();
    await userEvent.click(screen.getByRole('button', { name: /back to trips/i }));
    expect(screen.getByTestId('trips-list')).toBeInTheDocument();
  });
});
