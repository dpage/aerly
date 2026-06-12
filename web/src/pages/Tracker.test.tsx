import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { setMatchMedia } from '../test/setup';

import type { PlanPart, Trip } from '../api/types';

// Stub the MUI date picker: its date-fns adapter trips vitest's ESM resolution,
// and these tests only need a labelled control, not real date parsing. The stub
// forwards a real Date to onChange on input so the page's window handlers run.
vi.mock('@mui/x-date-pickers/DatePicker', () => ({
  DatePicker: ({ label, onChange }: { label: string; onChange?: (d: Date | null) => void }) => (
    <input
      aria-label={label}
      onChange={(e) => onChange?.(e.target.value ? new Date(e.target.value) : null)}
    />
  ),
}));

const loadTracker = vi.fn();
const setTrackerWindow = vi.fn().mockResolvedValue(undefined);
const listTrips = vi.fn();

const state = {
  loadTracker,
  setTrackerWindow,
  listTrips,
  trackerParts: [] as PlanPart[],
  trackerTag: '',
  trackerWindow: {} as { from?: string; to?: string },
  trackerLoading: false,
  trips: [] as Trip[],
};

vi.mock('../state/store', () => ({
  useStore: (sel: (s: typeof state) => unknown) => sel(state),
}));

// PlanMapView is covered by its own test; capture the parts it receives.
const planMapSpy = vi.fn();
vi.mock('../components/PlanMapView', () => ({
  default: (props: { parts: PlanPart[]; initialSelectedPartId?: number | null }) => {
    planMapSpy(props);
    return <div data-testid="plan-map-view" />;
  },
}));

import Tracker from './Tracker';

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

function renderTracker(initial = '/tracker') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Tracker />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  state.trackerParts = [];
  state.trackerTag = '';
  state.trackerWindow = {};
  state.trackerLoading = false;
  state.trips = [];
});

describe('Tracker page', () => {
  it('loads on mount with a default From/To window and renders the controls', async () => {
    renderTracker();
    await waitFor(() => expect(loadTracker).toHaveBeenCalled());
    const w = loadTracker.mock.calls[0][0].window as { from: string; to: string };
    expect(w.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(w.to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(screen.getByLabelText('Tag')).toBeInTheDocument();
    expect(screen.getByLabelText('From')).toBeInTheDocument();
    expect(screen.getByLabelText('To')).toBeInTheDocument();
  });

  it('forwards the parts and ?part= selection to PlanMapView', () => {
    state.trackerParts = [{ id: 5 } as PlanPart];
    renderTracker('/tracker?part=5');
    const props = planMapSpy.mock.calls.at(-1)![0] as {
      parts: PlanPart[];
      initialSelectedPartId?: number | null;
    };
    expect(props.parts).toHaveLength(1);
    expect(props.initialSelectedPartId).toBe(5);
  });

  it('changing the tag seeds the window from the tag span and reloads', async () => {
    const user = userEvent.setup();
    const now = Date.now();
    const dayMs = 24 * 60 * 60 * 1000;
    state.trips = [
      trip({
        id: 1,
        tags: ['pgconf'],
        starts_on: new Date(now - 3 * dayMs).toISOString().slice(0, 10),
        ends_on: new Date(now + 4 * dayMs).toISOString().slice(0, 10),
      }),
    ];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByText('pgconf'));
    const call = loadTracker.mock.calls.find((c) => c[0]?.tag === 'pgconf');
    expect(call).toBeTruthy();
    const w = call![0].window as { from?: string; to?: string };
    expect(w.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(w.to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });

  it('reuses a persisted window on mount instead of the default span', async () => {
    state.trackerWindow = { from: '2026-01-01' };
    renderTracker();
    await waitFor(() => expect(loadTracker).toHaveBeenCalled());
    expect(loadTracker.mock.calls[0][0].window).toEqual({ from: '2026-01-01' });
  });

  it('the From/To pickers push the chosen day into the tracker window', async () => {
    renderTracker();
    // Fire a single complete-date change (avoids partial values from per-keystroke typing).
    fireEvent.change(screen.getByLabelText('From'), { target: { value: '2026-10-12' } });
    expect(setTrackerWindow).toHaveBeenCalledWith({ from: '2026-10-12' });
    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-10-20' } });
    expect(setTrackerWindow).toHaveBeenCalledWith({ to: '2026-10-20' });
    // The falsy branch (cleared input → null) is a no-op, exercised here.
    fireEvent.change(screen.getByLabelText('From'), { target: { value: '' } });
  });

  it('ignores a non-numeric ?part= deep link (passes null to the map)', () => {
    renderTracker('/tracker?part=banana');
    const props = planMapSpy.mock.calls.at(-1)![0] as { initialSelectedPartId?: number | null };
    expect(props.initialSelectedPartId).toBeNull();
  });

  it('merges the spans of several trips sharing a tag (min start / max end)', async () => {
    const user = userEvent.setup();
    state.trips = [
      trip({ id: 1, tags: ['pgconf'], starts_on: '2026-10-10', ends_on: '2026-10-14' }),
      trip({ id: 2, tags: ['pgconf'], starts_on: '2026-10-05', ends_on: '2026-10-20' }),
    ];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByText('pgconf'));
    const call = loadTracker.mock.calls.find((c) => c[0]?.tag === 'pgconf');
    const w = call![0].window as { from: string; to: string };
    // Padded a day each side of the merged span. The date-only end runs
    // through the end of 20 Oct, so the padded "to" lands on 22 Oct.
    expect(w.from).toBe('2026-10-04');
    expect(w.to).toBe('2026-10-22');
  });

  it('a tag with only a start bound yields a from-only window (to is undefined)', async () => {
    const user = userEvent.setup();
    // effective_start with no end → tripSpan has start, null end.
    state.trips = [trip({ id: 1, tags: ['oneway'], starts_on: '2026-10-10' })];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByText('oneway'));
    const call = loadTracker.mock.calls.find((c) => c[0]?.tag === 'oneway');
    const w = call![0].window as { from?: string; to?: string };
    expect(w.from).toBe('2026-10-09');
    expect(w.to).toBeUndefined();
  });

  it('a tag with only an end bound yields a to-only window (from is undefined)', async () => {
    const user = userEvent.setup();
    state.trips = [trip({ id: 1, tags: ['arrival'], ends_on: '2026-10-20' })];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByText('arrival'));
    const call = loadTracker.mock.calls.find((c) => c[0]?.tag === 'arrival');
    const w = call![0].window as { from?: string; to?: string };
    expect(w.from).toBeUndefined();
    // End-only span runs through the end of 20 Oct; padded "to" lands on 22 Oct.
    expect(w.to).toBe('2026-10-22');
  });

  it('selecting a tag whose trips have no derivable span keeps the current window', async () => {
    const user = userEvent.setup();
    state.trackerWindow = { from: '2026-05-01', to: '2026-05-10' };
    // A tagged trip with no dates at all → tagWindow returns null → keep `win`.
    state.trips = [trip({ id: 1, tags: ['someday'] })];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await user.click(within(listbox).getByText('someday'));
    const call = loadTracker.mock.calls.find((c) => c[0]?.tag === 'someday');
    expect(call).toBeTruthy();
    expect(call![0].window).toEqual({ from: '2026-05-01', to: '2026-05-10' });
  });

  it('switching back to the untagged view (empty tag) keeps the current window', async () => {
    const user = userEvent.setup();
    state.trackerWindow = { from: '2026-05-01', to: '2026-05-10' };
    state.trackerTag = 'pgconf';
    state.trips = [trip({ id: 1, tags: ['pgconf'] })];
    renderTracker();
    await user.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    // tagWindow('') returns null (no label), so the existing window is kept.
    await user.click(within(listbox).getByText(/untagged view/i));
    const call = loadTracker.mock.calls.find((c) => c[0]?.tag === '');
    expect(call).toBeTruthy();
    expect(call![0].window).toEqual({ from: '2026-05-01', to: '2026-05-10' });
  });
});

