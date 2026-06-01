import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import type { PlanPart } from '../api/types';
import maplibreMock, { FakeMap, resetMaplibreMock } from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

import PlanMapView from './PlanMapView';

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

  it('selecting a map feature highlights its list row (bidirectional)', async () => {
    render(<PlanMapView parts={[flight(), hotel()]} />);
    const map = FakeMap.instances[0];
    // Simulate clicking the hotel's pin on the map.
    map.fireLayer('click', 'pmv-points', { features: [{ properties: { partId: 2 } }] });
    expect(await screen.findByTestId('part-detail-block')).toBeInTheDocument();
    expect(screen.getByTestId('plan-row-2')).toHaveClass('Mui-selected');
  });

  it('shows an empty state when there are no mappable parts', () => {
    render(<PlanMapView parts={[]} />);
    expect(screen.getByText(/no mappable plans/i)).toBeInTheDocument();
  });
});
