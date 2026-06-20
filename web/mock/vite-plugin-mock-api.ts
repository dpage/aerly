/**
 * Vite dev-server plugin that intercepts API calls and returns mock data
 * when the MOCK environment variable is set to "1".
 *
 * Usage:  MOCK=1 npm run dev
 *
 * Intercepted routes (everything the app's boot sequence calls):
 *   GET  /api/me          → mock user (already "authenticated")
 *   GET  /api/config      → capabilities
 *   GET  /api/version     → build info
 *   GET  /api/trips       → 100 past + now + upcoming mock trips
 *   GET  /api/friends     → empty
 *   GET  /api/notifications → zero counts
 *   GET  /auth/providers  → empty (no OAuth buttons)
 *   GET  /auth/dev-info   → { enabled: true }  (shows dev-login form)
 *   GET  /auth/dev-login  → sets a fake session cookie, redirects to /
 *
 * Every other path is passed through (Vite serves the SPA shell, etc.).
 */

import type { Plugin, ViteDevServer } from 'vite';
import type { IncomingMessage, ServerResponse } from 'http';
import { generateMockTrips } from './trips';

function json(res: ServerResponse, body: unknown, status = 200) {
  const data = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(data),
  });
  res.end(data);
}

function redirect(res: ServerResponse, location: string) {
  res.writeHead(302, { Location: location });
  res.end();
}

const MOCK_USER = {
  id: 1,
  username: 'mockdev',
  name: 'Mock Dev User',
  avatar_url: '',
  is_superuser: false,
  is_active: true,
  has_logged_in: true,
  home_address: '',
  last_login_at: new Date().toISOString(),
};

const MOCK_CAPABILITIES = {
  resolver_available: false,
  poll_interval_sec: 60,
  email_ingest_enabled: false,
};

const MOCK_VERSION = {
  commit: 'mock',
  short: 'mock',
  build_time: new Date().toISOString(),
};

// Session cookie name — must match what the SPA reads for auth state.
// The app determines auth by calling /api/me; a 200 means authenticated.
// We just need the cookie to persist so /api/me keeps returning 200 after
// the redirect. The mock plugin intercepts /api/me unconditionally, so any
// cookie value works.
const SESSION_COOKIE = 'aerly_mock_session=1; Path=/; SameSite=Lax';

const MOCK_TRIPS = generateMockTrips();

// ── Demo trip with meeting + event plans ─────────────────────────────────────
const now = new Date();
function iso(offset: number, h = 10, m = 0) {
  const d = new Date(now);
  d.setUTCDate(d.getUTCDate() + offset);
  d.setUTCHours(h, m, 0, 0);
  return d.toISOString();
}
function ymd(offset: number) {
  const d = new Date(now);
  d.setUTCDate(d.getUTCDate() + offset);
  return d.toISOString().slice(0, 10);
}

const CONF_TRIP_ID = 9001;
const CONF_TRIP = {
  id: CONF_TRIP_ID,
  name: 'PGConf Europe 2026',
  destination: 'Vienna, Austria',
  starts_on: ymd(2),
  ends_on: ymd(5),
  my_role: 'owner',
  viewer_is_passenger: true,
  members: [{ user_id: 1, role: 'owner' }],
  passenger_ids: [1],
  tags: ['PGConf', 'Vienna', 'Conference'],
  country_code: 'at',
  reminder_opted_in: false,
  reminder_lead_hours: 24,
  created_at: now.toISOString(),
  updated_at: now.toISOString(),
};

