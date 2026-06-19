import type {
  AcceptFriendTokenResult,
  AddTripMemberInput,
  AdminInfo,
  AlertPrefs,
  AuthProvider,
  AutoShare,
  AutoShareRole,
  CalendarScope,
  CalendarToken,
  Capabilities,
  ConfirmPlanInput,
  CreatePlanInput,
  CreateTripInput,
  Flight,
  Friendship,
  IngestInput,
  IngestResult,
  ImportResult,
  InviteFriendInput,
  InviteUserInput,
  LinkPlansInput,
  MovePlanInput,
  NotificationItem,
  NotifySharesInput,
  Notifications,
  PaperSize,
  Plan,
  PlanPart,
  PlanVisibility,
  ResolveFlightInput,
  ResolvedFlight,
  ShareByEmailTripInput,
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
  VapidKey,
  PushSubscriptionInput,
  PushPrefs,
  PushKind,
  VersionInfo,
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

// downloadFile fetches a file response (session cookie included) and triggers a
// browser save. The filename comes from the Content-Disposition header when the
// server supplies one, otherwise the caller's fallback. Errors surface as
// ApiError, matching request()'s contract, so callers can toast on failure.
async function downloadFile(path: string, fallbackName: string): Promise<void> {
  const res = await fetch(path, { credentials: 'include' });
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const j = (await res.json()) as { error?: string };
      if (j.error) msg = j.error;
    } catch {
      // non-JSON body; keep status-only message.
    }
    throw new ApiError(res.status, msg);
  }
  const blob = await res.blob();
  const name = filenameFromDisposition(res.headers.get('Content-Disposition')) ?? fallbackName;
  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
  } finally {
    URL.revokeObjectURL(url);
  }
}

