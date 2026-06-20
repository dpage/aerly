import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Plan, Trip } from '../api/types';

const h = vi.hoisted(() => ({
  updatePlan: vi.fn(),
  updatePlanPart: vi.fn(),
  movePlan: vi.fn(),
  splitPlanPart: vi.fn(),
  listTrips: vi.fn(),
  setError: vi.fn(),
  setNotice: vi.fn(),
  state: { trips: [] as Trip[] },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      trips: h.state.trips,
      updatePlan: h.updatePlan,
      updatePlanPart: h.updatePlanPart,
      movePlan: h.movePlan,
      splitPlanPart: h.splitPlanPart,
      listTrips: h.listTrips,
      setError: h.setError,
      setNotice: h.setNotice,
      // PlanAttachments (mounted by the dialog) reads capabilities; off here so
      // it renders nothing and these tests stay focused on the editor.
      capabilities: { attachments_enabled: false },
    }),
}));

const ha = vi.hoisted(() => ({ resolveMapsUrl: vi.fn() }));
vi.mock('../api/client', () => ({
  api: { resolveMapsUrl: ha.resolveMapsUrl },
  ApiError: class extends Error {},
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
    ticket_number: '',
    notes: 'window seat',
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
  it('opens read-only with Save disabled while offline', () => {
    const original = navigator.onLine;
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false });
    try {
      render_();
      // Still opens so the full details (beyond the tile) are visible…
      expect(screen.getByRole('textbox', { name: /title/i })).toHaveValue('BA123');
      // …but it's read-only: a notice, disabled fields, no Save, Cancel→Close.
      expect(screen.getByText(/viewing only/i)).toBeInTheDocument();
      expect(screen.getByRole('textbox', { name: /title/i })).toBeDisabled();
      expect(screen.getByRole('button', { name: /save/i })).toBeDisabled();
      expect(screen.getByRole('button', { name: /close/i })).toBeInTheDocument();
      // The move-target picker is disabled offline, so we skip refreshing trips.
      expect(h.listTrips).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(navigator, 'onLine', { configurable: true, value: original });
    }
  });

  it('prefills the fields from the plan', () => {
    render_();
    expect(screen.getByRole('textbox', { name: /title/i })).toHaveValue('BA123');
    expect(screen.getByRole('textbox', { name: /confirmation/i })).toHaveValue('XYZ');
    expect(screen.getByRole('textbox', { name: /notes/i })).toHaveValue('window seat');
  });

  it('prefills ticket number and an existing cost/currency', () => {
    render_(plan({ ticket_number: 'E9', cost_amount: 100, cost_currency: 'EUR' }));
    expect(screen.getByRole('textbox', { name: /ticket number/i })).toHaveValue('E9');
    expect(screen.getByRole('spinbutton', { name: /cost/i })).toHaveValue(100);
    expect(screen.getByRole('textbox', { name: /currency/i })).toHaveValue('EUR');
  });

  it('does not call updatePlan when nothing changed', async () => {
    const onClose = vi.fn();
    render(
      <PlanEditDialog
        open
        plan={plan({ ticket_number: 'E9', cost_amount: 100, cost_currency: 'EUR' })}
        onClose={onClose}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(h.updatePlan).not.toHaveBeenCalled();
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
        ticket_number: '',
        notes: 'window seat',
        cost_currency: '',
        supplier_name: '',
        contact_email: '',
        contact_phone: '',
        website: '',
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
    expect(
      screen.queryByRole('combobox', { name: /move to another trip/i }),
    ).not.toBeInTheDocument();
  });
  it('numbers multiple parts and skips dismissed ones', () => {
    render_(
      plan({
        parts: [
          part({ id: 100 }),
          part({ id: 101 }),
          part({ id: 102, dismissed_at: '2026-01-01T00:00:00Z' }),
        ],
      }),
    );
    // Two editable parts → "Flight 1" / "Flight 2" dividers; dismissed one hidden.
    expect(screen.getByText(/Flight 1/)).toBeInTheDocument();
    expect(screen.getByText(/Flight 2/)).toBeInTheDocument();
    expect(screen.queryByText(/Flight 3/)).not.toBeInTheDocument();
  });

  it('offers "Split out" on each leg of a multi-part flight and splits it', async () => {
    h.splitPlanPart.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(
      <PlanEditDialog
        open
        plan={plan({ parts: [part({ id: 100 }), part({ id: 101 })] })}
        onClose={onClose}
      />,
    );
    const buttons = screen.getAllByRole('button', { name: /split out/i });
    expect(buttons).toHaveLength(2);
    await userEvent.click(buttons[1]);
    await waitFor(() => expect(h.splitPlanPart).toHaveBeenCalledWith(101));
    expect(onClose).toHaveBeenCalled();
  });

  it('does not offer "Split out" on a single-part plan', () => {
    render_(plan({ parts: [part({ id: 100 })] }));
    expect(screen.queryByRole('button', { name: /split out/i })).not.toBeInTheDocument();
  });

  it('offers "Split out" on each leg of a multi-part ground (transfer) plan', () => {
    render_(
      plan({
        type: 'ground',
        parts: [part({ id: 100, type: 'ground' }), part({ id: 101, type: 'ground' })],
      }),
    );
    expect(screen.getAllByRole('button', { name: /split out/i })).toHaveLength(2);
  });

  it('does not offer "Split out" on a multi-part non-linkable plan (hotel)', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({ id: 100, type: 'hotel', end_label: '' }),
          part({ id: 101, type: 'hotel', end_label: '' }),
        ],
      }),
    );
    expect(screen.queryByRole('button', { name: /split out/i })).not.toBeInTheDocument();
  });

  it('resets the move target to empty when cleared', async () => {
    render_();
    const combo = screen.getByRole('combobox', { name: /move to another trip/i });
    await userEvent.click(combo);
    await userEvent.click(await screen.findByRole('option', { name: 'Porto' }));
    // Move is now enabled.
    expect(screen.getByRole('button', { name: /^move$/i })).toBeEnabled();
  });

  it('surfaces move errors via setError', async () => {
    h.movePlan.mockRejectedValue(new Error('move boom'));
    render_();
    await userEvent.click(screen.getByRole('combobox', { name: /move to another trip/i }));
    await userEvent.click(await screen.findByRole('option', { name: 'Porto' }));
    await userEvent.click(screen.getByRole('button', { name: /^move$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('move boom'));
  });

  it('edits confirmation ref and notes and saves them', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_();
    const conf = screen.getByRole('textbox', { name: /confirmation/i });
    const notes = screen.getByRole('textbox', { name: /notes/i });
    await userEvent.clear(conf);
    await userEvent.type(conf, 'ABC');
    await userEvent.clear(notes);
    await userEvent.type(notes, 'aisle seat');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA123',
        confirmation_ref: 'ABC',
        ticket_number: '',
        notes: 'aisle seat',
        cost_currency: '',
        supplier_name: '',
        contact_email: '',
        contact_phone: '',
        website: '',
      }),
    );
  });

  it('prefills and saves edited supplier contact details', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_(
      plan({
        supplier_name: 'British Airways',
        contact_email: 'old@ba.example',
        contact_phone: '+44 1',
        website: 'ba.example',
      }),
    );
    // Prefilled from the plan.
    expect(screen.getByRole('textbox', { name: /^supplier/i })).toHaveValue('British Airways');
    expect(screen.getByRole('textbox', { name: /contact email/i })).toHaveValue('old@ba.example');
    expect(screen.getByRole('textbox', { name: /contact phone/i })).toHaveValue('+44 1');
    expect(screen.getByRole('textbox', { name: /^website/i })).toHaveValue('ba.example');

    const email = screen.getByRole('textbox', { name: /contact email/i });
    await userEvent.clear(email);
    await userEvent.type(email, 'new@ba.example');
    const site = screen.getByRole('textbox', { name: /^website/i });
    await userEvent.clear(site);
    await userEvent.type(site, 'https://ba.example/manage');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA123',
        confirmation_ref: 'XYZ',
        ticket_number: '',
        notes: 'window seat',
        cost_currency: '',
        supplier_name: 'British Airways',
        contact_email: 'new@ba.example',
        contact_phone: '+44 1',
        website: 'https://ba.example/manage',
      }),
    );
  });

  it('saves an edited ticket number and cost with currency', async () => {
    h.updatePlan.mockResolvedValue(undefined);
    render_(plan({ cost_currency: 'GBP' }));
    await userEvent.type(screen.getByRole('textbox', { name: /ticket number/i }), 'E1234567890');
    await userEvent.type(screen.getByRole('spinbutton', { name: /cost/i }), '250.5');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updatePlan).toHaveBeenCalledWith(42, {
        title: 'BA123',
        confirmation_ref: 'XYZ',
        ticket_number: 'E1234567890',
        notes: 'window seat',
        cost_currency: 'GBP',
        supplier_name: '',
        contact_email: '',
        contact_phone: '',
        website: '',
        cost_amount: 250.5,
      }),
    );
  });

  it('closes via Cancel without writing', async () => {
    const onClose = vi.fn();
    render(<PlanEditDialog open plan={plan()} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: /^cancel$/i }));
    expect(onClose).toHaveBeenCalled();
    expect(h.updatePlan).not.toHaveBeenCalled();
  });

  it('does nothing on open=false', () => {
    render(<PlanEditDialog open={false} plan={plan()} onClose={() => {}} />);
    expect(h.listTrips).not.toHaveBeenCalled();
  });

  it('surfaces non-Error save failures by stringifying them', async () => {
    h.updatePlan.mockRejectedValue('plain string boom');
    render_();
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('plain string boom'));
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
