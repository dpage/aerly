import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

const h = vi.hoisted(() => ({
  api: {
    getAuthProviders: vi.fn(),
    getDevAuthBypassEnabled: vi.fn(),
  },
}));

vi.mock('../api/client', () => ({ api: h.api }));

import Login from './Login';

describe('Login', () => {
  beforeEach(() => {
    h.api.getAuthProviders.mockReset();
    h.api.getDevAuthBypassEnabled.mockReset();
    h.api.getAuthProviders.mockResolvedValue([
      { name: 'github', label: 'GitHub' },
    ]);
  });

  it('renders the heading and a GitHub sign-in link', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    expect(screen.getByRole('heading', { name: 'Aerly' })).toBeInTheDocument();
    const link = await screen.findByRole('link', { name: /sign in with github/i });
    expect(link).toHaveAttribute('href', '/auth/github/login');
    await waitFor(() => expect(h.api.getDevAuthBypassEnabled).toHaveBeenCalled());
  });

  it('renders one button per configured provider', async () => {
    h.api.getAuthProviders.mockResolvedValue([
      { name: 'github', label: 'GitHub' },
      { name: 'google', label: 'Google' },
    ]);
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    const gh = await screen.findByRole('link', { name: /sign in with github/i });
    const goog = await screen.findByRole('link', { name: /sign in with google/i });
    expect(gh).toHaveAttribute('href', '/auth/github/login');
    expect(goog).toHaveAttribute('href', '/auth/google/login');
  });

  it('does not render the dev-login form when DEV_AUTH_BYPASS is off', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    await waitFor(() => expect(h.api.getDevAuthBypassEnabled).toHaveBeenCalled());
    expect(
      screen.queryByRole('button', { name: /sign in as dev user/i }),
    ).not.toBeInTheDocument();
  });

  it('renders the dev-login form when DEV_AUTH_BYPASS is enabled', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(true);
    render(<Login />);
    const submit = await screen.findByRole('button', {
      name: /sign in as dev user/i,
    });
    // Submit is inside a plain GET form pointed at /auth/dev-login. We
    // verify the form action/method and the input name so the browser
    // navigates to /auth/dev-login?login=<value>.
    const form = submit.closest('form');
    expect(form).not.toBeNull();
    expect(form).toHaveAttribute('action', '/auth/dev-login');
    expect(form?.getAttribute('method')?.toLowerCase()).toBe('get');
    const input = screen.getByLabelText(/dev login username/i);
    expect(input).toHaveAttribute('name', 'login');
  });

  it('renders links to Privacy Policy and Terms of Service', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    await waitFor(() => expect(h.api.getDevAuthBypassEnabled).toHaveBeenCalled());
    const privacyLink = screen.getByRole('link', { name: /privacy policy/i });
    const termsLink = screen.getByRole('link', { name: /terms of service/i });
    expect(privacyLink).toHaveAttribute('href', '/privacy');
    expect(termsLink).toHaveAttribute('href', '/terms');
  });

  it('falls back to the generic LoginIcon for an unknown provider', async () => {
    h.api.getAuthProviders.mockResolvedValue([{ name: 'okta', label: 'Okta' }]);
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    // The button exists and points at the provider — what we're proving here
    // is just that the iconFor default branch doesn't blow up.
    const link = await screen.findByRole('link', { name: /sign in with okta/i });
    expect(link).toHaveAttribute('href', '/auth/okta/login');
  });

  it('stashes ?friend_accept=<token> in sessionStorage when a provider link is clicked', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    window.history.pushState({}, '', '/?friend_accept=tok-LOGIN');
    window.sessionStorage.removeItem('aerly.pending_friend_accept');
    render(<Login />);
    const link = await screen.findByRole('link', { name: /sign in with github/i });
    // Prevent the actual navigation so jsdom doesn't reload.
    link.addEventListener('click', (e) => e.preventDefault(), { once: true });
    await userEvent.click(link);
    expect(window.sessionStorage.getItem('aerly.pending_friend_accept')).toBe('tok-LOGIN');
    // Cleanup so other tests don't see the stash.
    window.sessionStorage.removeItem('aerly.pending_friend_accept');
    window.history.pushState({}, '', '/');
  });

  it('does not stash when no friend_accept token is in the URL', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    window.history.pushState({}, '', '/');
    window.sessionStorage.removeItem('aerly.pending_friend_accept');
    render(<Login />);
    const link = await screen.findByRole('link', { name: /sign in with github/i });
    link.addEventListener('click', (e) => e.preventDefault(), { once: true });
    await userEvent.click(link);
    expect(window.sessionStorage.getItem('aerly.pending_friend_accept')).toBeNull();
  });

  it('silently drops the stash when sessionStorage throws', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    window.history.pushState({}, '', '/?friend_accept=tok-X');
    const originalStorage = window.sessionStorage;
    Object.defineProperty(window, 'sessionStorage', {
      configurable: true,
      value: {
        getItem: () => null,
        setItem: () => {
          throw new Error('blocked');
        },
        removeItem: () => {},
        clear: () => {},
        key: () => null,
        length: 0,
      },
    });
    try {
      render(<Login />);
      const link = await screen.findByRole('link', { name: /sign in with github/i });
      link.addEventListener('click', (e) => e.preventDefault(), { once: true });
      // Should not throw despite sessionStorage.setItem rejecting.
      await userEvent.click(link);
    } finally {
      Object.defineProperty(window, 'sessionStorage', {
        configurable: true,
        value: originalStorage,
      });
      window.history.pushState({}, '', '/');
    }
  });

  it('shows a loading placeholder until /auth/providers resolves', async () => {
    // Hold the providers fetch open so the first paint reflects loading.
    let resolveProviders!: (v: { name: string; label: string }[]) => void;
    h.api.getAuthProviders.mockReturnValue(
      new Promise((res) => {
        resolveProviders = res;
      }),
    );
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    // Loading placeholder is present, but no provider links yet.
    expect(
      screen.getByRole('button', { name: /loading sign-in options/i }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole('link', { name: /sign in with/i }),
    ).not.toBeInTheDocument();
    // Resolve and confirm the placeholder is replaced by the real buttons.
    resolveProviders([{ name: 'github', label: 'GitHub' }]);
    const link = await screen.findByRole('link', { name: /sign in with github/i });
    expect(link).toHaveAttribute('href', '/auth/github/login');
    expect(
      screen.queryByRole('button', { name: /loading sign-in options/i }),
    ).not.toBeInTheDocument();
  });
});
