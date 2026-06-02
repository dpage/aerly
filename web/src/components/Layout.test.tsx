import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

import type { User } from '../api/types';

const h = vi.hoisted(() => ({
  logout: vi.fn(),
  setPreference: vi.fn(),
  openHelp: vi.fn(),
  state: {
    me: null as User | null,
    capabilities: { email_ingest_enabled: false } as { email_ingest_enabled: boolean },
    notifications: { friend_requests_pending: 0 } as { friend_requests_pending: number },
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      me: h.state.me,
      logout: h.logout,
      capabilities: h.state.capabilities,
      notifications: h.state.notifications,
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
  h.state.me = user();
  h.state.capabilities = { email_ingest_enabled: false };
  h.state.notifications = { friend_requests_pending: 0 };
});

describe('Layout', () => {
  it('renders the chrome and routed outlet', () => {
    renderLayout();
    expect(screen.getByText('Aerly')).toBeInTheDocument();
    // The nav items are MUI Buttons rendered as router links (anchors).
    expect(screen.getByRole('link', { name: 'Trips' })).toBeInTheDocument();
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

  it('opens help to the overview on the tracker screen', async () => {
    renderLayout('/tracker');
    await userEvent.click(screen.getByRole('button', { name: 'Help' }));
    expect(h.openHelp).toHaveBeenCalledWith('overview');
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

  it('hides the friend-request badge/chip when there are none', async () => {
    renderLayout();
    await openMenu();
    expect(screen.queryByText('Friends…')?.parentElement).not.toBeNull();
    // No chip count rendered.
    expect(screen.queryByText('3')).not.toBeInTheDocument();
  });

  it('shows a pending friend-request chip when there are some', async () => {
    h.state.notifications = { friend_requests_pending: 3 };
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
