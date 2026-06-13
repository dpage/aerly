import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, waitFor, within, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setMatchMedia } from '../test/setup';

import type { PlanPart, Position, User } from '../api/types';
import { initialBearing } from '../lib/great-circle';
import maplibreMock, {
  FakeAttributionControl,
  FakeMap,
  FakeMarker,
  resetMaplibreMock,
} from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

import PlanMapView from './PlanMapView';
import { fmtScrubTime, planePlacementAt, positionAt, trackFC, tracksFC } from '../lib/flight-track';

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
  it('re-homes the OSM attribution off the bottom edge (clear of the time slider)', () => {
    render(<PlanMapView parts={[flight()]} />);
    const map = FakeMap.instances[0];
    // The default bottom-right attribution is disabled and a compact one added
    // at the top, so its ⓘ + credit don't poke out from under the slider.
    expect(map.opts).toMatchObject({ attributionControl: false });
    const attribution = map.controls.find((c) => c instanceof FakeAttributionControl);
    expect(attribution).toBeDefined();
    expect((attribution as FakeAttributionControl).opts).toMatchObject({ compact: true });
    expect(map.controlPositions.get(attribution)).toBe('top-left');
  });

  it('moves the OSM attribution to the bottom-right on mobile (clear of the filter pill)', () => {
    setMatchMedia(true);
    render(<PlanMapView parts={[flight()]} />);
    const map = FakeMap.instances[0];
    const attribution = map.controls.find((c) => c instanceof FakeAttributionControl);
    expect(attribution).toBeDefined();
    expect(map.controlPositions.get(attribution)).toBe('bottom-right');
  });

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

  it('does not re-fit the map when a selected flight refreshes (keeps a manual zoom)', async () => {
    const user = userEvent.setup();
    const { rerender } = render(<PlanMapView parts={[flight(), hotel()]} />);
    const map = FakeMap.instances[0];
    await user.click(screen.getByTestId('plan-row-1'));
    expect(map.fitBounds).toHaveBeenCalled(); // the initial fit on selection
    // The user has now zoomed in by hand; clear the spy and simulate a live
    // tracking refresh (new part objects with an updated position + track).
    map.fitBounds.mockClear();
    map.flyTo.mockClear();
    rerender(
      <PlanMapView
        parts={[
          flight({
            flight: {
              ...flight().flight!,
              flight_status: 'Enroute',
              latest_position: {
                ts: '2026-10-12T11:31:00Z',
                lat: 47,
                lon: -35,
                is_estimated: false,
              },
              track: [
                ...flight().flight!.track!,
                { ts: '2026-10-12T11:31:00Z', lat: 47, lon: -35, is_estimated: false },
              ],
            },
          }),
          hotel(),
        ]}
      />,
    );
    // The selection is unchanged, so the refresh must not move the camera.
    expect(map.fitBounds).not.toHaveBeenCalled();
    expect(map.flyTo).not.toHaveBeenCalled();
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

  // Find the plane-icon marker for a given flight part.
  const planeFor = (partId: number) =>
    FakeMarker.instances.find(
      (m) =>
        m.getElement()?.dataset.role === 'plane' &&
        m.getElement()?.dataset.partId === String(partId),
    );

  // The default flight departs 09:00, arrives 13:00 (2026-10-12). The plane icon
  // is only shown inside its window (2h before departure … 2h after arrival), so
  // these tests pin the clock there. Fake timers also satisfy the minute-tick
  // interval the component installs.
  describe('plane icon (within its visibility window)', () => {
    afterEach(() => {
      vi.useRealTimers();
    });
    const at = (iso: string) => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date(iso));
    };

    it('parks the plane icon at the origin, along the route, before departure', () => {
      at('2026-10-12T08:00:00Z'); // 1h before departure → inside the window
      render(<PlanMapView parts={[flight()]} />); // Scheduled, no live position
      const plane = planeFor(1);
      expect(plane).toBeDefined();
      expect(plane!.lngLat).toEqual([-0.45, 51.47]); // origin (LHR)
      expect(plane!.getElement().dataset.estimated).toBe('0');
      // Oriented along the great-circle to IAD (≈west), not due north.
      expect(plane!.rotation).toBeCloseTo(initialBearing(51.47, -0.45, 38.95, -77.46), 5);
    });

    it('draws the plane at the live position, rotated and dimmed when dead-reckoned', () => {
      at('2026-10-12T11:30:00Z'); // mid-flight
      render(
        <PlanMapView
          parts={[
            flight({
              flight: {
                ...flight().flight!,
                flight_status: 'Enroute',
                latest_position: {
                  ts: '2026-10-12T11:30:00Z',
                  lat: 48,
                  lon: -30,
                  heading_deg: 270,
                  is_estimated: true,
                },
              },
            }),
          ]}
        />,
      );
      const plane = planeFor(1)!;
      expect(plane.lngLat).toEqual([-30, 48]);
      expect(plane.rotation).toBe(270); // live ADS-B heading, not the route bearing
      expect(plane.getElement().dataset.estimated).toBe('1'); // dead-reckoned → dimmed
    });

    it('parks the plane at the destination once the flight has arrived', () => {
      at('2026-10-12T13:30:00Z'); // 30m after arrival → still inside the window
      render(
        <PlanMapView
          parts={[flight({ flight: { ...flight().flight!, flight_status: 'Arrived' } })]}
        />,
      );
      expect(planeFor(1)!.lngLat).toEqual([-77.46, 38.95]); // destination (IAD)
    });

    it('orients the live fix along the route when the fix carries no heading', () => {
      at('2026-10-12T11:30:00Z'); // mid-flight, position without heading_deg
      render(
        <PlanMapView
          parts={[
            flight({
              flight: {
                ...flight().flight!,
                flight_status: 'Enroute',
                latest_position: {
                  ts: '2026-10-12T11:30:00Z',
                  lat: 48,
                  lon: -30,
                  is_estimated: false,
                },
              },
            }),
          ]}
        />,
      );
      const plane = planeFor(1)!;
      expect(plane.lngLat).toEqual([-30, 48]);
      // No ADS-B heading → fall back to the route bearing (LHR → IAD).
      expect(plane.rotation).toBeCloseTo(initialBearing(51.47, -0.45, 38.95, -77.46), 5);
    });

    it('parks an end-only flight at its destination, pointing north (no route bearing)', () => {
      at('2026-10-12T08:00:00Z'); // before departure, inside the window
      render(<PlanMapView parts={[flight({ start_lat: undefined, start_lon: undefined })]} />);
      const plane = planeFor(1)!;
      expect(plane.lngLat).toEqual([-77.46, 38.95]); // destination (IAD)
      expect(plane.rotation).toBe(0); // only one endpoint → no bearing, due north
    });

    it('windows a flight with no known arrival on departure + 2h', () => {
      // No arrival time anywhere (no actual/estimated/scheduled_in, no ends_at):
      // the window falls back to [departure − 2h, departure + 2h].
      const noArr = () =>
        flight({
          ends_at: undefined,
          flight: { ...flight().flight!, scheduled_in: undefined as unknown as string },
        });
      at('2026-10-12T10:30:00Z'); // 1.5h after departure → still inside
      const { unmount } = render(<PlanMapView parts={[noArr()]} />);
      expect(planeFor(1)).toBeDefined();
      unmount();
      resetMaplibreMock();
      at('2026-10-12T11:30:00Z'); // 2.5h after departure → past the window
      render(<PlanMapView parts={[noArr()]} />);
      expect(planeFor(1)).toBeUndefined();
    });

    it('hides the plane before its window opens and after it closes', () => {
      at('2026-10-12T06:00:00Z'); // 3h before departure → window opens at 07:00
      const { unmount } = render(<PlanMapView parts={[flight()]} />);
      expect(planeFor(1)).toBeUndefined();
      unmount();
      resetMaplibreMock();
      at('2026-10-12T16:00:00Z'); // 3h after arrival → window closed at 15:00
      render(<PlanMapView parts={[flight()]} />);
      expect(planeFor(1)).toBeUndefined();
    });

    it('shows only one plane across a connection, handing off at the layover midpoint', () => {
      // Two legs of one booking (same plan_id): LHR→DXB (09:00–13:00) then
      // DXB→SYD (14:00–22:00). The windows overlap, so the handoff is the
      // midpoint of the layover (13:00…14:00) = 13:30.
      const leg1 = flight({ id: 1, plan_id: 7 });
      const leg2 = flight({
        id: 2,
        plan_id: 7,
        seq: 1,
        starts_at: '2026-10-12T14:00:00Z',
        ends_at: '2026-10-12T22:00:00Z',
        effective_at: '2026-10-12T14:00:00Z',
        start_label: 'IAD',
        end_label: 'SYD',
        start_lat: 38.95,
        start_lon: -77.46,
        end_lat: -33.95,
        end_lon: 151.18,
        flight: {
          ...flight().flight!,
          ident: 'QF8',
          scheduled_out: '2026-10-12T14:00:00Z',
          scheduled_in: '2026-10-12T22:00:00Z',
          origin_iata: 'IAD',
          dest_iata: 'SYD',
        },
      });

      at('2026-10-12T13:15:00Z'); // just before the handoff → only leg 1
      const { unmount } = render(<PlanMapView parts={[leg1, leg2]} />);
      expect(planeFor(1)).toBeDefined();
      expect(planeFor(2)).toBeUndefined();
      unmount();
      resetMaplibreMock();

      at('2026-10-12T13:45:00Z'); // just after the handoff → only leg 2
      render(<PlanMapView parts={[leg1, leg2]} />);
      expect(planeFor(1)).toBeUndefined();
      expect(planeFor(2)).toBeDefined();
    });
  });

  it('draws no plane icon for a non-flight part', () => {
    render(<PlanMapView parts={[hotel()]} />);
    expect(planeFor(2)).toBeUndefined();
  });

  // The time scrubber lets you drag back and replay where flights were. The
  // default flight departs 09:00, arrives 13:00 (2026-10-12). These tests pin
  // the clock so the slider's window (trip start … min(now, trip end)) is open.
  describe('time slider', () => {
    afterEach(() => {
      vi.useRealTimers();
    });
    const clockAt = (iso: string) => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date(iso));
    };
    const ms = (iso: string) => new Date(iso).getTime();
    // A mid-flight fixture: an Enroute leg with a live fix and a 3-point track.
    const enroute = (over: Partial<PlanPart> = {}) =>
      flight({
        flight: {
          ...flight().flight!,
          flight_status: 'Enroute',
          latest_position: { ts: '2026-10-12T11:30:00Z', lat: 47, lon: -35, is_estimated: false },
          track: [
            { ts: '2026-10-12T10:00:00Z', lat: 50, lon: -10, is_estimated: false },
            { ts: '2026-10-12T11:00:00Z', lat: 48, lon: -30, is_estimated: false },
            { ts: '2026-10-12T11:30:00Z', lat: 47, lon: -35, is_estimated: false },
          ],
        },
        ...over,
      });
    const scrubTo = (iso: string) =>
      fireEvent.change(screen.getByRole('slider'), { target: { value: String(ms(iso)) } });

    it('shows the slider only once a flight trip has started', () => {
      clockAt('2026-10-12T11:30:00Z'); // mid-flight → window [09:00 … 11:30] open
      render(<PlanMapView parts={[enroute()]} />);
      expect(screen.getByTestId('time-slider')).toBeInTheDocument();
    });

    it('hides the slider with no flights, or before the trip starts', () => {
      clockAt('2026-10-12T11:30:00Z');
      const { unmount } = render(<PlanMapView parts={[hotel()]} />); // nothing moves
      expect(screen.queryByTestId('time-slider')).toBeNull();
      unmount();
      resetMaplibreMock();
      clockAt('2026-10-01T00:00:00Z'); // before the flight → no past to scrub
      render(<PlanMapView parts={[enroute()]} />);
      expect(screen.queryByTestId('time-slider')).toBeNull();
    });

    it('pins the right edge to the live view while the flight is in progress', () => {
      clockAt('2026-10-12T11:30:00Z');
      render(<PlanMapView parts={[enroute()]} />);
      // Live: the LIVE badge, no reset button, and the plane at its live fix.
      expect(screen.getByTestId('time-slider-live')).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: 'Live' })).toBeNull();
      expect(planeFor(1)!.lngLat).toEqual([-35, 47]);
    });

    it('replays the plane along its flown track when scrubbed back', () => {
      clockAt('2026-10-12T11:30:00Z');
      render(<PlanMapView parts={[enroute(), hotel()]} />); // hotel: a static part
      fireEvent.click(screen.getByTestId('plan-row-1')); // select → draw the track
      scrubTo('2026-10-12T10:30:00Z'); // midway between the 10:00 and 11:00 fixes
      // Interpolated half-way: lat 50→48, lon -10→-30.
      expect(planeFor(1)!.lngLat).toEqual([-20, 49]);
      // The orange trail is clipped to the scrubbed instant + an interpolated tip.
      const track = FakeMap.instances[0].getSource('pmv-track')!;
      const fc = track.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
      expect((fc.features[0].geometry as GeoJSON.LineString).coordinates).toEqual([
        [-10, 50],
        [-20, 49],
      ]);
      // Scrubbing swaps the LIVE badge for the scrubbed time + a re-lock button.
      expect(screen.queryByTestId('time-slider-live')).toBeNull();
      expect(screen.getByText('Positions at')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Live' })).toBeInTheDocument();
    });

    it('draws the trail of an active flight while scrubbing, even with nothing selected', () => {
      clockAt('2026-10-12T11:30:00Z');
      const second = enroute({
        id: 2,
        plan_id: 2,
        flight: {
          ...enroute().flight!,
          track: [
            pos('2026-10-12T10:00:00Z', 60, 5),
            pos('2026-10-12T11:00:00Z', 58, 10),
            pos('2026-10-12T11:30:00Z', 57, 12),
          ],
        },
      });
      render(<PlanMapView parts={[enroute(), second]} />); // no selection
      // Live: only the selected flight's trail is ever drawn — here that's none.
      const track = FakeMap.instances[0].getSource('pmv-track')!;
      let fc = track.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
      expect(fc.features).toHaveLength(0);
      // Scrub back: both active flights' trails appear, keyed by part id.
      scrubTo('2026-10-12T10:30:00Z');
      fc = track.setData.mock.calls.at(-1)![0] as GeoJSON.FeatureCollection;
      expect(fc.features.map((f) => f.properties?.partId).sort()).toEqual([1, 2]);
    });

    it('re-locks to live when dragged back to the right edge', () => {
      clockAt('2026-10-12T11:30:00Z');
      render(<PlanMapView parts={[enroute()]} />);
      scrubTo('2026-10-12T10:30:00Z');
      expect(screen.queryByTestId('time-slider-live')).toBeNull();
      scrubTo('2026-10-12T11:30:00Z'); // the live edge
      expect(screen.getByTestId('time-slider-live')).toBeInTheDocument();
    });

    it('re-locks to live via the Live button', () => {
      clockAt('2026-10-12T11:30:00Z');
      render(<PlanMapView parts={[enroute()]} />);
      scrubTo('2026-10-12T10:30:00Z');
      fireEvent.click(screen.getByRole('button', { name: 'Live' }));
      expect(screen.getByTestId('time-slider-live')).toBeInTheDocument();
      expect(planeFor(1)!.lngLat).toEqual([-35, 47]); // back at the live fix
    });

    it('drops the scrub back to live when the parts (trip/tag) change', () => {
      clockAt('2026-10-12T11:30:00Z');
      const { rerender } = render(<PlanMapView parts={[enroute()]} />);
      scrubTo('2026-10-12T10:30:00Z');
      expect(screen.queryByTestId('time-slider-live')).toBeNull();
      // A different dataset (new start instant) → the scrub resets to the live edge.
      rerender(
        <PlanMapView
          parts={[
            enroute({
              id: 9,
              starts_at: '2026-10-12T08:00:00Z',
              effective_at: '2026-10-12T08:00:00Z',
            }),
          ]}
        />,
      );
      expect(screen.getByTestId('time-slider-live')).toBeInTheDocument();
    });

    it('offers a "Latest" reset for a wholly past trip and replays it on scrub', () => {
      clockAt('2026-10-12T20:00:00Z'); // hours after arrival → the trip is past
      render(<PlanMapView parts={[enroute()]} />);
      // Past trip: no LIVE badge, and the reset reads "Latest", not "Live".
      expect(screen.queryByTestId('time-slider-live')).toBeNull();
      expect(screen.getByRole('button', { name: 'Latest' })).toBeInTheDocument();
      // Default (now is past the window) shows no plane until you scrub in.
      expect(planeFor(1)).toBeUndefined();
      scrubTo('2026-10-12T10:30:00Z'); // replay mid-flight → interpolated on the track
      const plane = planeFor(1)!;
      expect(plane.lngLat).toEqual([-20, 49]);
      fireEvent.click(screen.getByRole('button', { name: 'Latest' }));
      expect(plane.remove).toHaveBeenCalled(); // reset → back to the (empty) live edge
    });

    it('parks the plane at the origin when scrubbed before departure', () => {
      clockAt('2026-10-12T11:30:00Z');
      // A hotel from 08:00 widens the window so we can scrub before the 09:00 push-back.
      render(
        <PlanMapView
          parts={[
            enroute(),
            hotel({
              id: 3,
              starts_at: '2026-10-12T08:00:00Z',
              effective_at: '2026-10-12T08:00:00Z',
            }),
          ]}
        />,
      );
      scrubTo('2026-10-12T08:30:00Z'); // before departure → parked at LHR
      expect(planeFor(1)!.lngLat).toEqual([-0.45, 51.47]);
    });
  });

  it('clicking a pin highlights its list row (bidirectional)', async () => {
    render(<PlanMapView parts={[flight(), hotel()]} />);
    pinFor(2)
      .getElement()
      .dispatchEvent(new MouseEvent('click', { bubbles: true }));
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

  it('shows the unlocated notice when an addressed part has no coordinates', () => {
    const stranded = hotel({
      id: 3,
      start_lat: undefined,
      start_lon: undefined,
      start_address: '8028 Leesburg Pike',
    });
    render(<PlanMapView parts={[flight(), stranded]} />);
    expect(screen.getByText(/couldn't be placed on the map/i)).toBeInTheDocument();
  });

  it('hides the unlocated notice when every part is located', () => {
    render(<PlanMapView parts={[flight(), hotel()]} />);
    expect(screen.queryByText(/couldn't be placed on the map/i)).toBeNull();
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

  it('shows the booked supplier (airline) on the list row when present', () => {
    render(<PlanMapView parts={[flight({ supplier_name: 'Ryanair' })]} />);
    expect(screen.getByTestId('plan-row-1')).toHaveTextContent('Ryanair');
  });

  it('omits the supplier segment on the list row when unknown', () => {
    render(<PlanMapView parts={[flight()]} />);
    const row = screen.getByTestId('plan-row-1');
    // No empty " ·  · " segment — the join drops the blank supplier.
    expect(row.textContent).not.toMatch(/·\s+·/);
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
    await waitFor(() => expect(screen.getByTestId('plan-row-2')).not.toHaveClass('Mui-selected'));
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

  describe('mobile bottom-sheet layout', () => {
    beforeEach(() => {
      setMatchMedia(true);
      // jsdom's clientHeight is always 0; shim it to a realistic value so
      // sheetPad is non-zero and the offset/padding branches under test are reached.
      Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
        configurable: true,
        value: 800,
      });
    });
    afterEach(() => {
      vi.useRealTimers();
      delete (HTMLElement.prototype as { clientHeight?: number }).clientHeight;
    });

    it('puts the list in a bottom sheet at peek, with a plan-count summary', () => {
      render(<PlanMapView parts={[flight(), hotel()]} />);
      expect(screen.getByTestId('bottom-sheet')).toHaveAttribute('data-snap', 'peek');
      expect(screen.getByTestId('sheet-peek')).toHaveTextContent('2 plans');
      // The list itself still renders (inside the sheet) for when it's raised.
      expect(screen.getByTestId('plan-row-1')).toBeInTheDocument();
    });

    it('shows the selected plan one-liner in the peek bar', () => {
      render(<PlanMapView parts={[flight(), hotel()]} initialSelectedPartId={1} />);
      const peek = screen.getByTestId('sheet-peek');
      expect(peek).toHaveTextContent('BA217');
      expect(peek).toHaveTextContent('Flight');
    });

    it('shows a loading peek before the parts arrive', () => {
      render(<PlanMapView parts={[]} loading />);
      expect(screen.getByTestId('sheet-peek')).toHaveTextContent('Loading…');
    });

    it('shows "No plans" in the peek bar when nothing is mappable', () => {
      render(<PlanMapView parts={[]} />);
      expect(screen.getByTestId('sheet-peek')).toHaveTextContent('No plans');
    });

    it('tapping the peek bar raises the sheet to half', () => {
      render(<PlanMapView parts={[flight()]} />);
      const handle = screen.getByTestId('sheet-handle');
      fireEvent.pointerDown(handle, { pointerId: 1, clientY: 500 });
      fireEvent.pointerUp(handle, { pointerId: 1, clientY: 500 });
      expect(screen.getByTestId('bottom-sheet')).toHaveAttribute('data-snap', 'half');
    });

    it('rides the time scrubber above the sheet and hides it at full', () => {
      // The scrubber only shows once there's a past to look back over: pin the
      // clock mid-flight, as the existing "time slider" describe block does.
      vi.useFakeTimers();
      vi.setSystemTime(new Date('2026-10-12T11:30:00Z'));
      render(<PlanMapView parts={[flight()]} />);
      const strip = screen.getByTestId('sheet-above');
      expect(within(strip).getByTestId('time-slider')).toBeInTheDocument();
      expect(strip).toHaveAttribute('data-hidden', '0');
      const handle = screen.getByTestId('sheet-handle');
      fireEvent.keyDown(handle, { key: 'ArrowUp' }); // peek → half
      fireEvent.keyDown(handle, { key: 'ArrowUp' }); // half → full
      expect(strip).toHaveAttribute('data-hidden', '1');
    });

    it('expands a selected row to its detail inside the sheet', async () => {
      render(<PlanMapView parts={[flight(), hotel()]} />);
      await userEvent.click(screen.getByTestId('plan-row-2'));
      expect(screen.getByText('8028 Leesburg Pike')).toBeInTheDocument();
    });

    it('renders no bottom sheet on the desktop layout', () => {
      setMatchMedia(false);
      render(<PlanMapView parts={[flight()]} />);
      expect(screen.queryByTestId('bottom-sheet')).not.toBeInTheDocument();
    });

    it('uses per-animation offset (not persistent padding) when flying to a single-point selection on mobile', async () => {
      // peek snap: sheetPad = min(sheetHeightPx('peek', 800), round(800 * 0.5))
      //           = min(64, 400) = 64 → flyOffset = { offset: [0, -32] }
      render(<PlanMapView parts={[hotel()]} initialSelectedPartId={2} />);
      const map = FakeMap.instances[0];
      expect(map.flyTo).toHaveBeenCalled();
      const call = map.flyTo.mock.calls[0][0] as Record<string, unknown>;
      expect(call).toMatchObject({ offset: [0, -32] });
      expect(call).not.toHaveProperty('padding');
    });

    it('passes per-call padding to fitBounds for a multi-point selection on mobile', async () => {
      // peek snap: sheetPad = 64, so boundsPad bottom = 80 + 64 = 144
      render(<PlanMapView parts={[flight()]} initialSelectedPartId={1} />);
      const map = FakeMap.instances[0];
      expect(map.fitBounds).toHaveBeenCalled();
      const call = map.fitBounds.mock.calls[0][1] as Record<string, unknown>;
      expect(call).toMatchObject({ padding: { top: 80, left: 80, right: 80, bottom: 144 } });
    });
  });
});

const pos = (iso: string, lat: number, lon: number, extra: Partial<Position> = {}): Position => ({
  ts: iso,
  lat,
  lon,
  is_estimated: false,
  ...extra,
});

describe('positionAt', () => {
  const track: Position[] = [
    pos('2026-10-12T10:00:00Z', 50, -10),
    pos('2026-10-12T11:00:00Z', 48, -30),
  ];
  const t = (iso: string) => new Date(iso).getTime();

  it('returns null for an empty track or an instant before the first fix', () => {
    expect(positionAt([], t('2026-10-12T10:30:00Z'))).toBeNull();
    expect(positionAt(track, t('2026-10-12T09:00:00Z'))).toBeNull();
  });

  it('returns the last fix at or after the final sample', () => {
    expect(positionAt(track, t('2026-10-12T12:00:00Z'))).toMatchObject({ lat: 48, lon: -30 });
  });

  it('linearly interpolates between the bracketing samples', () => {
    expect(positionAt(track, t('2026-10-12T10:30:00Z'))).toMatchObject({ lat: 49, lon: -20 });
  });

  it('selects the right bracket within a multi-segment track', () => {
    const three = [
      pos('2026-10-12T10:00:00Z', 50, -10),
      ...track.slice(1),
      pos('2026-10-12T12:00:00Z', 44, -50),
    ];
    // 11:30 sits in the 11:00→12:00 segment (lat 48→44, lon -30→-50).
    expect(positionAt(three, t('2026-10-12T11:30:00Z'))).toMatchObject({ lat: 46, lon: -40 });
  });

  it('derives heading from the later fix, the earlier one, then the bearing', () => {
    const withB = [
      pos('2026-10-12T10:00:00Z', 50, -10),
      pos('2026-10-12T11:00:00Z', 48, -30, { heading_deg: 270 }),
    ];
    expect(positionAt(withB, t('2026-10-12T10:30:00Z'))!.heading).toBe(270);
    const withA = [
      pos('2026-10-12T10:00:00Z', 50, -10, { heading_deg: 99 }),
      pos('2026-10-12T11:00:00Z', 48, -30),
    ];
    expect(positionAt(withA, t('2026-10-12T10:30:00Z'))!.heading).toBe(99);
    expect(positionAt(track, t('2026-10-12T10:30:00Z'))!.heading).toBeCloseTo(
      initialBearing(50, -10, 48, -30),
      5,
    );
  });

  it('flags the interpolated point estimated when either end is, and copes with equal timestamps', () => {
    const est = [
      pos('2026-10-12T10:00:00Z', 50, -10, { is_estimated: true }),
      pos('2026-10-12T11:00:00Z', 48, -30),
    ];
    expect(positionAt(est, t('2026-10-12T10:30:00Z'))!.estimated).toBe(true);
    // Equal-instant leading samples are skipped without dividing by zero — the
    // bracket advances to the later of the duplicates.
    const dup = [
      pos('2026-10-12T10:00:00Z', 50, -10),
      pos('2026-10-12T10:00:00Z', 60, 5),
      pos('2026-10-12T11:00:00Z', 48, -30),
    ];
    expect(positionAt(dup, t('2026-10-12T10:00:00Z'))).toMatchObject({ lat: 60, lon: 5 });
  });
});

describe('planePlacementAt', () => {
  const t = (iso: string) => new Date(iso).getTime();
  const routeHeading = initialBearing(51.47, -0.45, 38.95, -77.46);
  const track: Position[] = [
    pos('2026-10-12T10:00:00Z', 50, -10),
    pos('2026-10-12T11:00:00Z', 48, -30),
  ];

  it('returns null for a non-flight part', () => {
    expect(planePlacementAt(hotel(), t('2026-10-12T10:30:00Z'))).toBeNull();
  });

  it('parks at the origin (along the route) before push-back', () => {
    expect(planePlacementAt(flight(), t('2026-10-12T08:00:00Z'))).toEqual({
      lon: -0.45,
      lat: 51.47,
      heading: routeHeading,
      estimated: false,
    });
  });

  it('parks an origin-less flight at its destination before departure', () => {
    const endOnly = flight({ start_lat: undefined, start_lon: undefined });
    expect(planePlacementAt(endOnly, t('2026-10-12T08:00:00Z'))).toMatchObject({
      lon: -77.46,
      lat: 38.95,
    });
  });

  it('parks at the destination once it has landed', () => {
    const arrived = flight({ flight: { ...flight().flight!, flight_status: 'Arrived' } });
    expect(planePlacementAt(arrived, t('2026-10-12T14:00:00Z'))).toMatchObject({
      lon: -77.46,
      lat: 38.95,
    });
  });

  it('falls back to the origin for a landed flight with no destination coordinate', () => {
    const arrivedNoEnd = flight({
      end_lat: undefined,
      end_lon: undefined,
      flight: { ...flight().flight!, flight_status: 'Arrived' },
    });
    expect(planePlacementAt(arrivedNoEnd, t('2026-10-12T14:00:00Z'))).toMatchObject({
      lon: -0.45,
      lat: 51.47,
    });
  });

  it('interpolates along the flown track while airborne', () => {
    const enroute = flight({ flight: { ...flight().flight!, flight_status: 'Enroute', track } });
    expect(planePlacementAt(enroute, t('2026-10-12T10:30:00Z'))).toMatchObject({
      lon: -20,
      lat: 49,
    });
  });

  it('orients along the route when the last fix carries no heading', () => {
    // Enroute past the final fix (which has no heading) but not flagged Arrived.
    const enroute = flight({ flight: { ...flight().flight!, flight_status: 'Enroute', track } });
    expect(planePlacementAt(enroute, t('2026-10-12T12:30:00Z'))!.heading).toBeCloseTo(
      routeHeading,
      5,
    );
  });

  it('falls back by phase when there is no usable track sample', () => {
    const noTrack = flight({ flight: { ...flight().flight!, track: [] } });
    // Mid-flight, no track → parked at the origin.
    expect(planePlacementAt(noTrack, t('2026-10-12T11:00:00Z'))).toMatchObject({
      lon: -0.45,
      lat: 51.47,
    });
    // Past the (scheduled) arrival, not flagged Arrived, no track → the destination.
    expect(planePlacementAt(noTrack, t('2026-10-12T14:00:00Z'))).toMatchObject({
      lon: -77.46,
      lat: 38.95,
    });
  });

  it('handles a flight with no resolvable departure time', () => {
    const noDep = flight({
      starts_at: '',
      ends_at: undefined,
      effective_at: '',
      flight: {
        ...flight().flight!,
        scheduled_out: '',
        scheduled_in: '',
        track: [],
      },
    });
    expect(planePlacementAt(noDep, t('2026-10-12T10:00:00Z'))).toMatchObject({
      lon: -0.45,
      lat: 51.47,
    });
  });
});

describe('trackFC', () => {
  const coordsOf = (fc: GeoJSON.FeatureCollection) =>
    (fc.features[0]?.geometry as GeoJSON.LineString | undefined)?.coordinates;
  const t = (iso: string) => new Date(iso).getTime();

  it('returns an empty collection without a selection or with a sparse track', () => {
    expect(trackFC(null, null).features).toHaveLength(0);
    expect(
      trackFC(
        flight({ flight: { ...flight().flight!, track: [pos('2026-10-12T10:00:00Z', 50, -10)] } }),
        null,
      ).features,
    ).toHaveLength(0);
  });

  it('draws the full trail when not scrubbing', () => {
    expect(coordsOf(trackFC(flight(), null))).toEqual([
      [-10, 50],
      [-30, 48],
    ]);
  });

  it('clips the trail to the scrubbed instant plus an interpolated tip', () => {
    expect(coordsOf(trackFC(flight(), t('2026-10-12T10:30:00Z')))).toEqual([
      [-10, 50],
      [-20, 49],
    ]);
  });

  it('keeps every sample (no duplicate tip) when scrubbed to/after the last fix', () => {
    expect(coordsOf(trackFC(flight(), t('2026-10-12T11:00:00Z')))).toEqual([
      [-10, 50],
      [-30, 48],
    ]);
  });

  it('returns nothing when scrubbed before the first fix', () => {
    expect(trackFC(flight(), t('2026-10-12T09:00:00Z')).features).toHaveLength(0);
  });
});

describe('tracksFC', () => {
  const t = (iso: string) => new Date(iso).getTime();

  it('clips every flight part to the scrubbed instant, tagging each with its part id', () => {
    const a = flight({ id: 1 });
    const b = flight({
      id: 2,
      flight: {
        ...flight().flight!,
        track: [pos('2026-10-12T10:00:00Z', 40, 0), pos('2026-10-12T11:00:00Z', 42, 5)],
      },
    });
    const fc = tracksFC([a, b], t('2026-10-12T10:30:00Z'));
    expect(fc.features).toHaveLength(2);
    expect(fc.features.map((f) => f.properties?.partId)).toEqual([1, 2]);
    // Each trail is clipped to 10:30 with an interpolated tip.
    expect((fc.features[0].geometry as GeoJSON.LineString).coordinates).toEqual([
      [-10, 50],
      [-20, 49],
    ]);
    expect((fc.features[1].geometry as GeoJSON.LineString).coordinates).toEqual([
      [0, 40],
      [2.5, 41],
    ]);
  });

  it('skips parts with a sparse track or none clipped before the instant', () => {
    const sparse = flight({
      id: 3,
      flight: { ...flight().flight!, track: [pos('2026-10-12T10:00:00Z', 50, -10)] },
    });
    const future = flight({ id: 4 }); // track starts at 10:00
    expect(tracksFC([sparse, future], t('2026-10-12T09:00:00Z')).features).toHaveLength(0);
  });
});

describe('fmtScrubTime', () => {
  it('formats a valid instant and blanks an invalid one', () => {
    expect(fmtScrubTime(new Date('2026-10-12T11:30:00Z').getTime())).toMatch(/\d{1,2}:\d{2}/);
    expect(fmtScrubTime(NaN)).toBe('');
  });
});
