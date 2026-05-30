import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
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

const state = {
  trips: [] as Trip[],
  tripsLoading: false,
  users: [] as User[],
  me: null as User | null,
  listTrips,
  createTrip,
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

import TripList from './TripList';

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

function renderList() {
  return render(
    <MemoryRouter>
      <TripList />
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

  it('renders destination and shared-member avatars (excluding the viewer)', () => {
    state.me = user({ id: 1, username: 'me' });
    state.users = [user({ id: 1, username: 'me' }), user({ id: 2, username: 'amy' })];
    state.trips = [
      trip({
        id: 5,
        name: 'Shared',
        destination: 'Lisbon',
        members: [
          { user_id: 1, role: 'owner' },
          { user_id: 2, role: 'viewer' },
        ],
      }),
    ];
    renderList();
    expect(screen.getByText('Lisbon')).toBeInTheDocument();
    // amy's avatar fallback initial; the viewer (me) is excluded.
    expect(screen.getByText('A')).toBeInTheDocument();
    expect(screen.queryByText('M')).not.toBeInTheDocument();
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
});
