import { useEffect, useState } from 'react';
import { Chip, Stack, Typography } from '@mui/material';

import type { FlightDetail } from '../api/types';
import { fmtAgo } from '../lib/format';
import { fmtGate } from '../lib/gate';
import { Mono, Row, Section, TimeRow } from './DetailRows';

interface Props {
  flight: FlightDetail;
  /** Departure-airport tz, for the "out" schedule rows. */
  startTz?: string;
  /** Arrival-airport tz, for the "in" schedule rows. */
  endTz?: string;
}

/** The rich flight detail block shown beneath a selected flight in the map list
 * — the same information the pre-trip-planning flight panel showed: aircraft,
 * full schedule (scheduled/estimated/actual out + in), live position, status,
 * and polling freshness. Collapses gracefully when telemetry is absent. */
export default function FlightDetailCard({ flight, startTz, endTz }: Props) {
  // Tick once a second so "Fix age" / "Last polled" stay live.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  const pos = flight.latest_position;
  const fixAgeSec = pos ? Math.max(0, Math.floor((now - new Date(pos.ts).getTime()) / 1000)) : 0;
  const fixIsStale = pos != null && fixAgeSec > 5 * 60;

  return (
    <Stack spacing={1.5} data-testid="flight-detail-card">
      <Section title="Aircraft">
        <Row label="Flight" value={flight.ident || null} />
        <Row label="Type" value={flight.aircraft_type || null} />
        <Row label="ICAO24" value={flight.icao24 ? <Mono>{flight.icao24}</Mono> : null} />
        <Row label="Callsign" value={flight.callsign || null} />
      </Section>

      <Section title="Route">
        <Row label="From" value={flight.origin_iata || null} />
        <Row label="To" value={flight.dest_iata || null} />
        <Row label="Departure gate" value={fmtGate(flight.origin_terminal, flight.origin_gate)} />
        <Row label="Arrival gate" value={fmtGate(flight.dest_terminal, flight.dest_gate)} />
        <Row label="Baggage belt" value={flight.dest_baggage_belt || null} />
      </Section>

      <Section title="Schedule">
        <TimeRow label="Scheduled out" iso={flight.scheduled_out} tz={startTz} />
        <TimeRow label="Estimated out" iso={flight.estimated_out} tz={startTz} />
        <TimeRow label="Actual out" iso={flight.actual_out} tz={startTz} />
        <TimeRow label="Scheduled in" iso={flight.scheduled_in} tz={endTz} />
        <TimeRow label="Estimated in" iso={flight.estimated_in} tz={endTz} />
        <TimeRow label="Actual in" iso={flight.actual_in} tz={endTz} />
      </Section>

      {pos && (
        <Section
          title="Current position"
          titleAdornment={
            pos.is_estimated ? (
              <Chip
                label="estimated"
                size="small"
                color="warning"
                variant="outlined"
                sx={{ height: 18, fontSize: 10 }}
              />
            ) : null
          }
        >
          <Row label="Latitude" value={`${pos.lat.toFixed(4)}°`} />
          <Row label="Longitude" value={`${pos.lon.toFixed(4)}°`} />
          <Row
            label="Altitude"
            value={pos.altitude_ft != null ? `${pos.altitude_ft.toLocaleString()} ft` : null}
          />
          <Row
            label="Groundspeed"
            value={pos.groundspeed_kt != null ? `${pos.groundspeed_kt} kt` : null}
          />
          <Row label="Heading" value={pos.heading_deg != null ? `${pos.heading_deg}°` : null} />
          <Row
            label="Fix age"
            value={
              <Typography
                variant="body2"
                component="span"
                color={fixIsStale ? 'warning.main' : 'text.primary'}
              >
                {fmtAgo(pos.ts, now)}
              </Typography>
            }
          />
        </Section>
      )}

      <Section title="Status">
        <Row label="Flight status" value={flight.flight_status || null} />
        <Row
          label="Last polled"
          value={flight.last_polled_at ? fmtAgo(flight.last_polled_at, now) : null}
        />
      </Section>
    </Stack>
  );
}
