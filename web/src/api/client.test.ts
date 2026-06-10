import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';

import { api, ApiError } from './client';

function mockFetch(impl: (path: string, init?: RequestInit) => Response | Promise<Response>) {
  const spy = vi.fn(impl);
  globalThis.fetch = spy as unknown as typeof fetch;
  return spy;
}

function jsonResponse(body: unknown, status = 200): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  } as unknown as Response;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('request via api.*', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('returns undefined for 204', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    const out = await api.deleteUser(7);
    expect(out).toBeUndefined();
  });

  it('throws ApiError with server {error} message on non-ok JSON', async () => {
    mockFetch(() => jsonResponse({ error: 'boom' }, 400));
    await expect(api.listFlights()).rejects.toMatchObject({
      name: 'Error',
      status: 400,
      message: 'boom',
    });
  });

  it('keeps status-only message when non-ok body is not JSON', async () => {
    mockFetch(
      () =>
        ({
          status: 500,
          ok: false,
          json: async () => {
            throw new Error('not json');
          },
        }) as unknown as Response,
    );
    await expect(api.listUsers()).rejects.toMatchObject({ status: 500, message: 'HTTP 500' });
  });

  it('keeps status-only message when JSON has no error field', async () => {
    mockFetch(() => jsonResponse({ nope: true }, 403));
    await expect(api.getMe()).rejects.toMatchObject({ status: 403, message: 'HTTP 403' });
  });

  it('returns parsed JSON on ok', async () => {
    mockFetch(() => jsonResponse([{ id: 1 }]));
    const flights = await api.listFlights();
    expect(flights).toEqual([{ id: 1 }]);
  });

  it('sends a JSON body and Content-Type when a body is provided', async () => {
    const spy = mockFetch(() => jsonResponse({ ident: 'BA1' }));
    await api.resolveFlight({ ident: 'BA1', date: '2024-01-01' });
    const [path, init] = spy.mock.calls[0];
    expect(path).toBe('/api/flights/resolve');
    expect(init?.method).toBe('POST');
    expect(init?.body).toBe(JSON.stringify({ ident: 'BA1', date: '2024-01-01' }));
    expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
  });

  it('omits body and Content-Type when there is no body', async () => {
    const spy = mockFetch(() => jsonResponse({ id: 1 }));
    await api.getMe();
    const [, init] = spy.mock.calls[0];
    expect(init?.body).toBeUndefined();
    expect((init?.headers as Record<string, string>)['Content-Type']).toBeUndefined();
  });
});

describe('api.isAuthError', () => {
  it('true only for ApiError with status 401', () => {
    expect(api.isAuthError(new ApiError(401, 'x'))).toBe(true);
    expect(api.isAuthError(new ApiError(500, 'x'))).toBe(false);
    expect(api.isAuthError(new Error('x'))).toBe(false);
    expect(api.isAuthError('x')).toBe(false);
  });
});

