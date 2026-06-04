import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

import type { Trip, User } from '../api/types';

const navigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigate };
});

const listTrips = vi.fn();
const createTrip = vi.fn();
const setError = vi.fn();

const state = {
  trips: [] as Trip[],
  tripsLoading: false,
  users: [] as User[],
  me: null as User | null,
  listTrips,
  createTrip,
  setError,
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

vi.mock('../api/client', () => ({ api: { listTrips: vi.fn(), importTrip: vi.fn() } }));

import TripList from './TripList';
import { api } from '../api/client';
const mockApiListTrips = api.listTrips as unknown as ReturnType<typeof vi.fn>;
const mockImportTrip = api.importTrip as unknown as ReturnType<typeof vi.fn>;

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'Trip',
    destination: '',
    my_role: 'owner',
    viewer_is_passenger: false,
    members: [],
    tags: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'octocat',
    name: '',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    ...over,
  };
}

function renderList(scope: 'mine' | 'friends' = 'mine') {
  return render(
    <MemoryRouter>
      <TripList scope={scope} />
    </MemoryRouter>,
  );
}

const DAY = 24 * 60 * 60 * 1000;
function dateOnly(offsetDays: number): string {
  return new Date(Date.now() + offsetDays * DAY).toISOString().slice(0, 10);
}

beforeEach(() => {
  vi.clearAllMocks();
  state.trips = [];
  state.tripsLoading = false;
  state.users = [];
  state.me = null;
  mockApiListTrips.mockResolvedValue([]);
});

