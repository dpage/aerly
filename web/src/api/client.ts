import type {
  AcceptFriendTokenResult,
  AddTripMemberInput,
  AlertPrefs,
  AuthProvider,
  CalendarScope,
  CalendarToken,
  Capabilities,
  ConfirmPlanInput,
  CreatePlanInput,
  CreateTripInput,
  Flight,
  FlightAlert,
  Friendship,
  IngestInput,
  IngestResult,
  ImportResult,
  InviteFriendInput,
  InviteUserInput,
  LinkPlansInput,
  MovePlanInput,
  Notifications,
  Plan,
  PlanPart,
  PlanVisibility,
  ResolveFlightInput,
  ResolvedFlight,
  TagSuggestion,
  TrackerResponse,
  Trip,
  UpdateAlertPrefsInput,
  UpdatePlanInput,
  UpdatePlanPartInput,
  UpdateTripInput,
  UpdateUserInput,
  User,
  UserEmail,
} from './types';

class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    credentials: 'include',
    headers: { Accept: 'application/json' },
  };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
  }
  const res = await fetch(path, init);
  return handleResponse<T>(res);
}

// requestMultipart sends a FormData body. The Content-Type header is left unset
// on purpose so the browser fills in the multipart boundary; everything else
// (credentials, error shape) matches request().
async function requestMultipart<T>(method: string, path: string, form: FormData): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers: { Accept: 'application/json' },
    body: form,
  });
  return handleResponse<T>(res);
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (res.status === 204) return undefined as T;
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const j = (await res.json()) as { error?: string };
      if (j.error) msg = j.error;
    } catch {
      // body wasn't JSON; keep status-only message.
    }
    throw new ApiError(res.status, msg);
  }
  return (await res.json()) as T;
}