describe('every api.* method calls fetch with the right method/path/body', () => {
  let spy: ReturnType<typeof mockFetch>;
  beforeEach(() => {
    spy = mockFetch(() => jsonResponse({ ok: true }));
  });

  const last = () => spy.mock.calls[spy.mock.calls.length - 1];

  it('getMe', async () => {
    await api.getMe();
    expect(last()[0]).toBe('/api/me');
    expect(last()[1]?.method).toBe('GET');
  });

  it('getConfig', async () => {
    await api.getConfig();
    expect(last()[0]).toBe('/api/config');
  });

  it('listFlights reads the plan-model rollup endpoint', async () => {
    await api.listFlights();
    expect(last()[0]).toBe('/api/me/flights');
  });

  it('listFlights ignores legacy opts (endpoint returns full history)', async () => {
    await api.listFlights({ showAll: true, showOld: true });
    // The legacy /api/flights query flags are gone; the rollup endpoint takes
    // no params and always returns the viewer's full flight history.
    expect(last()[0]).toBe('/api/me/flights');
  });

  it('resolveFlight', async () => {
    await api.resolveFlight({ ident: 'BA1', date: '2024-01-01' });
    expect(last()[0]).toBe('/api/flights/resolve');
    expect(last()[1]?.method).toBe('POST');
  });

  it('listUsers', async () => {
    await api.listUsers();
    expect(last()[0]).toBe('/api/users');
  });

  it('inviteUser', async () => {
    await api.inviteUser({ username: 'oct' });
    expect(last()[0]).toBe('/api/users');
    expect(last()[1]?.method).toBe('POST');
  });

  it('updateUser', async () => {
    await api.updateUser(2, { name: 'n' });
    expect(last()[0]).toBe('/api/users/2');
    expect(last()[1]?.method).toBe('PATCH');
  });

  it('deleteUser', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.deleteUser(2);
  });

  it('listMyEmails', async () => {
    await api.listMyEmails();
    expect(last()[0]).toBe('/api/me/emails');
    expect(last()[1]?.method).toBe('GET');
  });

  it('addMyEmail', async () => {
    await api.addMyEmail('alice@example.com');
    expect(last()[0]).toBe('/api/me/emails');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ address: 'alice@example.com' }));
  });

  it('resendMyEmail', async () => {
    await api.resendMyEmail(7);
    expect(last()[0]).toBe('/api/me/emails/7/resend');
    expect(last()[1]?.method).toBe('POST');
  });

  it('deleteMyEmail', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.deleteMyEmail(7);
  });

  it('listFriends', async () => {
    await api.listFriends();
    expect(last()[0]).toBe('/api/friends');
    expect(last()[1]?.method).toBe('GET');
  });

  it('inviteFriend posts the email body and resolves undefined (no-leak shape)', async () => {
    const r = await api.inviteFriend({ email: 'bob@example.com', message: 'hi' });
    expect(r).toBeUndefined();
    expect(last()[0]).toBe('/api/friends/invite');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ email: 'bob@example.com', message: 'hi' }));
  });

  it('acceptFriend', async () => {
    await api.acceptFriend(5);
    expect(last()[0]).toBe('/api/friends/5/accept');
    expect(last()[1]?.method).toBe('POST');
  });

  it('removeFriend', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.removeFriend(5);
  });

  it('logout posts to /auth/logout and resolves undefined', async () => {
    const s = mockFetch(() => Promise.resolve({ status: 200, ok: true } as unknown as Response));
    const r = await api.logout();
    expect(r).toBeUndefined();
    expect(s.mock.calls[0][0]).toBe('/auth/logout');
    expect(s.mock.calls[0][1]?.method).toBe('POST');
  });

  it('ingest sends JSON when no file is attached', async () => {
    await api.ingest(3, { text: 'a paste', source: 'paste' });
    expect(last()[0]).toBe('/api/trips/3/ingest');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ text: 'a paste', source: 'paste' }));
    // JSON path sets the Content-Type header.
    const headers = last()[1]?.headers as Record<string, string>;
    expect(headers['Content-Type']).toBe('application/json');
  });

  it('ingest sends multipart/form-data when a file is attached', async () => {
    const file = new File([new Uint8Array([1, 2, 3])], 'ticket.pdf', {
      type: 'application/pdf',
    });
    await api.ingest(3, { file, source: 'upload' });
    expect(last()[0]).toBe('/api/trips/3/ingest');
    expect(last()[1]?.method).toBe('POST');
    const body = last()[1]?.body;
    expect(body).toBeInstanceOf(FormData);
    const form = body as FormData;
    expect((form.get('file') as File).name).toBe('ticket.pdf');
    expect(form.get('source')).toBe('upload');
    // The browser must set the multipart boundary itself, so we must NOT send
    // an explicit Content-Type header.
    const headers = last()[1]?.headers as Record<string, string>;
    expect(headers['Content-Type']).toBeUndefined();
  });

  it('getTracker passes the from/to/tag query params', async () => {
    await api.getTracker({ from: '2026-10-01', to: '2026-10-31', tag: 'pgconf' });
    expect(last()[0]).toBe('/api/tracker?from=2026-10-01&to=2026-10-31&tag=pgconf');
    expect(last()[1]?.method).toBe('GET');
  });

  it('getTracker hits the bare endpoint when given no opts', async () => {
    await api.getTracker();
    expect(last()[0]).toBe('/api/tracker');
    expect(last()[1]?.method).toBe('GET');
  });

  it('updateMe', async () => {
    await api.updateMe({ home_address: '1 Main St' });
    expect(last()[0]).toBe('/api/me');
    expect(last()[1]?.method).toBe('PATCH');
    expect(last()[1]?.body).toBe(JSON.stringify({ home_address: '1 Main St' }));
  });

  it('cancelOutgoingInvite sends the email body and resolves undefined', async () => {
    const r = await api.cancelOutgoingInvite('bob@example.com');
    expect(r).toBeUndefined();
    expect(last()[0]).toBe('/api/friends/outgoing');
    expect(last()[1]?.method).toBe('DELETE');
    expect(last()[1]?.body).toBe(JSON.stringify({ email: 'bob@example.com' }));
  });

  it('listTrips', async () => {
    await api.listTrips();
    expect(last()[0]).toBe('/api/trips');
    expect(last()[1]?.method).toBe('GET');
  });

  it('getTrip', async () => {
    await api.getTrip(8);
    expect(last()[0]).toBe('/api/trips/8');
    expect(last()[1]?.method).toBe('GET');
  });

  it('createTrip', async () => {
    await api.createTrip({ name: 'Lisbon' });
    expect(last()[0]).toBe('/api/trips');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ name: 'Lisbon' }));
  });

  it('updateTrip', async () => {
    await api.updateTrip(8, { name: 'Porto' });
    expect(last()[0]).toBe('/api/trips/8');
    expect(last()[1]?.method).toBe('PATCH');
  });

  it('deleteTrip', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.deleteTrip(8);
  });

  it('addTripMember', async () => {
    await api.addTripMember(8, { user_id: 2, role: 'viewer' });
    expect(last()[0]).toBe('/api/trips/8/members');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ user_id: 2, role: 'viewer' }));
  });

  it('removeTripMember', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.removeTripMember(8, 2);
  });

  it('addTripPassenger', async () => {
    await api.addTripPassenger(8, 2);
    expect(last()[0]).toBe('/api/trips/8/passengers');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ user_id: 2 }));
  });

  it('removeTripPassenger', async () => {
    const s = mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.removeTripPassenger(8, 2);
    expect(s.mock.calls[0][0]).toBe('/api/trips/8/passengers/2');
    expect(s.mock.calls[0][1]?.method).toBe('DELETE');
  });

  it('setTripTags', async () => {
    await api.setTripTags(8, ['pgconf', 'work']);
    expect(last()[0]).toBe('/api/trips/8/tags');
    expect(last()[1]?.method).toBe('PUT');
    expect(last()[1]?.body).toBe(JSON.stringify({ labels: ['pgconf', 'work'] }));
  });

  it('suggestTags url-encodes the query', async () => {
    await api.suggestTags('p g');
    expect(last()[0]).toBe('/api/tags/suggest?q=p%20g');
    expect(last()[1]?.method).toBe('GET');
  });

  it('createPlan', async () => {
    await api.createPlan(8, { type: 'flight', title: 'Out' } as Parameters<
      typeof api.createPlan
    >[1]);
    expect(last()[0]).toBe('/api/trips/8/plans');
    expect(last()[1]?.method).toBe('POST');
  });

  it('updatePlan', async () => {
    await api.updatePlan(3, { title: 'New' });
    expect(last()[0]).toBe('/api/plans/3');
    expect(last()[1]?.method).toBe('PATCH');
  });

  it('deletePlan', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.deletePlan(3);
  });

  it('addPlanPassenger', async () => {
    await api.addPlanPassenger(3, 2);
    expect(last()[0]).toBe('/api/plans/3/passengers');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ user_id: 2 }));
  });

  it('removePlanPassenger', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.removePlanPassenger(3, 2);
  });

  it('setPlanVisibility', async () => {
    await api.setPlanVisibility(3, { mode: 'everyone', user_ids: [] });
    expect(last()[0]).toBe('/api/plans/3/visibility');
    expect(last()[1]?.method).toBe('PUT');
  });

  it('movePlan', async () => {
    await api.movePlan(3, { trip_id: 9 });
    expect(last()[0]).toBe('/api/plans/3/move');
    expect(last()[1]?.method).toBe('POST');
  });

  it('updatePlanPart', async () => {
    await api.updatePlanPart(5, { starts_at: '2026-10-12T09:00:00Z' });
    expect(last()[0]).toBe('/api/plan-parts/5');
    expect(last()[1]?.method).toBe('PATCH');
  });

  it('dismissPlanPart', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.dismissPlanPart(5);
  });

  it('ingestConfirm', async () => {
    await api.ingestConfirm(8, []);
    expect(last()[0]).toBe('/api/trips/8/ingest/confirm');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ plans: [] }));
  });

  it('listCalendarTokens', async () => {
    await api.listCalendarTokens();
    expect(last()[0]).toBe('/api/calendar/tokens');
    expect(last()[1]?.method).toBe('GET');
  });

  it('issueCalendarToken', async () => {
    await api.issueCalendarToken('me');
    expect(last()[0]).toBe('/api/calendar/tokens');
    expect(last()[1]?.method).toBe('POST');
    expect(last()[1]?.body).toBe(JSON.stringify({ scope: 'me', id: undefined }));
  });

  it('revokeCalendarToken url-encodes the token', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.revokeCalendarToken('a/b c');
  });

  it('getAlertPrefs', async () => {
    await api.getAlertPrefs();
    expect(last()[0]).toBe('/api/alert-prefs');
    expect(last()[1]?.method).toBe('GET');
  });

  it('updateAlertPrefs', async () => {
    await api.updateAlertPrefs({} as Parameters<typeof api.updateAlertPrefs>[0]);
    expect(last()[0]).toBe('/api/alert-prefs');
    expect(last()[1]?.method).toBe('PUT');
  });

  it('optInPlanAlerts', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.optInPlanAlerts(3);
  });

  it('optOutPlanAlerts', async () => {
    mockFetch(() => ({ status: 204, ok: true }) as unknown as Response);
    await api.optOutPlanAlerts(3);
  });

  it('setTripReminder PUTs the lead hours', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 204));
    await api.setTripReminder(7, 12);
    expect(spy.mock.calls[0][0]).toBe('/api/trips/7/reminder');
    expect(spy.mock.calls[0][1]).toMatchObject({
      method: 'PUT',
      body: JSON.stringify({ lead_hours: 12 }),
    });
  });

  it('clearTripReminder DELETEs the trip reminder', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 204));
    await api.clearTripReminder(7);
    expect(spy.mock.calls[0][0]).toBe('/api/trips/7/reminder');
    expect(spy.mock.calls[0][1]).toMatchObject({ method: 'DELETE' });
  });

  it('setPlanReminder PUTs enabled + lead hours', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 204));
    await api.setPlanReminder(3, true, 6);
    expect(spy.mock.calls[0][0]).toBe('/api/plans/3/reminder');
    expect(spy.mock.calls[0][1]).toMatchObject({
      method: 'PUT',
      body: JSON.stringify({ enabled: true, lead_hours: 6 }),
    });
  });

  it('clearPlanReminder DELETEs the plan reminder', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 204));
    await api.clearPlanReminder(3);
    expect(spy.mock.calls[0][0]).toBe('/api/plans/3/reminder');
    expect(spy.mock.calls[0][1]).toMatchObject({ method: 'DELETE' });
  });
});

