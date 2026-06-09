import type { FlightAlert, Notifications, TrackerPart } from './api/types';

export interface SSEHandlers {
  onNotifications: (n: Notifications) => void;
  /** A trackable part refreshed: the poller broadcasts plan_part.updated with
   * a TrackerPartDTO. Drives the tracker convergence list and the open trip's
   * timeline live (PRD live-updating shared timeline). */
  onPlanPart: (part: TrackerPart) => void;
  /** Optional: a trip's metadata changed. The backend emits `trip.updated`
   * after trip create/update/delete, member, and tag mutations, scoped to the
   * trip's members. Payload carries at least the trip id. */
  onTrip?: (id: number) => void;
  /** Optional: a plan changed or was deleted. The backend emits `plan.updated`
   * and `plan.deleted` after plan mutations; both are routed here. Payload
   * carries at least the trip id so the client can refresh the right trip. */
  onPlan?: (tripId: number) => void;
  /** A flight-change alert arrived for the viewer. The poller publishes
   * alert.created (user-private) carrying a NotificationsDTO with `alert` set. */
  onAlert?: (alert: FlightAlert) => void;
}

export interface SSEOptions {
  /** Only honored server-side for superusers; the param is otherwise ignored. */
  showAll?: boolean;
}

// connectSSE returns a teardown function. It auto-reconnects with backoff on
// transient errors. The poller pushes plan_part.updated events carrying the
// locked TrackerPartDTO; notifications.updated tracks the friendship badge;
// trip.updated / plan.updated / plan.deleted fire on user-driven trip & plan
// edits so the open trip + tracker refresh live.
export function connectSSE(handlers: SSEHandlers, opts: SSEOptions = {}): () => void {
  let es: EventSource | null = null;
  let stopped = false;
  let retry = 1000;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  const url = opts.showAll ? '/api/events?show_all=1' : '/api/events';

  function open() {
    if (stopped) return;
    es = new EventSource(url, { withCredentials: true });
    es.addEventListener('open', () => {
      retry = 1000;
    });
    es.addEventListener('plan_part.updated', (ev) => {
      try {
        const part = JSON.parse((ev as MessageEvent).data) as TrackerPart;
        handlers.onPlanPart(part);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
    es.addEventListener('trip.updated', (ev) => {
      try {
        const { id } = JSON.parse((ev as MessageEvent).data) as { id: number };
        handlers.onTrip?.(id);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
    const onPlanEvent = (ev: Event) => {
      try {
        const { trip_id } = JSON.parse((ev as MessageEvent).data) as { trip_id: number };
        handlers.onPlan?.(trip_id);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    };
    es.addEventListener('plan.updated', onPlanEvent);
    es.addEventListener('plan.deleted', onPlanEvent);
    es.addEventListener('notifications.updated', (ev) => {
      try {
        const n = JSON.parse((ev as MessageEvent).data) as Notifications;
        handlers.onNotifications(n);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
    es.addEventListener('alert.created', (ev) => {
      try {
        const { alert } = JSON.parse((ev as MessageEvent).data) as { alert?: FlightAlert };
        if (alert) handlers.onAlert?.(alert);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
    es.addEventListener('error', () => {
      es?.close();
      es = null;
      if (stopped) return;
      const delay = Math.min(retry, 30_000);
      retry = Math.min(retry * 2, 30_000);
      reconnectTimer = setTimeout(open, delay);
    });
  }

  open();
  return () => {
    stopped = true;
    // Clear any pending reconnect so a teardown during the backoff window
    // doesn't leave a timer that fires after unmount (and so rapid
    // re-subscribes can't stack timers / race connections).
    if (reconnectTimer !== undefined) clearTimeout(reconnectTimer);
    es?.close();
  };
}