export const api = {
  isAuthError(err: unknown): boolean {
    return err instanceof ApiError && err.status === 401;
  },

  getMe: () => request<User>('GET', '/api/me'),
  updateMe: (patch: { home_address?: string }) => request<User>('PATCH', '/api/me', patch),
  getConfig: () => request<Capabilities>('GET', '/api/config'),

  // Lists the OAuth providers the backend has configured, so the login
  // page can render one button per provider. Returns an empty list on
  // network errors so the page can fall back to the dev-login form.
  // The payload is shape-narrowed before we trust it — a malformed
  // `providers` field (non-array, or entries missing name/label) is
  // treated as empty rather than propagated to consumers.
  async getAuthProviders(): Promise<AuthProvider[]> {
    try {
      const res = await fetch('/auth/providers', {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      });
      if (!res.ok) return [];
      const j = (await res.json()) as { providers?: unknown };
      if (!Array.isArray(j.providers)) return [];
      return j.providers.filter(
        (p): p is AuthProvider =>
          typeof p === 'object' &&
          p !== null &&
          typeof (p as { name?: unknown }).name === 'string' &&
          typeof (p as { label?: unknown }).label === 'string',
      );
    } catch {
      return [];
    }
  },

  // Probes the dev-only DEV_AUTH_BYPASS endpoint. Returns true when the
  // backend is running with DEV_AUTH_BYPASS=1 (the route only exists then),
  // false otherwise (404, network error, non-OK response). The login page
  // uses this to decide whether to render the dev-login form.
  async getDevAuthBypassEnabled(): Promise<boolean> {
    try {
      const res = await fetch('/auth/dev-info', {
        credentials: 'include',
        headers: { Accept: 'application/json' },
      });
      if (!res.ok) return false;
      const j = (await res.json()) as { enabled?: boolean };
      return j.enabled === true;
    } catch {
      return false;
    }
  },

  // The legacy single-flight collection routes were retired in the trip-planning
  // cut-over. Two pieces survive:
  //   - listFlights backs the Statistics dialog's flown/upcoming rollup. It now
  //     reads /api/me/flights, which rebuilds the FlightDTO shape from the plan
  //     model (the viewer's flight-type plan_parts) — the legacy flights table
  //     is gone. The opts are accepted for call-site compatibility but ignored;
  //     the endpoint always returns the viewer's full flight history.
  //   - resolveFlight backs the manual flight-add (ident + date → metadata).
  listFlights: (_opts?: { showAll?: boolean; showOld?: boolean }) =>
    request<Flight[]>('GET', '/api/me/flights'),
  resolveFlight: (input: ResolveFlightInput) =>
    request<ResolvedFlight>('POST', '/api/flights/resolve', input),

  listUsers: () => request<User[]>('GET', '/api/users'),
  inviteUser: (input: InviteUserInput) => request<User>('POST', '/api/users', input),
  updateUser: (id: number, patch: UpdateUserInput) =>
    request<User>('PATCH', `/api/users/${id}`, patch),
  deleteUser: (id: number) => request<void>('DELETE', `/api/users/${id}`),

  listFriends: () => request<Friendship[]>('GET', '/api/friends'),
  // The server returns the same response for "matched an existing user"
  // and "queued an invite to an unknown address" so callers can't enumerate
  // registered users. We expose a single Promise<void> reflecting that.
  inviteFriend: (input: InviteFriendInput) =>
    request<void>('POST', '/api/friends/invite', input).then(() => undefined),
  acceptFriend: (userId: number) => request<Friendship>('POST', `/api/friends/${userId}/accept`),
  removeFriend: (userId: number) => request<void>('DELETE', `/api/friends/${userId}`),
  cancelOutgoingInvite: (email: string) =>
    request<void>('DELETE', '/api/friends/outgoing', { email }).then(() => undefined),
  acceptFriendToken: (token: string) =>
    request<AcceptFriendTokenResult>('POST', '/api/friends/accept-token', { token }),

  getNotifications: () => request<Notifications>('GET', '/api/notifications'),
  getAlerts: () => request<FlightAlert[]>('GET', '/api/alerts'),
  markAlertsRead: () => request<void>('POST', '/api/alerts/read'),

  listMyEmails: () => request<UserEmail[]>('GET', '/api/me/emails'),
  addMyEmail: (address: string) => request<UserEmail>('POST', '/api/me/emails', { address }),
  resendMyEmail: (id: number) => request<UserEmail>('POST', `/api/me/emails/${id}/resend`),
  deleteMyEmail: (id: number) => request<void>('DELETE', `/api/me/emails/${id}`),

  logout: () =>
    fetch('/auth/logout', { method: 'POST', credentials: 'include' }).then(() => undefined),

  // -------------------------------------------------------------------------
  // Trips (spec §5.2). The list returns my trips plus those shared with me.
  // -------------------------------------------------------------------------
  // include (superuser only): 'friends' = all friends' trips even if unshared;
  // 'all' = every trip in the system. Ignored server-side for non-superusers.
  listTrips: (include?: 'friends' | 'all') =>
    request<Trip[]>('GET', '/api/trips' + (include ? `?include=${include}` : '')),
  // The single-trip payload carries the timeline data (plans + parts) so the
  // detail view can render without further fetches.
  getTrip: (id: number) => request<Trip & { plans: Plan[] }>('GET', `/api/trips/${id}`),
  createTrip: (input: CreateTripInput) => request<Trip>('POST', '/api/trips', input),
  updateTrip: (id: number, patch: UpdateTripInput) =>
    request<Trip>('PATCH', `/api/trips/${id}`, patch),
  deleteTrip: (id: number) => request<void>('DELETE', `/api/trips/${id}`),
  addTripMember: (tripId: number, input: AddTripMemberInput) =>
    request<Trip>('POST', `/api/trips/${tripId}/members`, input),
  removeTripMember: (tripId: number, userId: number) =>
    request<void>('DELETE', `/api/trips/${tripId}/members/${userId}`),
  // Trip-level passengers (issue #20): travellers on the whole trip.
  addTripPassenger: (tripId: number, userId: number) =>
    request<Trip>('POST', `/api/trips/${tripId}/passengers`, { user_id: userId }),
  removeTripPassenger: (tripId: number, userId: number) =>
    request<void>('DELETE', `/api/trips/${tripId}/passengers/${userId}`),

  // Tags: set the full label list on a trip; suggest autocompletes over the
  // tags the viewer can see.
  setTripTags: (tripId: number, labels: string[]) =>
    request<Trip>('PUT', `/api/trips/${tripId}/tags`, { labels }),
  suggestTags: (q: string) =>
    request<TagSuggestion[]>('GET', `/api/tags/suggest?q=${encodeURIComponent(q)}`),

  // -------------------------------------------------------------------------
  // Plans & parts (spec §5.2).
  // -------------------------------------------------------------------------
  createPlan: (tripId: number, input: CreatePlanInput) =>
    request<Plan>('POST', `/api/trips/${tripId}/plans`, input),
  updatePlan: (id: number, patch: UpdatePlanInput) =>
    request<Plan>('PATCH', `/api/plans/${id}`, patch),
  deletePlan: (id: number) => request<void>('DELETE', `/api/plans/${id}`),
  addPlanPassenger: (planId: number, userId: number) =>
    request<Plan>('POST', `/api/plans/${planId}/passengers`, { user_id: userId }),
  removePlanPassenger: (planId: number, userId: number) =>
    request<void>('DELETE', `/api/plans/${planId}/passengers/${userId}`),
  setPlanVisibility: (planId: number, visibility: PlanVisibility) =>
    request<Plan>('PUT', `/api/plans/${planId}/visibility`, visibility),
  movePlan: (planId: number, input: MovePlanInput) =>
    request<Plan>('POST', `/api/plans/${planId}/move`, input),
  // Link the named plans into the primary as one multi-part booking (#12).
  linkPlans: (primaryId: number, input: LinkPlansInput) =>
    request<Plan>('POST', `/api/plans/${primaryId}/link`, input),
  // Split one leg out of a multi-part booking into its own plan (#12).
  splitPlanPart: (partId: number) =>
    request<Plan>('POST', `/api/plan-parts/${partId}/split`),
  updatePlanPart: (partId: number, patch: UpdatePlanPartInput) =>
    request<PlanPart>('PATCH', `/api/plan-parts/${partId}`, patch),
  // Tidy away a superseded part; stamps dismissed_at so the timeline omits it.
  dismissPlanPart: (partId: number) => request<void>('POST', `/api/plan-parts/${partId}/dismiss`),

  // -------------------------------------------------------------------------
  // Ingest (spec §5.2 / §6): paste/upload → proposed plans, then commit.
  // -------------------------------------------------------------------------
  // When a file is attached the request goes out as multipart/form-data so the
  // backend can forward the bytes to the document extractor (PDF tickets);
  // otherwise it stays a plain JSON {text, source} request.
  ingest: (tripId: number, input: IngestInput) => {
    if (input.file) {
      const form = new FormData();
      form.append('file', input.file, input.file.name);
      if (input.text) form.append('text', input.text);
      if (input.source) form.append('source', input.source);
      return requestMultipart<IngestResult>('POST', `/api/trips/${tripId}/ingest`, form);
    }
    return request<IngestResult>('POST', `/api/trips/${tripId}/ingest`, {
      text: input.text,
      source: input.source,
    });
  },
  ingestConfirm: (tripId: number, plans: ConfirmPlanInput[]) =>
    request<Plan[]>('POST', `/api/trips/${tripId}/ingest/confirm`, { plans }),

  // Import a whole TripIt .ics as its own trip (creates/reuses the trip from
  // the export and commits its plans, deduped — see POST /api/trips/import).
  importTrip: (file: File) => {
    const form = new FormData();
    form.append('file', file, file.name);
    return requestMultipart<ImportResult>('POST', '/api/trips/import', form);
  },

  // -------------------------------------------------------------------------
  // Calendar tokens (spec §5.2 / §8). The .ics feeds themselves are fetched
  // by external calendar clients via the token URL, not this client.
  // -------------------------------------------------------------------------
  listCalendarTokens: () => request<CalendarToken[]>('GET', '/api/calendar/tokens'),
  // Issue or regenerate (revoking the old one) a token for the given scope.
  issueCalendarToken: (scope: CalendarScope, id?: number) =>
    request<CalendarToken>('POST', '/api/calendar/tokens', { scope, id }),
  revokeCalendarToken: (token: string) =>
    request<void>('DELETE', `/api/calendar/tokens/${encodeURIComponent(token)}`),

  // -------------------------------------------------------------------------
  // Tracker (spec §5.2 / §7): convergence view of trackable parts.
  // -------------------------------------------------------------------------
  getTracker: (opts?: { from?: string; to?: string; tag?: string }) => {
    const params = new URLSearchParams();
    if (opts?.from) params.set('from', opts.from);
    if (opts?.to) params.set('to', opts.to);
    if (opts?.tag) params.set('tag', opts.tag);
    const qs = params.toString();
    return request<TrackerResponse>('GET', qs ? `/api/tracker?${qs}` : '/api/tracker');
  },

  // -------------------------------------------------------------------------
  // Alerts (spec §5.2 / §9).
  // -------------------------------------------------------------------------
  getAlertPrefs: () => request<AlertPrefs>('GET', '/api/alert-prefs'),
  updateAlertPrefs: (patch: UpdateAlertPrefsInput) =>
    request<AlertPrefs>('PUT', '/api/alert-prefs', patch),
  // Viewer opt-in to a specific plan's alerts.
  optInPlanAlerts: (planId: number) => request<void>('POST', `/api/plans/${planId}/alerts/optin`),
  optOutPlanAlerts: (planId: number) =>
    request<void>('DELETE', `/api/plans/${planId}/alerts/optin`),
};

export { ApiError };
