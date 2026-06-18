import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { useRef } from 'react';
import { MemoryRouter, useNavigate } from 'react-router-dom';

import { useScrollRestoration } from './useScrollRestoration';

// Controllable ResizeObserver: tests grow the page and then fire the observers
// by hand (the jsdom global stub never invokes its callback).
type FakeEntry = { cb: ResizeObserverCallback; disconnected: boolean };
let observers: FakeEntry[] = [];
function fireResize() {
  act(() => {
    for (const o of observers) if (!o.disconnected) o.cb([], o as unknown as ResizeObserver);
  });
}
class FakeResizeObserver {
  private entry: FakeEntry;
  constructor(cb: ResizeObserverCallback) {
    this.entry = { cb, disconnected: false };
    observers.push(this.entry);
  }
  observe() {}
  unobserve() {}
  disconnect() {
    this.entry.disconnected = true;
  }
}
const activeObservers = () => observers.filter((o) => !o.disconnected).length;

// jsdom has no layout, so scrollHeight/clientHeight read 0; let tests dictate them.
function setMetrics(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => scrollHeight });
  Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => clientHeight });
}

/** A persistent scroll container (like the Layout's) whose route changes without
 *  unmounting it. `withChild` mirrors the real content wrapper inside it. */
function Harness({ withChild = true }: { withChild?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  useScrollRestoration(ref);
  const navigate = useNavigate();
  return (
    <>
      <div data-testid="scroller" ref={ref}>
        {withChild && <div data-testid="content" />}
      </div>
      <button onClick={() => navigate('/a')}>go-a</button>
      <button onClick={() => navigate('/b')}>go-b</button>
    </>
  );
}

/** Uses the hook with a ref that's never attached, exercising the null guards. */
function Detached() {
  const ref = useRef<HTMLDivElement>(null);
  useScrollRestoration(ref);
  return <div>ready</div>;
}

function renderHarness(props?: { withChild?: boolean }) {
  render(
    <MemoryRouter initialEntries={['/a']}>
      <Harness {...props} />
    </MemoryRouter>,
  );
  return screen.getByTestId('scroller');
}

beforeEach(() => {
  observers = [];
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('useScrollRestoration', () => {
  it("saves each path's scroll offset and restores it on return", () => {
    const scroller = renderHarness();
    setMetrics(scroller, 1000, 500); // tall enough that any saved offset sticks

    // Scroll the /a list, then navigate away: /b has no saved offset → top.
    scroller.scrollTop = 120;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-b'));
    expect(scroller.scrollTop).toBe(0);

    // Scroll /b, go back to /a → its 120 is restored.
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-a'));
    expect(scroller.scrollTop).toBe(120);

    // Forward to /b again → its own 300 comes back.
    fireEvent.click(screen.getByText('go-b'));
    expect(scroller.scrollTop).toBe(300);
  });

  it('stops immediately when the page is already tall enough', () => {
    const scroller = renderHarness();
    setMetrics(scroller, 1000, 500);
    scroller.scrollTop = 120;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-b'));
    fireEvent.click(screen.getByText('go-a'));
    // Reachable on the first paint → no observer left watching.
    expect(activeObservers()).toBe(0);
  });

  it('re-applies the offset as late-loading content grows the page', () => {
    const scroller = renderHarness();
    setMetrics(scroller, 1000, 500);
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-b'));

    // Return to /a while the content is still short — the target can't stick yet.
    setMetrics(scroller, 100, 500);
    fireEvent.click(screen.getByText('go-a'));
    expect(activeObservers()).toBe(1); // still waiting for the page to grow

    // Content arrives and the page grows; the observer fires and the offset sticks.
    setMetrics(scroller, 1000, 500);
    fireResize();
    expect(scroller.scrollTop).toBe(300);
    expect(activeObservers()).toBe(0); // done — stopped watching
  });

  it('stops re-applying once the user scrolls', () => {
    const scroller = renderHarness();
    setMetrics(scroller, 1000, 500);
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-b'));
    setMetrics(scroller, 100, 500);
    fireEvent.click(screen.getByText('go-a'));
    expect(activeObservers()).toBe(1);

    fireEvent.wheel(scroller);
    expect(activeObservers()).toBe(0);
    // A later resize must not yank the user back.
    scroller.scrollTop = 42;
    setMetrics(scroller, 1000, 500);
    fireResize();
    expect(scroller.scrollTop).toBe(42);
  });

  it('gives up after the grace period if the page never grows', () => {
    vi.useFakeTimers();
    const scroller = renderHarness();
    setMetrics(scroller, 1000, 500);
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-b'));
    setMetrics(scroller, 100, 500);
    fireEvent.click(screen.getByText('go-a'));
    expect(activeObservers()).toBe(1);

    act(() => vi.advanceTimersByTime(1500));
    expect(activeObservers()).toBe(0);
  });

  it('falls back to the grace timer when ResizeObserver is unavailable', () => {
    vi.stubGlobal('ResizeObserver', undefined);
    vi.useFakeTimers();
    const scroller = renderHarness();
    setMetrics(scroller, 100, 500); // short, and stays short
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-b'));
    fireEvent.click(screen.getByText('go-a'));
    // No observer was created; the timer is the only thing keeping it alive.
    expect(observers).toHaveLength(0);
    act(() => vi.advanceTimersByTime(1500)); // must not throw
  });

  it('works when the scroll container has no child wrapper to observe', () => {
    const scroller = renderHarness({ withChild: false });
    setMetrics(scroller, 100, 500);
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);
    fireEvent.click(screen.getByText('go-b'));
    fireEvent.click(screen.getByText('go-a'));
    // Observer still watches the container itself.
    expect(activeObservers()).toBe(1);
    setMetrics(scroller, 1000, 500);
    fireResize();
    expect(scroller.scrollTop).toBe(300);
  });

  it('is a no-op when the ref is not attached to an element', () => {
    render(
      <MemoryRouter>
        <Detached />
      </MemoryRouter>,
    );
    expect(screen.getByText('ready')).toBeInTheDocument();
  });
});
