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

describe('PlanEditDialog — part detail editors', () => {
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
});