describe('revokeCalendarToken / issueCalendarToken paths', () => {
  it('revokeCalendarToken hits the encoded token path', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 204));
    await api.revokeCalendarToken('a/b c');
    expect(spy.mock.calls[0][0]).toBe('/api/calendar/tokens/a%2Fb%20c');
    expect(spy.mock.calls[0][1]?.method).toBe('DELETE');
  });

  it('issueCalendarToken forwards an explicit id for regeneration', async () => {
    const spy = mockFetch(() => jsonResponse({ token: 't' }));
    await api.issueCalendarToken('trip', 42);
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ scope: 'trip', id: 42 }));
  });
});

describe('api.getAuthProviders', () => {
  it('returns the providers array on 200', async () => {
    const spy = mockFetch(() =>
      jsonResponse({
        providers: [
          { name: 'github', label: 'GitHub' },
          { name: 'google', label: 'Google' },
        ],
      }),
    );
    await expect(api.getAuthProviders()).resolves.toEqual([
      { name: 'github', label: 'GitHub' },
      { name: 'google', label: 'Google' },
    ]);
    expect(spy.mock.calls[0][0]).toBe('/auth/providers');
  });

  it('returns an empty list on non-ok responses', async () => {
    mockFetch(() => jsonResponse({}, 500));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('returns an empty list when the body lacks providers', async () => {
    mockFetch(() => jsonResponse({}));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('returns an empty list when fetch rejects (network down)', async () => {
    mockFetch(() => Promise.reject(new Error('boom')));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('returns an empty list when providers is not an array', async () => {
    mockFetch(() => jsonResponse({ providers: 'oops' }));
    await expect(api.getAuthProviders()).resolves.toEqual([]);
  });

  it('drops entries that do not match the AuthProvider shape', async () => {
    mockFetch(() =>
      jsonResponse({
        providers: [
          { name: 'github', label: 'GitHub' },
          // missing label
          { name: 'no-label' },
          // wrong types
          { name: 42, label: 'Bad' },
          null,
          'not-an-object',
          { name: 'google', label: 'Google' },
        ],
      }),
    );
    await expect(api.getAuthProviders()).resolves.toEqual([
      { name: 'github', label: 'GitHub' },
      { name: 'google', label: 'Google' },
    ]);
  });
});

describe('api.getDevAuthBypassEnabled', () => {
  it('returns true when /auth/dev-info responds with enabled=true', async () => {
    const spy = mockFetch(() => jsonResponse({ enabled: true }));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(true);
    expect(spy.mock.calls[0][0]).toBe('/auth/dev-info');
  });

  it('returns false on 404 (route only registered when DEV_AUTH_BYPASS=1)', async () => {
    mockFetch(() => jsonResponse({}, 404));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(false);
  });

  it('returns false when the JSON body lacks enabled=true', async () => {
    mockFetch(() => jsonResponse({ enabled: false }));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(false);
  });

  it('returns false when fetch rejects (network down)', async () => {
    mockFetch(() => Promise.reject(new Error('boom')));
    await expect(api.getDevAuthBypassEnabled()).resolves.toBe(false);
  });
});

describe('setTripShareAllFriends', () => {
  it('PUT /api/trips/:id/share-all-friends with the given role', async () => {
    const spy = mockFetch(() => jsonResponse({ id: 7 }));
    await api.setTripShareAllFriends(7, 'viewer');
    expect(spy.mock.calls[0][0]).toBe('/api/trips/7/share-all-friends');
    expect(spy.mock.calls[0][1]?.method).toBe('PUT');
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ role: 'viewer' }));
  });

  it('sends {role:""} when role is null (disable share-all-friends)', async () => {
    const spy = mockFetch(() => jsonResponse({ id: 7 }));
    await api.setTripShareAllFriends(7, null);
    expect(spy.mock.calls[0][0]).toBe('/api/trips/7/share-all-friends');
    expect(spy.mock.calls[0][1]?.method).toBe('PUT');
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ role: '' }));
  });
});

