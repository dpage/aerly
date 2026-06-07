import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';

import type { FlightDetail, Position } from '../api/types';
import FlightDetailCard from './FlightDetailCard';

function flight(over: Partial<FlightDetail> = {}): FlightDetail {
  return {
    ident: 'BA217',
    callsign: 'BAW217',
    scheduled_out: '2026-10-12T09:00:00Z',
    scheduled_in: '2026-10-12T13:00:00Z',
    origin_iata: 'LHR',
    dest_iata: 'IAD',
    flight_status: 'Scheduled',
    ...over,
  };
}

function position(over: Partial<Position> = {}): Position {
  return {
    ts: '2026-10-12T09:30:00Z',
    lat: 51.4775,
    lon: -0.4614,
    altitude_ft: 35000,
    groundspeed_kt: 480,
    heading_deg: 270,
    is_estimated: false,
    ...over,
  };
}

describe('FlightDetailCard', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Pin "now" so fix-age / last-polled text is deterministic.
    vi.setSystemTime(new Date('2026-10-12T10:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders aircraft, route and schedule rows with optional times present', () => {
    render(
      <FlightDetailCard
        flight={flight({
          icao24: '4ca7b3',
          estimated_out: '2026-10-12T09:15:00Z',
          actual_out: '2026-10-12T09:20:00Z',
          estimated_in: '2026-10-12T13:10:00Z',
          actual_in: '2026-10-12T13:05:00Z',
        })}
        startTz="Europe/London"
        endTz="America/New_York"
      />,
    );
    expect(screen.getByTestId('flight-detail-card')).toBeInTheDocument();
    expect(screen.getByText('Aircraft')).toBeInTheDocument();
    expect(screen.getByText('BA217')).toBeInTheDocument();
    expect(screen.getByText('4ca7b3')).toBeInTheDocument();
    expect(screen.getByText('BAW217')).toBeInTheDocument();
    expect(screen.getByText('Route')).toBeInTheDocument();
    expect(screen.getByText('LHR')).toBeInTheDocument();
    expect(screen.getByText('IAD')).toBeInTheDocument();
    expect(screen.getByText('Schedule')).toBeInTheDocument();
    expect(screen.getByText('Estimated out')).toBeInTheDocument();
    expect(screen.getByText('Actual out')).toBeInTheDocument();
    expect(screen.getByText('Estimated in')).toBeInTheDocument();
    expect(screen.getByText('Actual in')).toBeInTheDocument();
  });

  it('shows the aircraft type when present and collapses its row when absent', () => {
    const { rerender } = render(
      <FlightDetailCard flight={flight({ aircraft_type: 'Boeing 777-300ER' })} />,
    );
    expect(screen.getByText('Type')).toBeInTheDocument();
    expect(screen.getByText('Boeing 777-300ER')).toBeInTheDocument();
    // Absent aircraft type → the Type row collapses to null.
    rerender(<FlightDetailCard flight={flight()} />);
    expect(screen.queryByText('Type')).not.toBeInTheDocument();
  });

  it('shows the arrival baggage belt when present and collapses its row when absent', () => {
    const { rerender } = render(<FlightDetailCard flight={flight({ dest_baggage_belt: '34' })} />);
    expect(screen.getByText('Baggage belt')).toBeInTheDocument();
    expect(screen.getByText('34')).toBeInTheDocument();
    // Absent belt → the row collapses to null.
    rerender(<FlightDetailCard flight={flight()} />);
    expect(screen.queryByText('Baggage belt')).not.toBeInTheDocument();
  });

  it('collapses the schedule to only the rows present', () => {
    render(<FlightDetailCard flight={flight({ callsign: '' })} />);
    // Only scheduled out/in are set; estimated/actual collapse.
    expect(screen.getByText('Scheduled out')).toBeInTheDocument();
    expect(screen.getByText('Scheduled in')).toBeInTheDocument();
    expect(screen.queryByText('Estimated out')).not.toBeInTheDocument();
    expect(screen.queryByText('Actual in')).not.toBeInTheDocument();
    // Empty callsign collapses its row.
    expect(screen.queryByText('Callsign')).not.toBeInTheDocument();
  });

  it('renders the current-position section with a fresh fix', () => {
    render(
      <FlightDetailCard
        flight={flight({
          latest_position: position({ ts: '2026-10-12T09:59:57Z' }),
          last_polled_at: '2026-10-12T09:59:00Z',
        })}
      />,
    );
    expect(screen.getByText('Current position')).toBeInTheDocument();
    expect(screen.getByText('Latitude')).toBeInTheDocument();
    expect(screen.getByText('Longitude')).toBeInTheDocument();
    expect(screen.getByText('Altitude')).toBeInTheDocument();
    expect(screen.getByText('Groundspeed')).toBeInTheDocument();
    expect(screen.getByText('Heading')).toBeInTheDocument();
    expect(screen.getByText('Fix age')).toBeInTheDocument();
    // Fresh fix (3s old, under the 5s threshold) → "just now".
    expect(screen.getByText('just now')).toBeInTheDocument();
    // last_polled_at present → Last polled row shows.
    expect(screen.getByText('Last polled')).toBeInTheDocument();
    // No estimated chip for a real fix.
    expect(screen.queryByText('estimated')).not.toBeInTheDocument();
  });

  it('shows the estimated chip and a stale (warning) fix age for an old estimated fix', () => {
    render(
      <FlightDetailCard
        flight={flight({
          latest_position: position({ ts: '2026-10-12T09:50:00Z', is_estimated: true }),
        })}
      />,
    );
    // is_estimated → chip in the section adornment.
    expect(screen.getByText('estimated')).toBeInTheDocument();
    // 10 minutes old (> 5 min) → stale; fmtAgo renders "10m ago".
    expect(screen.getByText('10m ago')).toBeInTheDocument();
  });

  it('omits altitude/groundspeed/heading rows when those fields are absent', () => {
    render(
      <FlightDetailCard
        flight={flight({
          latest_position: position({
            altitude_ft: undefined,
            groundspeed_kt: undefined,
            heading_deg: undefined,
          }),
        })}
      />,
    );
    expect(screen.getByText('Current position')).toBeInTheDocument();
    expect(screen.queryByText('Altitude')).not.toBeInTheDocument();
    expect(screen.queryByText('Groundspeed')).not.toBeInTheDocument();
    expect(screen.queryByText('Heading')).not.toBeInTheDocument();
  });

  it('omits the position and last-polled rows when telemetry is absent', () => {
    render(<FlightDetailCard flight={flight({ flight_status: '' })} />);
    // No latest_position → the whole position Section is conditionally absent.
    expect(screen.queryByText('Current position')).not.toBeInTheDocument();
    // No last_polled_at → that Row collapses to null.
    expect(screen.queryByText('Last polled')).not.toBeInTheDocument();
    // Empty flight_status → the Flight status Row collapses to null.
    expect(screen.queryByText('Flight status')).not.toBeInTheDocument();
  });

  it('collapses the ident, callsign, route and icao rows when those fields are empty', () => {
    render(
      <FlightDetailCard
        flight={flight({
          ident: '',
          callsign: '',
          icao24: undefined,
          origin_iata: '',
          dest_iata: '',
        })}
      />,
    );
    // The Aircraft section's rows (Flight/ICAO24/Callsign) are all empty, so
    // the whole section — header included — collapses; no empty heading shows.
    expect(screen.queryByText('Aircraft')).not.toBeInTheDocument();
    expect(screen.queryByText('Flight')).not.toBeInTheDocument();
    expect(screen.queryByText('ICAO24')).not.toBeInTheDocument();
    expect(screen.queryByText('Callsign')).not.toBeInTheDocument();
    // Route survives: From/To collapse (empty IATA) but Departure/Arrival
    // always render via fmtGate's "Unknown" fallback.
    expect(screen.getByText('Route')).toBeInTheDocument();
    expect(screen.queryByText('From')).not.toBeInTheDocument();
    expect(screen.queryByText('To')).not.toBeInTheDocument();
    expect(screen.getByText('Departure')).toBeInTheDocument();
  });

  it('ticks the live fix-age on its interval', () => {
    render(
      <FlightDetailCard
        flight={flight({ latest_position: position({ ts: '2026-10-12T09:59:57Z' }) })}
      />,
    );
    expect(screen.getByText('just now')).toBeInTheDocument();
    // Set the clock so that after the +1s tick it reads 10:00:10 — 13s past the
    // 09:59:57 fix. (advanceTimersByTime also moves the mocked clock forward.)
    vi.setSystemTime(new Date('2026-10-12T10:00:09Z'));
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByText('13s ago')).toBeInTheDocument();
  });
});

describe('FlightDetailCard gate', () => {
  it('shows departure gate/terminal and Unknown arrival', () => {
    const base: FlightDetail = {
      ident: 'BA286',
      callsign: '',
      scheduled_out: '2026-06-01T09:00:00Z',
      scheduled_in: '2026-06-01T12:00:00Z',
      origin_iata: 'LHR',
      dest_iata: 'SFO',
      flight_status: 'Scheduled',
    };
    render(<FlightDetailCard flight={{ ...base, origin_terminal: '5', origin_gate: 'B32' }} />);
    expect(screen.getByText('Terminal 5 · Gate B32')).toBeInTheDocument();
    // Arrival gate unknown.
    expect(screen.getByText('Unknown')).toBeInTheDocument();
  });
});
