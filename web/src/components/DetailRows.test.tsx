import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import { Mono, Row, Section, TimeRow } from './DetailRows';

describe('Row', () => {
  it('renders label + value when value is present', () => {
    render(<Row label="Flight" value="BA217" />);
    expect(screen.getByText('Flight')).toBeInTheDocument();
    expect(screen.getByText('BA217')).toBeInTheDocument();
  });

  it('returns null for nullish value', () => {
    const { container } = render(<Row label="Flight" value={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('returns null for an empty-string value', () => {
    const { container } = render(<Row label="Flight" value="" />);
    expect(container).toBeEmptyDOMElement();
  });

  it('returns null for a false value', () => {
    const { container } = render(<Row label="Flight" value={false} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('Section', () => {
  it('renders title, adornment and children when a child is present', () => {
    render(
      <Section title="Aircraft" titleAdornment={<span>badge</span>}>
        <Row label="Flight" value="BA217" />
      </Section>,
    );
    expect(screen.getByText('Aircraft')).toBeInTheDocument();
    expect(screen.getByText('badge')).toBeInTheDocument();
    expect(screen.getByText('BA217')).toBeInTheDocument();
  });

  it('collapses to null when every (array) child is null/false', () => {
    // Section inspects its children array; the conditional-render pattern the
    // real callers use yields literal null/false entries.
    const { container } = render(
      <Section title="Aircraft">
        {null}
        {false}
      </Section>,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('collapses to null when a single non-array child is null', () => {
    const { container } = render(<Section title="Empty">{null}</Section>);
    expect(container).toBeEmptyDOMElement();
  });

  it('collapses to null when a child is false', () => {
    const { container } = render(<Section title="Empty">{false}</Section>);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders without an adornment', () => {
    render(
      <Section title="Route">
        <Row label="From" value="LHR" />
      </Section>,
    );
    expect(screen.getByText('Route')).toBeInTheDocument();
  });
});

describe('TimeRow', () => {
  it('returns null without an iso', () => {
    const { container } = render(<TimeRow label="Scheduled out" />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders local time + UTC secondary line when a tz is given', () => {
    render(<TimeRow label="Scheduled out" iso="2026-07-01T14:00:00Z" tz="America/New_York" />);
    expect(screen.getByText('Scheduled out')).toBeInTheDocument();
    // Local line (EDT, no UTC suffix) plus the secondary UTC caption line.
    expect(screen.getByText(/UTC$/)).toBeInTheDocument();
  });

  it('renders only the UTC line (no secondary caption) without a tz', () => {
    render(<TimeRow label="Scheduled out" iso="2026-07-01T14:00:00Z" />);
    const utcMatches = screen.getAllByText(/UTC$/);
    // Without a tz the primary value already carries the UTC suffix; there is
    // no separate caption line.
    expect(utcMatches).toHaveLength(1);
  });
});

describe('Mono', () => {
  it('renders its children', () => {
    render(<Mono>4ca7b3</Mono>);
    expect(screen.getByText('4ca7b3')).toBeInTheDocument();
  });
});
