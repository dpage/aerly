import type { StateCreator } from 'zustand';

import type { StoreState } from './store';

/** Shared state for the in-app Help panel (a right-side drawer rendered once in
 * Layout). Held in the store so any component — the top-bar help button or a
 * deep dialog like "Share trip" — can open the panel, optionally to a specific
 * topic, without prop-drilling. */
export interface HelpSlice {
  helpOpen: boolean;
  /** A context hint the panel maps to a topic page (e.g. 'sharing'), or null to
   * open on the overview. */
  helpPage: string | null;
  /** Open the panel, optionally seeded to the topic for `context`. */
  openHelp: (context?: string) => void;
  closeHelp: () => void;
}

export const createHelpSlice: StateCreator<StoreState, [], [], HelpSlice> = (set) => ({
  helpOpen: false,
  helpPage: null,
  openHelp: (context) => set({ helpOpen: true, helpPage: context ?? null }),
  closeHelp: () => set({ helpOpen: false }),
});
