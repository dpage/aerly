import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import { useLocation } from 'react-router-dom';

// How long, after a route change, to keep re-applying the saved offset while
// the page is still growing. Bounds the effort so a list that legitimately got
// shorter doesn't fight the user forever.
const RESTORE_GRACE_MS = 1500;

/**
 * Remember and restore the scroll offset of a container across route changes.
 *
 * The app has a single scroll container (in the Layout) shared by every routed
 * page, so opening a trip from a list and tapping Back would otherwise leave
 * the list scrolled to the top. We key the last offset by pathname, keep it
 * current as the user scrolls, and reapply it once the matching route renders.
 * A path with no saved offset (a first visit, or opening a trip) restores to
 * the top.
 *
 * Some routes finish loading their content *after* the navigation commit — the
 * Friends' trips diagnostic views, for instance, fetch their list on mount, so
 * the page is briefly near-empty when you return to it. Restoring once against
 * that short page would clamp to the top, so we keep re-applying the offset as
 * the content grows (watching it with a ResizeObserver) until the target is
 * reachable, the user scrolls, or a short grace period elapses.
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

  // Reapply the saved offset after the new route's DOM has committed, then keep
  // it pinned while late-loading content grows the page underneath.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const target = positions.current.get(pathname) ?? 0;
    el.scrollTop = target;
    // At the top there's nothing to wait for — the assignment above suffices.
    if (target <= 0) return;

    let done = false;
    let observer: ResizeObserver | null = null;
    let timer = 0;
    const stop = () => {
      done = true;
      observer?.disconnect();
      if (timer) window.clearTimeout(timer);
      el.removeEventListener('wheel', onUserScroll);
      el.removeEventListener('touchmove', onUserScroll);
    };
    // The user taking over wins outright — stop forcing the offset on them.
    const onUserScroll = () => stop();
    const reapply = () => {
      if (done) return;
      el.scrollTop = target;
      // Once the page is tall enough for the target to actually stick, we're done.
      if (el.scrollHeight - el.clientHeight >= target) stop();
    };

    reapply();
    if (done) return; // already reachable on the first paint

    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(reapply);
      observer.observe(el);
      // The content lives in a child wrapper; its height — not the fixed-height
      // scroll container's — is what changes as data arrives.
      if (el.firstElementChild) observer.observe(el.firstElementChild);
    }
    el.addEventListener('wheel', onUserScroll, { passive: true });
    el.addEventListener('touchmove', onUserScroll, { passive: true });
    timer = window.setTimeout(stop, RESTORE_GRACE_MS);
    return stop;
  }, [ref, pathname]);
}
