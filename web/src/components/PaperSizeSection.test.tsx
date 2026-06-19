import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { User } from '../api/types';

const h = vi.hoisted(() => ({
  setPaperSize: vi.fn(),
  setError: vi.fn(),
  state: { me: null as User | null },
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ me: h.state.me, setPaperSize: h.setPaperSize, setError: h.setError }),
}));

import PaperSizeSection from './PaperSizeSection';

function user(over: Partial<User> = {}): User {
  return {
    id: 1, username: 'octocat', name: 'Octo Cat', avatar_url: '',
    is_superuser: false, is_active: true, has_logged_in: true, home_address: '', ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.me = user();
  h.setPaperSize.mockResolvedValue(undefined);
});

describe('PaperSizeSection', () => {
  it('defaults to A4 when no preference is set', () => {
    h.state.me = user({ paper_size: undefined });
    render(<PaperSizeSection />);
    expect(screen.getByRole('radio', { name: 'A4' })).toBeChecked();
    expect(screen.getByRole('radio', { name: 'US Letter' })).not.toBeChecked();
  });

  it('reflects the stored Letter preference', () => {
    h.state.me = user({ paper_size: 'letter' });
    render(<PaperSizeSection />);
    expect(screen.getByRole('radio', { name: 'US Letter' })).toBeChecked();
  });

  it('saves the chosen size on change', async () => {
    render(<PaperSizeSection />);
    await userEvent.click(screen.getByRole('radio', { name: 'US Letter' }));
    await waitFor(() => expect(h.setPaperSize).toHaveBeenCalledWith('letter'));
  });

  it('does not save when picking the already-selected size', async () => {
    render(<PaperSizeSection />);
    await userEvent.click(screen.getByRole('radio', { name: 'A4' }));
    expect(h.setPaperSize).not.toHaveBeenCalled();
  });

  it('surfaces a save error', async () => {
    h.setPaperSize.mockRejectedValue(new Error('save boom'));
    render(<PaperSizeSection />);
    await userEvent.click(screen.getByRole('radio', { name: 'US Letter' }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
  });
});
