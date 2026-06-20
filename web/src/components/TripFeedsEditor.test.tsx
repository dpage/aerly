import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { TripFeed } from '../api/types';

const h = vi.hoisted(() => ({
  listTripFeeds: vi.fn(),
  addTripFeed: vi.fn(),
  updateTripFeed: vi.fn(),
  deleteTripFeed: vi.fn(),
  setError: vi.fn(),
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) => sel({ setError: h.setError }),
}));

vi.mock('../api/client', () => ({
  api: {
    listTripFeeds: h.listTripFeeds,
    addTripFeed: h.addTripFeed,
    updateTripFeed: h.updateTripFeed,
    deleteTripFeed: h.deleteTripFeed,
  },
}));

import TripFeedsEditor from './TripFeedsEditor';

function feed(over: Partial<TripFeed> = {}): TripFeed {
  return { id: 1, trip_id: 7, url: 'https://cal.example/a.ics', name: 'Conf', ...over };
}

beforeEach(() => {
  vi.clearAllMocks();
  h.listTripFeeds.mockResolvedValue([]);
});

describe('TripFeedsEditor', () => {
  it('lists the trip’s existing feeds on mount', async () => {
    h.listTripFeeds.mockResolvedValue([feed(), feed({ id: 2, url: 'https://x/b.ics', name: 'B' })]);
    render(<TripFeedsEditor tripId={7} />);
    await waitFor(() => expect(h.listTripFeeds).toHaveBeenCalledWith(7));
    expect(await screen.findByDisplayValue('https://cal.example/a.ics')).toBeInTheDocument();
    expect(screen.getByDisplayValue('https://x/b.ics')).toBeInTheDocument();
  });

  it('surfaces a load failure via setError', async () => {
    h.listTripFeeds.mockRejectedValue(new Error('nope'));
    render(<TripFeedsEditor tripId={7} />);
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('nope'));
  });

  it('adds a feed, appends it and clears the inputs', async () => {
    h.addTripFeed.mockResolvedValue(feed({ id: 9, url: 'https://new/c.ics', name: 'New' }));
    render(<TripFeedsEditor tripId={7} />);
    const addBtn = screen.getByRole('button', { name: /^add$/i });
    expect(addBtn).toBeDisabled(); // blank URL
    const urlField = screen.getByLabelText('Feed URL');
    await userEvent.type(urlField, '  https://new/c.ics  ');
    await userEvent.type(screen.getByLabelText('Name (optional)'), 'New');
    await userEvent.click(addBtn);
    await waitFor(() =>
      expect(h.addTripFeed).toHaveBeenCalledWith(7, 'https://new/c.ics', 'New'),
    );
    expect(await screen.findByDisplayValue('https://new/c.ics')).toBeInTheDocument();
    // Inputs reset after a successful add. The added feed now renders its own
    // row, so the add-form fields are the last pair.
    const urls = screen.getAllByLabelText('Feed URL');
    const names = screen.getAllByLabelText('Name (optional)');
    expect(urls[urls.length - 1]).toHaveValue('');
    expect(names[names.length - 1]).toHaveValue('');
  });

  it('omits a blank name when adding', async () => {
    h.addTripFeed.mockResolvedValue(feed({ id: 9, url: 'https://new/c.ics', name: '' }));
    render(<TripFeedsEditor tripId={7} />);
    await userEvent.type(screen.getByLabelText('Feed URL'), 'https://new/c.ics');
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    await waitFor(() =>
      expect(h.addTripFeed).toHaveBeenCalledWith(7, 'https://new/c.ics', undefined),
    );
  });

  it('surfaces an add failure via setError', async () => {
    h.addTripFeed.mockRejectedValue(new Error('bad url'));
    render(<TripFeedsEditor tripId={7} />);
    await userEvent.type(screen.getByLabelText('Feed URL'), 'https://new/c.ics');
    await userEvent.click(screen.getByRole('button', { name: /^add$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('bad url'));
  });

  it('removes a feed', async () => {
    h.listTripFeeds.mockResolvedValue([feed()]);
    h.deleteTripFeed.mockResolvedValue(undefined);
    render(<TripFeedsEditor tripId={7} />);
    await screen.findByDisplayValue('https://cal.example/a.ics');
    await userEvent.click(screen.getByRole('button', { name: /remove feed/i }));
    await waitFor(() => expect(h.deleteTripFeed).toHaveBeenCalledWith(7, 1));
    await waitFor(() =>
      expect(screen.queryByDisplayValue('https://cal.example/a.ics')).not.toBeInTheDocument(),
    );
  });

  it('surfaces a remove failure via setError', async () => {
    h.listTripFeeds.mockResolvedValue([feed()]);
    h.deleteTripFeed.mockRejectedValue(new Error('cannot delete'));
    render(<TripFeedsEditor tripId={7} />);
    await screen.findByDisplayValue('https://cal.example/a.ics');
    await userEvent.click(screen.getByRole('button', { name: /remove feed/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('cannot delete'));
  });

  it('shows a Save only when a row is edited, then patches and reflects the update', async () => {
    h.listTripFeeds.mockResolvedValue([feed()]);
    h.updateTripFeed.mockResolvedValue(feed({ name: 'Renamed' }));
    render(<TripFeedsEditor tripId={7} />);
    await screen.findByDisplayValue('https://cal.example/a.ics');
    // No Save until something changes.
    expect(screen.queryByRole('button', { name: /^save$/i })).not.toBeInTheDocument();
    const nameField = screen.getByDisplayValue('Conf');
    await userEvent.clear(nameField);
    await userEvent.type(nameField, 'Renamed');
    const save = await screen.findByRole('button', { name: /^save$/i });
    await userEvent.click(save);
    await waitFor(() =>
      expect(h.updateTripFeed).toHaveBeenCalledWith(7, 1, 'https://cal.example/a.ics', 'Renamed'),
    );
  });

  it('surfaces a save failure via setError', async () => {
    h.listTripFeeds.mockResolvedValue([feed()]);
    h.updateTripFeed.mockRejectedValue(new Error('save failed'));
    render(<TripFeedsEditor tripId={7} />);
    await screen.findByDisplayValue('https://cal.example/a.ics');
    const urlField = screen.getByDisplayValue('https://cal.example/a.ics');
    await userEvent.type(urlField, '2');
    await userEvent.click(await screen.findByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save failed'));
  });

  it('flags an unhealthy feed with its last fetch error', async () => {
    h.listTripFeeds.mockResolvedValue([feed({ last_error: 'timeout' })]);
    render(<TripFeedsEditor tripId={7} />);
    expect(await screen.findByText(/last fetch failed: timeout/i)).toBeInTheDocument();
  });
});
