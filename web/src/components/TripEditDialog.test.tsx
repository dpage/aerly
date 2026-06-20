import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { Trip } from '../api/types';

const h = vi.hoisted(() => ({
  updateTrip: vi.fn(),
  deleteTrip: vi.fn(),
  setTripTags: vi.fn(),
  setError: vi.fn(),
}));

vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({
      updateTrip: h.updateTrip,
      deleteTrip: h.deleteTrip,
      setTripTags: h.setTripTags,
      setError: h.setError,
    }),
}));

// TripFeedsEditor manages its own state via the API and is covered by its own
// tests; stub it here so the dialog tests stay focused on the trip fields (and
// its feed URL/name inputs don't collide with the trip's Name field).
vi.mock('./TripFeedsEditor', () => ({
  default: () => <div data-testid="trip-feeds-editor" />,
}));

// TagInput: expose the current tags and a button to push a new tag, so we can
// exercise the "tags changed → setTripTags" branch.
vi.mock('./TagInput', () => ({
  default: ({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) => (
    <div data-testid="tag-input">
      <span>{value.join(',')}</span>
      <button type="button" onClick={() => onChange([...value, 'added'])}>
        add-tag
      </button>
    </div>
  ),
}));

import TripEditDialog from './TripEditDialog';

function trip(over: Partial<Trip> = {}): Trip {
  return {
    id: 1,
    name: 'Lisbon',
    destination: 'Portugal',
    starts_on: '2026-10-10',
    ends_on: '2026-10-15',
    my_role: 'owner',
    members: [],
    tags: ['beach'],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('TripEditDialog', () => {
  it('renders nothing when closed', () => {
    render(<TripEditDialog open={false} trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    expect(screen.queryByText('Edit trip')).not.toBeInTheDocument();
  });

  it('prefills fields from the trip', () => {
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    expect(screen.getByRole('textbox', { name: /name/i })).toHaveValue('Lisbon');
    expect(screen.getByRole('textbox', { name: /destination/i })).toHaveValue('Portugal');
    expect(screen.getByLabelText('Starts')).toHaveValue('2026-10-10');
    expect(screen.getByLabelText('Ends')).toHaveValue('2026-10-15');
  });

  it('handles a trip with no dates (optional fields absent)', () => {
    render(
      <TripEditDialog
        open
        trip={trip({ starts_on: undefined, ends_on: undefined })}
        onClose={() => {}}
        onDeleted={() => {}}
      />,
    );
    expect(screen.getByLabelText('Starts')).toHaveValue('');
    expect(screen.getByLabelText('Ends')).toHaveValue('');
  });

  it('saves name/destination/dates (no tag change → no setTripTags)', async () => {
    h.updateTrip.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<TripEditDialog open trip={trip()} onClose={onClose} onDeleted={() => {}} />);
    const name = screen.getByRole('textbox', { name: /name/i });
    await userEvent.clear(name);
    await userEvent.type(name, '  Porto  ');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updateTrip).toHaveBeenCalledWith(1, {
        name: 'Porto',
        destination: 'Portugal',
        starts_on: '2026-10-10',
        ends_on: '2026-10-15',
      }),
    );
    expect(h.setTripTags).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('edits destination and start date then saves them', async () => {
    h.updateTrip.mockResolvedValue(undefined);
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    const dest = screen.getByRole('textbox', { name: /destination/i });
    await userEvent.clear(dest);
    await userEvent.type(dest, 'Spain');
    const starts = screen.getByLabelText('Starts');
    await userEvent.clear(starts);
    await userEvent.type(starts, '2026-10-12');
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updateTrip).toHaveBeenCalledWith(1, {
        name: 'Lisbon',
        destination: 'Spain',
        starts_on: '2026-10-12',
        ends_on: '2026-10-15',
      }),
    );
  });

  it('omits empty dates as undefined in the patch', async () => {
    h.updateTrip.mockResolvedValue(undefined);
    render(
      <TripEditDialog
        open
        trip={trip({ starts_on: undefined, ends_on: undefined })}
        onClose={() => {}}
        onDeleted={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() =>
      expect(h.updateTrip).toHaveBeenCalledWith(1, {
        name: 'Lisbon',
        destination: 'Portugal',
        starts_on: undefined,
        ends_on: undefined,
      }),
    );
  });

  it('writes tags via setTripTags when they change', async () => {
    h.updateTrip.mockResolvedValue(undefined);
    h.setTripTags.mockResolvedValue(undefined);
    render(
      <TripEditDialog open trip={trip({ tags: ['a'] })} onClose={() => {}} onDeleted={() => {}} />,
    );
    await userEvent.click(screen.getByRole('button', { name: 'add-tag' }));
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setTripTags).toHaveBeenCalledWith(1, ['a', 'added']));
  });

  it('does not save with a blank name', async () => {
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    await userEvent.clear(screen.getByRole('textbox', { name: /name/i }));
    const save = screen.getByRole('button', { name: /^save$/i });
    expect(save).toBeDisabled();
  });

  it('shows a date-order error and disables save when end precedes start', async () => {
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    const ends = screen.getByLabelText('Ends');
    await userEvent.clear(ends);
    await userEvent.type(ends, '2026-10-01');
    expect(await screen.findByText('End is before start')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled();
  });

  it('surfaces save errors via setError', async () => {
    h.updateTrip.mockRejectedValue(new Error('save boom'));
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('save boom'));
  });

  it('coerces non-Error save rejections to a string', async () => {
    h.updateTrip.mockRejectedValue('nope');
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('nope'));
  });

  it('owners see Delete; deleting confirms then closes and notifies', async () => {
    h.deleteTrip.mockResolvedValue(undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const onClose = vi.fn();
    const onDeleted = vi.fn();
    render(<TripEditDialog open trip={trip()} onClose={onClose} onDeleted={onDeleted} />);
    await userEvent.click(screen.getByRole('button', { name: /delete/i }));
    await waitFor(() => expect(h.deleteTrip).toHaveBeenCalledWith(1));
    expect(onClose).toHaveBeenCalled();
    expect(onDeleted).toHaveBeenCalled();
  });

  it('aborts delete when the confirm is dismissed', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: /delete/i }));
    expect(h.deleteTrip).not.toHaveBeenCalled();
  });

  it('surfaces delete errors via setError', async () => {
    h.deleteTrip.mockRejectedValue(new Error('del boom'));
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<TripEditDialog open trip={trip()} onClose={() => {}} onDeleted={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: /delete/i }));
    await waitFor(() => expect(h.setError).toHaveBeenCalledWith('del boom'));
  });

  it('hides Delete for non-owners (editor)', () => {
    render(
      <TripEditDialog
        open
        trip={trip({ my_role: 'editor' })}
        onClose={() => {}}
        onDeleted={() => {}}
      />,
    );
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
  });

  it('cancel closes the dialog', async () => {
    const onClose = vi.fn();
    render(<TripEditDialog open trip={trip()} onClose={onClose} onDeleted={() => {}} />);
    await userEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onClose).toHaveBeenCalled();
  });
});
