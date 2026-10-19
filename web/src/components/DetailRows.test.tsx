import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import { setViewerZone } from '../lib/time';
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

  it('collapses when every Row child would render null (empty values)', () => {
    // Regression: a <Row value={null} /> is itself a truthy element, so the
    // section must inspect the row's props, not the child's truthiness, to
    // decide whether to hide the heading.
    const { container } = render(
      <Section title="Aircraft">
        <Row label="Flight" value={null} />
        <Row label="Callsign" value="" />
      </Section>,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('collapses when every TimeRow child has no iso', () => {
    const { container } = render(
      <Section title="Schedule">
        <TimeRow label="Scheduled out" />
        <TimeRow label="Scheduled in" />
      </Section>,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders when at least one Row child has a value', () => {
    render(
      <Section title="Aircraft">
        <Row label="Flight" value={null} />
        <Row label="Callsign" value="BAW217" />
      </Section>,
    );
    expect(screen.getByText('Aircraft')).toBeInTheDocument();
    expect(screen.getByText('BAW217')).toBeInTheDocument();
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

  it("renders the zone's time and the viewer's time when they differ", () => {
    setViewerZone('Europe/London');
    render(<TimeRow label="Scheduled out" iso="2026-10-18T09:00:00Z" tz="Asia/Tashkent" />);
    expect(screen.getByText('Scheduled out')).toBeInTheDocument();
    expect(screen.getByText('Sun 18 Oct, 14:00 UTC+5')).toBeInTheDocument();
    expect(screen.getByText('Your time: Sun 18 Oct, 10:00 (UTC+1)')).toBeInTheDocument();
    setViewerZone(null);
  });

  it('renders no caption without a tz', () => {
    render(<TimeRow label="Scheduled out" iso="2026-07-01T14:00:00Z" />);
    expect(screen.getByText('Wed 1 Jul, 14:00')).toBeInTheDocument();
    expect(screen.queryByText(/Your time/)).not.toBeInTheDocument();
  });
});

describe('Mono', () => {
  it('renders its children', () => {
    render(<Mono>4ca7b3</Mono>);
    expect(screen.getByText('4ca7b3')).toBeInTheDocument();
  });
});