const CONF_PLANS = [
  {
    id: 101,
    trip_id: CONF_TRIP_ID,
    type: 'flight',
    title: 'OS 123 LHR→VIE',
    confirmation_ref: 'ABCDEF',
    ticket_number: '',
    notes: '',
    source: 'manual',
    cost_currency: '',
    supplier_name: 'Austrian Airlines',
    contact_email: '',
    contact_phone: '',
    website: '',
    passenger_ids: [1],
    visibility: { mode: 'everyone', user_ids: [] },
    parts: [
      {
        id: 1001, plan_id: 101, type: 'flight', seq: 0,
        starts_at: iso(2, 8, 0), ends_at: iso(2, 11, 15),
        start_tz: 'Europe/London', end_tz: 'Europe/Vienna',
        start_label: 'LHR', end_label: 'VIE',
        start_address: '', end_address: '',
        status: 'confirmed', effective_at: iso(2, 8, 0),
        flight: {
          ident: 'OS123', callsign: 'AUA123',
          scheduled_out: iso(2, 8, 0), scheduled_in: iso(2, 11, 15),
          origin_iata: 'LHR', dest_iata: 'VIE',
          flight_status: 'Scheduled', resolved: true,
        },
      },
    ],
    created_at: now.toISOString(),
    updated_at: now.toISOString(),
  },
  {
    id: 102,
    trip_id: CONF_TRIP_ID,
    type: 'meeting',
    title: 'Speakers & Volunteers Stand-up',
    confirmation_ref: '',
    ticket_number: '',
    notes: 'Bring your badge',
    source: 'manual',
    cost_currency: '',
    supplier_name: '',
    contact_email: '',
    contact_phone: '',
    website: '',
    passenger_ids: [1],
    visibility: { mode: 'everyone', user_ids: [] },
    parts: [
      {
        id: 1002, plan_id: 102, type: 'meeting', seq: 0,
        starts_at: iso(3, 8, 30),
        start_tz: 'Europe/Vienna', end_tz: 'Europe/Vienna',
        start_label: 'Room A – Congress Center', end_label: '',
        start_address: 'Messe Wien Exhibition & Congress Center, Vienna', end_address: '',
        status: 'confirmed', effective_at: iso(3, 8, 30),
        meeting: { location: 'Room A – Congress Center', organiser: 'Dave Page', platform: '' },
      },
    ],
    created_at: now.toISOString(),
    updated_at: now.toISOString(),
  },
  {
    id: 103,
    trip_id: CONF_TRIP_ID,
    type: 'event',
    title: 'Keynote: The Future of Postgres',
    confirmation_ref: '',
    ticket_number: 'TKT-42',
    notes: '',
    source: 'manual',
    cost_currency: 'EUR',
    cost_amount: 0,
    supplier_name: '',
    contact_email: '',
    contact_phone: '',
    website: 'https://pgconf.eu',
    passenger_ids: [1],
    visibility: { mode: 'everyone', user_ids: [] },
    parts: [
      {
        id: 1003, plan_id: 103, type: 'event', seq: 0,
        starts_at: iso(3, 10, 0),
        start_tz: 'Europe/Vienna', end_tz: 'Europe/Vienna',
        start_label: 'Main Hall – Congress Center', end_label: '',
        start_address: 'Messe Wien Exhibition & Congress Center, Vienna', end_address: '',
        status: 'confirmed', effective_at: iso(3, 10, 0),
        event: {
          performer: 'Dave Page',
          category: 'Talk',
          venue_area: 'Main Hall',
          url: 'https://pgconf.eu/schedule',
        },
      },
    ],
    created_at: now.toISOString(),
    updated_at: now.toISOString(),
  },
  {
    id: 104,
    trip_id: CONF_TRIP_ID,
    type: 'event',
    title: 'Conference Dinner & Live Music',
    confirmation_ref: '',
    ticket_number: '',
    notes: 'Dinner at 19:00, band starts 21:00',
    source: 'manual',
    cost_currency: '',
    supplier_name: '',
    contact_email: '',
    contact_phone: '',
    website: '',
    passenger_ids: [1],
    visibility: { mode: 'everyone', user_ids: [] },
    parts: [
      {
        id: 1004, plan_id: 104, type: 'event', seq: 0,
        starts_at: iso(4, 19, 0),
        start_tz: 'Europe/Vienna', end_tz: 'Europe/Vienna',
        start_label: 'Palais Ferstel', end_label: '',
        start_address: 'Palais Ferstel, Strauchgasse 4, 1010 Vienna', end_address: '',
        status: 'confirmed', effective_at: iso(4, 19, 0),
        event: {
          performer: 'Vienna Jazz Ensemble',
          category: 'Concert',
          venue_area: 'Grand Ballroom',
          url: '',
        },
      },
    ],
    created_at: now.toISOString(),
    updated_at: now.toISOString(),
  },
];

