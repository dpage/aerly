// Frontend build identity + "a newer build is deployed" detection.
//
// The SPA is embedded in the Go binary, so the commit the UI was built from and
// the commit the server reports are the same for a given deploy. They diverge
// only when a browser is still running an older cached bundle after a new build
// shipped — exactly the case where the user should refresh (issue: Magnus saw a
// fix only "after two refreshes"). useUpdateAvailable polls the server's build
// and flips to true on a mismatch so the app can prompt a refresh.

import { useEffect, useState } from 'react';
import { api } from './api/client';

/** The git commit this UI bundle was built from, or "" when unstamped (dev). */
export const UI_COMMIT: string = __APP_COMMIT__;

/** How often to re-check the server's build while the tab stays open. */
const POLL_MS = 5 * 60 * 1000;

/** First 12 chars of a commit SHA, matching the server's `short` form. */
export function shortCommit(sha: string): string {
  return sha.length > 12 ? sha.slice(0, 12) : sha;
}

/** A newer build is available when both commits are known and they differ. */
export function isNewerBuild(serverCommit: string, uiCommit: string): boolean {
  return Boolean(serverCommit) && Boolean(uiCommit) && serverCommit !== uiCommit;
}

/** The label for the "UI build" row in the About panel: the short commit, or
 * "dev" for an unstamped build. */
export function uiBuildLabel(uiCommit: string): string {
  return uiCommit ? shortCommit(uiCommit) : 'dev';
}

/** React hook: returns true once the server reports a build different from the
 * one this UI was built from. Checks on mount, whenever the tab regains focus,
 * and on a slow interval; dormant when inactive or when the UI commit is
 * unknown (dev builds never prompt). Transient fetch failures are ignored — the
 * next trigger retries. */
export function useUpdateAvailable(active: boolean, uiCommit: string): boolean {
  const [available, setAvailable] = useState(false);
  useEffect(() => {
    if (!active || !uiCommit) return;
    const check = async () => {
      try {
        const v = await api.getVersion();
        if (isNewerBuild(v.commit, uiCommit)) setAvailable(true);
      } catch {
        // Transient network/build-restart hiccup — retry on the next trigger.
      }
    };
    void check();
    const onFocus = () => void check();
    window.addEventListener('focus', onFocus);
    const id = window.setInterval(() => void check(), POLL_MS);
    return () => {
      window.removeEventListener('focus', onFocus);
      window.clearInterval(id);
    };
  }, [active, uiCommit]);
  return available;
}
