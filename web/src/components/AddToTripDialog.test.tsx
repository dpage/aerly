import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Capabilities, Trip } from '../api/types';

// Drive the DateTimePicker through a plain controlled input so the manual
// form's dates are deterministic.
vi.mock('@mui/x-date-pickers/DateTimePicker', () => ({
  DateTimePicker: ({
    label,
    value,
    onChange,
  }: {
    label: string;
    value: Date | null;
    onChange: (d: Date | null) => void;
  }) => (
    <input
      aria-label={label}
      type="datetime-local"
      value={value ? new Date(value).toISOString().slice(0, 16) : ''}
      onChange={(e) => onChange(e.target.value ? new Date(e.target.value) : null)}
    />
  ),
}));

const h = vi.hoisted(() => ({
  state: {
    trips: [] as Trip[],
    listTrips: vi.fn(),
    currentTrip: null as (Trip & { plans: [] }) | null,
    capabilities: { resolver_available: false } as Capabilities,
    ingestProposals: [] as ProposedPlan[],
    ingestBusy: false,
    createPlan: vi.fn(),
    ingest: vi.fn(),
    confirmIngest: vi.fn(),
    clearIngest: vi.fn(),
    setError: vi.fn(),
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof h.state) => unknown) => sel(h.state),
}));

import AddToTripDialog from './AddToTripDialog';

beforeEach(() => {
  vi.clearAllMocks();
  h.state.trips = [];
  h.state.currentTrip = null;
  h.state.capabilities = { resolver_available: false } as Capabilities;
  h.state.ingestProposals = [];
  h.state.ingestBusy = false;
});

describe('AddToTripDialog - shell', () => {
  it('renders the four capture tabs', () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    expect(screen.getByRole('tab', { name: 'Manual' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Paste text' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Upload' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'From email' })).toBeInTheDocument();
  });

  it('cancel calls onClose and clears pending proposals', async () => {
    const onClose = vi.fn();
    render(<AddToTripDialog open tripId={1} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalled();
    expect(h.state.clearIngest).toHaveBeenCalled();
  });

  it('shows the title "New plan" (always trip-scoped, no picker)', () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    expect(screen.getByRole('heading', { name: 'New plan' })).toBeInTheDocument();
    // No trip picker — the trip is always known from the page it opened from.
    expect(screen.queryByLabelText('Trip')).not.toBeInTheDocument();
  });
});

