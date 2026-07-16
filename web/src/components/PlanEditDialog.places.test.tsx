import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Plan, Trip, User } from '../api/types';

const h = vi.hoisted(() => ({
  updatePlan: vi.fn(),
  updatePlanPart: vi.fn(),
  movePlan: vi.fn(),
  splitPlanPart: vi.fn(),
  listTrips: vi.fn(),
  setError: vi.fn(),
  setNotice: vi.fn(),
  state: { trips: [] as Trip[], me: null as User | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      trips: h.state.trips,
      me: h.state.me,
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
  h.state.me = null;
  h.state.trips = [
    trip({ id: 7, name: 'Lisbon', my_role: 'owner' }), // current trip
    trip({ id: 8, name: 'Porto', my_role: 'editor' }), // editable elsewhere
    trip({ id: 9, name: 'Madrid', my_role: 'viewer' }), // not editable
  ];
});

function me(over: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'octocat',
    name: 'Octo Cat',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    home_address: '',
    ...over,
  };
}

function render_(p: Plan = plan()) {
  return render(<PlanEditDialog open plan={p} onClose={() => {}} />);
}

describe('PlanEditDialog — places & coordinates', () => {
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

  it('offers no "Use my home" button when no home location is pinned', () => {
    render_(
      plan({
        type: 'hotel',
        parts: [part({ type: 'hotel', start_label: 'Lake', end_label: '' })],
      }),
    );
    expect(screen.queryByRole('button', { name: /use my home/i })).not.toBeInTheDocument();
  });

  it('fills the coordinate override from the pinned home when "Use my home" is clicked', async () => {
    h.state.me = me({ home_lat: 51.50735, home_lon: -0.12776 });
    h.updatePlanPart.mockResolvedValue(part({ type: 'hotel' }));
    render_(
      plan({
        type: 'hotel',
        parts: [
          part({
            type: 'hotel',
            start_label: 'Lake',
            start_lat: undefined,
            start_lon: undefined,
            end_label: '',
          }),
        ],
      }),
    );
    await userEvent.click(screen.getByRole('button', { name: /use my home/i }));
    expect(screen.getByLabelText(/coordinates/i)).toHaveValue('51.50735, -0.12776');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.updatePlanPart).toHaveBeenCalled());
    const [, patch] = h.updatePlanPart.mock.calls[0];
    expect(patch.start_lat).toBe(51.50735);
    expect(patch.start_lon).toBe(-0.12776);
    expect(patch.start_coords_pinned).toBe(true);
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

    it('resolves a full Maps URL with no embedded coordinates via the backend', async () => {
      // A place-only URL names a spot but embeds no coordinates. The backend
      // follows it and reads the location off the rendered map page, so we hand
      // it over rather than giving up client-side.
      ha.resolveMapsUrl.mockResolvedValue({ lat: 48.8584, lon: 2.2945 });
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, 'https://maps.google.com/maps/place/Somewhere');
      field.blur();
      await waitFor(() => expect(field.value).toBe('48.8584, 2.2945'));
      expect(ha.resolveMapsUrl).toHaveBeenCalledWith('https://maps.google.com/maps/place/Somewhere');
    });

    it('shows an inline error when the backend cannot read a location', async () => {
      ha.resolveMapsUrl.mockRejectedValue(new Error('unprocessable'));
      const field = openHotelCoords();
      await userEvent.clear(field);
      await userEvent.type(field, 'https://maps.google.com/maps/place/Somewhere');
      field.blur();
      expect(await screen.findByText(/couldn't read a location/i)).toBeInTheDocument();
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
