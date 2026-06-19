import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
  friendsShowAllFriends: false,
  friendsShowAllTrips: false,
  setFriendsShowAllFriends: vi.fn(),
  setFriendsShowAllTrips: vi.fn(),
  listTrips,
  createTrip,
  setError,
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

vi.mock('../api/client', () => ({ api: { listTrips: vi.fn(), importTrip: vi.fn() } }));

const pwa = vi.hoisted(() => ({ online: true }));
vi.mock('../pwa', () => ({ useOnlineStatus: () => pwa.online }));

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
  state.friendsShowAllFriends = false;
  state.friendsShowAllTrips = false;
  pwa.online = true;
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

  it('disables New trip and Import while offline', () => {
    pwa.online = false;
    state.trips = [trip({ id: 1, name: 'MineTrip', my_role: 'owner' })];
    renderList('mine');
    expect(screen.getByRole('button', { name: /new trip/i })).toBeDisabled();
    // When disabled the Tooltip title becomes the button's accessible name.
    expect(screen.getByRole('button', { name: /import trips/i })).toBeDisabled();
  });

  it('does not import a file while offline even if the picker fires', () => {
    pwa.online = false;
    renderList('mine');
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(['x'], 'trip.ics')] } });
    expect(mockImportTrip).not.toHaveBeenCalled();
  });

  it('disables Create if the connection drops while the New trip dialog is open', async () => {
    const view = renderList('mine');
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    await userEvent.type(screen.getByLabelText(/name/i), 'Trip X');
    expect(screen.getByRole('button', { name: /^create$/i })).toBeEnabled();
    // Connection drops with the dialog still open → re-render reflects offline.
    pwa.online = false;
    view.rerender(
      <MemoryRouter>
        <TripList scope="mine" />
      </MemoryRouter>,
    );
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled();
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

  it('fetches all trips via the API when "All trips" is enabled', async () => {
    state.me = user({ id: 1, is_superuser: true });
    // The toggles persist in the store (so they survive opening a trip and
    // tapping Back); seed the persisted flag as if it were already on.
    state.friendsShowAllTrips = true;
    mockApiListTrips.mockResolvedValue([trip({ id: 50, name: 'StrangerTrip', my_role: 'viewer' })]);
    renderList('friends');
    await waitFor(() => expect(mockApiListTrips).toHaveBeenCalledWith('all'));
    expect(await screen.findByText('StrangerTrip')).toBeInTheDocument();
  });

  it('persists the diagnostic toggles to the store so they survive navigation', async () => {
    state.me = user({ id: 1, is_superuser: true });
    renderList('friends');
    await userEvent.click(screen.getByLabelText(/All trips/i));
    expect(state.setFriendsShowAllTrips).toHaveBeenCalledWith(true);
    await userEvent.click(screen.getByLabelText(/All friends' trips/i));
    expect(state.setFriendsShowAllFriends).toHaveBeenCalledWith(true);
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

  it("shows no avatar on the viewer's own trip", () => {
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
    // The originating list path rides along in the navigation state so the
    // trip's Back button can return to it (home vs Friends' trips).
    expect(navigate).toHaveBeenCalledWith('/trips/7', { state: { from: '/' } });
  });

  it('creates a trip via the New trip dialog and navigates to it', async () => {
    createTrip.mockResolvedValue(trip({ id: 99, name: 'Brand New' }));
    renderList();
    await userEvent.click(screen.getByRole('button', { name: /new trip/i }));
    const dialog = screen.getByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText(/name/i), 'Brand New');
    await userEvent.click(within(dialog).getByRole('button', { name: /create/i }));
    expect(createTrip).toHaveBeenCalledWith(expect.objectContaining({ name: 'Brand New' }));
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
    await userEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: /cancel/i }),
    );
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
    const imported = trip({ id: 77, name: 'Imported' });
    mockImportTrip.mockResolvedValue({
      trip: imported,
      trips: [imported],
      added: 7,
      skipped: 0,
    });
    const { container } = renderList('mine');
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['BEGIN:VCALENDAR\nEND:VCALENDAR'], 'trip.ics', {
      type: 'text/calendar',
    });
    await userEvent.upload(input, file);
    expect(mockImportTrip).toHaveBeenCalledWith(file);
    expect(listTrips).toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith('/trips/77');
  });

  it('imports a Kayak feed: stays on the refreshed list for multiple trips', async () => {
    mockImportTrip.mockResolvedValue({
      trip: trip({ id: 1, name: 'A' }),
      trips: [trip({ id: 1, name: 'A' }), trip({ id: 2, name: 'B' })],
      added: 9,
      skipped: 0,
    });
    const { container } = renderList('mine');
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['BEGIN:VCALENDAR\nEND:VCALENDAR'], 'kayak.ics', {
      type: 'text/calendar',
    });
    await userEvent.upload(input, file);
    expect(mockImportTrip).toHaveBeenCalledWith(file);
    expect(listTrips).toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
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

  // ── Filter functionality tests ──────────────────────────────────────────

  it('shows a filter bar when trips are present', () => {
    state.trips = [trip({ id: 1, name: 'Test Trip' })];
    renderList();
    expect(screen.getByPlaceholderText(/Filter trips/i)).toBeInTheDocument();
  });

  it('hides the filter bar when no trips are present', () => {
    state.trips = [];
    renderList();
    expect(screen.queryByPlaceholderText(/Filter trips/i)).not.toBeInTheDocument();
  });

  it('filters trips by name', async () => {
    state.trips = [
      trip({ id: 1, name: 'Stockholm Adventure', starts_on: dateOnly(-10), ends_on: dateOnly(-5) }),
      trip({ id: 2, name: 'Paris Weekend', starts_on: dateOnly(-20), ends_on: dateOnly(-18) }),
      trip({ id: 3, name: 'Tokyo Business', starts_on: dateOnly(-30), ends_on: dateOnly(-28) }),
    ];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    await userEvent.type(filterInput, 'Stock');
    
    expect(screen.getByText('Stockholm Adventure')).toBeInTheDocument();
    expect(screen.queryByText('Paris Weekend')).not.toBeInTheDocument();
    expect(screen.queryByText('Tokyo Business')).not.toBeInTheDocument();
    expect(screen.getByText('1 trip matched')).toBeInTheDocument();
  });

  it('filters trips by destination', async () => {
    state.trips = [
      trip({ id: 1, name: 'Trip A', destination: 'Stockholm, Sweden', starts_on: dateOnly(-10) }),
      trip({ id: 2, name: 'Trip B', destination: 'Paris, France', starts_on: dateOnly(-20) }),
    ];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    await userEvent.type(filterInput, 'Sweden');
    
    expect(screen.getByText('Trip A')).toBeInTheDocument();
    expect(screen.queryByText('Trip B')).not.toBeInTheDocument();
  });

  it('filters trips by tags', async () => {
    state.trips = [
      trip({ id: 1, name: 'Trip A', tags: ['ARN', 'Nordic'], starts_on: dateOnly(-10) }),
      trip({ id: 2, name: 'Trip B', tags: ['CDG', 'Europe'], starts_on: dateOnly(-20) }),
    ];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    await userEvent.type(filterInput, 'ARN');
    
    expect(screen.getByText('Trip A')).toBeInTheDocument();
    expect(screen.queryByText('Trip B')).not.toBeInTheDocument();
  });

  it('shows clear button when filter is active', async () => {
    state.trips = [trip({ id: 1, name: 'Test Trip', starts_on: dateOnly(-10) })];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    
    expect(screen.queryByLabelText(/clear filter/i)).not.toBeInTheDocument();
    
    await userEvent.type(filterInput, 'Test');
    expect(screen.getByLabelText(/clear filter/i)).toBeInTheDocument();
  });

  it('clears filter when clear button is clicked', async () => {
    state.trips = [
      trip({ id: 1, name: 'Stockholm Trip', starts_on: dateOnly(-10) }),
      trip({ id: 2, name: 'Paris Trip', starts_on: dateOnly(-20) }),
    ];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    
    await userEvent.type(filterInput, 'Stockholm');
    expect(screen.queryByText('Paris Trip')).not.toBeInTheDocument();
    
    await userEvent.click(screen.getByLabelText(/clear filter/i));
    expect(filterInput).toHaveValue('');
    expect(screen.getByText('Paris Trip')).toBeInTheDocument();
  });

  it('clears filter when Escape is pressed', async () => {
    state.trips = [trip({ id: 1, name: 'Test Trip', starts_on: dateOnly(-10) })];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    
    await userEvent.type(filterInput, 'Test');
    expect(filterInput).toHaveValue('Test');
    
    await userEvent.type(filterInput, '{Escape}');
    expect(filterInput).toHaveValue('');
  });

  it('orders filtered results chronologically rather than by store order', async () => {
    // Store order is arbitrary (updated_at DESC); the filtered flat list should
    // still come out most-recent-first within Past, like the grouped view.
    state.trips = [
      trip({ id: 1, name: 'Bel 2010', starts_on: '2010-02-05', ends_on: '2010-02-07' }),
      trip({ id: 2, name: 'Bel 2013', starts_on: '2013-01-31', ends_on: '2013-02-03' }),
      trip({ id: 3, name: 'Bel 2008', starts_on: '2008-02-22', ends_on: '2008-02-24' }),
    ];
    renderList();
    await userEvent.type(screen.getByPlaceholderText(/Filter trips/i), 'Bel');

    const cards = screen.getAllByText(/^Bel \d{4}$/).map((el) => el.textContent);
    expect(cards).toEqual(['Bel 2013', 'Bel 2010', 'Bel 2008']);
  });

  it('shows "No matching trips" when filter yields no results', async () => {
    state.trips = [trip({ id: 1, name: 'Stockholm Trip', starts_on: dateOnly(-10) })];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    
    await userEvent.type(filterInput, 'Nonexistent');
    expect(screen.getByText('No matching trips')).toBeInTheDocument();
    expect(screen.queryByText('Stockholm Trip')).not.toBeInTheDocument();
  });

  it('restores year folding when filter is cleared', async () => {
    state.trips = [
      trip({ id: 1, name: 'Trip 2023', starts_on: '2023-06-15', ends_on: '2023-06-20' }),
      trip({ id: 2, name: 'Trip 2024', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
    ];
    renderList();
    
    // Initially should show year sections
    expect(screen.getByText('2024')).toBeInTheDocument();
    expect(screen.getByText('2023')).toBeInTheDocument();
    
    // Filter hides year sections
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);
    await userEvent.type(filterInput, '2023');
    expect(screen.queryByText('2024')).not.toBeInTheDocument(); // Year header gone
    expect(screen.getByText('Trip 2023')).toBeInTheDocument();
    
    // Clear filter restores year sections
    await userEvent.clear(filterInput);
    expect(screen.getByText('2024')).toBeInTheDocument();
    expect(screen.getByText('2023')).toBeInTheDocument();
  });

  // ── Year-based folding tests ──────────────────────────────────────────

  it('groups past trips by year, most recent first', () => {
    state.trips = [
      trip({ id: 1, name: 'Trip A', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'Trip B', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
      trip({ id: 3, name: 'Trip C', starts_on: '2024-01-05', ends_on: '2024-01-10' }),
    ];
    renderList();
    
    expect(screen.getByText('2024')).toBeInTheDocument();
    expect(screen.getByText('2023')).toBeInTheDocument();
    
    // Should show trip counts per year
    expect(screen.getByText('2')).toBeInTheDocument(); // 2024 has 2 trips
    expect(screen.getByText('1')).toBeInTheDocument(); // 2023 has 1 trip
  });

  it('expands the most recent year by default, collapses others', async () => {
    state.trips = [
      trip({ id: 1, name: 'Recent Trip', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'Old Trip', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
    ];
    renderList();
    
    // Most recent year (2024) should be expanded - trips visible
    expect(screen.getByText('Recent Trip')).toBeInTheDocument();
    
    // Older year (2023) should be collapsed - trip not visible
    expect(screen.queryByText('Old Trip')).not.toBeInTheDocument();
    
    // But year header should still be there
    expect(screen.getByText('2023')).toBeInTheDocument();
  });

  it('toggles year section when year header is clicked', async () => {
    state.trips = [
      trip({ id: 1, name: 'Recent Trip', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'Old Trip', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
    ];
    renderList();
    
    // Initially, 2023 section should be collapsed (old trip not visible)
    expect(screen.queryByText('Old Trip')).not.toBeInTheDocument();
    
    // Find and click the 2023 year button to expand
    const yearButton = screen.getByText('2023').closest('[role="button"]') as HTMLElement;
    await userEvent.click(yearButton);
    
    // Wait for the expand animation
    await waitFor(() => {
      expect(screen.getByText('Old Trip')).toBeInTheDocument();
    });
    
    // Click again to collapse
    await userEvent.click(yearButton);
    
    // Wait for the collapse animation
    await waitFor(() => {
      expect(screen.queryByText('Old Trip')).not.toBeInTheDocument();
    });
  });

  it('toggles year section via keyboard (Enter/Space) for accessibility', async () => {
    state.trips = [
      trip({ id: 1, name: 'Recent Trip', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'Old Trip', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
    ];
    renderList();

    // Initially, 2023 section is collapsed (old trip not visible)
    expect(screen.queryByText('Old Trip')).not.toBeInTheDocument();

    const yearButton = screen.getByText('2023').closest('[role="button"]') as HTMLElement;
    expect(yearButton).toHaveAttribute('tabindex', '0');

    // Enter expands
    yearButton.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => {
      expect(screen.getByText('Old Trip')).toBeInTheDocument();
    });

    // Space collapses
    await userEvent.keyboard(' ');
    await waitFor(() => {
      expect(screen.queryByText('Old Trip')).not.toBeInTheDocument();
    });
  });

  it('shows expand/collapse all button when multiple years exist', () => {
    state.trips = [
      trip({ id: 1, name: 'Trip A', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'Trip B', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
    ];
    renderList();
    
    expect(screen.getByLabelText(/Expand all years|Collapse all years/)).toBeInTheDocument();
  });

  it('hides expand/collapse all button when only one year exists', () => {
    state.trips = [
      trip({ id: 1, name: 'Trip A', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
    ];
    renderList();
    
    expect(screen.queryByLabelText(/Expand all years|Collapse all years/)).not.toBeInTheDocument();
  });

  it('expands all years when expand all is clicked', async () => {
    state.trips = [
      trip({ id: 1, name: 'Recent Trip', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'Old Trip', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
    ];
    renderList();
    
    // Initially old trip is collapsed
    expect(screen.queryByText('Old Trip')).not.toBeInTheDocument();
    
    // Click expand all
    await userEvent.click(screen.getByLabelText(/Expand all years/));
    
    // Now both trips should be visible
    expect(screen.getByText('Recent Trip')).toBeInTheDocument();
    expect(screen.getByText('Old Trip')).toBeInTheDocument();
  });

  it('collapses all years when collapse all is clicked', async () => {
    state.trips = [
      trip({ id: 1, name: 'Recent Trip', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'Old Trip', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
    ];
    renderList();
    
    // First expand all so we can then collapse all
    const expandAllButton = screen.getByLabelText(/Expand all years/);
    await userEvent.click(expandAllButton);
    
    await waitFor(() => {
      expect(screen.getByText('Recent Trip')).toBeInTheDocument();
      expect(screen.getByText('Old Trip')).toBeInTheDocument();
    });
    
    // Now collapse all - the button text should change to "Collapse all years"
    const collapseAllButton = screen.getByLabelText(/Collapse all years/);
    await userEvent.click(collapseAllButton);
    
    // Both trips should be hidden
    await waitFor(() => {
      expect(screen.queryByText('Recent Trip')).not.toBeInTheDocument();
      expect(screen.queryByText('Old Trip')).not.toBeInTheDocument();
    });
  });

  // ── Helper function tests ──────────────────────────────────────────────

  it('tripMatchesFilter: filters by name, destination, dates, and tags via the rendered UI', async () => {
    state.trips = [
      trip({ id: 1, name: 'Stockholm Adventure', destination: 'Stockholm, Sweden', starts_on: '2024-06-15', ends_on: '2024-06-20', tags: ['ARN', 'Nordic'] }),
      trip({ id: 2, name: 'Paris Weekend', destination: 'Paris, France', starts_on: '2024-07-01', ends_on: '2024-07-03', tags: ['CDG'] }),
    ];
    renderList();
    const filterInput = screen.getByPlaceholderText(/Filter trips/i);

    // matches name
    await userEvent.type(filterInput, 'Stock');
    expect(screen.getByText('Stockholm Adventure')).toBeInTheDocument();
    expect(screen.queryByText('Paris Weekend')).not.toBeInTheDocument();

    // matches destination
    await userEvent.clear(filterInput);
    await userEvent.type(filterInput, 'France');
    expect(screen.getByText('Paris Weekend')).toBeInTheDocument();
    expect(screen.queryByText('Stockholm Adventure')).not.toBeInTheDocument();

    // matches date
    await userEvent.clear(filterInput);
    await userEvent.type(filterInput, '2024-06-15');
    expect(screen.getByText('Stockholm Adventure')).toBeInTheDocument();
    expect(screen.queryByText('Paris Weekend')).not.toBeInTheDocument();

    // matches tag
    await userEvent.clear(filterInput);
    await userEvent.type(filterInput, 'ARN');
    expect(screen.getByText('Stockholm Adventure')).toBeInTheDocument();
    expect(screen.queryByText('Paris Weekend')).not.toBeInTheDocument();

    // no match
    await userEvent.clear(filterInput);
    await userEvent.type(filterInput, 'Nonexistent');
    expect(screen.getByText('No matching trips')).toBeInTheDocument();
  });

  it('groupPastByYear: groups trips by year with correct counts via the rendered UI', () => {
    state.trips = [
      trip({ id: 1, name: 'A', starts_on: '2024-06-15', ends_on: '2024-06-20' }),
      trip({ id: 2, name: 'B', starts_on: '2024-01-05', ends_on: '2024-01-10' }),
      trip({ id: 3, name: 'C', starts_on: '2023-03-10', ends_on: '2023-03-15' }),
      trip({ id: 4, name: 'D', starts_on: '2022-12-25', ends_on: '2022-12-30' }),
    ];
    renderList();

    // Three year headers present, most recent first
    const yearHeaders = ['2024', '2023', '2022'];
    for (const y of yearHeaders) expect(screen.getByText(y)).toBeInTheDocument();

    // 2024 is expanded by default and shows 2 trips; others are collapsed
    expect(screen.getByText('A')).toBeInTheDocument();
    expect(screen.getByText('B')).toBeInTheDocument();
    expect(screen.queryByText('C')).not.toBeInTheDocument();
    expect(screen.queryByText('D')).not.toBeInTheDocument();
  });
});
