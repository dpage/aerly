import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { PoiResponse } from '../api/types';

const h = vi.hoisted(() => ({
  fetchPois: vi.fn<[], Promise<PoiResponse>>(),
}));
vi.mock('../api/client', () => ({ api: { fetchPois: h.fetchPois } }));
// AddToTripDialog is heavy; stub it so we assert it opens with the right prefill.
vi.mock('./AddToTripDialog', () => ({
  default: ({ open, prefill }: { open: boolean; prefill?: { title: string } }) =>
    open ? <div data-testid="add-dialog">{prefill?.title}</div> : null,
}));

import ExplorePanel from './ExplorePanel';

beforeEach(() => {
  vi.clearAllMocks();
  h.fetchPois.mockResolvedValue({
    center: { lat: 51.5, lon: -0.12 },
    center_label: 'London',
    pois: [
      {
        id: 'node/1',
        name: 'Example Tower',
        category: 'sights',
        lat: 51.5,
        lon: -0.12,
        distance_m: 40,
        address: '1 Example Square',
        wikidata: 'Q1',
        wikipedia: 'en:Example Article',
        website: 'https://example.com',
      },
      {
        id: 'node/2',
        name: 'Big Museum',
        category: 'museum',
        lat: 51.51,
        lon: -0.1,
        distance_m: 1800,
      },
      {
        id: 'node/3',
        name: 'Old Castle',
        category: 'landmark',
        lat: 51.52,
        lon: -0.11,
        distance_m: 500,
      },
      {
        id: 'node/4',
        name: 'Green Park',
        category: 'park',
        lat: 51.53,
        lon: -0.13,
        distance_m: 600,
      },
      {
        id: 'node/5',
        name: 'Corner Cafe',
        category: 'food',
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
    expect(towerLinks.some((l) => l.getAttribute('href') === 'https://www.openstreetmap.org/node/1')).toBe(
      true,
    );
    expect(
      towerLinks.some((l) => l.getAttribute('href') === 'https://www.wikidata.org/wiki/Q1'),
    ).toBe(true);
    expect(
      towerLinks.some(
        (l) => l.getAttribute('href') === 'https://en.wikipedia.org/wiki/Example_Article',
      ),
    ).toBe(true);
    expect(towerLinks.some((l) => l.getAttribute('href') === 'https://example.com')).toBe(true);
    // the row caption uses the polished category label, not the raw key
    expect(screen.getByText('Sights · 40 m away')).toBeInTheDocument();
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

  it('re-fetches with updated categories when a category chip is toggled off', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    await userEvent.click(screen.getByRole('button', { name: /^food$/i }));
    expect(h.fetchPois).toHaveBeenCalledWith(
      7,
      expect.objectContaining({ cats: expect.arrayContaining(['food']) }),
    );

    h.fetchPois.mockClear();
    await userEvent.click(screen.getByRole('button', { name: /^sights$/i }));
    const lastCall = h.fetchPois.mock.calls[0][1] as { cats?: string[] };
    expect(lastCall.cats).not.toContain('sights');
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
    const placeField = screen.getByLabelText(/place/i);
    await userEvent.clear(placeField);
    await userEvent.type(placeField, 'Paris');
    await userEvent.click(screen.getByRole('button', { name: /search/i }));
    expect(h.fetchPois).toHaveBeenCalledWith(7, expect.objectContaining({ place: 'Paris' }));
  });

  it('does not re-fetch on every keystroke in the place field', async () => {
    render(<ExplorePanel tripId={7} initialPlace="London" />);
    await screen.findByText('Example Tower');
    h.fetchPois.mockClear();
    const placeField = screen.getByLabelText(/place/i);
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
});
