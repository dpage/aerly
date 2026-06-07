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

  it('edits every endpoint field on a transfer part and patches them', async () => {
    h.updatePlanPart.mockResolvedValue(undefined);
    render_(plan({ parts: [part()] }));
    // A transfer (flight) shows From/To headings and both endpoints.
    expect(screen.getByText('From')).toBeInTheDocument();
    expect(screen.getByText('To')).toBeInTheDocument();

    const labels = screen.getAllByRole('textbox', { name: /^place$/i });
    const addresses = screen.getAllByRole('textbox', { name: /^address$/i });
    const dates = screen.getAllByLabelText(/^date$/i);
    const tzs = screen.getAllByRole('textbox', { name: /^timezone$/i });

    await userEvent.clear(labels[0]);
    await userEvent.type(labels[0], 'Heathrow');
    await userEvent.clear(addresses[0]);
    await userEvent.type(addresses[0], 'TW6');
    await userEvent.clear(dates[0]);
    await userEvent.type(dates[0], '2026-10-13');
    await userEvent.clear(tzs[0]);
    await userEvent.type(tzs[0], 'Europe/Paris');

    // End endpoint too.
    await userEvent.clear(labels[1]);
    await userEvent.type(labels[1], 'Dulles');
    await userEvent.clear(addresses[1]);
    await userEvent.type(addresses[1], 'VA');

    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.start_label).toBe('Heathrow');
    expect(patch.start_address).toBe('TW6');
    expect(patch.start_tz).toBe('Europe/Paris');
    expect(patch.end_label).toBe('Dulles');
    expect(patch.end_address).toBe('VA');
  });

  it('shows a single "Where" endpoint for a non-transfer single-point part', () => {
    render_(
      plan({
        type: 'dining',
        parts: [part({ type: 'dining', ends_at: undefined, end_label: '', end_tz: '' })],
      }),
    );
    expect(screen.getByText('Where')).toBeInTheDocument();
    expect(screen.queryByText('To')).not.toBeInTheDocument();
    expect(screen.queryByText('Until')).not.toBeInTheDocument();
  });

  it('shows "Until" for a non-transfer part that carries an end time (hotel)', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [part({ type: 'hotel', start_label: 'Hotel', end_label: '' })],
      }),
    );
    expect(screen.getByText('Where')).toBeInTheDocument();
    expect(screen.getByText('Until')).toBeInTheDocument();
  });

  it('gives a hotel a single place/address — check-out is the same location, time only', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [part({ type: 'hotel', start_label: 'Hotel', end_label: '' })],
      }),
    );
    // The "Until" (check-out) block carries date/time only, not a second
    // Place/Address — a hotel is one location.
    expect(screen.getAllByLabelText(/^place$/i)).toHaveLength(1);
    expect(screen.getAllByLabelText(/^address$/i)).toHaveLength(1);
  });

  it('gives a transfer two independent endpoints, each with its own address', () => {
    render_(
      plan({
        type: 'train',
        parts: [part({ type: 'train', start_label: 'Folkestone', end_label: 'Calais' })],
      }),
    );
    expect(screen.getByText('From')).toBeInTheDocument();
    expect(screen.getByText('To')).toBeInTheDocument();
    expect(screen.getAllByLabelText(/^place$/i)).toHaveLength(2);
    expect(screen.getAllByLabelText(/^address$/i)).toHaveLength(2);
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
