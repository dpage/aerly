import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Capabilities, PlanPart, ProposedPlan, Trip } from '../api/types';

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

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 0,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T09:00:00Z',
    start_tz: 'UTC',
    end_tz: 'UTC',
    start_label: 'LHR',
    end_label: 'LIS',
    status: 'planned',
    effective_at: '2026-10-12T09:00:00Z',
    ...over,
  };
}

function proposal(over: Partial<ProposedPlan> = {}): ProposedPlan {
  return {
    type: 'flight',
    title: 'BA286',
    confirmation_ref: 'ABC123',
    ticket_number: '',
    notes: '',
    cost_currency: '',
    confidence: 0.95,
    parts: [part()],
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.trips = [];
  h.state.currentTrip = null;
  h.state.capabilities = { resolver_available: false } as Capabilities;
  h.state.ingestProposals = [];
  h.state.ingestBusy = false;
});

describe('AddToTripDialog - confirm step field coverage', () => {
  it('edits confirmation ref and notes in the confirm step', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ title: 'BA286' })];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'BA286');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    const conf = screen.getByLabelText('Confirmation ref');
    await userEvent.clear(conf);
    await userEvent.type(conf, 'NEWREF');
    // Short values: typing into several controlled MUI fields is slow.
    await userEvent.type(screen.getByLabelText('Supplier'), 'BA');
    await userEvent.type(screen.getByLabelText('Contact email'), 'a@b.co');
    await userEvent.type(screen.getByLabelText('Contact phone'), '+1');
    await userEvent.type(screen.getByLabelText('Website'), 'b.co');
    const notes = screen.getByLabelText('Notes');
    await userEvent.type(notes, 'seat');
    // Ticket / cost / currency edits on the proposal (set atomically to stay fast).
    fireEvent.change(screen.getByLabelText('Ticket number'), { target: { value: 'TK1' } });
    fireEvent.change(screen.getByLabelText('Cost'), { target: { value: '10' } });
    fireEvent.change(screen.getByLabelText('Currency'), { target: { value: 'usd' } });
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans[0].confirmation_ref).toBe('NEWREF');
    expect(plans[0].notes).toBe('seat');
    expect(plans[0].supplier_name).toBe('BA');
    expect(plans[0].contact_email).toBe('a@b.co');
    expect(plans[0].contact_phone).toBe('+1');
    expect(plans[0].website).toBe('b.co');
    expect(plans[0].ticket_number).toBe('TK1');
    expect(plans[0].cost_amount).toBe(10);
    expect(plans[0].cost_currency).toBe('USD');
  }, 30000);

  it('renders a proposal whose tz/labels are empty and that has no times', async () => {
    // Exercises toDraft `|| undefined` falsy branches and the part-label
    // fallback / no-time branch in the confirm step.
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({
          confirmation_ref: '',
          parts: [
            part({
              type: 'excursion',
              start_tz: '',
              end_tz: '',
              start_label: '',
              end_label: '',
              start_address: '',
              end_address: '',
              starts_at: '',
              ends_at: undefined,
            }),
          ],
        }),
      ];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'sparse');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans[0].confirmation_ref).toBeUndefined();
  });

  it('renders a multi-part proposal label and an invalid date verbatim', async () => {
    // parts.length > 1 → "· N parts"; a non-parseable starts_at falls back to
    // the raw string (fmtIso NaN guard).
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({
          parts: [
            part({ id: 1, starts_at: 'not-a-date', ends_at: undefined }),
            part({ id: 2, starts_at: 'also-bad', ends_at: undefined }),
          ],
        }),
      ];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'multi');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    expect(screen.getByText(/· 2 parts/)).toBeInTheDocument();
    expect(screen.getByText(/not-a-date/)).toBeInTheDocument();
  });

  it('renders a hotel proposal with an unparseable check-in date verbatim', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({
          type: 'hotel',
          parts: [part({ type: 'hotel', starts_at: 'bad-checkin', ends_at: undefined })],
        }),
      ];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'hotel');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    expect(screen.getByText(/Check in bad-checkin/)).toBeInTheDocument();
  });
});

