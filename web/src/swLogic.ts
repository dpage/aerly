// Pure, runtime-independent logic for the service worker's push handling.
//
// The service worker itself (src/sw.ts) can't be unit-tested — it references
// the ServiceWorkerGlobalScope `self` and imports Workbox — so everything that
// can be expressed as a plain function lives here and is covered by
// swLogic.test.ts. sw.ts stays a thin shell wiring these into the SW events.

/** The JSON shape the backend (internal/push.Payload) sends in a push. */
export interface PushPayload {
  title: string;
  body: string;
  url?: string;
  tag?: string;
  kind?: string;
}

/** Fallback notification title when a push arrives without (or with a broken)
 * payload — better a generic nudge than a silent drop. */
export const DEFAULT_TITLE = 'Aerly';

/** Parse the raw push data into a PushPayload, tolerating missing/garbage
 * input: invalid JSON becomes a body-only notification, missing fields fall
 * back to sensible defaults. Never throws. */
export function parsePushPayload(raw: string | null | undefined): PushPayload {
  if (!raw) return { title: DEFAULT_TITLE, body: '' };
  let j: Partial<PushPayload>;
  try {
    j = JSON.parse(raw) as Partial<PushPayload>;
  } catch {
    // Not JSON: show the raw text as the body.
    return { title: DEFAULT_TITLE, body: raw };
  }
  return {
    title: typeof j.title === 'string' && j.title !== '' ? j.title : DEFAULT_TITLE,
    body: typeof j.body === 'string' ? j.body : '',
    url: typeof j.url === 'string' ? j.url : undefined,
    tag: typeof j.tag === 'string' ? j.tag : undefined,
    kind: typeof j.kind === 'string' ? j.kind : undefined,
  };
}

/** A shown notification: the title plus the options bag passed to
 * showNotification. */
export interface ShownNotification {
  title: string;
  options: NotificationOptions;
}

/** Build the showNotification arguments from a payload. The deep-link URL is
 * stashed in `data` so the notificationclick handler can route to it. */
export function toNotification(p: PushPayload): ShownNotification {
  return {
    title: p.title,
    options: {
      body: p.body,
      tag: p.tag,
      icon: '/pwa-192.png',
      badge: '/pwa-192.png',
      data: { url: p.url ?? '/' },
    },
  };
}

/** The slice of WindowClient the click/suppression logic needs, narrowed to an
 * interface so it can be exercised without the SW runtime. */
export interface FocusableClient {
  focused: boolean;
  url: string;
  focus(): Promise<unknown>;
  navigate?(url: string): Promise<unknown>;
}

/** Whether to suppress the OS notification: only when a client window is
 * actually focused, since the open, in-focus app has already been updated live
 * over SSE. A backgrounded (open but not focused) tab still gets the push. */
export function hasFocusedClient(clients: readonly FocusableClient[]): boolean {
  return clients.some((c) => c.focused);
}

/** Extract the deep-link URL stashed on a notification's `data`, defaulting to
 * the app root when absent or malformed. */
export function urlForNotification(data: unknown): string {
  if (data && typeof data === 'object') {
    const u = (data as { url?: unknown }).url;
    if (typeof u === 'string' && u !== '') return u;
  }
  return '/';
}

/** Handle a notification click: focus an already-open window (navigating it to
 * the target first when it supports it), or open a fresh one. */
export async function focusOrOpen(
  url: string,
  clients: readonly FocusableClient[],
  openWindow: (url: string) => Promise<unknown>,
): Promise<void> {
  const existing = clients[0];
  if (existing) {
    if (existing.navigate) await existing.navigate(url);
    await existing.focus();
    return;
  }
  await openWindow(url);
}