describe('Tracker page (mobile)', () => {
  beforeEach(() => {
    setMatchMedia(true);
  });

  it('hides the heading and controls row, floating a filter pill over the map', () => {
    state.trackerTag = 'family';
    state.trackerWindow = { from: '2026-06-05', to: '2026-07-12' };
    renderTracker();
    expect(screen.queryByRole('heading', { name: 'Tracker' })).not.toBeInTheDocument();
    const pill = screen.getByTestId('tracker-filter-pill');
    expect(pill).toHaveTextContent('family');
    expect(pill).toHaveTextContent('5 Jun – 12 Jul');
  });

  it('labels the pill "Everyone" with no tag selected', () => {
    renderTracker();
    expect(screen.getByTestId('tracker-filter-pill')).toHaveTextContent('Everyone');
  });

  it('opens the controls in a popover and changes the tag from it', async () => {
    state.trips = [trip({ id: 1, tags: ['family'] }), trip({ id: 2, tags: ['work'] })];
    renderTracker();
    await userEvent.click(screen.getByTestId('tracker-filter-pill'));
    // The popover hosts the same Tag select + From/To pickers as desktop.
    expect(screen.getByLabelText('From')).toBeInTheDocument();
    expect(screen.getByLabelText('To')).toBeInTheDocument();
    await userEvent.click(screen.getByLabelText('Tag'));
    const listbox = await screen.findByRole('listbox');
    await userEvent.click(within(listbox).getByText('work'));
    await waitFor(() =>
      expect(loadTracker).toHaveBeenLastCalledWith(expect.objectContaining({ tag: 'work' })),
    );
  });

  it('keeps the desktop header (no pill) on wide screens', () => {
    setMatchMedia(false);
    renderTracker();
    expect(screen.getByRole('heading', { name: 'Tracker' })).toBeInTheDocument();
    expect(screen.queryByTestId('tracker-filter-pill')).not.toBeInTheDocument();
  });

  it('exposes the pill as a popover trigger to assistive tech', async () => {
    renderTracker();
    const pill = screen.getByTestId('tracker-filter-pill');
    expect(pill).toHaveAttribute('aria-haspopup', 'true');
    expect(pill).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(pill);
    expect(pill).toHaveAttribute('aria-expanded', 'true');
  });
});