describe('shareTripByEmail', () => {
  it('POST /api/trips/:id/share-by-email with the email + role', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 202));
    await api.shareTripByEmail(7, { email: 'x@y.com', role: 'viewer' });
    expect(spy.mock.calls[0][0]).toBe('/api/trips/7/share-by-email');
    expect(spy.mock.calls[0][1]?.method).toBe('POST');
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ email: 'x@y.com', role: 'viewer' }));
  });
});

describe('sharePlanByEmail', () => {
  it('POST /api/plans/:id/share-by-email with the email', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 202));
    await api.sharePlanByEmail(42, 'x@y.com');
    expect(spy.mock.calls[0][0]).toBe('/api/plans/42/share-by-email');
    expect(spy.mock.calls[0][1]?.method).toBe('POST');
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ email: 'x@y.com' }));
  });
});

describe('notifications', () => {
  it('GET /api/notifications returns the typed body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ friend_requests_pending: 3 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const n = await api.getNotifications();
    expect(n.friend_requests_pending).toBe(3);
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/notifications',
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('POST /api/friends/accept-token sends the token in the body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ already: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const r = await api.acceptFriendToken('abc.def');
    expect(r.already).toBe(true);
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/friends/accept-token',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ token: 'abc.def' }),
      }),
    );
  });
});

