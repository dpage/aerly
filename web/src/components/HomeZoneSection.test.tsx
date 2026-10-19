import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';

const h = vi.hoisted(() => ({
  setHomeTz: vi.fn(),
  setError: vi.fn(),
  state: { me: null as User | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ me: h.state.me, setHomeTz: h.setHomeTz, setError: h.setError }),
}));

import HomeZoneSection from './HomeZoneSection';

function user(over: Partial<User> = {}): User {
  return {
    id: 1, username: 'octocat', name: 'Octo Cat', avatar_url: '',
    is_superuser: false, is_active: true, has_logged_in: true, home_address: '', ...over,
  };
}

const device = Intl.DateTimeFormat().resolvedOptions().timeZone;

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = user();
  h.setHomeTz.mockResolvedValue(undefined);
});

describe('HomeZoneSection', () => {
  it('shows the heading, helper text and current zone', () => {
    h.state.me = user({ home_tz: 'Asia/Tashkent' });
    render(<HomeZoneSection />);
    expect(screen.getByRole('heading', { name: 'Home time zone' })).toBeInTheDocument();
    expect(screen.getByText(/Leave it blank to use this device's zone/)).toBeInTheDocument();
    expect(screen.getByRole('combobox')).toHaveValue('Asia/Tashkent');
    expect(screen.queryByRole('button', { name: /Use this device's zone/ })).toBeNull();
  });

  it('saves a picked zone', async () => {
    render(<HomeZoneSection />);
    await userEvent.type(screen.getByRole('combobox'), 'Asia/Tashkent');
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(h.setHomeTz).toHaveBeenCalledWith('Asia/Tashkent'));
    expect(h.setHomeTz).not.toHaveBeenCalledWith('A');
  });

  it('saves an empty string when cleared', async () => {
    h.state.me = user({ home_tz: 'Asia/Tashkent' });
    render(<HomeZoneSection />);
    await userEvent.clear(screen.getByRole('combobox'));
    await waitFor(() => expect(h.setHomeTz).toHaveBeenCalledWith(''));
  });

  it('saves the device zone from the button', async () => {
    render(<HomeZoneSection />);
    await userEvent.click(screen.getByRole('button', { name: `Use this device's zone (${device})` }));
    await waitFor(() => expect(h.setHomeTz).toHaveBeenCalledWith(device));
  });

  it('surfaces a save error', async () => {
    h.setHomeTz.mockRejectedValue(new Error('save boom'));
    render(<HomeZoneSection />);
    await userEvent.click(screen.getByRole('button', { name: /Use this device's zone/ }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
  });
});
