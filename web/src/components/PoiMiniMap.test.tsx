import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/react';

import maplibreMock, { FakeMap, FakeMarker, resetMaplibreMock } from '../test/maplibre-mock';

vi.mock('maplibre-gl', () => ({ default: maplibreMock, ...maplibreMock }));

import PoiMiniMap from './PoiMiniMap';
import type { Poi } from '../api/types';

function poi(over: Partial<Poi> = {}): Poi {
  return { id: 'node/1', name: 'A', category: 'sights', lat: 51.5, lon: -0.12, distance_m: 40, ...over };
}

function pinFor(name: string): HTMLElement | undefined {
  return FakeMarker.instances
    .map((m) => m.opts?.element as HTMLElement | undefined)
    .find((el) => el?.getAttribute('aria-label') === name);
}

beforeEach(() => {
  resetMaplibreMock();
});

describe('PoiMiniMap', () => {
  it('renders a pin per POI plus the centre anchor, and fits the bounds', () => {
    render(
      <PoiMiniMap
        pois={[poi({ id: 'node/1', name: 'A' }), poi({ id: 'node/2', name: 'B', lat: 51.51, lon: -0.13 })]}
        center={{ lat: 51.5, lon: -0.12 }}
        onSelectPoi={vi.fn()}
      />,
    );
    // Two POI pins + one anchor marker.
    expect(FakeMarker.instances.length).toBe(3);
    expect(pinFor('A')).toBeDefined();
    expect(pinFor('B')).toBeDefined();
    expect(FakeMap.instances[0].fitBounds).toHaveBeenCalled();
  });

  it('calls onSelectPoi with the id when a pin is clicked', () => {
    const onSelect = vi.fn();
    render(<PoiMiniMap pois={[poi({ id: 'node/9', name: 'Tower' })]} center={{ lat: 51.5, lon: -0.12 }} onSelectPoi={onSelect} />);
    const pin = pinFor('Tower');
    expect(pin).toBeDefined();
    fireEvent.click(pin!);
    expect(onSelect).toHaveBeenCalledWith('node/9');
  });

  it('flies to the selected POI rather than fitting all bounds', () => {
    render(
      <PoiMiniMap
        pois={[poi({ id: 'node/1', lat: 51.5, lon: -0.12 }), poi({ id: 'node/2', lat: 51.6, lon: -0.2 })]}
        center={{ lat: 51.5, lon: -0.12 }}
        selectedId="node/2"
        onSelectPoi={vi.fn()}
      />,
    );
    expect(FakeMap.instances[0].flyTo).toHaveBeenCalledWith(expect.objectContaining({ center: [-0.2, 51.6] }));
  });

  it('renders without a centre anchor when none is given', () => {
    render(<PoiMiniMap pois={[poi({ id: 'node/1', name: 'Solo' })]} onSelectPoi={vi.fn()} />);
    // Just the single POI pin, no anchor.
    expect(FakeMarker.instances.length).toBe(1);
    expect(FakeMap.instances[0].flyTo).toHaveBeenCalled();
  });

  it('places no markers and neither flies nor fits when there is nothing to show', () => {
    render(<PoiMiniMap pois={[]} onSelectPoi={vi.fn()} />);
    expect(FakeMarker.instances.length).toBe(0);
    expect(FakeMap.instances[0].flyTo).not.toHaveBeenCalled();
    expect(FakeMap.instances[0].fitBounds).not.toHaveBeenCalled();
  });
});
