import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

import type { Plan, PlanPart, Trip } from '../api/types';

const state = {
  currentTrip: null as (Trip & { plans: Plan[] }) | null,
  deletePlan: vi.fn(async () => {}),
  setError: vi.fn(),
  // Fields the child dialogs (PlanEditDialog/PlanPrivacyDialog/PlanAlertToggle)
  // pull off the store when they're opened from an expanded tile.
  trips: [] as Trip[],
  users: [] as unknown[],
  listTrips: vi.fn(async () => {}),
  updatePlan: vi.fn(async () => {}),
  updatePlanPart: vi.fn(async () => {}),
  movePlan: vi.fn(async () => {}),
  linkPlans: vi.fn(async () => {}),
  splitPlanPart: vi.fn(async () => {}),
  setPlanVisibility: vi.fn(async () => {}),
  addPlanPassenger: vi.fn(async () => {}),
  removePlanPassenger: vi.fn(async () => {}),
  optInPlanAlerts: vi.fn(async () => {}),
  optOutPlanAlerts: vi.fn(async () => {}),
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

// AddToTripDialog pulls a large ingest-flow slice off the store; the timeline
// only needs to mount it when `open`, so stub it to a minimal dialog.
vi.mock('../components/AddToTripDialog', () => ({
  default: ({ open }: { open: boolean }) =>
    open ? <div role="dialog">Add to trip dialog</div> : null,
}));

import TripTimeline from './TripTimeline';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 1,
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

function plan(parts: PlanPart[], over: Partial<Plan> = {}): Plan {
  return {
    id: parts[0]?.plan_id ?? 1,
    trip_id: 1,
    type: parts[0]?.type ?? 'flight',
    title: 'Outbound',
    confirmation_ref: '',
    notes: '',
    source: '',
    passenger_ids: [],
    visibility: { mode: 'everyone', user_ids: [] },
    parts,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function tripWith(plans: Plan[]): Trip & { plans: Plan[] } {
  return {
    id: 1,
    name: 'Lisbon',
    destination: 'Lisbon',
    my_role: 'owner',
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    plans,
  };
}

function renderTimeline() {
  return render(
    <MemoryRouter>
      <TripTimeline />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  state.currentTrip = null;
  vi.clearAllMocks();
});

describe('TripTimeline', () => {
  it('shows a loading state when no trip is loaded', () => {
    renderTimeline();
    expect(screen.getByText(/Loading/i)).toBeInTheDocument();
  });

  it('shows an empty state when the trip has no plans', () => {
    state.currentTrip = tripWith([]);
    renderTimeline();
    expect(screen.getByText(/Nothing on this trip yet/i)).toBeInTheDocument();
  });

  it('makes the empty-state "New plan" a clickable control', () => {
    state.currentTrip = tripWith([]);
    renderTimeline();
    expect(screen.getByRole('button', { name: /new plan/i })).toBeInTheDocument();
  });

  it('renders day headers and a card per part', () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1, effective_at: '2026-10-12T09:00:00Z' })], {
        id: 1,
        title: 'Flight out',
      }),
    ]);
    renderTimeline();
    expect(screen.getByText(/Oct.*2026/)).toBeInTheDocument();
    expect(screen.getByText('Flight out')).toBeInTheDocument();
    expect(screen.getByTestId('part-card-1')).toBeInTheDocument();
  });

  it('drops dismissed parts', () => {
    state.currentTrip = tripWith([plan([part({ id: 1, dismissed_at: '2026-09-01T00:00:00Z' })])]);
    renderTimeline();
    expect(screen.getByText(/Nothing on this trip yet/i)).toBeInTheDocument();
  });

  it('marks a multi-part plan with a chip and ties the parts together', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({ id: 1, plan_id: 1, effective_at: '2026-10-12T09:00:00Z' }),
          part({
            id: 2,
            plan_id: 1,
            effective_at: '2026-10-18T09:00:00Z',
            start_label: 'LIS',
            end_label: 'LHR',
          }),
        ],
        { id: 1, title: 'Return flights' },
      ),
    ]);
    renderTimeline();
    // Both legs render and both carry the multi-part marker.
    expect(screen.getByTestId('part-card-1')).toBeInTheDocument();
    expect(screen.getByTestId('part-card-2')).toBeInTheDocument();
    expect(screen.getAllByText('multi-part')).toHaveLength(2);
  });

  it('renders a multi-night hotel as separate check-in and check-out tiles', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 9,
            plan_id: 2,
            type: 'hotel',
            starts_at: '2026-10-12T15:00:00Z',
            effective_at: '2026-10-12T15:00:00Z',
            ends_at: '2026-10-15T10:00:00Z',
            start_label: 'Hotel Lisboa',
            end_label: '',
          }),
        ],
        { id: 2, type: 'hotel', title: 'Hotel Lisboa' },
      ),
    ]);
    renderTimeline();
    // One tile on the check-in day, one on the check-out day — not a single band.
    expect(screen.getByTestId('part-card-9-check-in')).toBeInTheDocument();
    expect(screen.getByTestId('part-card-9-check-out')).toBeInTheDocument();
    expect(screen.getByText(/Check in/)).toBeInTheDocument();
    expect(screen.getByText(/Check out/)).toBeInTheDocument();
    // Day headers for both the arrival and departure dates.
    expect(screen.getByText(/12 Oct 2026/)).toBeInTheDocument();
    expect(screen.getByText(/15 Oct 2026/)).toBeInTheDocument();
  });

  it('greys a cancelled (superseded old) part and tags it, not the replacement', () => {
    // On a rebooking the OLD part is stamped status='cancelled'; the NEW part
    // carries supersedes_id and stays full-colour. The greying/tag keys on the
    // cancelled status, so the OLD leg (id 3) is greyed+tagged and the NEW leg
    // (id 4, supersedes_id set, planned) is not.
    state.currentTrip = tripWith([
      plan(
        [
          part({ id: 3, status: 'cancelled', effective_at: '2026-10-12T09:00:00Z' }),
          part({
            id: 4,
            status: 'planned',
            supersedes_id: 3,
            effective_at: '2026-10-12T14:00:00Z',
          }),
        ],
        { id: 1 },
      ),
    ]);
    renderTimeline();
    expect(screen.getByText('cancelled')).toBeInTheDocument();
    // Only the cancelled part is greyed.
    const oldCard = screen.getByTestId('part-card-3');
    const newCard = screen.getByTestId('part-card-4');
    expect(oldCard).toHaveStyle({ opacity: '0.55' });
    expect(newCard).toHaveStyle({ opacity: '1' });
  });

  it('expands a tile inline (no modal) when tapped, and allows several open at once', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1, effective_at: '2026-10-12T09:00:00Z' })], {
        id: 1,
        title: 'Outbound',
      }),
      plan([part({ id: 2, plan_id: 2, effective_at: '2026-10-18T09:00:00Z' })], {
        id: 2,
        title: 'Return',
      }),
    ]);
    renderTimeline();
    // Collapsed: the per-plan actions aren't mounted, and there's no modal.
    expect(screen.queryByRole('button', { name: /^Edit$/i })).not.toBeInTheDocument();
    await userEvent.click(screen.getByTestId('part-card-1'));
    await userEvent.click(screen.getByTestId('part-card-2'));
    // Both expanded at once → an Edit action per tile, and never a dialog.
    expect(screen.getAllByRole('button', { name: /^Edit$/i })).toHaveLength(2);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('shows part addresses on the collapsed tile, without expanding', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 1,
            plan_id: 1,
            type: 'ground',
            start_label: 'Home',
            end_label: 'LHR T5',
            start_address: '12 Acacia Avenue, Reading',
            end_address: 'Heathrow Terminal 5',
          }),
        ],
        { id: 1, type: 'ground', title: "Kev's taxi" },
      ),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    // No click — the address is at-a-glance info on the collapsed tile.
    expect(card).toHaveTextContent('12 Acacia Avenue, Reading');
    expect(card).toHaveTextContent('Heathrow Terminal 5');
  });

  it('omits the collapsed address line when it adds nothing over the place label', () => {
    state.currentTrip = tripWith([
      // A flight carries only airport labels and no street address, so there is
      // no distinct address worth repeating under the route.
      plan([part({ id: 1, plan_id: 1, start_label: 'LHR', end_label: 'LIS' })], {
        id: 1,
        title: 'Flight out',
      }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    expect(card).toHaveTextContent('LHR → LIS');
    // The route appears exactly once — no duplicate address line beneath it.
    expect(card.textContent?.match(/LHR/g) ?? []).toHaveLength(1);
  });

  it('surfaces privacy/passenger, edit and delete in the expanded tile (owner/editor)', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 1, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    expect(within(card).getByRole('button', { name: /Privacy & passengers/i })).toBeInTheDocument();
    expect(within(card).getByRole('button', { name: /^Edit$/i })).toBeInTheDocument();
    expect(within(card).getByRole('button', { name: /Delete/i })).toBeInTheDocument();
    // Owners receive alerts via their own prefs, so the per-plan opt-in is hidden.
    expect(within(card).queryByLabelText(/Notify me of changes/i)).not.toBeInTheDocument();
  });

  it('surfaces the per-plan alert opt-in to viewers, not the edit controls', async () => {
    state.currentTrip = {
      ...tripWith([plan([part({ id: 1, plan_id: 1 })], { id: 1, title: 'Flight out' })]),
      my_role: 'viewer',
    };
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    expect(within(card).getByLabelText(/Notify me of changes/i)).toBeInTheDocument();
    expect(
      within(card).queryByRole('button', { name: /Privacy & passengers/i }),
    ).not.toBeInTheDocument();
    expect(within(card).queryByRole('button', { name: /^Edit$/i })).not.toBeInTheDocument();
    expect(within(card).queryByRole('button', { name: /Delete/i })).not.toBeInTheDocument();
  });

  it('opens the empty-state New plan dialog when the link is clicked', async () => {
    state.currentTrip = tripWith([]);
    renderTimeline();
    await userEvent.click(screen.getByRole('button', { name: /new plan/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('shows type-specific detail lines for each plan type when expanded', async () => {
    const cases: Array<{ p: Partial<PlanPart>; expect: RegExp[] }> = [
      {
        p: { id: 11, type: 'hotel', hotel: { room_type: 'Suite', phone: '+351 1' } },
        expect: [/Suite/, /\+351 1/],
      },
      {
        p: {
          id: 12,
          type: 'train',
          train: {
            operator: 'Eurostar',
            service_no: '9012',
            class: 'Standard',
            coach: '7',
            seat: '12A',
            platform: '4',
          },
        },
        expect: [/Eurostar/, /Coach 7/, /Seat 12A/, /Platform 4/],
      },
      {
        p: {
          id: 13,
          type: 'ground',
          ground: { provider: 'Addison Lee', vehicle: 'Saloon', phone: '020' },
        },
        expect: [/Addison Lee/, /Saloon/, /020/],
      },
      {
        p: { id: 14, type: 'dining', dining: { reservation_name: 'Page', phone: '555' } },
        expect: [/Reservation: Page/, /555/],
      },
      {
        p: { id: 15, type: 'excursion', excursion: { provider: 'GetYourGuide' } },
        expect: [/GetYourGuide/],
      },
      {
        p: {
          id: 16,
          type: 'flight',
          flight: {
            ident: 'TP123',
            callsign: '',
            scheduled_out: '',
            scheduled_in: '',
            origin_iata: 'LHR',
            dest_iata: 'LIS',
            flight_status: 'Scheduled',
          },
        },
        expect: [/TP123/, /Scheduled/],
      },
    ];
    for (const c of cases) {
      state.currentTrip = tripWith([
        plan([part({ plan_id: 1, ...(c.p as Partial<PlanPart>) })], {
          id: 1,
          type: (c.p.type as Plan['type']) ?? 'flight',
          title: 'Detail plan',
        }),
      ]);
      const { unmount } = renderTimeline();
      const card = screen.getByTestId(`part-card-${c.p.id}`);
      await userEvent.click(card);
      for (const re of c.expect) expect(card).toHaveTextContent(re);
      unmount();
    }
  });

  it('renders no detail lines when type-specific objects are absent', async () => {
    // Hotel/train/ground/dining/excursion parts whose nested objects are missing
    // exercise the `if (part.x)` false branches in partDetailLines.
    for (const type of ['hotel', 'train', 'ground', 'dining', 'excursion'] as const) {
      state.currentTrip = tripWith([
        plan([part({ id: 20, plan_id: 1, type })], { id: 1, type, title: 'Bare' }),
      ]);
      const { unmount } = renderTimeline();
      await userEvent.click(screen.getByTestId('part-card-20'));
      expect(screen.getByText('Bare')).toBeInTheDocument();
      unmount();
    }
  });

  it('opens the edit dialog from an expanded tile (owner)', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 1, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    await userEvent.click(within(card).getByRole('button', { name: /^Edit$/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('opens the privacy dialog from an expanded tile (owner)', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 1, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    await userEvent.click(within(card).getByRole('button', { name: /Privacy & passengers/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('deletes a plan after confirmation', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 7, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    await userEvent.click(within(card).getByRole('button', { name: /Delete/i }));
    expect(state.deletePlan).toHaveBeenCalledWith(7);
    confirmSpy.mockRestore();
  });

  it('does not delete when confirmation is cancelled', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 7, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    await userEvent.click(within(card).getByRole('button', { name: /Delete/i }));
    expect(state.deletePlan).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('surfaces an error when deletion fails', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    state.deletePlan.mockRejectedValueOnce(new Error('nope'));
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 7, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    await userEvent.click(within(card).getByRole('button', { name: /Delete/i }));
    await vi.waitFor(() => expect(state.setError).toHaveBeenCalledWith('nope'));
    confirmSpy.mockRestore();
  });

  it('shows a confirmed chip up front and the confirmation reference once expanded', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1, status: 'confirmed' })], {
        id: 1,
        title: 'Flight out',
        confirmation_ref: 'XIIVFQ',
      }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    // The status chip is essential and stays up front.
    expect(within(card).getByText('confirmed')).toBeInTheDocument();
    // The booking ref is admin detail — only in the expanded body.
    expect(card).not.toHaveTextContent('Ref: XIIVFQ');
    await userEvent.click(card);
    expect(card).toHaveTextContent('Ref: XIIVFQ');
  });

  it('keeps the ticket number up front but moves cost into the expanded body', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], {
        id: 1,
        title: 'Flight out',
        ticket_number: '1252300000001',
        cost_amount: 523.4,
        cost_currency: 'GBP',
      }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    // Ticket is essential (needed to check in) and stays up front; cost does not.
    expect(card).toHaveTextContent('Ticket: 1252300000001');
    expect(card).not.toHaveTextContent('Cost: £523.40');
    await userEvent.click(card);
    expect(card).toHaveTextContent('Cost: £523.40');
  });

  it('shows the flight number up front on a flight tile', () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1, flight: { ident: 'FR9226' } as PlanPart['flight'] })], {
        id: 1,
        title: 'Flight to Faro',
      }),
    ]);
    renderTimeline();
    // Visible without expanding — it's plan-essential.
    expect(screen.getByTestId('part-card-1')).toHaveTextContent('Flight: FR9226');
  });

  it('shows the supplier and contact details with an open-in-new-tab website link', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], {
        id: 1,
        title: 'Flight out',
        supplier_name: 'British Airways',
        contact_email: 'help@ba.example',
        contact_phone: '+44 20 7946 0000',
        website: 'www.ba.example/manage',
      }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    // Supplier shows in the header without expanding.
    expect(card).toHaveTextContent('Supplier: British Airways');
    // The contact links live in the expanded body.
    await userEvent.click(card);
    const email = within(card).getByRole('link', { name: /help@ba.example/ });
    expect(email).toHaveAttribute('href', 'mailto:help@ba.example');
    const phone = within(card).getByRole('link', { name: /\+44 20 7946 0000/ });
    expect(phone).toHaveAttribute('href', 'tel:+442079460000');
    const site = within(card).getByRole('link', { name: /www.ba.example\/manage/ });
    // A bare host is normalised to https:// and opened in a new tab.
    expect(site).toHaveAttribute('href', 'https://www.ba.example/manage');
    expect(site).toHaveAttribute('target', '_blank');
    expect(site).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  it('preserves an explicit https website but suppresses unsafe schemes', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], {
        id: 1,
        title: 'Safe',
        website: 'https://safe.example/path',
      }),
      plan([part({ id: 2, plan_id: 2 })], {
        id: 2,
        title: 'Unsafe',
        // A persisted javascript: URL must never render as a clickable link.
        website: 'javascript:alert(1)',
      }),
    ]);
    renderTimeline();
    const safe = screen.getByTestId('part-card-1');
    await userEvent.click(safe);
    expect(within(safe).getByRole('link', { name: /safe.example\/path/ })).toHaveAttribute(
      'href',
      'https://safe.example/path',
    );
    const unsafe = screen.getByTestId('part-card-2');
    await userEvent.click(unsafe);
    expect(within(unsafe).queryByRole('link', { name: /alert/ })).toBeNull();
    expect(unsafe).not.toHaveTextContent('Website:');
  });

  it('closes the edit dialog via its onClose callback', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 1, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    await userEvent.click(within(card).getByRole('button', { name: /^Edit$/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    await userEvent.keyboard('{Escape}');
    await vi.waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('closes the privacy dialog via its onClose callback', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1 })], { id: 1, title: 'Flight out' }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    await userEvent.click(within(card).getByRole('button', { name: /Privacy & passengers/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    await userEvent.keyboard('{Escape}');
    await vi.waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('keeps a tile expanded when a click lands inside the expanded body', async () => {
    state.currentTrip = tripWith([
      plan([part({ id: 1, plan_id: 1, type: 'ground', start_address: '12 Acacia Avenue' })], {
        id: 1,
        type: 'ground',
        title: 'Taxi',
        notes: 'Ring on arrival',
      }),
    ]);
    renderTimeline();
    const card = screen.getByTestId('part-card-1');
    await userEvent.click(card);
    // Clicking the notes text (inside the expanded body) must not fold it back.
    await userEvent.click(screen.getByText('Ring on arrival'));
    expect(within(card).getByRole('button', { name: /^Edit$/i })).toBeInTheDocument();
  });

  it('shows the departure gate on a flight tile face', async () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 5,
            plan_id: 1,
            type: 'flight',
            flight: {
              ident: 'TP456',
              callsign: '',
              scheduled_out: '2026-10-12T09:00:00Z',
              scheduled_in: '2026-10-12T11:00:00Z',
              origin_iata: 'LHR',
              dest_iata: 'LIS',
              flight_status: 'Scheduled',
              origin_terminal: '5',
              origin_gate: 'B32',
            },
          }),
        ],
        { id: 1, type: 'flight', title: 'Flight out' },
      ),
    ]);
    renderTimeline();
    expect(await screen.findByText('Departure: Terminal 5 · Gate B32')).toBeInTheDocument();
  });

  it('labels the departure gate as Unknown on a flight tile when neither terminal nor gate is known', async () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 5,
            plan_id: 1,
            type: 'flight',
            flight: {
              ident: 'TP456',
              callsign: '',
              scheduled_out: '2026-10-12T09:00:00Z',
              scheduled_in: '2026-10-12T11:00:00Z',
              origin_iata: 'LHR',
              dest_iata: 'LIS',
              flight_status: 'Scheduled',
              origin_terminal: '',
              origin_gate: '',
            },
          }),
        ],
        { id: 1, type: 'flight', title: 'Flight out' },
      ),
    ]);
    renderTimeline();
    expect(await screen.findByText('Departure: Unknown')).toBeInTheDocument();
  });

  it('shows a "Not on map" chip on a tile whose addressed part has no coordinates', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 1,
            plan_id: 1,
            type: 'ground',
            start_label: 'Home',
            start_address: '12 Acacia Avenue, Reading',
            start_lat: undefined,
            start_lon: undefined,
          }),
        ],
        { id: 1, type: 'ground', title: 'Taxi' },
      ),
    ]);
    renderTimeline();
    expect(screen.getByText(/not on map/i)).toBeInTheDocument();
  });

  it('omits the "Not on map" chip once the addressed part is located', () => {
    state.currentTrip = tripWith([
      plan(
        [
          part({
            id: 1,
            plan_id: 1,
            type: 'ground',
            start_label: 'Home',
            start_address: '12 Acacia Avenue, Reading',
            start_lat: 51.45,
            start_lon: -0.97,
          }),
        ],
        { id: 1, type: 'ground', title: 'Taxi' },
      ),
    ]);
    renderTimeline();
    expect(screen.queryByText(/not on map/i)).toBeNull();
  });

  it('shows the arrival baggage belt on a flight tile face only once published', async () => {
    const flightLeg = (belt?: string) =>
      tripWith([
        plan(
          [
            part({
              id: 5,
              plan_id: 1,
              type: 'flight',
              flight: {
                ident: 'TP456',
                callsign: '',
                scheduled_out: '2026-10-12T09:00:00Z',
                scheduled_in: '2026-10-12T11:00:00Z',
                origin_iata: 'LHR',
                dest_iata: 'LIS',
                flight_status: 'Scheduled',
                ...(belt ? { dest_baggage_belt: belt } : {}),
              },
            }),
          ],
          { id: 1, type: 'flight', title: 'Flight out' },
        ),
      ]);

    // No belt yet → no belt line on the tile face.
    state.currentTrip = flightLeg();
    const { unmount } = renderTimeline();
    expect(await screen.findByText('Flight out')).toBeInTheDocument();
    expect(screen.queryByText(/Baggage belt:/)).not.toBeInTheDocument();
    unmount();

    // Belt published → it shows.
    state.currentTrip = flightLeg('34');
    renderTimeline();
    expect(await screen.findByText('Baggage belt: 34')).toBeInTheDocument();
  });

  describe('link bookings mode', () => {
    function twoFlights() {
      return tripWith([
        plan([part({ id: 1, plan_id: 1, starts_at: '2026-10-12T09:00:00Z' })], {
          id: 1,
          title: 'Outbound',
        }),
        plan([part({ id: 2, plan_id: 2, starts_at: '2026-10-20T09:00:00Z' })], {
          id: 2,
          title: 'Return',
        }),
      ]);
    }

    it('hides the "Link bookings" control for viewers', () => {
      const trip = twoFlights();
      trip.my_role = 'viewer';
      state.currentTrip = trip;
      renderTimeline();
      expect(screen.queryByRole('button', { name: /link bookings/i })).not.toBeInTheDocument();
    });

    it('links two selected same-type plans, earliest as primary', async () => {
      state.currentTrip = twoFlights();
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));

      await userEvent.click(screen.getByRole('checkbox', { name: /select outbound/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select return/i }));

      const linkBtn = screen.getByRole('button', { name: /^link 2$/i });
      expect(linkBtn).toBeEnabled();
      await userEvent.click(linkBtn);
      // Primary is the earliest-starting plan (id 1); the later one folds in.
      expect(state.linkPlans).toHaveBeenCalledWith(1, [2]);
    });

    it('links two ground (transfer) plans of the same type', async () => {
      state.currentTrip = tripWith([
        plan([part({ id: 1, plan_id: 1, type: 'ground', starts_at: '2026-10-12T09:00:00Z' })], {
          id: 1,
          type: 'ground',
          title: 'Airport pickup',
        }),
        plan([part({ id: 2, plan_id: 2, type: 'ground', starts_at: '2026-10-20T09:00:00Z' })], {
          id: 2,
          type: 'ground',
          title: 'Airport drop-off',
        }),
      ]);
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select airport pickup/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select airport drop-off/i }));
      const linkBtn = screen.getByRole('button', { name: /^link 2$/i });
      expect(linkBtn).toBeEnabled();
      await userEvent.click(linkBtn);
      expect(state.linkPlans).toHaveBeenCalledWith(1, [2]);
    });

    it('cancels link mode back to the "Link bookings" control', async () => {
      state.currentTrip = twoFlights();
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      expect(screen.getByRole('button', { name: /^cancel$/i })).toBeInTheDocument();
      await userEvent.click(screen.getByRole('button', { name: /^cancel$/i }));
      expect(screen.getByRole('button', { name: /link bookings/i })).toBeInTheDocument();
    });

    it('surfaces link errors via setError', async () => {
      state.linkPlans.mockRejectedValueOnce(new Error('link boom'));
      state.currentTrip = twoFlights();
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select outbound/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select return/i }));
      await userEvent.click(screen.getByRole('button', { name: /^link 2$/i }));
      expect(state.setError).toHaveBeenCalledWith('link boom');
    });

    it('stringifies a non-Error link rejection', async () => {
      state.linkPlans.mockRejectedValueOnce('plain boom');
      state.currentTrip = twoFlights();
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select outbound/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select return/i }));
      await userEvent.click(screen.getByRole('button', { name: /^link 2$/i }));
      expect(state.setError).toHaveBeenCalledWith('plain boom');
    });

    it('toggles a selection off when its checkbox is clicked again', async () => {
      state.currentTrip = twoFlights();
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      const cb = screen.getByRole('checkbox', { name: /select outbound/i });
      await userEvent.click(cb); // select
      expect(screen.getByRole('button', { name: /^link 1$/i })).toBeInTheDocument();
      await userEvent.click(cb); // deselect
      expect(screen.getByRole('button', { name: /^link$/i })).toBeInTheDocument();
    });

    it('labels the checkbox by type when a plan has no title', async () => {
      state.currentTrip = tripWith([
        plan([part({ id: 1, plan_id: 1, type: 'flight', starts_at: '2026-10-12T09:00:00Z' })], {
          id: 1,
          type: 'flight',
          title: '',
        }),
        plan([part({ id: 2, plan_id: 2, type: 'flight', starts_at: '2026-10-20T09:00:00Z' })], {
          id: 2,
          type: 'flight',
          title: '',
        }),
      ]);
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      // Empty title → the accessible name falls back to the type label "Flight".
      expect(screen.getAllByRole('checkbox', { name: /select flight/i })).toHaveLength(2);
    });

    it('does not offer a checkbox for an ineligible (hotel) plan in link mode', async () => {
      state.currentTrip = tripWith([
        plan([part({ id: 1, plan_id: 1, type: 'flight', starts_at: '2026-10-12T09:00:00Z' })], {
          id: 1,
          type: 'flight',
          title: 'A flight',
        }),
        plan([part({ id: 2, plan_id: 2, type: 'flight', starts_at: '2026-10-20T09:00:00Z' })], {
          id: 2,
          type: 'flight',
          title: 'Another flight',
        }),
        plan(
          [
            part({
              id: 3,
              plan_id: 3,
              type: 'hotel',
              end_label: '',
              starts_at: '2026-10-13T15:00:00Z',
            }),
          ],
          { id: 3, type: 'hotel', title: 'A hotel' },
        ),
      ]);
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      // The two flights are selectable; the hotel is not.
      expect(screen.getAllByRole('checkbox')).toHaveLength(2);
      expect(screen.queryByRole('checkbox', { name: /select a hotel/i })).not.toBeInTheDocument();
      // Clicking the inert hotel card neither selects nor throws.
      await userEvent.click(screen.getByTestId('part-card-3'));
      expect(screen.getByRole('button', { name: /^link$/i })).toBeDisabled();
    });

    it('keeps Link disabled when the selection mixes types', async () => {
      state.currentTrip = tripWith([
        plan([part({ id: 1, plan_id: 1, type: 'flight', starts_at: '2026-10-12T09:00:00Z' })], {
          id: 1,
          type: 'flight',
          title: 'A flight',
        }),
        plan(
          [
            part({
              id: 2,
              plan_id: 2,
              type: 'train',
              end_label: 'PAR',
              starts_at: '2026-10-13T09:00:00Z',
            }),
          ],
          { id: 2, type: 'train', title: 'A train' },
        ),
      ]);
      renderTimeline();
      await userEvent.click(screen.getByRole('button', { name: /link bookings/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select a flight/i }));
      await userEvent.click(screen.getByRole('checkbox', { name: /select a train/i }));
      expect(screen.getByRole('button', { name: /^link 2$/i })).toBeDisabled();
      expect(state.linkPlans).not.toHaveBeenCalled();
    });
  });
});
