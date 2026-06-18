import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import { useLocation } from 'react-router-dom';

/**
 * Remember and restore the scroll offset of a container across route changes.
 *
 * The app has a single scroll container (in the Layout) shared by every routed
 * page, so opening a trip from a list and tapping Back would otherwise leave
 * the list scrolled to the top. We key the last offset by pathname, keep it
 * current as the user scrolls, and reapply it once the matching route's content
 * has rendered. A path with no saved offset (a first visit, or opening a trip)
 * restores to the top.
 *
 * In-memory only: offsets live for the session and reset on a full reload.
 */
export function useScrollRestoration(ref: RefObject<HTMLElement | null>): void {
  const { pathname } = useLocation();
  const positions = useRef(new Map<string, number>());

  // Track the live offset for the current path. The most recent scroll event
  // captures the resting position before the user navigates away, so we never
  // read scrollTop after a route swap (when it already holds the next page's
  // restored value).
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const save = () => {
      positions.current.set(pathname, el.scrollTop);
    };
    el.addEventListener('scroll', save, { passive: true });
    return () => el.removeEventListener('scroll', save);
  }, [ref, pathname]);

  // Reapply the saved offset once the new route's DOM has committed (a layout
  // effect runs after the content is in the tree, so the container is already
  // tall enough to scroll).
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.scrollTop = positions.current.get(pathname) ?? 0;
  }, [ref, pathname]);
}
