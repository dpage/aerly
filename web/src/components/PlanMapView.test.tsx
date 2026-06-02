import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { PlanPart, User } from '../api/types';
import maplibreMock, { FakeMap, FakeMarker, resetMaplibreMock } from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

import PlanMapView from './PlanMapView';

function user(over: Partial<User> = {}): User {
  return {
    id: 1,
    username: 'u',
    name: 'User',
    avatar_url: '',
    is_superuser: false,
    is_active: true,
    has_logged_in: true,
    home_address: '',
    ...over,
  };
}

function flight(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'flight',
    seq: 0,
    starts_at: '2026-10-12T09:00:00Z',
    ends_at: '2026-10-12T13:00:00Z',
    start_tz: 'Europe/London',
    end_tz: 'America/New_York',
    start_label: 'LHR',
    end_label: 'IAD',
    start_lat: 51.47,
    start_lon: -0.45,
    end_lat: 38.95,
    end_lon: -77.46,
    start_address: '',
    end_address: '',
    status: 'planned',
    effective_at: '2026-10-12T09:00:00Z',
    flight: {
      ident: 'BA217',
      callsign: 'BAW217',
      scheduled_out: '2026-10-12T09:00:00Z',
      scheduled_in: '2026-10-12T13:00:00Z',
      origin_iata: 'LHR',
      dest_iata: 'IAD',
      flight_status: 'Scheduled',
      track: [
        { ts: '2026-10-12T10:00:00Z', lat: 50, lon: -10, is_estimated: false },
        { ts: '2026-10-12T11:00:00Z', lat: 48, lon: -30, is_estimated: false },
      ],
    },
    ...over,
  };
}

function hotel(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 2,
    plan_id: 2,
    type: 'hotel',
    seq: 0,
    starts_at: '2026-10-12T15:00:00Z',
    ends_at: '2026-10-15T11:00:00Z',
    start_tz: 'America/New_York',
    end_tz: 'America/New_York',
    start_label: 'Tysons Marriott',
    end_label: '',
    start_lat: 38.91,
    start_lon: -77.22,
    start_address: '8028 Leesburg Pike',
    end_address: '',
    status: 'planned',
    effective_at: '2026-10-12T15:00:00Z',
    hotel: {
      property_name: 'Tysons Marriott',
      address: '8028 Leesburg Pike',
      phone: '+1 703 555 0100',
      room_type: 'King',
    },
    ...over,
  };
}

beforeEach(() => {
  resetMaplibreMock();
});

