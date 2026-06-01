import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Plan, Trip } from '../api/types';

const h = vi.hoisted(() => ({
  updatePlan: vi.fn(),
  updatePlanPart: vi.fn(),
  movePlan: vi.fn(),
  listTrips: vi.fn(),
  setError: vi.fn(),
  state: { trips: [] as Trip[] },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      trips: h.state.trips,
      updatePlan: h.updatePlan,
      updatePlanPart: h.updatePlanPart,
      movePlan: h.movePlan,
      listTrips: h.listTrips,
      setError: h.setError,
    }),
}));

import type { PlanPart } from '../api/types';
import PlanEditDialog from './PlanEditDialog';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 100,
    plan_id: 42,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T11:35:00Z',
    ends_at: '2026-10-12T19:55:00Z',
    start_tz: 'Europe/London',
    end_tz: 'America/New_York',
    start_label: 'LHR',
    end_label: 'IAD',
    status: 'planned',
    effective_at: '2026-10-12T11:35:00Z',
    ...over,
  };
}

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
    notes: 'window seat',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    alert_opted_in: false,
    parts: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.trips = [
    trip({ id: 7, name: 'Lisbon', my_role: 'owner' }), // current trip
    trip({ id: 8, name: 'Porto', my_role: 'editor' }), // editable elsewhere
    trip({ id: 9, name: 'Madrid', my_role: 'viewer' }), // not editable
  ];
});

function render_(p: Plan = plan()) {
  return render(<PlanEditDialog open plan={p} onClose={() => {}} />);
}

describe('PlanEditDialog', () => {
  it('prefills the fields from the plan', () => {
    render_();
    expect(screen.getByRole('textbox', { name: /title/i })).toHaveValue('BA123');
    expect(screen.getByRole('textbox', { name: /confirmation/i })).toHaveValue('XYZ');
    expect(screen.getByRole('textbox', { name: /notes/i })).toHaveValue('window seat');
  });

  it('refreshes the trip list on open (for move targets)', () => {
    render_();
    expect(h.listTrips).toHaveBeenCalled();
  });

  it('saves edited title/confirmation/notes', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_();
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA999',
        confirmation_ref: 'XYZ',
        notes: 'window seat',
      }),
    );
  });

  it('lists only other trips the viewer can edit as move targets', async () => {
    render_();
    await userEvent.click(screen.getByRole('combobox', { name: /move to another trip/i }));
    expect(await screen.findByRole('option', { name: 'Porto' })).toBeInTheDocument();
    // Current trip and viewer-only trips are not move targets.
    expect(screen.queryByRole('option', { name: 'Lisbon' })).not.toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Madrid' })).not.toBeInTheDocument();
  });

  it('moves the plan to the chosen trip', async () => {
    h.movePlan.mockResolvedValue(undefined);
    render_();
    await userEvent.click(screen.getByRole('combobox', { name: /move to another trip/i }));
    await userEvent.click(await screen.findByRole('option', { name: 'Porto' }));
    await userEvent.click(screen.getByRole('button', { name: /^move$/i }));
    await waitFor(() => expect(h.movePlan).toHaveBeenCalledWith(42, 8));
  });

  it('hides the move control when there is nowhere to move to', () => {
    h.state.trips = [trip({ id: 7, name: 'Lisbon', my_role: 'owner' })];
    render_();
    expect(screen.queryByRole('combobox', { name: /move to another trip/i })).not.toBeInTheDocument();
  });

  it('edits a part time and saves it as a UTC instant in the part tz', async () => {
    h.updatePlanPart.mockResolvedValue(undefined);
    render_(plan({ parts: [part()] }));
    // The departure time prefills as London-local 12:35 (11:35Z in BST).
    const times = screen.getAllByLabelText(/^time$/i);
    await userEvent.clear(times[0]);
    await userEvent.type(times[0], '13:35');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [partId, patch] = h.updatePlanPart.mock.calls[0];
    expect(partId).toBe(100);
    // 13:35 BST → 12:35Z, carrying the tz.
    expect(patch.starts_at).toBe('2026-10-12T12:35:00.000Z');
    expect(patch.start_tz).toBe('Europe/London');
  });

  it('does not write parts that were not edited', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_(plan({ parts: [part()] }));
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlan).toHaveBeenCalled());
    expect(h.updatePlanPart).not.toHaveBeenCalled();
  });

  it('surfaces save errors via setError', async () => {
    h.updatePlan.mockRejectedValue(new Error('save boom'));
    render_();
    // Make a change so Save actually writes (an unchanged Save is a no-op).
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
  });
});
