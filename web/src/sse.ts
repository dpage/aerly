import type { Flight } from './api/types';

export interface SSEHandlers {
  onFlight: (flight: Flight) => void;
  onDelete: (id: number) => void;
}

// connectSSE returns a teardown function. It auto-reconnects with backoff on
// transient errors. The server pushes flight.updated events from both the
// poller and user-driven writes (create / update / passenger ops) and
// flight.deleted events when a flight is removed.
export function connectSSE(handlers: SSEHandlers): () => void {
  let es: EventSource | null = null;
  let stopped = false;
  let retry = 1000;

  function open() {
    if (stopped) return;
    es = new EventSource('/api/events', { withCredentials: true });
    es.addEventListener('open', () => {
      retry = 1000;
    });
    es.addEventListener('flight.updated', (ev) => {
      try {
        const f = JSON.parse((ev as MessageEvent).data) as Flight;
        handlers.onFlight(f);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
    es.addEventListener('flight.deleted', (ev) => {
      try {
        const { id } = JSON.parse((ev as MessageEvent).data) as { id: number };
        handlers.onDelete(id);
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
      setTimeout(open, delay);
    });
  }

  open();
  return () => {
    stopped = true;
    es?.close();
  };
}
