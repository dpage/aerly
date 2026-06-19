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
 *   GET  /api/me/friends  → empty
 *   GET  /api/me/notifications → zero counts
 *   GET  /auth/providers  → empty (no OAuth buttons)
 *   GET  /auth/dev-info   → { enabled: true }  (shows dev-login form)
 *   GET  /auth/dev-login  → sets a fake session cookie, redirects to /
 *
 * Every other path is passed through (Vite serves the SPA shell, etc.).
 */

import type { Plugin, ViteDevServer } from 'vite';
import type { IncomingMessage, ServerResponse } from 'http';
import { generateMockTrips } from './trips.js';

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
        if (method === 'GET' && url === '/api/me/friends') {
          return json(res, []);
        }
        if (method === 'GET' && url === '/api/friends') {
          return json(res, []);
        }
        if (method === 'GET' && url === '/api/me/notifications') {
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
