import { viewerZone } from '../lib/time';
import { useStore } from './store';

/** The viewer's zone, for components that show a 'Your time'. lib/time keeps
 * the zone outside the store, so subscribing to the home zone it mirrors
 * (`me.home_tz`, set alongside it on sign-in and in settings) re-renders a
 * mounted view when it changes. */
export function useViewerZone(): string {
  useStore((s) => s.me?.home_tz ?? '');
  return viewerZone();
}
