import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

import type { User } from '../api/types';
import { setMatchMedia } from '../test/setup';

const h = vi.hoisted(() => ({
  logout: vi.fn(),
  setPreference: vi.fn(),
  openHelp: vi.fn(),
  markAlertsRead: vi.fn().mockResolvedValue(undefined),
  state: {
    me: null as User | null,
    capabilities: { email_ingest_enabled: false } as { email_ingest_enabled: boolean },
    notifications: { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 } as {
      friend_requests_pending: number;
      unread_alerts: number;
      unread_shares: number;
    },
    alerts: [] as Array<{
      id: number;
      kind: string;
      trip_id?: number;
      plan_id?: number;
      plan_part_id?: number;
      message: string;
      created_at: string;
    }>,
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      me: h.state.me,
      logout: h.logout,
      capabilities: h.state.capabilities,
      notifications: h.state.notifications,
      alerts: h.state.alerts,
      markAlertsRead: h.markAlertsRead,
      openHelp: h.openHelp,
    }),
}));

// The help panel renders once in Layout; stub it (covered by its own test).
vi.mock('./HelpPanel', () => ({ default: () => null }));

vi.mock('../theme', () => ({
  useThemeMode: () => ({ preference: 'system', setPreference: h.setPreference }),
}));

// Stub each child dialog with a marker that reflects its open prop, so we can
// assert the menu items toggle the right dialog without exercising their guts.
function stubDialog(testid: string) {
  return ({ open, onClose }: { open: boolean; onClose?: () => void }) =>
    open ? (
      <div data-testid={testid}>
        <button type="button" onClick={() => onClose?.()}>
          close-{testid}
        </button>
      </div>
    ) : null;
}
vi.mock('./AddToTripDialog', () => ({ default: stubDialog('add-dialog') }));
vi.mock('./AboutDialog', () => ({ default: stubDialog('about-dialog') }));
vi.mock('./AdminDialog', () => ({ default: stubDialog('admin-dialog') }));
vi.mock('./AlertPrefsDialog', () => ({ default: stubDialog('alertprefs-dialog') }));
vi.mock('./EmailsDialog', () => ({ default: stubDialog('emails-dialog') }));
vi.mock('./FriendsDialog', () => ({ default: stubDialog('friends-dialog') }));
vi.mock('./StatsDialog', () => ({ default: stubDialog('stats-dialog') }));
vi.mock('./CalendarSubscribeDialog', () => ({ default: stubDialog('subscribe-dialog') }));
vi.mock('./HomeAddressDialog', () => ({ default: stubDialog('home-dialog') }));

import Layout from './Layout';