describe('AddToTripDialog - manual tab', () => {
  it('builds a CreatePlanInput and calls createPlan', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<AddToTripDialog open tripId={1} onClose={onClose} />);

    await userEvent.type(screen.getByLabelText(/Title/), 'Flight to Lisbon');
    await userEvent.type(screen.getByLabelText(/Flight number/), 'ba286');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    expect(h.state.createPlan).toHaveBeenCalledTimes(1);
    const [tripId, input] = h.state.createPlan.mock.calls[0];
    expect(tripId).toBe(1);
    expect(input.type).toBe('flight');
    expect(input.title).toBe('Flight to Lisbon');
    expect(input.parts).toHaveLength(1);
    expect(input.parts[0].flight.ident).toBe('BA286');
    expect(onClose).toHaveBeenCalled();
  });

  it('carries the ticket number and cost (with currency) into the CreatePlanInput', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.type(screen.getByLabelText(/Title/), 'Flight to Lisbon');
    await userEvent.type(screen.getByLabelText(/Ticket number/), 'E1234567890');
    await userEvent.type(screen.getByLabelText(/^Cost/), '250.5');
    await userEvent.type(screen.getByLabelText(/Currency/), 'gbp');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const [, input] = h.state.createPlan.mock.calls[0];
    expect(input.ticket_number).toBe('E1234567890');
    expect(input.cost_amount).toBe(250.5);
    expect(input.cost_currency).toBe('GBP');
  });

  it('disables submit until a title is entered', async () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Add plan' })).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/Title/), 'Dinner');
    expect(screen.getByRole('button', { name: 'Add plan' })).toBeEnabled();
  });

  it('surfaces createPlan errors via setError', async () => {
    h.state.createPlan.mockRejectedValue(new Error('create failed'));
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/Title/), 'X');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    expect(h.state.setError).toHaveBeenCalledWith('create failed');
  });

  it('builds a hotel plan (no flight ident, hotel field labels)', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    // Switch the type to Hotel — exercises the per-type label helpers and the
    // showEnd/isTransfer branches (hotel shows an end but is not a transfer).
    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: /hotel/i }));

    // Hotel-specific labels appear; flight ident field does not. A hotel isn't a
    // transfer, so there's no "To" label — the room is its own detail field now.
    expect(screen.getByLabelText('Property')).toBeInTheDocument();
    expect(screen.getByLabelText('Check-in')).toBeInTheDocument();
    expect(screen.getByLabelText('Check-out')).toBeInTheDocument();
    expect(screen.getByLabelText(/Room type/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/Flight number/)).not.toBeInTheDocument();

    await userEvent.type(screen.getByLabelText(/^Title/), 'Hotel Lisboa');
    await userEvent.type(screen.getByLabelText('Property'), 'Lobby');
    await userEvent.type(screen.getByLabelText(/Room type/), 'Suite');
    await userEvent.type(screen.getByLabelText('Guests'), '2');
    await userEvent.type(screen.getByLabelText('Property address'), '1 Rua');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const [, input] = h.state.createPlan.mock.calls[0];
    expect(input.type).toBe('hotel');
    expect(input.parts[0].flight).toBeUndefined();
    expect(input.parts[0].start_label).toBe('Lobby');
    // The room/guests land on the hotel detail, and the property name mirrors
    // the single "Property" (start_label) field rather than a duplicate input.
    expect(input.parts[0].hotel).toMatchObject({
      property_name: 'Lobby',
      room_type: 'Suite',
      guests: 2,
    });
  });

  it('builds a dining plan (single endpoint, no end field)', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: /dining/i }));

    // Dining is single-point: only a Location/Time start, no "To" / arrival.
    expect(screen.getByLabelText('Location')).toBeInTheDocument();
    expect(screen.getByLabelText('Time')).toBeInTheDocument();
    expect(screen.queryByLabelText('To address')).not.toBeInTheDocument();

    await userEvent.type(screen.getByLabelText(/^Title/), 'Dinner at Belcanto');
    fireEvent.change(screen.getByLabelText(/Reservation name/), { target: { value: 'Page' } });
    fireEvent.change(screen.getByLabelText('Party size'), { target: { value: '4' } });
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const [, input] = h.state.createPlan.mock.calls[0];
    expect(input.type).toBe('dining');
    expect(input.parts[0].ends_at).toBeUndefined();
    expect(input.parts[0].dining).toMatchObject({ reservation_name: 'Page', party_size: 4 });
  });

  it('builds a train plan with operator/coach/seat detail', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: /train/i }));

    // A train is a transfer (From/To) and exposes its own detail fields.
    expect(screen.getByLabelText('To')).toBeInTheDocument();
    expect(screen.getByLabelText(/Operator/)).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText(/^Title/), 'Eurostar');
    await userEvent.type(screen.getByLabelText(/Operator/), 'Eurostar');
    await userEvent.type(screen.getByLabelText(/Service no/), '9024');
    await userEvent.type(screen.getByLabelText('Coach'), '12');
    await userEvent.type(screen.getByLabelText('Seat'), '44');
    fireEvent.change(screen.getByLabelText('Class'), { target: { value: 'Standard' } });
    fireEvent.change(screen.getByLabelText('Platform'), { target: { value: '5' } });
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const [, input] = h.state.createPlan.mock.calls[0];
    expect(input.type).toBe('train');
    expect(input.parts[0].train).toMatchObject({
      operator: 'Eurostar',
      service_no: '9024',
      coach: '12',
      seat: '44',
      class: 'Standard',
      platform: '5',
    });
  });
});

