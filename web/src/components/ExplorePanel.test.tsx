import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { PoiResponse } from '../api/types';
import { DEFAULT_CATS } from './exploreCategories';

const h = vi.hoisted(() => ({
  fetchPois: vi.fn<[], Promise<PoiResponse>>(),
  resolveCategories: vi.fn(),
  state: {
    capabilities: undefined as { explore_search_enabled?: boolean } | undefined,
  },
}));
vi.mock('../api/client', () => ({
  api: { fetchPois: h.fetchPois, resolveCategories: h.resolveCategories },
}));
vi.mock('../state/store', () => ({
  useStore: (sel: (s: Record<string, unknown>) => unknown) =>
    sel({ capabilities: h.state.capabilities }),
}));
// AddToTripDialog is heavy; stub it so we assert it opens with the right prefill.
vi.mock('./AddToTripDialog', () => ({
  default: ({ open, prefill }: { open: boolean; prefill?: { title: string } }) =>
    open ? <div data-testid="add-dialog">{prefill?.title}</div> : null,
}));
// PoiMiniMap needs WebGL (MapLibre); stub it and expose a "pin" per POI plus the
// current selection, so the list↔map wiring can be asserted without a real map.
vi.mock('./PoiMiniMap', () => ({
  default: ({
    pois,
    selectedId,
    onSelectPoi,
  }: {
    pois: { id: string; name: string }[];
    selectedId?: string;
    onSelectPoi: (id: string) => void;
  }) => (
    <div data-testid="poi-mini-map" data-selected={selectedId ?? ''}>
      {pois.map((p) => (
        <button
          key={p.id}
          data-testid={`map-pin-${p.id}`}
          aria-label={`map pin ${p.id}`}
          onClick={() => onSelectPoi(p.id)}
        />
      ))}
    </div>
  ),
}));

// jsdom doesn't implement scrollIntoView; the panel calls it on row selection.
Element.prototype.scrollIntoView = vi.fn();

import ExplorePanel from './ExplorePanel';

// Expands a theme accordion by clicking its expand icon rather than the
// FormControlLabel region, whose onClick stops propagation so that toggling
// the theme checkbox doesn't also flip the accordion open/closed.
async function expandTheme(themeLabel: string) {
  const summary = screen.getByText(themeLabel).closest('.MuiAccordionSummary-root');
  expect(summary).not.toBeNull();
  const expandIcon = (summary as HTMLElement).querySelector(
    '.MuiAccordionSummary-expandIconWrapper',
  );
  expect(expandIcon).not.toBeNull();
  await userEvent.click(expandIcon as HTMLElement);
}

beforeEach(() => {
  vi.clearAllMocks();
  h.state.capabilities = undefined;
  h.fetchPois.mockResolvedValue({
    center: { lat: 51.5, lon: -0.12 },
    center_label: 'London',
    pois: [
      {
        id: 'node/1',
        name: 'Example Tower',
        category: 'attractions',
        lat: 51.5,
        lon: -0.12,
        distance_m: 40,
        address: '1 Example Square',
        description: 'A tall example landmark',
        wikidata: 'Q1',
        wikipedia: 'en:Example Article',
        website: 'https://example.com',
      },
      {
        id: 'node/2',
        name: 'Big Museum',
        category: 'museums',
        lat: 51.51,
        lon: -0.1,
        distance_m: 1800,
      },
      {
        id: 'node/3',
        name: 'Old Castle',
        category: 'monuments_heritage',
        lat: 51.52,
        lon: -0.11,
        distance_m: 500,
      },
      {
        id: 'node/4',
        name: 'Green Park',
        category: 'parks_gardens',
        lat: 51.53,
        lon: -0.13,
        distance_m: 600,
      },
      {
        id: 'node/5',
        name: 'Corner Cafe',
        category: 'cafes',
        lat: 51.54,
        lon: -0.14,
        distance_m: 700,
      },
    ],
  });
});

