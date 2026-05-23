import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

const h = vi.hoisted(() => ({
  api: { getDevAuthBypassEnabled: vi.fn() },
}));

vi.mock('../api/client', () => ({ api: h.api }));

import Login from './Login';

describe('Login', () => {
  beforeEach(() => {
    h.api.getDevAuthBypassEnabled.mockReset();
  });

  it('renders the heading and a GitHub sign-in link', async () => {
    h.api.getDevAuthBypassEnabled.mockResolvedValue(false);
    render(<Login />);
    expect(screen.getByRole('heading', { name: 'Aerly' })).toBeInTheDocument();
    const link = screen.getByRole('link', { name: /sign in with github/i });
    expect(link).toHaveAttribute('href', '/auth/github/login');
    // Wait for the probe promise to settle so the effect cleanup doesn't
    // run a setState on an unmounted component (we assert below).
    await waitFor(() => expect(h.api.getDevAuthBypassEnabled).toHaveBeenCalled());
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
    const input = screen.getByLabelText(/dev login github handle/i);
    expect(input).toHaveAttribute('name', 'login');
  });
});