// filenameFromDisposition pulls the filename out of a Content-Disposition
// header (e.g. `attachment; filename="Paris-2026.ics"`). Returns null when the
// header is absent or carries no filename.
function filenameFromDisposition(header: string | null): string | null {
  if (!header) return null;
  const m = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(header);
  return m ? decodeURIComponent(m[1]) : null;
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
  updateMe: (patch: { home_address?: string; paper_size?: PaperSize }) =>
    request<User>('PATCH', '/api/me', patch),
  getConfig: () => request<Capabilities>('GET', '/api/config'),
  getVersion: () => request<VersionInfo>('GET', '/api/version'),

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

  // Superuser-only build/runtime/config diagnostics for the About dialog.
  getAdminInfo: () => request<AdminInfo>('GET', '/api/admin/info'),

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
  getAlerts: () => request<NotificationItem[]>('GET', '/api/alerts'),
  markAlertsRead: () => request<void>('POST', '/api/alerts/read'),
  deleteAlert: (source: NotificationItem['source'], id: number) =>
    request<void>('DELETE', `/api/alerts/${source}/${id}`),
  clearAlerts: () => request<void>('DELETE', '/api/alerts'),

  listMyEmails: () => request<UserEmail[]>('GET', '/api/me/emails'),
  addMyEmail: (address: string) => request<UserEmail>('POST', '/api/me/emails', { address }),
  resendMyEmail: (id: number) => request<UserEmail>('POST', `/api/me/emails/${id}/resend`),
  deleteMyEmail: (id: number) => request<void>('DELETE', `/api/me/emails/${id}`),

  // "Always share with" defaults: people every new trip the caller creates is
  // automatically shared with. setMyAutoShare returns the full updated list.
  listMyAutoShares: () => request<AutoShare[]>('GET', '/api/me/auto-shares'),
  setMyAutoShare: (userId: number, role: AutoShareRole) =>
    request<AutoShare[]>('PUT', `/api/me/auto-shares/${userId}`, { role }),
  removeMyAutoShare: (userId: number) => request<void>('DELETE', `/api/me/auto-shares/${userId}`),

  logout: () =>
    fetch('/auth/logout', { method: 'POST', credentials: 'include' }).then(() => undefined),

  // Sign out of every session (this device and all others) by bumping the
  // server-side session epoch, then clear the local cookie.
  logoutAll: () =>
    fetch('/auth/logout-all', { method: 'POST', credentials: 'include' }).then(() => undefined),

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
  // Downloads the trip's visible plans as a single .ics file (the inverse of
  // the TripIt/Kayak import). Session-authed; the server renders only the plans
  // this user can see and marks the response as an attachment, so we read it as
  // a blob and trigger a save with the server-supplied filename.
  exportTripIcs: (id: number) => downloadFile(`/api/trips/${id}/export.ics`, 'trip.ics'),
  // Downloads the trip's visible plans as a printable PDF itinerary, formatted
  // for the user's stored A4/US-Letter page size. Same session-auth + blob-save
  // path as the .ics export.
  exportTripPdf: (id: number) => downloadFile(`/api/trips/${id}/export.pdf`, 'trip.pdf'),
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
  splitPlanPart: (partId: number) => request<Plan>('POST', `/api/plan-parts/${partId}/split`),
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

  // Import a whole TripIt or Kayak .ics as its own trip(s) (creates/reuses the
  // trip(s) from the export and commits their plans, deduped; a Kayak feed can
  // yield several trips — see POST /api/trips/import).
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

  // Resolve a Google short link (maps.app.goo.gl etc.) to coordinates by
  // following its redirect server-side; full URLs are decoded client-side.
  resolveMapsUrl: (url: string) =>
    request<{ lat: number; lon: number }>('POST', '/api/maps/resolve', { url }),

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
  // Upcoming-plan reminders (#11): trip-level opt-in + per-plan override.
  setTripReminder: (tripId: number, leadHours: number) =>
    request<void>('PUT', `/api/trips/${tripId}/reminder`, { lead_hours: leadHours }),
  clearTripReminder: (tripId: number) => request<void>('DELETE', `/api/trips/${tripId}/reminder`),
  setPlanReminder: (planId: number, enabled: boolean, leadHours: number) =>
    request<void>('PUT', `/api/plans/${planId}/reminder`, { enabled, lead_hours: leadHours }),
  clearPlanReminder: (planId: number) => request<void>('DELETE', `/api/plans/${planId}/reminder`),

  // -------------------------------------------------------------------------
  // Share-all-friends & notify (share feature).
  // -------------------------------------------------------------------------
  setTripShareAllFriends: (tripId: number, role: 'viewer' | 'editor' | null) =>
    request<Trip>('PUT', `/api/trips/${tripId}/share-all-friends`, { role: role ?? '' }),
  setPlanShareAllFriends: (planId: number, enabled: boolean) =>
    request<Plan>('PUT', `/api/plans/${planId}/share-all-friends`, { enabled }),
  // Pre-share to an address that may not yet be an Aerly user. Returns 202 with
  // no body; we collapse it to Promise<void>.
  shareTripByEmail: (tripId: number, input: ShareByEmailTripInput) =>
    request<void>('POST', `/api/trips/${tripId}/share-by-email`, input).then(() => undefined),
  sharePlanByEmail: (planId: number, email: string) =>
    request<void>('POST', `/api/plans/${planId}/share-by-email`, { email }).then(() => undefined),
  notifyTripShares: (tripId: number, input: NotifySharesInput) =>
    request<void>('POST', `/api/trips/${tripId}/notify-shares`, input).then(() => undefined),
  notifyPlanShares: (planId: number, input: NotifySharesInput) =>
    request<void>('POST', `/api/plans/${planId}/notify-shares`, input).then(() => undefined),

  // -------------------------------------------------------------------------
  // Web Push (PWA push notifications). vapid-key reports whether push is
  // configured server-side and returns the public key needed to subscribe;
  // subscriptions register/unregister this device; prefs are per-kind toggles.
  // -------------------------------------------------------------------------
  getPushVapidKey: () => request<VapidKey>('GET', '/api/push/vapid-key'),
  subscribePush: (sub: PushSubscriptionInput) =>
    request<void>('POST', '/api/push/subscriptions', sub).then(() => undefined),
  unsubscribePush: (endpoint: string) =>
    request<void>('DELETE', '/api/push/subscriptions', { endpoint }).then(() => undefined),
  getPushPrefs: () => request<PushPrefs>('GET', '/api/push/prefs'),
  updatePushPref: (kind: PushKind, enabled: boolean) =>
    request<PushPrefs>('PATCH', '/api/push/prefs', { kind, enabled }),
};

export { ApiError };
