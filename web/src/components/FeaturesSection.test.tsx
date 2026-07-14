import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';

const h = vi.hoisted(() => ({
  setHiddenFeatures: vi.fn(),
  setError: vi.fn(),
  state: {
    me: null as User | null,
    capabilities: { explore_enabled: true } as { explore_enabled?: boolean },
  },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      me: h.state.me,
      capabilities: h.state.capabilities,
      setHiddenFeatures: h.setHiddenFeatures,
      setError: h.setError,
    }),
}));

import FeaturesSection from './FeaturesSection';

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

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = user();
  h.state.capabilities = { explore_enabled: true };
  h.setHiddenFeatures.mockResolvedValue(undefined);
});

describe('FeaturesSection', () => {
  it('defaults both boxes to unchecked (everything shown)', () => {
    render(<FeaturesSection />);
    expect(screen.getByRole('checkbox', { name: /hide explore/i })).not.toBeChecked();
    expect(screen.getByRole('checkbox', { name: /hide maps/i })).not.toBeChecked();
  });

  it('reflects stored hide preferences', () => {
    h.state.me = user({ hide_explore: true, hide_maps: true });
    render(<FeaturesSection />);
    expect(screen.getByRole('checkbox', { name: /hide explore/i })).toBeChecked();
    expect(screen.getByRole('checkbox', { name: /hide maps/i })).toBeChecked();
  });

  it('drops the "Hide Explore" toggle when Explore is unavailable server-side', () => {
    h.state.capabilities = { explore_enabled: false };
    render(<FeaturesSection />);
    // Nothing to hide when Explore isn't available at all.
    expect(screen.queryByRole('checkbox', { name: /hide explore/i })).not.toBeInTheDocument();
    // The maps toggle is unaffected.
    expect(screen.getByRole('checkbox', { name: /hide maps/i })).toBeInTheDocument();
  });

  it('saves hide_explore when the Explore box is ticked', async () => {
    render(<FeaturesSection />);
    await userEvent.click(screen.getByRole('checkbox', { name: /hide explore/i }));
    expect(h.setHiddenFeatures).toHaveBeenCalledWith({ hide_explore: true });
  });

  it('saves hide_maps when the maps box is ticked', async () => {
    render(<FeaturesSection />);
    await userEvent.click(screen.getByRole('checkbox', { name: /hide maps/i }));
    expect(h.setHiddenFeatures).toHaveBeenCalledWith({ hide_maps: true });
  });

  it('surfaces a save failure via setError', async () => {
    h.setHiddenFeatures.mockRejectedValueOnce(new Error('boom'));
    render(<FeaturesSection />);
    await userEvent.click(screen.getByRole('checkbox', { name: /hide explore/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('boom'));
  });
});