describe('AddToTripDialog - manual tab field coverage', () => {
  it('edits confirmation ref, notes, supplier/contact and a transfer To address', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    // Default type is flight (a transfer) → the "To address" field is shown.
    // Values are kept short: this types into many fields and userEvent is slow.
    await userEvent.type(screen.getByLabelText(/^Title/), 'BA');
    fireEvent.change(screen.getByLabelText('To'), { target: { value: 'Lisbon' } });
    await userEvent.type(screen.getByLabelText('To address'), 'LIS');
    await userEvent.type(screen.getByLabelText(/Confirmation ref/), 'REF42');
    await userEvent.type(screen.getByLabelText(/^Supplier/), 'BA');
    await userEvent.type(screen.getByLabelText(/Contact email/), 'a@b.co');
    await userEvent.type(screen.getByLabelText(/Contact phone/), '+1');
    await userEvent.type(screen.getByLabelText(/^Website/), 'b.co');
    await userEvent.type(screen.getByLabelText(/Notes/), 'seat');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    const [, input] = h.state.createPlan.mock.calls[0];
    expect(input.confirmation_ref).toBe('REF42');
    expect(input.notes).toBe('seat');
    expect(input.supplier_name).toBe('BA');
    expect(input.contact_email).toBe('a@b.co');
    expect(input.contact_phone).toBe('+1');
    expect(input.website).toBe('b.co');
    expect(input.parts[0].end_label).toBe('Lisbon');
    expect(input.parts[0].end_address).toBe('LIS');
  }, 30000);

  it('uses the per-type field labels for train, ground and excursion', async () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: /train/i }));
    expect(screen.getByLabelText('From')).toBeInTheDocument();
    expect(screen.getByLabelText('To')).toBeInTheDocument();
    expect(screen.getByLabelText('Departs')).toBeInTheDocument();
    expect(screen.getByLabelText('Arrives')).toBeInTheDocument();

    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: /car|ground|transport/i }));
    expect(screen.getByLabelText('From')).toBeInTheDocument();

    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: /excursion|activity/i }));
    // Excursion is single-point → Location/Time labels, no end.
    expect(screen.getByLabelText('Location')).toBeInTheDocument();
    expect(screen.getByLabelText('Time')).toBeInTheDocument();
  });

  it('builds a ground plan with provider/vehicle/driver/passengers detail', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: 'Ground transport' }));

    await userEvent.type(screen.getByLabelText(/^Title/), 'Transfer');
    // Set the detail fields atomically (fireEvent) to stay fast under coverage.
    fireEvent.change(screen.getByLabelText(/Provider/), { target: { value: 'Addison Lee' } });
    fireEvent.change(screen.getByLabelText(/Vehicle/), { target: { value: 'Saloon' } });
    fireEvent.change(screen.getByLabelText(/Driver/), { target: { value: 'Sam' } });
    fireEvent.change(screen.getByLabelText('Passengers'), { target: { value: '3' } });
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const [, input] = h.state.createPlan.mock.calls[0];
    expect(input.type).toBe('ground');
    expect(input.parts[0].ground).toMatchObject({
      provider: 'Addison Lee',
      vehicle: 'Saloon',
      driver: 'Sam',
      pax: 3,
    });
  });

  it('builds an excursion plan with provider and ticket count', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: 'Excursion' }));

    await userEvent.type(screen.getByLabelText(/^Title/), 'Tour');
    fireEvent.change(screen.getByLabelText(/Provider/), { target: { value: 'City Tours' } });
    fireEvent.change(screen.getByLabelText('Tickets'), { target: { value: '5' } });
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const [, input] = h.state.createPlan.mock.calls[0];
    expect(input.type).toBe('excursion');
    expect(input.parts[0].excursion).toMatchObject({ provider: 'City Tours', ticket_count: 5 });
  });

  it('builds point-in-time meeting and event plans (Location/Time labels, no detail block)', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    // Meeting: single point (Location/Time), no per-type detail block.
    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: 'Meeting' }));
    expect(screen.getByLabelText('Location')).toBeInTheDocument();
    expect(screen.getByLabelText('Time')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText(/^Title/), 'Standup');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    expect(h.state.createPlan.mock.calls[0][1].type).toBe('meeting');

    h.state.createPlan.mockClear();

    // Event: likewise a single point in time.
    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: 'Event' }));
    expect(screen.getByLabelText('Location')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText(/^Title/), 'Keynote');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    expect(h.state.createPlan.mock.calls[0][1].type).toBe('event');
  });

  it('renders the ice cream stop with a reservation name and no ticket/supplier', async () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByLabelText('Type'));
    await userEvent.click(await screen.findByRole('option', { name: 'Ice cream' }));
    expect(screen.getByLabelText('Location')).toBeInTheDocument();
    expect(screen.getByLabelText('Time')).toBeInTheDocument();
    // An ice cream stop isn't a booking: its "confirmation" is the reservation
    // name, and the ticket-number/supplier section is hidden.
    expect(screen.getByLabelText(/Reservation name/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Ticket number')).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/^Supplier/)).not.toBeInTheDocument();
  });

  it('surfaces a non-Error create failure by stringifying it', async () => {
    h.state.createPlan.mockRejectedValue('string fail');
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.type(screen.getByLabelText(/^Title/), 'X');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    expect(h.state.setError).toHaveBeenCalledWith('string fail');
  });
});

describe('AddToTripDialog - prefill', () => {
  it('pre-fills the manual form from a POI prefill and pins the coordinates on submit', async () => {
    h.state.createPlan.mockResolvedValue(undefined);
    render(
      <AddToTripDialog
        open
        tripId={1}
        onClose={vi.fn()}
        prefill={{
          type: 'excursion',
          title: 'Example Tower',
          startLabel: 'Example Tower',
          startAddress: 'Example Road',
          startLat: 51.501,
          startLon: -0.1245,
        }}
      />,
    );

    // Fields are seeded, including the type-specific "Location" label that
    // only appears for a point-in-time type like excursion.
    expect(screen.getByLabelText(/^Title/)).toHaveValue('Example Tower');
    expect(screen.getByLabelText('Location')).toHaveValue('Example Tower');
    expect(screen.getByLabelText(/Location address/)).toHaveValue('Example Road');

    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    expect(h.state.createPlan).toHaveBeenCalledTimes(1);
    const [tripId, input] = h.state.createPlan.mock.calls[0];
    expect(tripId).toBe(1);
    expect(input.type).toBe('excursion');
    expect(input.parts[0]).toMatchObject({
      start_lat: 51.501,
      start_lon: -0.1245,
      start_coords_pinned: true,
    });
  });

  it('leaves the manual form at its plain defaults when no prefill is given', () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    expect(screen.getByLabelText(/^Title/)).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Add plan' })).toBeDisabled();
  });
});