describe('PlanMapView', () => {
  it('lists mappable parts in time order with type + time', () => {
    render(<PlanMapView parts={[hotel(), flight()]} />);
    const rows = screen.getAllByTestId(/^plan-row-/);
    // Flight (09:00) sorts before the hotel (15:00) regardless of input order.
    expect(rows[0]).toHaveAttribute('data-testid', 'plan-row-1');
    expect(rows[1]).toHaveAttribute('data-testid', 'plan-row-2');
    expect(screen.getByText('BA217')).toBeInTheDocument();
    expect(screen.getByText('Tysons Marriott')).toBeInTheDocument();
  });

  it('expands a flight to the flight detail card and fits the map to its path', async () => {
    const user = userEvent.setup();
    render(<PlanMapView parts={[flight(), hotel()]} />);
    const map = FakeMap.instances[0];
    map.fitBounds.mockClear();
    await user.click(screen.getByTestId('plan-row-1'));
    expect(screen.getByTestId('flight-detail-card')).toBeInTheDocument();
    // A transfer with both ends fits bounds (the whole path), not flyTo.
    expect(map.fitBounds).toHaveBeenCalled();
  });

  it('expands a non-flight to its detail block and flies to the point', async () => {
    const user = userEvent.setup();
    render(<PlanMapView parts={[flight(), hotel()]} />);
    const map = FakeMap.instances[0];
    map.flyTo.mockClear();
    await user.click(screen.getByTestId('plan-row-2'));
    const block = screen.getByTestId('part-detail-block');
    expect(within(block).getByText('+1 703 555 0100')).toBeInTheDocument();
    expect(map.flyTo).toHaveBeenCalled();
  });

  // Find the teardrop pin marker for a given part id.
  const pinFor = (partId: number) =>
    FakeMarker.instances.find((m) => m.getElement()?.dataset.partId === String(partId))!;

  it('plots both endpoints of a ground transfer with a grey crow-flight leg', () => {
    const ground: PlanPart = {
      ...hotel(),
      id: 3,
      plan_id: 3,
      type: 'ground',
      start_label: 'Alicante Airport',
      end_label: 'Melia Benidorm',
      start_lat: 38.28,
      start_lon: -0.55,
      end_lat: 38.54,
      end_lon: -0.1,
      hotel: undefined,
    };
    render(<PlanMapView parts={[ground]} />);
    // Both pickup and drop-off pins are plotted.
    const pins = FakeMarker.instances.filter((m) => m.getElement()?.dataset.partId === '3');
    expect(pins).toHaveLength(2);
    expect(pins.map((p) => p.lngLat)).toEqual(
      expect.arrayContaining([
        [-0.55, 38.28],
        [-0.1, 38.54],
      ]),
    );
    // The connecting leg is drawn grey (a crow-flight connector, not a route).
    const legsSrc = FakeMap.instances[0].getSource('pmv-legs')!;
    const lastData = legsSrc.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
    const leg = lastData.features.find((f) => f.properties?.partId === 3);
    expect(leg?.properties?.color).toBe('#9e9e9e');
  });

  it('clicking a pin highlights its list row (bidirectional)', async () => {
    render(<PlanMapView parts={[flight(), hotel()]} />);
    pinFor(2).getElement().dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(await screen.findByTestId('part-detail-block')).toBeInTheDocument();
    expect(screen.getByTestId('plan-row-2')).toHaveClass('Mui-selected');
  });

  it('clicking a transfer leg highlights its flight row', async () => {
    render(<PlanMapView parts={[flight(), hotel()]} />);
    const map = FakeMap.instances[0];
    map.fireLayer('click', 'pmv-legs', { features: [{ properties: { partId: 1 } }] });
    expect(await screen.findByTestId('flight-detail-card')).toBeInTheDocument();
    expect(screen.getByTestId('plan-row-1')).toHaveClass('Mui-selected');
  });

  it('builds a labelled popover for each pin', () => {
    render(<PlanMapView parts={[hotel()]} />);
    const pin = pinFor(2);
    // setPopup was given DOM content with the venue's title + type.
    expect(pin.popup?.html).toContain('Tysons Marriott');
    expect(pin.popup?.html).toContain('Hotel');
  });

  it('shows an empty state when there are no mappable parts', () => {
    render(<PlanMapView parts={[]} />);
    expect(screen.getByText(/no mappable plans/i)).toBeInTheDocument();
  });

  it('shows the plan owner (text) and passenger avatars on the list row', () => {
    render(
      <PlanMapView
        parts={[
          flight({
            owner: user({ id: 9, name: 'Dave Page' }),
            passengers: [
              user({ id: 2, name: 'Bob', avatar_url: 'https://gravatar/bob.png' }),
              user({ id: 3, name: 'Carol' }),
            ],
          }),
        ]}
      />,
    );
    const row = screen.getByTestId('plan-row-1');
    expect(row).toHaveTextContent(/Added by Dave Page/);
    // Passenger avatars: the one with a url renders an <img>.
    expect(row.querySelector('img[src="https://gravatar/bob.png"]')).not.toBeNull();
  });

  it('shows a loading state (not the empty copy) while parts are pending', () => {
    render(<PlanMapView parts={[]} loading />);
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
    expect(screen.queryByText(/no mappable plans/i)).not.toBeInTheDocument();
  });

  it('pre-selects a part from initialSelectedPartId on mount', () => {
    render(<PlanMapView parts={[flight(), hotel()]} initialSelectedPartId={2} />);
    expect(screen.getByTestId('plan-row-2')).toHaveClass('Mui-selected');
    expect(screen.getByTestId('part-detail-block')).toBeInTheDocument();
  });

  it('renders the optional controls slot above the list', () => {
    render(<PlanMapView parts={[flight()]} controls={<div data-testid="ctrl" />} />);
    expect(screen.getByTestId('ctrl')).toBeInTheDocument();
  });

  it('plots a part that has only an end coordinate (no start)', () => {
    const endOnly = hotel({
      id: 3,
      start_lat: undefined,
      start_lon: undefined,
      end_lat: 40.7,
      end_lon: -74,
    });
    render(<PlanMapView parts={[endOnly]} />);
    expect(screen.getByTestId('plan-row-3')).toBeInTheDocument();
  });

  it('toggles a selection off when its row is clicked twice', async () => {
    const user = userEvent.setup();
    render(<PlanMapView parts={[flight(), hotel()]} />);
    await user.click(screen.getByTestId('plan-row-2'));
    expect(screen.getByTestId('plan-row-2')).toHaveClass('Mui-selected');
    await user.click(screen.getByTestId('plan-row-2'));
    expect(screen.getByTestId('plan-row-2')).not.toHaveClass('Mui-selected');
  });

  it('clicking the same pin twice deselects it', async () => {
    render(<PlanMapView parts={[flight(), hotel()]} />);
    const el = pinFor(2).getElement();
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(await screen.findByTestId('plan-row-2')).toHaveClass('Mui-selected');
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await waitFor(() =>
      expect(screen.getByTestId('plan-row-2')).not.toHaveClass('Mui-selected'),
    );
  });

  it('ignores a leg click whose feature has no numeric partId', () => {
    render(<PlanMapView parts={[flight(), hotel()]} />);
    const map = FakeMap.instances[0];
    map.fireLayer('click', 'pmv-legs', { features: [{ properties: {} }] });
    map.fireLayer('click', 'pmv-legs', { features: undefined });
    expect(screen.queryByTestId('part-detail-block')).not.toBeInTheDocument();
  });

  it('sets/clears the pointer cursor on leg hover', () => {
    render(<PlanMapView parts={[flight()]} />);
    const map = FakeMap.instances[0];
    map.fireLayer('mouseenter', 'pmv-legs', {});
    expect(map.getCanvas().style.cursor).toBe('pointer');
    map.fireLayer('mouseleave', 'pmv-legs', {});
    expect(map.getCanvas().style.cursor).toBe('');
  });

  it('defers the fit to the idle event when the style is not yet loaded', async () => {
    const user = userEvent.setup();
    render(<PlanMapView parts={[flight(), hotel()]} />);
    const map = FakeMap.instances[0];
    map.styleLoaded = false; // force the once('idle', run) deferral path
    map.flyTo.mockClear();
    map.fitBounds.mockClear();
    await user.click(screen.getByTestId('plan-row-2'));
    // once('idle', …) fires synchronously in the mock, so the deferred fit still runs.
    expect(map.flyTo).toHaveBeenCalled();
  });

  it('draws a single-arc transfer leg as a LineString (no antimeridian split)', () => {
    // A short east-west hop stays one arc → the arc.length === 1 LineString branch.
    const shortHop = flight({
      id: 4,
      flight: undefined,
      start_lat: 51.5,
      start_lon: -0.1,
      end_lat: 48.9,
      end_lon: 2.4,
    });
    render(<PlanMapView parts={[shortHop]} />);
    const map = FakeMap.instances[0];
    const legs = map.getSource('pmv-legs');
    const lastCall = legs!.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(lastCall.features).toHaveLength(1);
    expect((lastCall.features[0].geometry as GeoJSON.LineString).type).toBe('LineString');
  });

  it('orders parts by starts_at when effective_at is absent, and titles by place/type', () => {
    // No effective_at → sort falls back to starts_at; a label-less, non-flight
    // part titles from its type (the fmtPartPlaces || planTypeLabel fallback).
    const bare = hotel({
      id: 6,
      type: 'excursion',
      hotel: undefined,
      excursion: { provider: 'GetYourGuide' },
      effective_at: undefined as unknown as string,
      starts_at: '2026-10-12T08:00:00Z',
      start_label: '',
      end_label: '',
      start_lat: 40,
      start_lon: -3,
      end_lat: undefined,
      end_lon: undefined,
    });
    render(<PlanMapView parts={[hotel(), bare]} />);
    const rows = screen.getAllByTestId(/^plan-row-/);
    // bare (08:00) sorts before hotel (15:00).
    expect(rows[0]).toHaveAttribute('data-testid', 'plan-row-6');
    // Title fell back to the type label, not a place line.
    expect(within(rows[0]).getAllByText(/Excursion/i).length).toBeGreaterThan(0);
  });

  it('clears a stale selection when the selected part disappears from the set', async () => {
    const { rerender } = render(
      <PlanMapView parts={[flight(), hotel()]} initialSelectedPartId={2} />,
    );
    expect(screen.getByTestId('plan-row-2')).toHaveClass('Mui-selected');
    // The hotel (id 2) drops out of the parts → selection must clear.
    rerender(<PlanMapView parts={[flight()]} />);
    await waitFor(() => expect(screen.queryByTestId('plan-row-2')).not.toBeInTheDocument());
    expect(screen.getByTestId('plan-row-1')).not.toHaveClass('Mui-selected');
  });

  it('draws an antimeridian-crossing transfer as a MultiLineString', () => {
    // A trans-Pacific hop crosses the antimeridian → great-circle splits into
    // two arcs → the arc.length > 1 MultiLineString branch.
    const transPacific = flight({
      id: 7,
      flight: undefined,
      start_lat: 35.6,
      start_lon: 139.7, // Tokyo
      end_lat: 37.6,
      end_lon: -122.4, // San Francisco
    });
    render(<PlanMapView parts={[transPacific]} />);
    const map = FakeMap.instances[0];
    const legs = map.getSource('pmv-legs');
    const lastCall = legs!.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(lastCall.features).toHaveLength(1);
    expect(lastCall.features[0].geometry.type).toBe('MultiLineString');
  });

  it('skips a transfer leg whose endpoints coincide (no arc)', () => {
    // Identical start/end → great-circle yields no drawable arc → skipped.
    const degenerate = flight({
      id: 8,
      flight: undefined,
      start_lat: 51.5,
      start_lon: -0.1,
      end_lat: 51.5,
      end_lon: -0.1,
    });
    render(<PlanMapView parts={[degenerate]} />);
    const map = FakeMap.instances[0];
    const legs = map.getSource('pmv-legs');
    const lastCall = legs!.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(lastCall.features).toHaveLength(0);
  });

  it('omits the flown track when a selected flight has fewer than two track points', async () => {
    const user = userEvent.setup();
    const sparse = flight({
      id: 5,
      flight: {
        ident: 'BA1',
        callsign: '',
        scheduled_out: '2026-10-12T09:00:00Z',
        scheduled_in: '2026-10-12T13:00:00Z',
        origin_iata: 'LHR',
        dest_iata: 'IAD',
        flight_status: 'Scheduled',
        track: [{ ts: '2026-10-12T10:00:00Z', lat: 50, lon: -10, is_estimated: false }],
      },
    });
    render(<PlanMapView parts={[sparse]} />);
    const map = FakeMap.instances[0];
    const track = map.getSource('pmv-track');
    await user.click(screen.getByTestId('plan-row-5'));
    const lastCall = track!.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
    expect(lastCall.features).toHaveLength(0);
  });
});
