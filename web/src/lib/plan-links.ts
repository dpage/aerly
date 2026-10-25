import type { PlanLink, PlanLinkInput } from '../api/types';

// Helpers for per-plan links (issue #109): title/URL pairs pointing at tickets,
// boarding passes and the like, kept wherever the user keeps them.

/** True for an absolute http(s) URL, the only kind the server accepts. */
export function isHttpUrl(raw: string): boolean {
  try {
    const u = new URL(raw.trim());
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== '';
  } catch {
    return false;
  }
}

/** Trims the editor's rows and drops the wholly blank ones, giving what to save. */
export function cleanLinks(rows: PlanLinkInput[]): PlanLinkInput[] {
  return rows
    .map((r) => ({ title: r.title.trim(), url: r.url.trim() }))
    .filter((r) => r.title !== '' || r.url !== '');
}

/** Whether the cleaned editor rows differ from the plan's saved links. The
 * server fills an empty title with the URL's host, so a blank title only counts
 * as unchanged against a saved title equal to that host. */
export function linksChanged(saved: PlanLink[], rows: PlanLinkInput[]): boolean {
  if (saved.length !== rows.length) return true;
  return saved.some((s, i) => {
    const r = rows[i];
    if (s.url !== r.url) return true;
    const title = r.title === '' ? hostOf(r.url) : r.title;
    return s.title !== title;
  });
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return '';
  }
}

const IMAGE_EXT = /\.(png|jpe?g|gif|webp|avif)$/i;

/** Whether a link looks like an image we can show in the app, judged by the
 * file extension on its path. Anything else opens in a new tab, since we can't
 * tell what a page is without fetching it, and most sites refuse framing. */
export function isInlineImage(url: string): boolean {
  try {
    const u = new URL(url);
    return u.protocol === 'https:' && IMAGE_EXT.test(u.pathname);
  } catch {
    return false;
  }
}
