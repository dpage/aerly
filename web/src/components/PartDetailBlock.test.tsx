import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import type { PlanPart } from '../api/types';
import PartDetailBlock from './PartDetailBlock';

function part(over: Partial<PlanPart> = {}): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'hotel',
    seq: 0,
    starts_at: '2026-10-12T15:00:00Z',
    start_tz: 'Europe/London',
    end_tz: 'Europe/London',
    start_label: '',
    end_label: '',
    start_address: '',
    end_address: '',
    status: 'planned',
    effective_at: '2026-10-12T15:00:00Z',
    ...over,
  };
}

describe('PartDetailBlock PlaceSection', () => {
  it('renders start/end labels and addresses when present', () => {
    render(
      <PartDetailBlock
        part={part({
          start_label: 'Hotel Lisboa',
          start_address: 'Rua Augusta 1',
          end_label: 'Airport',
          end_address: 'LIS Terminal 1',
        })}
      />,
    );
    expect(screen.getByText('Where')).toBeInTheDocument();
    expect(screen.getByText('Hotel Lisboa')).toBeInTheDocument();
    expect(screen.getByText('Rua Augusta 1')).toBeInTheDocument();
    expect(screen.getByText('Airport')).toBeInTheDocument();
    expect(screen.getByText('LIS Terminal 1')).toBeInTheDocument();
  });

  it('renders the Where header but no value rows when place fields are empty', () => {
    // PlaceSection always renders its header (its children are Row elements);
    // each individual Row collapses to null when its value is empty.
    render(<PartDetailBlock part={part({ type: 'hotel', hotel: undefined })} />);
    expect(screen.getByText('Where')).toBeInTheDocument();
    expect(screen.queryByText('Start')).not.toBeInTheDocument();
    expect(screen.queryByText('Start address')).not.toBeInTheDocument();
    expect(screen.queryByText('End')).not.toBeInTheDocument();
  });
});

describe('PartDetailBlock TypeSection', () => {
  it('renders hotel fields', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'hotel',
          hotel: {
            property_name: 'Hotel Lisboa',
            address: '1 Rua',
            phone: '+351 1',
            room_type: 'Suite',
            guests: 2,
          },
        })}
      />,
    );
    expect(screen.getByText('Hotel')).toBeInTheDocument();
    expect(screen.getByText('Hotel Lisboa')).toBeInTheDocument();
    expect(screen.getByText('Suite')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('renders ground transport fields', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'ground',
          ground: {
            provider: 'Addison Lee',
            phone: '+44 20',
            vehicle: 'Saloon',
            driver: 'Sam',
            pax: 3,
          },
        })}
      />,
    );
    expect(screen.getByText('Ground transport')).toBeInTheDocument();
    expect(screen.getByText('Addison Lee')).toBeInTheDocument();
    expect(screen.getByText('Sam')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('renders train fields', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'train',
          train: {
            operator: 'Eurostar',
            service_no: '9024',
            class: 'Standard',
            coach: '12',
            seat: '34A',
            platform: '5',
          },
        })}
      />,
    );
    expect(screen.getByText('Train')).toBeInTheDocument();
    expect(screen.getByText('Eurostar')).toBeInTheDocument();
    expect(screen.getByText('34A')).toBeInTheDocument();
  });

  it('renders dining fields', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'dining',
          dining: { reservation_name: 'Page', party_size: 4, phone: '+1 555' },
        })}
      />,
    );
    expect(screen.getByText('Dining')).toBeInTheDocument();
    expect(screen.getByText('Page')).toBeInTheDocument();
    expect(screen.getByText('4')).toBeInTheDocument();
  });

  it('renders excursion fields', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'excursion',
          excursion: { provider: 'City Tours', ticket_count: 5 },
        })}
      />,
    );
    expect(screen.getByText('Excursion')).toBeInTheDocument();
    expect(screen.getByText('City Tours')).toBeInTheDocument();
    expect(screen.getByText('5')).toBeInTheDocument();
  });

  it('collapses individual type detail rows when their fields are empty', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'dining',
          dining: { reservation_name: '', party_size: undefined, phone: '' },
        })}
      />,
    );
    // The Dining section header still renders (its children are Row elements),
    // but every value Row collapses to null.
    expect(screen.getByText('Dining')).toBeInTheDocument();
    expect(screen.queryByText('Reservation')).not.toBeInTheDocument();
    expect(screen.queryByText('Party size')).not.toBeInTheDocument();
    expect(screen.queryByText('Phone')).not.toBeInTheDocument();
  });

  it('collapses every hotel row when its fields are empty', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'hotel',
          hotel: {
            property_name: '',
            address: '',
            phone: '',
            room_type: '',
            guests: undefined,
          },
        })}
      />,
    );
    expect(screen.getByText('Hotel')).toBeInTheDocument();
    expect(screen.queryByText('Property')).not.toBeInTheDocument();
    expect(screen.queryByText('Room')).not.toBeInTheDocument();
    expect(screen.queryByText('Guests')).not.toBeInTheDocument();
  });

  it('collapses every ground row when its fields are empty', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'ground',
          ground: { provider: '', phone: '', vehicle: '', driver: '', pax: undefined },
        })}
      />,
    );
    expect(screen.getByText('Ground transport')).toBeInTheDocument();
    expect(screen.queryByText('Provider')).not.toBeInTheDocument();
    expect(screen.queryByText('Driver')).not.toBeInTheDocument();
    expect(screen.queryByText('Passengers')).not.toBeInTheDocument();
  });

  it('collapses every train row when its fields are empty', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'train',
          train: { operator: '', service_no: '', class: '', coach: '', seat: '', platform: '' },
        })}
      />,
    );
    expect(screen.getByText('Train')).toBeInTheDocument();
    expect(screen.queryByText('Operator')).not.toBeInTheDocument();
    expect(screen.queryByText('Platform')).not.toBeInTheDocument();
  });

  it('collapses every excursion row when its fields are empty', () => {
    render(
      <PartDetailBlock
        part={part({
          type: 'excursion',
          excursion: { provider: '', ticket_count: undefined },
        })}
      />,
    );
    expect(screen.getByText('Excursion')).toBeInTheDocument();
    expect(screen.queryByText('Provider')).not.toBeInTheDocument();
    expect(screen.queryByText('Tickets')).not.toBeInTheDocument();
  });

  it('renders no type section when no detail object is populated', () => {
    render(<PartDetailBlock part={part({ type: 'flight', start_label: 'LHR' })} />);
    expect(screen.getByText('Where')).toBeInTheDocument();
    expect(screen.queryByText('Hotel')).not.toBeInTheDocument();
    expect(screen.queryByText('Train')).not.toBeInTheDocument();
  });
});
