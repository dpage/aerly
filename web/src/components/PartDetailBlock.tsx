import { Stack } from '@mui/material';

import type { PlanPart } from '../api/types';
import { formatCost } from '../lib/format';
import { isTransferType } from '../lib/trip-format';
import { Row, Section } from './DetailRows';

/** The expanded detail for a non-flight part in the map list: the type-specific
 * fields a traveller wants at a glance — hotel address/phone, cab firm + driver,
 * dining reservation, train coach/seat, etc. Each Row collapses when empty, so a
 * sparse part shows a short block. (Flights use FlightDetailCard instead.) */
export default function PartDetailBlock({ part }: { part: PlanPart }) {
  return (
    <Stack spacing={1.5} data-testid="part-detail-block">
      <PlaceSection part={part} />
      <TypeSection part={part} />
    </Stack>
  );
}

function PlaceSection({ part }: { part: PlanPart }) {
  // Transfers go between two places, so From/To (each with its address) makes
  // sense. Everything else happens at a single venue — show just one "Address"
  // (no "Start"/"End"), which the title already names.
  if (isTransferType(part.type)) {
    return (
      <Section title="Where">
        <Row label="From" value={part.start_label || null} />
        <Row label="From address" value={part.start_address || null} />
        <Row label="To" value={part.end_label || null} />
        <Row label="To address" value={part.end_address || null} />
      </Section>
    );
  }
  const address = part.start_address || part.hotel?.address || '';
  return (
    <Section title="Where">
      <Row label="Address" value={address || null} />
    </Section>
  );
}

function TypeSection({ part }: { part: PlanPart }) {
  const h = part.hotel;
  if (h) {
    return (
      <Section title="Accommodation">
        <Row label="Kind" value={h.kind || null} />
        <Row label="Name" value={h.property_name || null} />
        <Row label="Phone" value={h.phone || null} />
        <Row label="Room / pitch" value={h.room_type || null} />
        <Row label="Guests" value={h.guests ?? null} />
      </Section>
    );
  }
  const g = part.ground;
  if (g) {
    return (
      <Section title="Ground transport">
        <Row label="Provider" value={g.provider || null} />
        <Row label="Phone" value={g.phone || null} />
        <Row label="Vehicle" value={g.vehicle || null} />
        <Row label="Driver" value={g.driver || null} />
        <Row label="Passengers" value={g.pax ?? null} />
      </Section>
    );
  }
  const vh = part.vehicle_hire;
  if (vh) {
    return (
      <Section title="Car hire">
        <Row label="Category" value={vh.category || null} />
        <Row label="Vehicle" value={vh.vehicle || null} />
        <Row label="Transmission" value={vh.transmission || null} />
        <Row label="Fuel policy" value={vh.fuel_policy || null} />
        <Row label="Mileage" value={vh.mileage || null} />
        {/* Each amount carries its own currency (not necessarily the plan's
            booking currency), and is absent — not zero — when unstated: a
            genuine £0 excess and "not stated" are different facts. */}
        <Row label="Excess" value={formatCost(vh.excess_amount, vh.excess_currency)} />
        <Row label="Deposit" value={formatCost(vh.deposit_amount, vh.deposit_currency)} />
      </Section>
    );
  }
  const t = part.train;
  if (t) {
    return (
      <Section title="Train">
        <Row label="Operator" value={t.operator || null} />
        <Row label="Service" value={t.service_no || null} />
        <Row label="Class" value={t.class || null} />
        <Row label="Coach" value={t.coach || null} />
        <Row label="Seat" value={t.seat || null} />
        <Row label="Platform" value={t.platform || null} />
      </Section>
    );
  }
  const d = part.dining;
  if (d) {
    return (
      <Section title="Dining">
        <Row label="Reservation" value={d.reservation_name || null} />
        <Row label="Party size" value={d.party_size ?? null} />
        <Row label="Phone" value={d.phone || null} />
      </Section>
    );
  }
  const e = part.excursion;
  if (e) {
    return (
      <Section title="Excursion">
        <Row label="Provider" value={e.provider || null} />
        <Row label="Tickets" value={e.ticket_count ?? null} />
      </Section>
    );
  }
  const m = part.meeting;
  if (m) {
    return (
      <Section title="Meeting">
        <Row label="Location" value={m.location || null} />
        <Row label="Organiser" value={m.organiser || null} />
        <Row label="Platform" value={m.platform || null} />
      </Section>
    );
  }
  const ev = part.event;
  if (ev) {
    return (
      <Section title="Event">
        <Row label="Performer" value={ev.performer || null} />
        <Row label="Category" value={ev.category || null} />
        <Row label="Venue area" value={ev.venue_area || null} />
        <Row label="Link" value={ev.url || null} />
      </Section>
    );
  }
  return null;
}