describe('AddToTripDialog - upload tab', () => {
  it('reads a text file inline and ingests file + text', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal()];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Upload' }));

    const file = new File(['BA286 LHR-LIS'], 'itin.txt', { type: 'text/plain' });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, file);

    expect(screen.getByText(/Selected: itin\.txt/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    expect(h.state.ingest).toHaveBeenCalledWith(
      1,
      expect.objectContaining({ source: 'upload', text: 'BA286 LHR-LIS' }),
    );
    const arg = h.state.ingest.mock.calls[0][1];
    expect(arg.file).toBe(file);
  });

  it('ignores a file-input change that selects no file', async () => {
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Upload' }));
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    // Fire a change event with no files (the `!chosen` early return in onFile).
    input.dispatchEvent(new Event('change', { bubbles: true }));
    expect(screen.queryByText(/Selected:/)).not.toBeInTheDocument();
    // Extract stays disabled with no file chosen.
    expect(screen.getByRole('button', { name: 'Extract plan' })).toBeDisabled();
  });

  it('falls back to empty text when reading a text file fails', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal()];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Upload' }));
    const file = new File(['x'], 'broken.txt', { type: 'text/plain' });
    // Force the inline text read to reject (the catch → setText('') path).
    vi.spyOn(file, 'text').mockRejectedValue(new Error('read fail'));
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, file);
    expect(await screen.findByText(/Selected: broken\.txt/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    const arg = h.state.ingest.mock.calls[0][1];
    expect(arg.file).toBe(file);
    expect(arg.text).toBeUndefined();
  });

  it('sends a binary (PDF) file without inlined text', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal()];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Upload' }));

    const pdf = new File([new Uint8Array([1, 2, 3])], 'ticket.pdf', { type: 'application/pdf' });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, pdf);
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    const arg = h.state.ingest.mock.calls[0][1];
    expect(arg.file).toBe(pdf);
    expect(arg.text).toBeUndefined();
  });

  it('surfaces confirmIngest errors via setError', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal()];
    });
    h.state.confirmIngest.mockRejectedValue(new Error('confirm boom'));
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    expect(h.state.setError).toHaveBeenCalledWith('confirm boom');
  });

  it('renders hotel check-in/out and a timed part in the confirm step', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({
          type: 'hotel',
          title: 'Hotel Lisboa',
          parts: [
            part({
              type: 'hotel',
              start_label: 'Hotel Lisboa',
              end_label: '',
              starts_at: '2026-10-12T14:00:00Z',
              ends_at: '2026-10-15T11:00:00Z',
            }),
          ],
        }),
        proposal({
          type: 'flight',
          title: 'BA286',
          parts: [part({ starts_at: '2026-10-12T09:00:00Z', ends_at: undefined })],
        }),
      ];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'mixed');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    // Hotel part renders check-in/check-out date strings (fmtIsoDate); the
    // flight part renders a date-time (fmtIso).
    expect(screen.getByText(/Check in/)).toBeInTheDocument();
    expect(screen.getByText(/Check out/)).toBeInTheDocument();
    // Two proposals → "Add 2 plans".
    expect(screen.getByRole('button', { name: /Add 2 plans/ })).toBeInTheDocument();
  });

  it('cancels the confirm step back to the input tabs', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal()];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    expect(screen.getByText('Confirm extracted plans')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Back' }));
    expect(screen.queryByText('Confirm extracted plans')).not.toBeInTheDocument();
    expect(h.state.clearIngest).toHaveBeenCalled();
  });
});

