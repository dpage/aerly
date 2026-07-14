import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Plan, Trip } from '../api/types';

const h = vi.hoisted(() => ({
  movePlan: vi.fn(),
  listTrips: vi.fn(),
  setError: vi.fn(),
  setNotice: vi.fn(),
  state: { trips: [] as Trip[] },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      trips: h.state.trips,
      movePlan: h.movePlan,
      listTrips: h.listTrips,
      setError: h.setError,
      setNotice: h.setNotice,
    }),
}));

import MovePlanDialog from './MovePlanDialog';

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

function plan(over: Partial<Plan> = {}): Plan {
  return {
    id: 42,
    trip_id: 7,
    type: 'flight',
    title: 'BA123',
    confirmation_ref: 'XYZ',
    ticket_number: '',
    notes: '',
    source: '',
    cost_currency: '',
    passenger_ids: [],
    supplier_name: '',
    contact_email: '',
    contact_phone: '',
    website: '',
    visibility: { mode: 'everyone', user_ids: [] },
    alert_opted_in: false,
    parts: [],
    attachments: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function render_(p: Plan = plan()) {
  return render(<MovePlanDialog open plan={p} onClose={() => {}} />);
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.trips = [
    trip({ id: 7, name: 'Lisbon', my_role: 'owner' }), // current trip
    trip({ id: 8, name: 'Porto', my_role: 'editor' }), // editable elsewhere
    trip({ id: 9, name: 'Madrid', my_role: 'viewer' }), // not editable
  ];
});

describe('MovePlanDialog', () => {
  it('renders nothing and does not refresh trips when closed', () => {
    render(<MovePlanDialog open={false} plan={plan()} onClose={() => {}} />);
    expect(screen.queryByRole('heading', { name: /move plan/i })).not.toBeInTheDocument();
    expect(h.listTrips).not.toHaveBeenCalled();
  });

  it('refreshes the trip list on open', () => {
    render_();
    expect(h.listTrips).toHaveBeenCalled();
  });

  it('falls back to "this plan" in the description when the plan has no title', () => {
    render_(plan({ title: '' }));
    expect(screen.getByText(/this plan/i)).toBeInTheDocument();
  });

  it('lists only other trips the viewer can edit as targets', async () => {
    render_();
    await userEvent.click(screen.getByRole('combobox', { name: /move to another trip/i }));
    expect(await screen.findByRole('option', { name: 'Porto' })).toBeInTheDocument();
    // Current trip and viewer-only trips are not targets.
    expect(screen.queryByRole('option', { name: 'Lisbon' })).not.toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Madrid' })).not.toBeInTheDocument();
  });

  it('keeps Move disabled until a trip is chosen, then moves and notifies', async () => {
    h.movePlan.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<MovePlanDialog open plan={plan()} onClose={onClose} />);
    expect(screen.getByRole('button', { name: /^move$/i })).toBeDisabled();
    await userEvent.click(screen.getByRole('combobox', { name: /move to another trip/i }));
    await userEvent.click(await screen.findByRole('option', { name: 'Porto' }));
    const move = screen.getByRole('button', { name: /^move$/i });
    expect(move).toBeEnabled();
    await userEvent.click(move);
    await waitFor(() => expect(h.movePlan).toHaveBeenCalledWith(42, 8));
    expect(h.setNotice).toHaveBeenCalledWith({ message: 'Moved to Porto', severity: 'success' });
    expect(onClose).toHaveBeenCalled();
  });

  it('surfaces move errors via setError and stays open', async () => {
    h.movePlan.mockRejectedValue(new Error('move boom'));
    const onClose = vi.fn();
    render(<MovePlanDialog open plan={plan()} onClose={onClose} />);
    await userEvent.click(screen.getByRole('combobox', { name: /move to another trip/i }));
    await userEvent.click(await screen.findByRole('option', { name: 'Porto' }));
    await userEvent.click(screen.getByRole('button', { name: /^move$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('move boom'));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('shows a helpful message and no Move button when there is nowhere to move to', () => {
    h.state.trips = [trip({ id: 7, name: 'Lisbon', my_role: 'owner' })];
    render_();
    expect(screen.getByText(/don.t have another trip/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^move$/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /close/i })).toBeInTheDocument();
  });

  it('is read-only offline: an info notice, no Move, and no trip refresh', () => {
    const original = navigator.onLine;
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false });
    try {
      render_();
      expect(screen.getByText(/offline/i)).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /^move$/i })).not.toBeInTheDocument();
      expect(h.listTrips).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: original });
    }
  });
});