MOCK_TRIPS.unshift(CONF_TRIP as unknown as (typeof MOCK_TRIPS)[0]);

export default function mockApiPlugin(): Plugin {
  return {
    name: 'aerly-mock-api',
    apply: 'serve',
    configureServer(server: ViteDevServer) {
      server.middlewares.use((req: IncomingMessage, res: ServerResponse, next: () => void) => {
        const url = req.url?.split('?')[0] ?? '';
        const method = req.method ?? 'GET';

        if (method === 'GET' && url === '/api/me') {
          return json(res, MOCK_USER);
        }
        if (method === 'GET' && url === '/api/config') {
          return json(res, MOCK_CAPABILITIES);
        }
        if (method === 'GET' && url === '/api/version') {
          return json(res, MOCK_VERSION);
        }
        if (method === 'GET' && url === '/api/trips') {
          return json(res, MOCK_TRIPS);
        }
        // Trip detail + plans — conference demo trip serves real data; all
        // others return the bare trip shape (no plans) so the page renders.
        if (method === 'GET' && url === `/api/trips/${CONF_TRIP_ID}`) {
          return json(res, { ...CONF_TRIP, plans: CONF_PLANS });
        }
        if (method === 'GET' && url === `/api/trips/${CONF_TRIP_ID}/plans`) {
          return json(res, CONF_PLANS);
        }
        // Catch-all for other trip detail/plan/attachment requests so the proxy
        // doesn't try to reach the (absent) Go backend and 500.
        if (method === 'GET' && /^\/api\/trips\/\d+$/.test(url)) {
          const id = Number(url.split('/').pop());
          const t = MOCK_TRIPS.find((tr) => tr.id === id);
          return t ? json(res, { ...t, plans: [] }) : json(res, { error: 'not found' }, 404);
        }
        if (method === 'GET' && /^\/api\/trips\/\d+\//.test(url)) {
          return json(res, []);
        }
        // Swallow mutations on the demo trip so the UI doesn't 404.
        // Scoped to the demo trip only — don't accidentally swallow mutations
        // for other routes (e.g. /api/trips POST to create a new trip).
        if (url.startsWith(`/api/trips/${CONF_TRIP_ID}`) && method !== 'GET') {
          return json(res, {});
        }
        if (method === 'GET' && url === '/api/friends') {
          return json(res, []);
        }
        if (method === 'GET' && url === '/api/notifications') {
          return json(res, { friend_requests_pending: 0, unread_alerts: 0, unread_shares: 0 });
        }
        if (method === 'GET' && url.startsWith('/api/me/auto-shares')) {
          return json(res, []);
        }
        if (method === 'GET' && url === '/api/alerts') {
          return json(res, []);
        }
        if (method === 'GET' && url.startsWith('/api/users')) {
          return json(res, []);
        }

        if (method === 'GET' && url === '/auth/providers') {
          return json(res, { providers: [] });
        }
        if (method === 'GET' && url === '/auth/dev-info') {
          return json(res, { enabled: true });
        }
        // Dev-login: set a mock session cookie and redirect home.
        if (method === 'GET' && url.startsWith('/auth/dev-login')) {
          res.setHeader('Set-Cookie', SESSION_COOKIE);
          return redirect(res, '/');
        }
        if (method === 'GET' && url === '/healthz') {
          res.writeHead(200);
          return res.end('ok');
        }

        next();
      });
    },
  };
}