describe('AddToTripDialog - paste/confirm flow', () => {
  it('ingests pasted text then shows the confirm step with proposals', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ title: 'BA286', confidence: 0.95 })];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'BA286 LHR-LIS');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    expect(h.state.ingest).toHaveBeenCalledWith(1, {
      text: 'BA286 LHR-LIS',
      source: 'paste',
    });
    // Confirm step takes over.
    expect(screen.getByText('Confirm extracted plans')).toBeInTheDocument();
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('BA286');
  });

  it('flags a low-confidence proposal', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ confidence: 0.3 })];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'fuzzy');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    expect(screen.getByText(/Low confidence/i)).toBeInTheDocument();
  });

  it('confirms edited proposals via confirmIngest', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ title: 'BA286' })];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<AddToTripDialog open tripId={1} onClose={onClose} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'BA286');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    const title = screen.getByLabelText('Title');
    await userEvent.clear(title);
    await userEvent.type(title, 'Edited title');
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    expect(h.state.confirmIngest).toHaveBeenCalledTimes(1);
    const [tripId, plans] = h.state.confirmIngest.mock.calls[0];
    expect(tripId).toBe(1);
    expect(plans).toHaveLength(1);
    expect(plans[0].title).toBe('Edited title');
    expect(onClose).toHaveBeenCalled();
  });

  it('strips provider-resolved read-only flight fields from the confirm payload', async () => {
    // A flight proposal carries live provider data (gate, terminal, aircraft,
    // baggage belt, resolved flag, positions). Those aren't part of the write
    // contract; echoing them back trips the server's strict json decoder
    // ("unknown field origin_gate"). The confirm payload must send only the
    // editable subset.
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({
          parts: [
            part({
              flight: {
                ident: 'BA286',
                callsign: 'BAW286',
                scheduled_out: '2026-10-12T09:00:00Z',
                scheduled_in: '2026-10-12T20:00:00Z',
                origin_iata: 'LHR',
                dest_iata: 'SFO',
                flight_status: 'scheduled',
                origin_gate: 'B32',
                dest_gate: 'A5',
                origin_terminal: '5',
                dest_terminal: 'I',
                aircraft_type: 'Boeing 777-300ER',
                dest_baggage_belt: '3',
                resolved: true,
                last_polled_at: '2026-10-11T09:00:00Z',
              },
            }),
          ],
        }),
      ];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'BA286');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const flight = h.state.confirmIngest.mock.calls[0][1][0].parts[0].flight;
    // The editable identifiers survive.
    expect(flight.ident).toBe('BA286');
    expect(flight.origin_iata).toBe('LHR');
    expect(flight.dest_iata).toBe('SFO');
    // The provider-resolved read-only fields are gone.
    expect(flight).not.toHaveProperty('origin_gate');
    expect(flight).not.toHaveProperty('dest_gate');
    expect(flight).not.toHaveProperty('origin_terminal');
    expect(flight).not.toHaveProperty('dest_terminal');
    expect(flight).not.toHaveProperty('aircraft_type');
    expect(flight).not.toHaveProperty('dest_baggage_belt');
    expect(flight).not.toHaveProperty('resolved');
    expect(flight).not.toHaveProperty('last_polled_at');
    expect(flight).not.toHaveProperty('latest_position');
    expect(flight).not.toHaveProperty('track');
  });

  it('strips derived hotel suggestion fields from the confirm payload', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({
          type: 'hotel',
          parts: [
            part({
              type: 'hotel',
              hotel: {
                property_name: 'Melia Tortuga Beach Resort and Spa',
                address: 'Sal, Cape Verde',
                phone: '',
                room_type: 'Double',
                standard_checkin: '15:00',
                standard_checkout: '11:00',
                checkin_suggested: '2026-10-12T15:00:00Z',
                checkout_suggested: '2026-10-25T11:00:00Z',
              },
            }),
          ],
        }),
      ];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'hotel');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const hotel = h.state.confirmIngest.mock.calls[0][1][0].parts[0].hotel;
    expect(hotel.property_name).toBe('Melia Tortuga Beach Resort and Spa');
    expect(hotel.standard_checkin).toBe('15:00');
    expect(hotel).not.toHaveProperty('checkin_suggested');
    expect(hotel).not.toHaveProperty('checkout_suggested');
  });

  it('offers a supersession choice and carries it through on confirm', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ supersedes_part_id: 42 })];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'rebooking');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    // Default keeps the supersession (replace existing).
    expect(screen.getByText(/replaces an existing plan part|rebooking/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));
    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans[0].supersedes_part_id).toBe(42);
  });

  it('drops the supersession when the user chooses to keep the existing part', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [proposal({ supersedes_part_id: 42 })];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'rebooking');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    // Switch the supersession select to "keep existing". The confirm step has
    // a single combobox (the supersession choice); open it and pick the
    // "add as a new part" option.
    await userEvent.click(screen.getByRole('combobox'));
    await userEvent.click(await screen.findByRole('option', { name: /Add as a new part/i }));
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans[0].supersedes_part_id).toBeUndefined();
  });

  it('skipping a proposal excludes it from the confirm payload', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [
        proposal({ title: 'Keep me' }),
        proposal({ title: 'Skip me', parts: [part({ id: 2 })] }),
      ];
    });
    h.state.confirmIngest.mockResolvedValue(undefined);
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);

    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'two plans');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));

    // Skip the second proposal.
    const second = screen.getByTestId('proposal-1');
    await userEvent.click(within(second).getByRole('button', { name: 'Skip this one' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add plan' }));

    const plans = h.state.confirmIngest.mock.calls[0][1];
    expect(plans).toHaveLength(1);
    expect(plans[0].title).toBe('Keep me');
  });

  it('shows a "nothing found" message when ingest returns no proposals', async () => {
    h.state.ingest.mockImplementation(async () => {
      h.state.ingestProposals = [];
    });
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'gibberish');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    expect(screen.getByText(/couldn.t find any plans/i)).toBeInTheDocument();
  });

  it('stays on the input step when ingest throws (e.g. 501)', async () => {
    h.state.ingest.mockRejectedValue(new Error('not implemented'));
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'Paste text' }));
    await userEvent.type(screen.getByLabelText('Confirmation text'), 'anything');
    await userEvent.click(screen.getByRole('button', { name: 'Extract plan' }));
    // No confirm step; input remains.
    expect(screen.queryByText('Confirm extracted plans')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Extract plan' })).toBeInTheDocument();
  });
});

describe('AddToTripDialog - from email tab', () => {
  it('shows the forwarding address when email ingest is enabled', async () => {
    h.state.capabilities = {
      resolver_available: false,
      email_ingest_enabled: true,
      email_ingest_address: 'trips@aerly.test',
    } as Capabilities;
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'From email' }));
    const link = screen.getByRole('link', { name: 'trips@aerly.test' });
    expect(link).toHaveAttribute('href', 'mailto:trips@aerly.test');
  });

  it('explains when email ingest is disabled', async () => {
    h.state.capabilities = {
      resolver_available: false,
      email_ingest_enabled: false,
    } as Capabilities;
    render(<AddToTripDialog open tripId={1} onClose={vi.fn()} />);
    await userEvent.click(screen.getByRole('tab', { name: 'From email' }));
    expect(screen.getByText(/isn.t enabled on this server/i)).toBeInTheDocument();
  });
});