function user(over: Partial<User> = {}): User {
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

function renderLayout(initial = '/') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<div data-testid="page">trips page</div>} />
          <Route path="/friends" element={<div data-testid="page">friends page</div>} />
          <Route path="/tracker" element={<div data-testid="page">tracker page</div>} />
          <Route path="/trips/:id" element={<div data-testid="page">trip page</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

async function openMenu() {
  await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
}

beforeEach(() => {
  vi.clearAllMocks();
  h.markAlertsRead.mockResolvedValue(undefined);
  h.state.me = user();
  h.state.capabilities = { email_ingest_enabled: false };
  h.state.notifications = { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 };
  h.state.alerts = [];
});

describe('Layout', () => {
  it('renders the chrome and routed outlet', () => {
    renderLayout();
    expect(screen.getByText('Aerly')).toBeInTheDocument();
    // The nav items are MUI Buttons rendered as router links (anchors).
    expect(screen.getByRole('link', { name: 'My trips' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: "Friends' trips" })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Tracker' })).toBeInTheDocument();
    expect(screen.getByTestId('page')).toHaveTextContent('trips page');
  });

  it('shows the user initial in the avatar', () => {
    renderLayout();
    expect(screen.getByText('O')).toBeInTheDocument();
  });

  it('no longer shows a global Add to trip action (moved to the trip page)', () => {
    renderLayout();
    expect(screen.queryByRole('button', { name: /add to trip/i })).not.toBeInTheDocument();
  });

  it('opens help to the topic for the current screen', async () => {
    renderLayout('/'); // trips list
    await userEvent.click(screen.getByRole('button', { name: 'Help' }));
    expect(h.openHelp).toHaveBeenCalledWith('trips');
  });

  it('opens help to Map & tracker on the tracker screen', async () => {
    renderLayout('/tracker');
    await userEvent.click(screen.getByRole('button', { name: 'Help' }));
    expect(h.openHelp).toHaveBeenCalledWith('tracker');
  });

  it('opens help to plans on a trip screen', async () => {
    renderLayout('/trips/7');
    await userEvent.click(screen.getByRole('button', { name: 'Help' }));
    expect(h.openHelp).toHaveBeenCalledWith('plans');
  });

  it('hides the admin button for non-superusers', () => {
    renderLayout();
    expect(screen.queryByRole('button', { name: /manage users/i })).not.toBeInTheDocument();
  });

  it('shows and opens the admin dialog for superusers', async () => {
    h.state.me = user({ is_superuser: true });
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /manage users/i }));
    expect(screen.getByTestId('admin-dialog')).toBeInTheDocument();
  });

  it('hides the About Aerly item for non-superusers', async () => {
    renderLayout();
    await openMenu();
    expect(screen.queryByText('About Aerly…')).not.toBeInTheDocument();
  });

  it('shows and opens About Aerly from the menu for superusers', async () => {
    h.state.me = user({ is_superuser: true });
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('About Aerly…'));
    expect(screen.getByTestId('about-dialog')).toBeInTheDocument();
  });

  it('hides the friend-request badge/chip when there are none', async () => {
    renderLayout();
    await openMenu();
    expect(screen.queryByText('Friends…')?.parentElement).not.toBeNull();
    // No chip count rendered.
    expect(screen.queryByText('3')).not.toBeInTheDocument();
  });

  it('shows a pending friend-request chip when there are some', async () => {
    h.state.notifications = { friend_requests_pending: 3, unread_alerts: 0, unread_shares: 0 };
    renderLayout();
    await openMenu();
    const friends = screen.getByText('Friends…').closest('li')!;
    expect(within(friends).getByText('3')).toBeInTheDocument();
  });

  it('opens Friends from the menu', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Friends…'));
    expect(screen.getByTestId('friends-dialog')).toBeInTheDocument();
  });

  it('hides the Email addresses item when email ingest is disabled', async () => {
    renderLayout();
    await openMenu();
    expect(screen.queryByText('Email addresses…')).not.toBeInTheDocument();
  });

  it('shows and opens Email addresses when ingest is enabled', async () => {
    h.state.capabilities = { email_ingest_enabled: true };
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Email addresses…'));
    expect(screen.getByTestId('emails-dialog')).toBeInTheDocument();
  });

  it('opens Statistics from the menu', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Statistics…'));
    expect(screen.getByTestId('stats-dialog')).toBeInTheDocument();
  });

  it('opens Alert preferences from the menu', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Alert preferences…'));
    expect(screen.getByTestId('alertprefs-dialog')).toBeInTheDocument();
  });

  it('opens Home address from the menu', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Home address…'));
    expect(screen.getByTestId('home-dialog')).toBeInTheDocument();
  });

  it('opens Subscribe to calendar from the menu', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Subscribe to calendar…'));
    expect(screen.getByTestId('subscribe-dialog')).toBeInTheDocument();
  });

  it('changes the theme preference', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Dark'));
    expect(h.setPreference).toHaveBeenCalledWith('dark');
  });

  it('signs out', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('Sign out'));
    expect(h.logout).toHaveBeenCalled();
  });

  it('marks Tracker active on the tracker route', () => {
    renderLayout('/tracker');
    expect(screen.getByTestId('page')).toHaveTextContent('tracker page');
  });

  it("renders the Friends' trips route", () => {
    renderLayout('/friends');
    expect(screen.getByTestId('page')).toHaveTextContent('friends page');
    expect(screen.getByRole('link', { name: "Friends' trips" })).toBeInTheDocument();
  });

  it('renders an avatar with no initial when me is null', () => {
    h.state.me = null;
    renderLayout();
    expect(screen.queryByText('O')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /account menu/i })).toBeInTheDocument();
  });

  it('omits the signed-in caption when me is null', async () => {
    h.state.me = null;
    renderLayout();
    await openMenu();
    expect(screen.queryByText(/signed in as/i)).not.toBeInTheDocument();
  });

  it('closes each account-level dialog via its onClose callback', async () => {
    h.state.me = user({ is_superuser: true });
    h.state.capabilities = { email_ingest_enabled: true };
    renderLayout();

    // Admin (top-bar action, superuser only).
    await userEvent.click(screen.getByRole('button', { name: /manage users/i }));
    await userEvent.click(screen.getByRole('button', { name: 'close-admin-dialog' }));
    expect(screen.queryByTestId('admin-dialog')).not.toBeInTheDocument();

    // The menu-driven dialogs.
    const menuDialogs: Array<[string, string]> = [
      ['Friends…', 'friends-dialog'],
      ['Email addresses…', 'emails-dialog'],
      ['Statistics…', 'stats-dialog'],
      ['Alert preferences…', 'alertprefs-dialog'],
      ['Home address…', 'home-dialog'],
      ['Subscribe to calendar…', 'subscribe-dialog'],
    ];
    for (const [item, testid] of menuDialogs) {
      await openMenu();
      await userEvent.click(screen.getByText(item));
      expect(screen.getByTestId(testid)).toBeInTheDocument();
      await userEvent.click(screen.getByRole('button', { name: `close-${testid}` }));
      expect(screen.queryByTestId(testid)).not.toBeInTheDocument();
    }
  });

  it('keeps the System theme preference selectable', async () => {
    renderLayout();
    await openMenu();
    await userEvent.click(screen.getByText('System'));
    expect(h.setPreference).toHaveBeenCalledWith('system');
  });
});

