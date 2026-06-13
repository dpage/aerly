import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import BottomSheet, { sheetHeightPx, type SheetSnap } from './BottomSheet';

// jsdom has no layout, so give every element a measurable height: the sheet
// reads its parent's clientHeight to compute the half/full snap points.
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    value: 800,
  });
});
afterEach(() => {
  // Remove the shadowing property so other suites see jsdom's default again.
  delete (HTMLElement.prototype as { clientHeight?: number }).clientHeight;
});

function renderSheet(snap: SheetSnap, onSnapChange = vi.fn(), above?: React.ReactNode) {
  const result = render(
    <div style={{ position: 'relative', height: 800 }}>
      <BottomSheet
        snap={snap}
        onSnapChange={onSnapChange}
        header={<div>peek header</div>}
        above={above}
      >
        <div>body content</div>
      </BottomSheet>
    </div>,
  );
  return { onSnapChange, rerender: result.rerender };
}

describe('BottomSheet', () => {
  it('computes snap heights as peek px / fractions of the container', () => {
    expect(sheetHeightPx('peek', 800)).toBe(64);
    expect(sheetHeightPx('half', 800)).toBe(360);
    expect(sheetHeightPx('full', 800)).toBe(720);
  });

  it('renders header and body, reporting the current snap', () => {
    renderSheet('peek');
    expect(screen.getByText('peek header')).toBeInTheDocument();
    expect(screen.getByText('body content')).toBeInTheDocument();
    expect(screen.getByTestId('bottom-sheet')).toHaveAttribute('data-snap', 'peek');
  });

  it('a tap on the handle (no movement) raises peek to half', () => {
    const { onSnapChange } = renderSheet('peek');
    const handle = screen.getByTestId('sheet-handle');
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 700 });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: 700 });
    expect(onSnapChange).toHaveBeenCalledWith('half');
  });

  it('dragging snaps to the nearest resting point on release', () => {
    const { onSnapChange } = renderSheet('peek');
    const handle = screen.getByTestId('sheet-handle');
    // Start at peek (64px); drag up 300px → 364px, nearest snap is half (360).
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 700 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: 400 });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: 400 });
    expect(onSnapChange).toHaveBeenCalledWith('half');
  });

  it('a long drag from peek can land on full', () => {
    const { onSnapChange } = renderSheet('peek');
    const handle = screen.getByTestId('sheet-handle');
    // 64 + 600 = 664px, nearest snap is full (720).
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 700 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: 100 });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: 100 });
    expect(onSnapChange).toHaveBeenCalledWith('full');
  });

  it('arrow keys step between snap points from the handle', () => {
    const { onSnapChange: up } = renderSheet('half');
    fireEvent.keyDown(screen.getByTestId('sheet-handle'), { key: 'ArrowUp' });
    expect(up).toHaveBeenCalledWith('full');
  });

  it('ArrowDown lowers the sheet and stops at peek', () => {
    const { onSnapChange: down } = renderSheet('half');
    const handle = screen.getByTestId('sheet-handle');
    fireEvent.keyDown(handle, { key: 'ArrowDown' });
    expect(down).toHaveBeenCalledWith('peek');
  });

  it('renders the above slot riding the sheet, hidden only at full', () => {
    renderSheet('half', vi.fn(), <div>scrubber</div>);
    expect(screen.getByText('scrubber')).toBeInTheDocument();
    expect(screen.getByTestId('sheet-above')).toHaveAttribute('data-hidden', '0');
  });

  it('marks the above slot hidden at the full snap', () => {
    renderSheet('full', vi.fn(), <div>scrubber</div>);
    expect(screen.getByTestId('sheet-above')).toHaveAttribute('data-hidden', '1');
  });

  it('omits the above strip entirely when no above content is given', () => {
    renderSheet('half');
    expect(screen.queryByTestId('sheet-above')).not.toBeInTheDocument();
  });

  it('a drag far past full still lands on full', () => {
    const { onSnapChange } = renderSheet('peek');
    const handle = screen.getByTestId('sheet-handle');
    // Drag up 2000px, far beyond full (720px) — should still snap to full.
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 700 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: -1300 });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: -1300 });
    expect(onSnapChange).toHaveBeenCalledWith('full');
  });

  it('a drag far below peek still lands on peek', () => {
    const { onSnapChange } = renderSheet('half');
    const handle = screen.getByTestId('sheet-handle');
    // Drag down 2000px, far below peek — should still snap to peek.
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 400 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: 2400 });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: 2400 });
    expect(onSnapChange).toHaveBeenCalledWith('peek');
  });

  it('a sub-slop release at half does not call onSnapChange', () => {
    const { onSnapChange } = renderSheet('half');
    const handle = screen.getByTestId('sheet-handle');
    // Move only 4px — below the 8px tap slop; at half there is no tap-raise.
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 400 });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: 396 });
    expect(onSnapChange).not.toHaveBeenCalled();
  });

  it('pointercancel mid-drag leaves sheet at its snap and does not call onSnapChange', () => {
    const { onSnapChange } = renderSheet('half');
    const handle = screen.getByTestId('sheet-handle');
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 400 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: 200 });
    fireEvent.pointerCancel(handle, { pointerId: 1 });
    expect(screen.getByTestId('bottom-sheet')).toHaveAttribute('data-snap', 'half');
    expect(onSnapChange).not.toHaveBeenCalled();
  });

  it('re-rendering with a new snap prop updates data-snap', () => {
    const onSnapChange = vi.fn();
    const { rerender } = renderSheet('peek', onSnapChange);
    expect(screen.getByTestId('bottom-sheet')).toHaveAttribute('data-snap', 'peek');
    rerender(
      <div style={{ position: 'relative', height: 800 }}>
        <BottomSheet snap="full" onSnapChange={onSnapChange} header={<div>peek header</div>}>
          <div>body content</div>
        </BottomSheet>
      </div>,
    );
    expect(screen.getByTestId('bottom-sheet')).toHaveAttribute('data-snap', 'full');
  });

  it('tolerates setPointerCapture throwing for an already-released pointer', () => {
    const onSnapChange = renderSheet('peek').onSnapChange;
    const handle = screen.getByTestId('sheet-handle');
    const original = Element.prototype.setPointerCapture;
    Element.prototype.setPointerCapture = () => {
      throw new DOMException('no active pointer', 'NotFoundError');
    };
    try {
      expect(() => {
        fireEvent.pointerDown(handle, { pointerId: 1, clientY: 700 });
        fireEvent.pointerMove(handle, { pointerId: 1, clientY: 400 });
        fireEvent.pointerUp(handle, { pointerId: 1, clientY: 400 });
      }).not.toThrow();
    } finally {
      Element.prototype.setPointerCapture = original;
    }
    // The drag still resolves to a snap despite the capture failure.
    expect(onSnapChange).toHaveBeenCalledWith('half');
  });

  it('seeds the drag from the rendered height when it is measurable', () => {
    const onSnapChange = renderSheet('peek').onSnapChange;
    const sheet = screen.getByTestId('bottom-sheet');
    const handle = screen.getByTestId('sheet-handle');
    // jsdom returns 0 height by default; simulate a real rendered height so the
    // pointerdown seeds startPx from the rect (the `rectH > 0` branch).
    const spy = vi
      .spyOn(sheet, 'getBoundingClientRect')
      .mockReturnValue({ height: 200 } as DOMRect);
    try {
      // From a 200px start, dragging up 160px → 360px, the nearest snap is half.
      fireEvent.pointerDown(handle, { pointerId: 1, clientY: 700 });
      fireEvent.pointerMove(handle, { pointerId: 1, clientY: 540 });
      fireEvent.pointerUp(handle, { pointerId: 1, clientY: 540 });
    } finally {
      spy.mockRestore();
    }
    expect(onSnapChange).toHaveBeenCalledWith('half');
  });

  it('ignores a second pointerdown while a drag is in flight', () => {
    const onSnapChange = renderSheet('peek').onSnapChange;
    const handle = screen.getByTestId('sheet-handle');
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 700 });
    // A second finger lands mid-drag; it must not hijack the active drag.
    fireEvent.pointerDown(handle, { pointerId: 2, clientY: 700 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientY: 400 });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: 400 });
    expect(onSnapChange).toHaveBeenCalledTimes(1);
    expect(onSnapChange).toHaveBeenCalledWith('half');
  });
});