describe('setPlanShareAllFriends', () => {
  it('PUT /api/plans/:id/share-all-friends with {enabled}', async () => {
    const spy = mockFetch(() => jsonResponse({ id: 42 }));
    await api.setPlanShareAllFriends(42, true);
    expect(spy.mock.calls[0][0]).toBe('/api/plans/42/share-all-friends');
    expect(spy.mock.calls[0][1]?.method).toBe('PUT');
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ enabled: true }));
  });
});

describe('notifyTripShares', () => {
  it('POST /api/trips/:id/notify-shares with user_ids + emails', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 204));
    await api.notifyTripShares(7, { user_ids: [1, 2], emails: ['a@b.com'] });
    expect(spy.mock.calls[0][0]).toBe('/api/trips/7/notify-shares');
    expect(spy.mock.calls[0][1]?.method).toBe('POST');
    expect(spy.mock.calls[0][1]?.body).toBe(
      JSON.stringify({ user_ids: [1, 2], emails: ['a@b.com'] }),
    );
  });
});

describe('notifyPlanShares', () => {
  it('POST /api/plans/:id/notify-shares with user_ids + emails', async () => {
    const spy = mockFetch(() => jsonResponse(undefined, 204));
    await api.notifyPlanShares(42, { user_ids: [3], emails: [] });
    expect(spy.mock.calls[0][0]).toBe('/api/plans/42/notify-shares');
    expect(spy.mock.calls[0][1]?.method).toBe('POST');
    expect(spy.mock.calls[0][1]?.body).toBe(JSON.stringify({ user_ids: [3], emails: [] }));
  });
});
