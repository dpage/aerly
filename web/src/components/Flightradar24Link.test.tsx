import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import Flightradar24Link from './Flightradar24Link';

describe('Flightradar24Link', () => {
  it('renders a new-tab anchor to the flight, passing clicks on', () => {
    const onClick = vi.fn();
    render(<Flightradar24Link flight={{ ident: 'LH 400', callsign: '' }} onClick={onClick} />);
    const link = screen.getByRole('link', { name: 'View on Flightradar24' });
    expect(link).toHaveAttribute('href', 'https://www.flightradar24.com/data/flights/lh400');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    fireEvent.click(link);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('renders nothing for a flight with no usable ident', () => {
    const { container } = render(<Flightradar24Link flight={{ ident: '', callsign: '' }} />);
    expect(container).toBeEmptyDOMElement();
  });
});
