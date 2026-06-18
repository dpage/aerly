import { describe, it, expect } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { useRef } from 'react';
import { MemoryRouter, useNavigate } from 'react-router-dom';

import { useScrollRestoration } from './useScrollRestoration';

/** A persistent scroll container (like the Layout's) plus links that change the
 * route without unmounting it — the scenario the hook restores across. */
function Harness() {
  const ref = useRef<HTMLDivElement>(null);
  useScrollRestoration(ref);
  const navigate = useNavigate();
  return (
    <>
      <div data-testid="scroller" ref={ref} />
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

describe('useScrollRestoration', () => {
  it('saves each path\'s scroll offset and restores it on return', () => {
    render(
      <MemoryRouter initialEntries={['/a']}>
        <Harness />
      </MemoryRouter>,
    );
    const scroller = screen.getByTestId('scroller');

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

  it('is a no-op when the ref is not attached to an element', () => {
    render(
      <MemoryRouter>
        <Detached />
      </MemoryRouter>,
    );
    expect(screen.getByText('ready')).toBeInTheDocument();
  });
});
