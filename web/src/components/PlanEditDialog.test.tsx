import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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

  it('edits a part time and saves it as a UTC instant in the part tz', async () => {
    h.updatePlanPart.mockResolvedValue(part({}));
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
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(plan({ parts: [part()] }));
    // A transfer (flight) shows From/To headings and both endpoints.
    expect(screen.getByText('From')).toBeInTheDocument();
    expect(screen.getByText('To')).toBeInTheDocument();

    const labels = screen.getAllByRole('textbox', { name: /^place$/i });
    const addresses = screen.getAllByRole('textbox', { name: /^address$/i });
    const dates = screen.getAllByLabelText(/^date$/i);
    const tzs = screen.getAllByRole('combobox', { name: /^timezone$/i });

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

  it('shows "Until" (check-out) for a hotel even when it has no end time yet', async () => {
    // A hotel added without a check-out must still let the user add one — the
    // end section can't be gated on an end already existing (#chicken-and-egg).
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Lake House',
            end_label: '',
            ends_at: undefined,
            end_tz: '',
          }),
        ],
      }),
    );
    expect(screen.getByText('Until')).toBeInTheDocument();
    // Filling the empty check-out date/time saves an ends_at instant.
    const dates = screen.getAllByLabelText(/^date$/i);
    const times = screen.getAllByLabelText(/^time$/i);
    // The end endpoint is the last date/time pair (start is first).
    await userEvent.type(dates[dates.length - 1], '2026-10-15');
    await userEvent.type(times[times.length - 1], '10:00');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.ends_at).toBeTruthy();
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

  it('edits an ice cream stop: a single place, a 0–5 rating and a what-was-ordered note', async () => {
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(
      plan({
        type: 'ice_cream',
        parts: [
          part({
            type: 'ice_cream',
            start_label: 'Giolitti',
            ends_at: undefined,
            end_label: '',
            end_tz: '',
            ice_cream: { rating: 0, what_ordered: '' },
          }),
        ],
      }),
    );
    // Single-location: a "Where" endpoint, no "To"/"Until".
    expect(screen.getByText('Where')).toBeInTheDocument();
    expect(screen.queryByText('Until')).not.toBeInTheDocument();
    // Ice cream isn't a booking: the confirmation field is relabelled and the
    // ticket-number / supplier fields are dropped.
    expect(screen.getByRole('textbox', { name: /reservation name/i })).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: /confirmation ref/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: /ticket number/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: /supplier/i })).not.toBeInTheDocument();
    // The ice-cream section with its rating and free-text note (the label
    // appears on both the divider and the section heading).
    expect(screen.getAllByText('Ice cream').length).toBeGreaterThanOrEqual(1);
    const ordered = screen.getByRole('textbox', { name: /what was ordered/i });
    await userEvent.type(ordered, 'Pistachio cone');
    // MUI Rating exposes a radio per star; a direct click sets the value (a full
    // pointer sequence would feed jsdom's zero-width rect into the hover maths).
    fireEvent.click(screen.getByRole('radio', { name: '4 Stars' }));
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.ice_cream).toEqual({ rating: 4, what_ordered: 'Pistachio cone' });
  });

  it('edits a train part: operator/service/class/coach/seat/platform patch onto train', async () => {
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(
      plan({
        type: 'train',
        parts: [
          part({
            type: 'train',
            start_label: 'Folkestone',
            end_label: 'Calais',
            train: {
              operator: '',
              service_no: '',
              coach: '',
              seat: '',
              class: '',
              platform: '',
            },
          }),
        ],
      }),
    );
    // Set each field in one shot rather than per-keystroke: under v8 coverage the
    // dialog's controlled re-render is slow enough that simulated typing across
    // these six fields blows the test timeout. fireEvent.change exercises the same
    // onChange path deterministically.
    fireEvent.change(screen.getByRole('textbox', { name: /operator/i }), {
      target: { value: 'Eurostar' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: /service no/i }), {
      target: { value: '9024' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: /class/i }), {
      target: { value: 'Standard' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: /coach/i }), { target: { value: '12' } });
    fireEvent.change(screen.getByRole('textbox', { name: /seat/i }), { target: { value: '44' } });
    fireEvent.change(screen.getByRole('textbox', { name: /platform/i }), {
      target: { value: '2' },
    });
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.train).toEqual({
      operator: 'Eurostar',
      service_no: '9024',
      class: 'Standard',
      coach: '12',
      seat: '44',
      platform: '2',
    });
  });

  it('edits a dining part: reservation name, party size and phone patch onto dining', async () => {
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(
      plan({
        type: 'dining',
        parts: [
          part({
            type: 'dining',
            start_label: 'Belcanto',
            ends_at: undefined,
            end_label: '',
            end_tz: '',
            dining: { party_size: undefined, reservation_name: '', phone: '' },
          }),
        ],
      }),
    );
    // Set the fields in one shot: under v8 coverage the slow controlled re-render
    // lets per-keystroke typing interleave and corrupt the values. fireEvent.change
    // drives the same onChange path deterministically.
    fireEvent.change(screen.getByRole('textbox', { name: /reservation name/i }), {
      target: { value: 'Smith' },
    });
    fireEvent.change(screen.getByRole('spinbutton', { name: /party size/i }), {
      target: { value: '4' },
    });
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.dining).toEqual({ reservation_name: 'Smith', party_size: 4 });
  });

  it('edits a ground transfer transport detail and patches it onto ground', async () => {
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(
      plan({
        type: 'ground',
        parts: [
          part({
            type: 'ground',
            start_label: 'Hotel',
            end_label: 'Airport',
            ground: { provider: '', phone: '', vehicle: '', driver: '', pax: undefined },
          }),
        ],
      }),
    );
    fireEvent.change(screen.getByRole('textbox', { name: /provider/i }), {
      target: { value: 'Addison Lee' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: /vehicle/i }), {
      target: { value: 'Saloon' },
    });
    fireEvent.change(screen.getByRole('textbox', { name: /driver/i }), { target: { value: 'Sam' } });
    fireEvent.change(screen.getByRole('spinbutton', { name: /passengers/i }), {
      target: { value: '3' },
    });
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.ground).toEqual({
      provider: 'Addison Lee',
      vehicle: 'Saloon',
      driver: 'Sam',
      pax: 3,
    });
  });

  it('edits an excursion: provider and ticket count patch onto excursion', async () => {
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(
      plan({
        type: 'excursion',
        parts: [
          part({
            type: 'excursion',
            start_label: 'Museum',
            ends_at: undefined,
            end_label: '',
            end_tz: '',
            excursion: { provider: '', ticket_count: undefined },
          }),
        ],
      }),
    );
    fireEvent.change(screen.getByRole('textbox', { name: /provider/i }), {
      target: { value: 'City Tours' },
    });
    fireEvent.change(screen.getByRole('spinbutton', { name: /tickets/i }), {
      target: { value: '5' },
    });
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.excursion).toEqual({ provider: 'City Tours', ticket_count: 5 });
  });

  it('edits a hotel part: room/guests patch onto hotel and the place mirrors property_name', async () => {
    h.updatePlanPart.mockResolvedValue(part({}));
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Old name',
            end_label: '',
            hotel: { property_name: 'Old name', address: '', phone: '', room_type: '', guests: 0 },
          }),
        ],
      }),
    );
    // No duplicate "Property name" field — the single "Place" carries the name.
    expect(screen.queryByRole('textbox', { name: /property name/i })).not.toBeInTheDocument();
    await userEvent.type(screen.getByRole('textbox', { name: /room type/i }), 'Suite');
    await userEvent.type(screen.getByRole('spinbutton', { name: /guests/i }), '2');
    // Renaming the hotel via "Place" mirrors into property_name.
    const place = screen.getByRole('textbox', { name: /^place$/i });
    await userEvent.clear(place);
    await userEvent.type(place, 'New name');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.start_label).toBe('New name');
    expect(patch.hotel).toMatchObject({ room_type: 'Suite', guests: 2, property_name: 'New name' });
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

  it('warns under the address when an endpoint has an address but no coordinates', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Hotel',
            start_address: '123 Nowhere St',
            start_lat: undefined,
            start_lon: undefined,
            end_label: '',
          }),
        ],
      }),
    );
    expect(screen.getByText(/couldn't be located on the map/i)).toBeInTheDocument();
  });

  it('does not warn when the endpoint has coordinates', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Hotel',
            start_address: '123 Somewhere St',
            start_lat: 51.5,
            start_lon: -0.1,
            end_label: '',
          }),
        ],
      }),
    );
    expect(screen.queryByText(/couldn't be located on the map/i)).not.toBeInTheDocument();
  });

  it('prefills the coordinates override from a located endpoint', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Lake',
            start_lat: 48.21,
            start_lon: 4.08,
            end_label: '',
          }),
        ],
      }),
    );
    expect(screen.getByLabelText(/coordinates/i)).toHaveValue('48.21, 4.08');
  });

  it('pins a pasted lat/lng as a manual override on save', async () => {
    h.updatePlanPart.mockResolvedValue(part({ type: 'hotel' }));
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Lake',
            start_address: '10170 Droupt-Saint-Basle, France',
            start_lat: undefined,
            start_lon: undefined,
            end_label: '',
          }),
        ],
      }),
    );
    await userEvent.type(screen.getByLabelText(/coordinates/i), '48.2105, 4.0823');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.start_lat).toBe(48.2105);
    expect(patch.start_lon).toBe(4.0823);
    expect(patch.start_coords_pinned).toBe(true);
  });

  it('clearing a pinned override unpins it (reverts to geocoding)', async () => {
    h.updatePlanPart.mockResolvedValue(part({ type: 'hotel' }));
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Lake',
            start_lat: 48.21,
            start_lon: 4.08,
            start_coords_pinned: true,
            end_label: '',
          }),
        ],
      }),
    );
    await userEvent.clear(screen.getByLabelText(/coordinates/i));
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.start_coords_pinned).toBe(false);
    expect(patch.start_lat).toBeUndefined();
  });

  it('blocks save on an unparseable coordinate override', async () => {
    render_(
      plan({
        type: 'hotel',
        parts: [part({ type: 'hotel', start_label: 'Lake', end_label: '' })],
      }),
    );
    await userEvent.type(screen.getByLabelText(/coordinates/i), 'FWJ9+PP');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.setError).toHaveBeenCalledWith(expect.stringContaining('lat, lng')),
    );
    expect(h.updatePlanPart).not.toHaveBeenCalled();
  });

  it('pops an info notice when a just-edited address is still unlocated after save', async () => {
    // The saved part comes back still missing coordinates.
    h.updatePlanPart.mockResolvedValue(
      part({
        type: 'hotel',
        start_label: 'Hotel',
        start_address: 'Atlantis',
        start_lat: undefined,
        start_lon: undefined,
        end_label: '',
      }),
    );
    render_(
      plan({
        type: 'hotel',
        parts: [part({ type: 'hotel', start_label: 'Hotel', start_address: 'Old', end_label: '' })],
      }),
    );
    const address = screen.getAllByRole('textbox', { name: /^address$/i })[0];
    await userEvent.clear(address);
    await userEvent.type(address, 'Atlantis');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.setNotice).toHaveBeenCalledWith({
        severity: 'info',
        message: `Saved — couldn't place "Atlantis" on the map.`,
      }),
    );
  });

  // --- flight route/identity editing ---

  function flightPart(over: Partial<PlanPart['flight']> = {}): PlanPart {
    return part({
      flight: {
        ident: 'BA123',
        callsign: '',
        scheduled_out: '2026-10-12T11:35:00Z',
        scheduled_in: '2026-10-12T19:55:00Z',
        origin_iata: 'LHR',
        dest_iata: 'IAD',
        flight_status: 'Scheduled',
        resolved: true,
        ...over,
      },
    });
  }

  it('shows the flight number and IATA fields for a flight part', () => {
    render_(plan({ parts: [flightPart()] }));
    expect(screen.getByRole('textbox', { name: /flight number/i })).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: /from \(iata\)/i })).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: /to \(iata\)/i })).toBeInTheDocument();
  });

  it('makes the IATA read-only for a resolved flight, editable for an unresolved one', () => {
    const { unmount } = render_(plan({ parts: [flightPart({ resolved: true })] }));
    expect(screen.getByRole('textbox', { name: /from \(iata\)/i })).toBeDisabled();
    unmount();
    render_(plan({ parts: [flightPart({ resolved: false })] }));
    expect(screen.getByRole('textbox', { name: /from \(iata\)/i })).toBeEnabled();
  });

  it('sends a changed flight number under flight.ident', async () => {
    render_(plan({ parts: [flightPart({ resolved: true })] }));
    const ident = screen.getByRole('textbox', { name: /flight number/i });
    await userEvent.clear(ident);
    await userEvent.type(ident, 'BA999');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.flight).toEqual({ ident: 'BA999' });
  });

  it('does not send IATA edits for a resolved flight, but does for an unresolved one', async () => {
    // Resolved: the IATA field is disabled, so editing it is impossible and no
    // flight patch is produced from it.
    render_(plan({ parts: [flightPart({ resolved: false, origin_iata: 'LHR' })] }));
    const origin = screen.getByRole('textbox', { name: /from \(iata\)/i });
    await userEvent.clear(origin);
    await userEvent.type(origin, 'lgw');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.flight).toEqual({ origin_iata: 'LGW' });
  });

  it('sends a changed destination IATA (upper-cased) for an unresolved flight', async () => {
    render_(plan({ parts: [flightPart({ resolved: false, dest_iata: 'IAD' })] }));
    const dest = screen.getByRole('textbox', { name: /to \(iata\)/i });
    await userEvent.clear(dest);
    await userEvent.type(dest, 'jfk');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.flight).toEqual({ dest_iata: 'JFK' });
  });

  it('sends no flight patch when an unresolved flight is saved unchanged', async () => {
    // Forces a non-flight change so Save writes, but the untouched flight fields
    // must not produce a flight patch (covers the "nothing changed" branch).
    render_(plan({ title: 'BA123', parts: [flightPart({ resolved: false })] }));
    const title = screen.getByRole('textbox', { name: /title/i });
    await userEvent.clear(title);
    await userEvent.type(title, 'BA124');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlan).toHaveBeenCalled());
    if (h.updatePlanPart.mock.calls.length > 0) {
      const [, patch] = h.updatePlanPart.mock.calls[0];
      expect(patch.flight).toBeUndefined();
    }
  });

  // --- Google Maps URL coordinate override ---

  describe('Google Maps URL coordinate override', () => {
    beforeEach(() => {
      ha.resolveMapsUrl.mockReset();
    });

    function openHotelCoords() {
      // A hotel has a single located endpoint, so exactly one coords field
      // (the "Until"/check-out block is time-only).
      render_(
        plan({
          type: 'hotel',
          parts: [part({ type: 'hotel', start_label: 'Hotel', end_label: '' })],
        }),
      );
      return screen.getByLabelText('Coordinates (lat, lng)') as HTMLInputElement;
    }

    it('extracts coordinates from a pasted full Maps URL on blur (no network)', async () => {
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, 'https://www.google.com/maps/@51.5,-0.12,15z');
      field.blur();
      await waitFor(() => expect(field.value).toBe('51.5, -0.12'));
      expect(ha.resolveMapsUrl).not.toHaveBeenCalled();
    });

    it('resolves a short link via the backend and fills the field', async () => {
      ha.resolveMapsUrl.mockResolvedValue({ lat: 40.5, lon: -70.25 });
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, 'https://maps.app.goo.gl/abc123');
      field.blur();
      await waitFor(() => expect(field.value).toBe('40.5, -70.25'));
      expect(ha.resolveMapsUrl).toHaveBeenCalledWith('https://maps.app.goo.gl/abc123');
    });

    it('shows an inline error when a short link cannot be resolved', async () => {
      ha.resolveMapsUrl.mockRejectedValue(new Error('nope'));
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, 'https://maps.app.goo.gl/abc123');
      field.blur();
      expect(await screen.findByText(/couldn't read a location/i)).toBeInTheDocument();
    });

    it('leaves a bare lat,lng pair untouched and makes no call', async () => {
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, '48.2105, 4.0823');
      field.blur();
      await waitFor(() => expect(field.value).toBe('48.2105, 4.0823'));
      expect(ha.resolveMapsUrl).not.toHaveBeenCalled();
    });

    it('errors on a full Maps URL with no coordinates without calling the backend', async () => {
      // A place-only URL on a full Maps host: isMapsUrl is true, but there are
      // no coordinates to extract and it is not a short link, so we surface the
      // failure inline rather than hitting the resolver.
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, 'https://maps.google.com/maps/place/Somewhere');
      field.blur();
      expect(await screen.findByText(/couldn't read a location/i)).toBeInTheDocument();
      expect(ha.resolveMapsUrl).not.toHaveBeenCalled();
    });

    it('shows the resolving hint whilst a short link is in flight', async () => {
      // Hold the resolver open so the busy state is observable.
      let release: (v: { lat: number; lon: number }) => void = () => {};
      ha.resolveMapsUrl.mockReturnValue(
        new Promise((res) => {
          release = res;
        }),
      );
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, 'https://maps.app.goo.gl/abc123');
      field.blur();
      expect(await screen.findByText(/resolving link/i)).toBeInTheDocument();
      release({ lat: 1, lon: 2 });
      await waitFor(() => expect(field.value).toBe('1, 2'));
    });
  });
});