describe('ExplorePanel', () => {
  it('loads POIs for the initial place and lists them', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    expect(await screen.findByText('Example Tower')).toBeInTheDocument();
    expect(screen.getByText('Big Museum')).toBeInTheDocument();
    expect(screen.getByText('Old Castle')).toBeInTheDocument();
    expect(screen.getByText('Green Park')).toBeInTheDocument();
    expect(screen.getByText('Corner Cafe')).toBeInTheDocument();
    expect(h.fetchPois).toHaveBeenCalledWith(7, expect.objectContaining({ place: 'London' }));
    expect(screen.getByText(/OpenStreetMap/i)).toBeInTheDocument();
    // distance formatting: metres under 1km, km above
    expect(screen.getByText(/40 m away/i)).toBeInTheDocument();
    expect(screen.getByText(/1\.8 km away/i)).toBeInTheDocument();
    // out-links present when data is available
    const towerLinks = screen.getAllByRole('link');
    // Location is now shown in-app via the mini-map, not an external OSM link.
    expect(screen.getAllByRole('button', { name: /show on map/i }).length).toBeGreaterThan(0);
    expect(screen.getByTestId('poi-mini-map')).toBeInTheDocument();
    expect(
      towerLinks.some((l) => l.getAttribute('href') === 'https://www.wikidata.org/wiki/Q1'),
    ).toBe(true);
    expect(
      towerLinks.some(
        (l) => l.getAttribute('href') === 'https://en.wikipedia.org/wiki/Example_Article',
      ),
    ).toBe(true);
    expect(towerLinks.some((l) => l.getAttribute('href') === 'https://example.com')).toBe(true);
    // the row caption uses the polished sub-category label, not the raw key
    expect(screen.getByText('Attractions · 40 m away')).toBeInTheDocument();
  });

  it('shows the description line only for POIs that have one', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    // The tower carries an OSM description, so its blurb renders.
    expect(await screen.findByText('A tall example landmark')).toBeInTheDocument();
    // The museum has no description, so no stray/empty blurb appears for it. Its
    // row is the one containing "Big Museum"; assert the blurb isn't in it.
    const museumRow = screen.getByText('Big Museum').closest('li') as HTMLElement;
    expect(within(museumRow).queryByText('A tall example landmark')).not.toBeInTheDocument();
  });

  it('renders theme headers and Worship is off by default', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    expect(screen.getByText('Worship')).toBeInTheDocument();
    expect(screen.getByText('Food & drink')).toBeInTheDocument();
    const cats = (h.fetchPois.mock.calls[0][1] as { cats?: string[] }).cats;
    expect(cats).not.toContain('worship');
    expect(cats).toEqual(expect.arrayContaining(DEFAULT_CATS));
  });

  it('expands a theme and reveals its child checkboxes', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    await expandTheme('Food & drink');
    const bars = await screen.findByLabelText('Bars');
    expect(bars).toBeInTheDocument();
  });

  it('toggling a theme checkbox selects all of its children', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    await userEvent.click(screen.getByLabelText('Food & drink'));
    const lastCall = h.fetchPois.mock.calls[0][1] as { cats?: string[] };
    expect(lastCall.cats).toEqual(
      expect.arrayContaining(['restaurants', 'cafes', 'bars', 'pubs', 'street_food']),
    );
  });

  it('toggling a fully-selected theme checkbox deselects all its children', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    // "Sights & landmarks" isn't fully selected by default (viewpoints is off),
    // so the first click turns everything on; the second click (now all on)
    // turns everything off.
    await userEvent.click(screen.getByLabelText('Sights & landmarks'));
    h.fetchPois.mockClear();
    await userEvent.click(screen.getByLabelText('Sights & landmarks'));
    const lastCall = h.fetchPois.mock.calls[0][1] as { cats?: string[] };
    expect(lastCall.cats).not.toEqual(
      expect.arrayContaining(['attractions', 'viewpoints', 'monuments_heritage']),
    );
  });

  it('remembers category choices across remounts via localStorage', async () => {
    const { unmount } = render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    await userEvent.click(screen.getByLabelText('Worship'));
    unmount();
    h.fetchPois.mockClear();
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    const cats = (h.fetchPois.mock.calls[0][1] as { cats?: string[] }).cats;
    expect(cats).toContain('worship');
  });

  it('persists a toggled sub-category to localStorage under aerly.explore.subcats', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    await expandTheme('Live music & nightlife');
    await userEvent.click(await screen.findByLabelText('Nightclubs'));
    const stored = JSON.parse(window.localStorage.getItem('aerly.explore.subcats') || '[]');
    expect(stored).toContain('nightclubs');
  });

  it('resets the selection to defaults', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    // Dirty the selection first.
    await userEvent.click(screen.getByLabelText('Sights & landmarks'));
    h.fetchPois.mockClear();
    await userEvent.click(screen.getByRole('button', { name: /reset to defaults/i }));
    const lastCall = h.fetchPois.mock.calls[0][1] as { cats?: string[] };
    expect(lastCall.cats).toEqual(expect.arrayContaining(DEFAULT_CATS));
    expect(lastCall.cats).not.toContain('viewpoints');
  });

  it('restores a stored category selection on mount', async () => {
    window.localStorage.setItem('aerly.explore.subcats', JSON.stringify(['cafes']));
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    expect((h.fetchPois.mock.calls[0][1] as { cats?: string[] }).cats).toEqual(['cafes']);
  });

  it('drops unknown categories from a stored selection', async () => {
    window.localStorage.setItem('aerly.explore.subcats', JSON.stringify(['cafes', 'bogus']));
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    expect((h.fetchPois.mock.calls[0][1] as { cats?: string[] }).cats).toEqual(['cafes']);
  });

  it('falls back to defaults when the stored selection is unparseable', async () => {
    window.localStorage.setItem('aerly.explore.subcats', 'not json');
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    expect((h.fetchPois.mock.calls[0][1] as { cats?: string[] }).cats).toEqual(DEFAULT_CATS);
  });

  it('falls back to defaults when the stored value is not an array', async () => {
    window.localStorage.setItem('aerly.explore.subcats', '{"nope":1}');
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    expect((h.fetchPois.mock.calls[0][1] as { cats?: string[] }).cats).toEqual(DEFAULT_CATS);
  });

  it('prefers initialCenter coords over place when both are supplied', async () => {
    render(
      <ExplorePanel
        tripId={7}
        initialPlace="London"
        initialCenter={{ lat: 51.5, lon: -0.12, label: 'London centre' }}
      />,
    );
    await screen.findByText('Example Tower');
    expect(h.fetchPois).toHaveBeenCalledWith(
      7,
      expect.objectContaining({ lat: 51.5, lon: -0.12 }),
    );
    const call = h.fetchPois.mock.calls[0][1] as { place?: string };
    expect(call.place).toBeUndefined();
    // Anchored to a fixed point, so the place search is hidden, not just disabled.
    expect(screen.queryByLabelText('Place')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^search$/i })).not.toBeInTheDocument();
  });

  it('does not refetch when re-rendered with a content-identical initialCenter object', async () => {
    const { rerender } = render(
      <ExplorePanel tripId={7} initialCenter={{ lat: 51.5, lon: -0.12 }} />,
    );
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    // A parent re-render handing us a brand-new but content-identical object
    // (the natural inline-literal call style) must not trigger a second fetch:
    // the effect keys on the coordinate values, not the object identity.
    rerender(<ExplorePanel tripId={7} initialCenter={{ lat: 51.5, lon: -0.12 }} />);
    expect(h.fetchPois).not.toHaveBeenCalled();
  });

  it('opens the add dialog pre-filled when a POI is added', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    const row = screen.getByText('Example Tower').closest('li');
    expect(row).not.toBeNull();
    await userEvent.click(within(row as HTMLElement).getByRole('button', { name: /add to trip/i }));
    expect(screen.getByTestId('add-dialog')).toHaveTextContent('Example Tower');
  });

  it('syncs selection both ways between the list and the map', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');

    // Map → list: clicking a map pin selects that POI (reflected on the map stub).
    await userEvent.click(screen.getByTestId('map-pin-node/1'));
    expect(screen.getByTestId('poi-mini-map')).toHaveAttribute('data-selected', 'node/1');

    // List → map: "Show on map" on a row sets the selection to that POI.
    const row = screen.getByText('Big Museum').closest('li') as HTMLElement;
    await userEvent.click(within(row).getByRole('button', { name: /show on map/i }));
    expect(screen.getByTestId('poi-mini-map')).toHaveAttribute('data-selected', 'node/2');
  });

  it('re-fetches with updated categories when a sub-category is toggled', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    await expandTheme('Food & drink');
    await userEvent.click(await screen.findByLabelText('Cafés'));
    expect(h.fetchPois).toHaveBeenCalledWith(
      7,
      expect.objectContaining({ cats: expect.arrayContaining(['cafes']) }),
    );

    h.fetchPois.mockClear();
    await expandTheme('Sights & landmarks');
    await userEvent.click(await screen.findByLabelText('Attractions'));
    const lastCall = h.fetchPois.mock.calls[0][1] as { cats?: string[] };
    expect(lastCall.cats).not.toContain('attractions');
  });

  it('re-fetches with the new radius when the radius selector changes', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    await userEvent.click(screen.getByRole('button', { name: '5 km' }));
    expect(h.fetchPois).toHaveBeenCalledWith(7, expect.objectContaining({ radius: 5000 }));
  });

  it('re-fetches when the place is submitted via the search button', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    const placeField = screen.getByLabelText('Place');
    await userEvent.clear(placeField);
    await userEvent.type(placeField, 'Paris');
    await userEvent.click(screen.getByRole('button', { name: /search/i }));
    expect(h.fetchPois).toHaveBeenCalledWith(7, expect.objectContaining({ place: 'Paris' }));
  });

  it('does not re-fetch on every keystroke in the place field', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    const placeField = screen.getByLabelText('Place');
    await userEvent.type(placeField, 'x');
    expect(h.fetchPois).not.toHaveBeenCalled();
  });

  it('filters the already-loaded POIs client-side by name, without re-fetching', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    const nameFilter = screen.getByLabelText(/filter by name/i);
    await userEvent.type(nameFilter, 'museum');
    expect(screen.queryByText('Example Tower')).not.toBeInTheDocument();
    expect(screen.getByText('Big Museum')).toBeInTheDocument();
    expect(h.fetchPois).not.toHaveBeenCalled();
  });

  it('shows an empty-state message when there are no POIs', async () => {
    h.fetchPois.mockResolvedValue({ center: { lat: 51.5, lon: -0.12 }, pois: [] });
    render(<ExplorePanel tripId={7} initialPlace="Nowhere" />);
    expect(await screen.findByText(/no.*found/i)).toBeInTheDocument();
  });

  it('shows an error message when the fetch fails', async () => {
    h.fetchPois.mockRejectedValue(new Error('boom'));
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    expect(await screen.findByText(/boom|couldn.t|error/i)).toBeInTheDocument();
  });

  it('retries the fetch when "Try again" is clicked after a failure', async () => {
    h.fetchPois.mockRejectedValueOnce(new Error('temporarily unavailable'));
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText(/temporarily unavailable/i);
    // The next call succeeds, so clicking Try again should surface the POIs.
    h.fetchPois.mockResolvedValue({
      center: { lat: 51.5, lon: -0.12 },
      pois: [
        { id: 'node/1', name: 'Example Tower', category: 'attractions', lat: 51.5, lon: -0.12, distance_m: 40 },
      ],
    });
    await userEvent.click(screen.getByRole('button', { name: /try again/i }));
    expect(await screen.findByText('Example Tower')).toBeInTheDocument();
  });

  it('defaults the place search to empty when no initialPlace is given', async () => {
    render(<ExplorePanel tripId={7} />);
    await screen.findByText('Example Tower');
    expect(h.fetchPois).toHaveBeenCalledWith(7, expect.objectContaining({ place: '' }));
  });

  it('ignores an in-flight fetch that resolves after unmount', async () => {
    let resolveFetch: (v: PoiResponse) => void = () => {};
    h.fetchPois.mockReturnValueOnce(
      new Promise<PoiResponse>((resolve) => {
        resolveFetch = resolve;
      }),
    );
    const { unmount } = render(<ExplorePanel tripId={7} initialPlace="London" />);
    unmount();
    resolveFetch({ center: { lat: 0, lon: 0 }, pois: [] });
    // No assertion beyond "doesn't throw" — this exercises the cancelled-guard
    // branch in the effect's then/catch/finally so a resolved fetch after
    // unmount is a safe no-op (no React "update on unmounted component" warning).
    await Promise.resolve();
  });

  it('ignores an in-flight fetch that rejects after unmount', async () => {
    let rejectFetch: (err: unknown) => void = () => {};
    h.fetchPois.mockReturnValueOnce(
      new Promise<PoiResponse>((_resolve, reject) => {
        rejectFetch = reject;
      }),
    );
    const { unmount } = render(<ExplorePanel tripId={7} initialPlace="London" />);
    unmount();
    rejectFetch(new Error('too late'));
    await Promise.resolve();
  });

  it('search applies resolved categories to the picker', async () => {
    h.state.capabilities = { explore_search_enabled: true };
    h.resolveCategories.mockResolvedValue({ categories: ['bars', 'live_venues'] });
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    await userEvent.type(
      await screen.findByLabelText('Search by interest'),
      'rooftop bars and live jazz',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Find' }));
    expect(h.resolveCategories).toHaveBeenCalledWith('rooftop bars and live jazz');
    const stored = JSON.parse(window.localStorage.getItem('aerly.explore.subcats') || '[]');
    expect(stored).toEqual(expect.arrayContaining(['bars', 'live_venues']));
  });

  it('shows a friendly message when the search resolves no categories', async () => {
    h.state.capabilities = { explore_search_enabled: true };
    h.resolveCategories.mockResolvedValue({ categories: [] });
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    await userEvent.type(await screen.findByLabelText('Search by interest'), 'nonsense');
    await userEvent.click(screen.getByRole('button', { name: 'Find' }));
    expect(await screen.findByText(/no matching categories/i)).toBeInTheDocument();
  });

  it('shows an error message when resolving categories fails', async () => {
    h.state.capabilities = { explore_search_enabled: true };
    h.resolveCategories.mockRejectedValue(new Error('llm down'));
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    await userEvent.type(await screen.findByLabelText('Search by interest'), 'jazz bars');
    await userEvent.click(screen.getByRole('button', { name: 'Find' }));
    expect(await screen.findByText(/llm down/i)).toBeInTheDocument();
  });

  it('does not search when the phrase is blank', async () => {
    h.state.capabilities = { explore_search_enabled: true };
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    await userEvent.click(screen.getByRole('button', { name: 'Find' }));
    expect(h.resolveCategories).not.toHaveBeenCalled();
  });

  it('hides the search box when the capability is off', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    expect(screen.queryByLabelText('Search by interest')).not.toBeInTheDocument();
  });
});