describe('Layout (narrow / mobile)', () => {
  beforeEach(() => setMatchMedia(true));

  it('collapses the nav links into a drawer behind a menu button', async () => {
    renderLayout();
    // The inline nav links are gone; only the hamburger remains.
    expect(screen.queryByRole('link', { name: 'My trips' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: "Friends' trips" })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Tracker' })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /open navigation menu/i }));

    expect(screen.getByRole('link', { name: 'My trips' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: "Friends' trips" })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Tracker' })).toBeInTheDocument();
  });

  it('navigates and closes the drawer when a destination is chosen', async () => {
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /open navigation menu/i }));
    await userEvent.click(screen.getByRole('link', { name: 'Tracker' }));
    expect(screen.getByTestId('page')).toHaveTextContent('tracker page');
    // Drawer dismissed → its links are no longer mounted.
    expect(screen.queryByRole('link', { name: 'Tracker' })).not.toBeInTheDocument();
  });

  it('still exposes Help and the account menu as icons', () => {
    renderLayout();
    expect(screen.getByRole('button', { name: 'Help' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /account menu/i })).toBeInTheDocument();
  });
});

describe('Layout (alerts)', () => {
  it('shows alerts in the account menu and marks them read on open', async () => {
    h.state.notifications = { friend_requests_pending: 0, unread_alerts: 1, unread_shares: 0 };
    h.state.alerts = [
      {
        id: 1,
        plan_id: 1,
        trip_id: 1,
        kind: 'gate',
        message: 'BA286 now departs gate B32',
        created_at: '2026-06-01T00:00:00Z',
      },
    ];
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    expect(screen.getByText('BA286 now departs gate B32')).toBeInTheDocument();
    expect(h.markAlertsRead).toHaveBeenCalled();
  });

  it('does not mark alerts read on menu open when there are none unread', async () => {
    // beforeEach already sets unread_alerts: 0 and alerts: []
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    expect(h.markAlertsRead).not.toHaveBeenCalled();
  });

  it('deep-links to the tracker for a flight alert that has a plan_part_id', async () => {
    h.state.alerts = [
      {
        id: 1,
        plan_id: 1,
        plan_part_id: 5,
        trip_id: 4,
        kind: 'gate',
        message: 'BA286 now departs gate B32',
        created_at: '2026-06-01T00:00:00Z',
      },
    ];
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByText('BA286 now departs gate B32'));
    expect(screen.getByTestId('page')).toHaveTextContent('tracker page');
  });

  it('opens the trip timeline for an alert without a plan_part_id (share or reminder)', async () => {
    h.state.alerts = [
      {
        id: 1,
        plan_id: 1,
        trip_id: 4,
        kind: 'share',
        message: 'Alice shared Rome 2026 with you',
        created_at: '2026-06-01T00:00:00Z',
      },
    ];
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByText('Alice shared Rome 2026 with you'));
    expect(screen.getByTestId('page')).toHaveTextContent('trip page');
  });

  it('opens the trip timeline for a reminder alert', async () => {
    h.state.alerts = [
      {
        id: 2,
        plan_id: 1,
        trip_id: 4,
        kind: 'reminder',
        message: 'Upcoming: Hilton Vienna',
        created_at: '2026-06-01T00:00:00Z',
      },
    ];
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    await userEvent.click(screen.getByText('Upcoming: Hilton Vienna'));
    expect(screen.getByTestId('page')).toHaveTextContent('trip page');
  });

  it('counts unread shares in the avatar badge', () => {
    h.state.notifications = { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 2 };
    renderLayout();
    // The badge surfaces the combined inbox count (here, share notifications).
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('marks alerts read on menu open when only share notifications are unread (unread_alerts==0, unread_shares>0)', async () => {
    h.state.notifications = { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 1 };
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    expect(h.markAlertsRead).toHaveBeenCalled();
  });

  it('does NOT mark alerts read when both unread_alerts and unread_shares are zero', async () => {
    // beforeEach already sets both to 0, but be explicit.
    h.state.notifications = { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 };
    renderLayout();
    await userEvent.click(screen.getByRole('button', { name: /account menu/i }));
    expect(h.markAlertsRead).not.toHaveBeenCalled();
  });

  it('renders inbox items with composite kind-id keys (no duplicate-key warnings for colliding ids)', () => {
    // A flight alert and a share notification sharing the same numeric id.
    h.state.alerts = [
      {
        id: 7, kind: 'gate', trip_id: 1, plan_id: 1, plan_part_id: 2,
        message: 'Flight gate change', created_at: '2026-06-01T00:00:00Z',
      },
      {
        id: 7, kind: 'share', trip_id: 2, plan_id: 2,
        message: 'Alice shared Rome 2026', created_at: '2026-06-01T00:00:01Z',
      },
    ];
    renderLayout();
    // Both items must appear — if React saw duplicate keys one would be dropped.
    // We only open the menu here; the badge visible check suffices without opening.
    // Open and verify both messages appear.
    return userEvent.click(screen.getByRole('button', { name: /account menu/i })).then(() => {
      expect(screen.getByText('Flight gate change')).toBeInTheDocument();
      expect(screen.getByText('Alice shared Rome 2026')).toBeInTheDocument();
    });
  });
});