describe('TripList', () => {
  it('loads trips on mount', () => {
    renderList();
    expect(listTrips).toHaveBeenCalled();
  });

  it('shows the empty state when there are no trips', () => {
    renderList();
    expect(screen.getByText(/No trips yet/i)).toBeInTheDocument();
  });

  it("scope='mine' shows only owned trips with a New trip action", () => {
    state.trips = [
      trip({ id: 1, name: 'MineTrip', my_role: 'owner' }),
      trip({ id: 2, name: 'TheirTrip', my_role: 'viewer' }),
      trip({ id: 3, name: 'EditTrip', my_role: 'editor' }),
    ];
    renderList('mine');
    expect(screen.getByText('Your trips')).toBeInTheDocument();
    expect(screen.getByText('MineTrip')).toBeInTheDocument();
    expect(screen.queryByText('TheirTrip')).not.toBeInTheDocument();
    expect(screen.queryByText('EditTrip')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /new trip/i })).toBeInTheDocument();
  });

  it("scope='friends' shows only shared trips and has no New trip action", () => {
    state.trips = [
      trip({ id: 1, name: 'MineTrip', my_role: 'owner' }),
      trip({ id: 2, name: 'TheirTrip', my_role: 'viewer' }),
      trip({ id: 3, name: 'EditTrip', my_role: 'editor' }),
    ];
    renderList('friends');
    expect(screen.getByText("Friends' trips")).toBeInTheDocument();
    expect(screen.getByText('TheirTrip')).toBeInTheDocument();
    expect(screen.getByText('EditTrip')).toBeInTheDocument();
    expect(screen.queryByText('MineTrip')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /new trip/i })).not.toBeInTheDocument();
  });

  it("scope='mine' includes trips the viewer is a passenger on, badged 'Passenger' (#19)", () => {
    state.trips = [
      trip({ id: 1, name: 'MineTrip', my_role: 'owner' }),
      trip({ id: 2, name: 'FlyingTrip', my_role: 'viewer', viewer_is_passenger: true }),
      trip({ id: 3, name: 'SharedTrip', my_role: 'viewer', viewer_is_passenger: false }),
    ];
    renderList('mine');
    // Owned + passenger trips both appear under My trips; a plain shared trip does not.
    expect(screen.getByText('MineTrip')).toBeInTheDocument();
    expect(screen.getByText('FlyingTrip')).toBeInTheDocument();
    expect(screen.queryByText('SharedTrip')).not.toBeInTheDocument();
    // Exactly the passenger trip carries the badge (the owned one does not).
    expect(screen.getAllByText('Passenger')).toHaveLength(1);
  });

  it("scope='friends' excludes trips the viewer is travelling on (#19)", () => {
    state.trips = [
      trip({ id: 2, name: 'FlyingTrip', my_role: 'viewer', viewer_is_passenger: true }),
      trip({ id: 3, name: 'SharedTrip', my_role: 'viewer', viewer_is_passenger: false }),
    ];
    renderList('friends');
    // Passenger trips moved to My trips; only the plain shared trip remains here.
    expect(screen.getByText('SharedTrip')).toBeInTheDocument();
    expect(screen.queryByText('FlyingTrip')).not.toBeInTheDocument();
    expect(screen.queryByText('Passenger')).not.toBeInTheDocument();
  });

  it("scope='friends' shows a tailored empty state", () => {
    renderList('friends');
    expect(screen.getByText(/No trips have been shared with you yet/i)).toBeInTheDocument();
  });

  it('shows the superuser diagnostic switches only on the Friends tab for a superuser', () => {
    state.me = user({ is_superuser: true });
    renderList('friends');
    expect(screen.getByLabelText(/All friends' trips/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/All trips/i)).toBeInTheDocument();
  });

  it('hides the diagnostic switches for non-superusers and on My trips', () => {
    state.me = user({ is_superuser: false });
    renderList('friends');
    expect(screen.queryByLabelText(/All friends' trips/i)).not.toBeInTheDocument();
    state.me = user({ is_superuser: true });
    renderList('mine');
    expect(screen.queryByLabelText(/All friends' trips/i)).not.toBeInTheDocument();
  });

  it('fetches all trips via the API when the superuser enables "All trips"', async () => {
    state.me = user({ id: 1, is_superuser: true });
    mockApiListTrips.mockResolvedValue([
      trip({ id: 50, name: 'StrangerTrip', my_role: 'viewer' }),
    ]);
    renderList('friends');
    await userEvent.click(screen.getByLabelText(/All trips/i));
    await waitFor(() => expect(mockApiListTrips).toHaveBeenCalledWith('all'));
    expect(await screen.findByText('StrangerTrip')).toBeInTheDocument();
  });

  it('renders a flag image for a trip with a country code', () => {
    state.trips = [trip({ id: 1, name: 'Beach', country_code: 'es' })];
    renderList();
    const img = document.querySelector('img[src*="flagcdn.com"]') as HTMLImageElement | null;
    expect(img).not.toBeNull();
    expect(img!.src).toContain('/es.png');
    expect(img!.srcset).toContain('h160/es.png 2x');
  });

  it('shows no flag when the country is absent or unknown', () => {
    state.trips = [
      trip({ id: 1, name: 'NoCountry', country_code: '' }),
      trip({ id: 2, name: 'Unknown', country_code: 'zz' }),
    ];
    renderList();
    expect(document.querySelector('img[src*="flagcdn.com"]')).toBeNull();
  });

  it('hides the flag image if it fails to load', () => {
    state.trips = [trip({ id: 1, name: 'Beach', country_code: 'fr' })];
    renderList();
    const img = document.querySelector('img[src*="flagcdn.com"]') as HTMLImageElement;
    img.dispatchEvent(new Event('error'));
    expect(img.style.display).toBe('none');
  });

  it('groups trips into Happening now / Upcoming / Past', () => {
    state.trips = [
      trip({ id: 1, name: 'NowTrip', starts_on: dateOnly(-2), ends_on: dateOnly(2) }),
      trip({ id: 2, name: 'FutureTrip', starts_on: dateOnly(10), ends_on: dateOnly(14) }),
      trip({ id: 3, name: 'OldTrip', starts_on: dateOnly(-20), ends_on: dateOnly(-15) }),
      trip({ id: 4, name: 'SomedayTrip' }), // date-less → upcoming
    ];
    renderList();
    expect(screen.getByText('Happening now')).toBeInTheDocument();
    expect(screen.getByText('Upcoming')).toBeInTheDocument();
    expect(screen.getByText('Past')).toBeInTheDocument();
    expect(screen.getByText('NowTrip')).toBeInTheDocument();
    expect(screen.getByText('FutureTrip')).toBeInTheDocument();
    expect(screen.getByText('SomedayTrip')).toBeInTheDocument();
    expect(screen.getByText('OldTrip')).toBeInTheDocument();
  });

  it('shows only the owner avatar on a trip shared with the viewer', () => {
    state.me = user({ id: 1, username: 'me' });
    state.users = [
      user({ id: 1, username: 'me' }),
      user({ id: 2, username: 'amy' }),
      user({ id: 3, username: 'carol' }),
    ];
    state.trips = [
      trip({
        id: 5,
        name: 'Shared',
        destination: 'Lisbon',
        my_role: 'viewer',
        members: [
          { user_id: 2, role: 'owner' },
          { user_id: 1, role: 'viewer' },
          { user_id: 3, role: 'viewer' },
        ],
      }),
    ];
    renderList('friends');
    expect(screen.getByText('Lisbon')).toBeInTheDocument();
    // The owner (amy) shows; the viewer (me) and other viewers (carol) don't.
    expect(screen.getByText('A')).toBeInTheDocument();
    expect(screen.queryByText('M')).not.toBeInTheDocument();
    expect(screen.queryByText('C')).not.toBeInTheDocument();
  });

  it('shows no avatar on the viewer\'s own trip', () => {
    state.me = user({ id: 1, username: 'me' });
    state.users = [user({ id: 1, username: 'me' }), user({ id: 2, username: 'amy' })];
    state.trips = [
      trip({
        id: 6,
        name: 'Solo',
        my_role: 'owner',
        members: [
          { user_id: 1, role: 'owner' },
          { user_id: 2, role: 'viewer' },
        ],
      }),
    ];
    renderList();
    // Owner is the viewer → no owner avatar; viewers are never shown.
    expect(screen.queryByText('M')).not.toBeInTheDocument();
    expect(screen.queryByText('A')).not.toBeInTheDocument();
  });

  it('navigates to the trip when a card is clicked', async () => {
    state.trips = [trip({ id: 7, name: 'ClickMe', starts_on: dateOnly(5) })];
    renderList();
    await userEvent.click(screen.getByText('ClickMe'));
    expect(navigate).toHaveBeenCalledWith('/trips/7');
  });

  it('creates a trip via the New trip dialog and navigates to it', async () => {
    createTrip.mockResolvedValue(trip({ id: 99, name: 'Brand New' }));
    renderList();
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    const dialog = screen.getByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText(/name/i), 'Brand New');
    await userEvent.click(within(dialog).getByRole('button', { name: /create/i }));
    expect(createTrip).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'Brand New' }),
    );
    expect(navigate).toHaveBeenCalledWith('/trips/99');
  });

  it('disables Create until a name is entered', async () => {
    renderList();
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByRole('button', { name: /create/i })).toBeDisabled();
  });

  it('shows a spinner while the first load is in flight', () => {
    state.tripsLoading = true;
    state.trips = [];
    renderList();
    expect(screen.getByRole('progressbar')).toBeInTheDocument();
    expect(screen.queryByText(/No trips yet/i)).not.toBeInTheDocument();
  });

  it('does not show the spinner once trips have loaded even if a refetch is in flight', () => {
    state.tripsLoading = true;
    state.trips = [trip({ id: 1, name: 'Already here', starts_on: dateOnly(3) })];
    renderList();
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    expect(screen.getByText('Already here')).toBeInTheDocument();
  });

  it('captures destination and dates from the New trip dialog', async () => {
    createTrip.mockResolvedValue(trip({ id: 12, name: 'Full' }));
    renderList();
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    const dialog = screen.getByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText(/name/i), 'Full');
    await userEvent.type(within(dialog).getByLabelText(/destination/i), 'Porto');
    await userEvent.type(within(dialog).getByLabelText(/starts/i), '2026-10-01');
    await userEvent.type(within(dialog).getByLabelText(/ends/i), '2026-10-05');
    await userEvent.click(within(dialog).getByRole('button', { name: /create/i }));
    expect(createTrip).toHaveBeenCalledWith({
      name: 'Full',
      destination: 'Porto',
      starts_on: '2026-10-01',
      ends_on: '2026-10-05',
    });
  });

  it('does not navigate when trip creation returns nothing', async () => {
    createTrip.mockResolvedValue(null);
    renderList();
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    const dialog = screen.getByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText(/name/i), 'Nope');
    await userEvent.click(within(dialog).getByRole('button', { name: /create/i }));
    expect(createTrip).toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });

  it('closes the New trip dialog via Cancel', async () => {
    renderList();
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /cancel/i }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('ignores a second Create click while a creation is in flight', async () => {
    // Hold the createTrip promise open so busy stays true across a 2nd click.
    let resolve!: (t: Trip) => void;
    createTrip.mockReturnValue(new Promise<Trip>((r) => (resolve = r)));
    renderList();
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    const dialog = screen.getByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText(/name/i), 'Once');
    const createBtn = within(dialog).getByRole('button', { name: /create/i });
    await userEvent.click(createBtn);
    // The button disables on busy; force a programmatic re-click to hit the
    // `busy` short-circuit in submit().
    createBtn.click();
    expect(createTrip).toHaveBeenCalledTimes(1);
    resolve(trip({ id: 1, name: 'Once' }));
  });

  it('imports a .ics: uploads, refreshes the list, and opens the new trip', async () => {
    mockImportTrip.mockResolvedValue({ trip: trip({ id: 77, name: 'Imported' }), added: 7, skipped: 0 });
    const { container } = renderList('mine');
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['BEGIN:VCALENDAR\nEND:VCALENDAR'], 'trip.ics', { type: 'text/calendar' });
    await userEvent.upload(input, file);
    expect(mockImportTrip).toHaveBeenCalledWith(file);
    expect(listTrips).toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith('/trips/77');
  });

  it('surfaces an error when the .ics import fails', async () => {
    mockImportTrip.mockRejectedValue(new Error('bad ics'));
    const { container } = renderList('mine');
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(input, new File(['x'], 'trip.ics', { type: 'text/calendar' }));
    expect(setError).toHaveBeenCalledWith('bad ics');
    expect(navigate).not.toHaveBeenCalled();
  });

  it('offers no import action on the Friends tab', () => {
    const { container } = renderList('friends');
    expect(screen.queryByRole('button', { name: /import \.ics/i })).not.toBeInTheDocument();
    expect(container.querySelector('input[type="file"]')).toBeNull();
  });
});
