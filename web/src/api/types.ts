export interface User {
  id: number;
  username: string;
  name: string;
  avatar_url: string;
  is_superuser: boolean;
  is_active: boolean;
  has_logged_in: boolean;
  /** Free-text home address, used as ingest context (e.g. "taxi from home"). */
  home_address: string;
  /** Preferred page size for the PDF itinerary download. Only present on the
   * signed-in user's own record (/api/me); absent for other viewers. */
  paper_size?: PaperSize;
  last_login_at?: string;
}

/** Page size for the downloadable PDF itinerary. A4 is the default. */
export type PaperSize = 'a4' | 'letter';

export interface AuthProvider {
  /** URL-safe identifier, used in /auth/{name}/login. */
  name: string;
  /** Human-readable name shown on the sign-in button. */
  label: string;
}

export interface Position {
  ts: string;
  lat: number;
  lon: number;
  altitude_ft?: number;
  groundspeed_kt?: number;
  heading_deg?: number;
  /** True for dead-reckoned positions filling an ADS-B coverage gap. */
  is_estimated: boolean;
}

export interface Capabilities {
  /** When true, the Add Flight dialog can drop to "ident + date" only. */
  resolver_available: boolean;
  /** Poll cadence in seconds; drives the "next update in N" footer. */
  poll_interval_sec: number;
  /** When true, the avatar menu shows the "Email addresses…" entry. */
  email_ingest_enabled: boolean;
  /** Forwarding address for email-ingest; absent when disabled. */
  email_ingest_address?: string;
  /** When true, plans can carry uploaded file attachments (issue #91). */
  attachments_enabled: boolean;
  /** Per-file upload cap in bytes; absent when attachments are disabled. */
  attachments_max_bytes?: number;
}

export interface UserEmail {
  id: number;
  address: string;
  verified: boolean;
  verified_at?: string;
  created_at: string;
}

/** Lightweight build identifier (GET /api/version), polled to detect a deploy. */
export interface VersionInfo {
  commit: string;
  short: string;
  build_time: string;
}

/** Superuser-only "About" / diagnostics payload (GET /api/admin/info). */
export interface AdminInfo {
  version: {
    commit: string;
    short: string;
    modified: boolean;
    build_time: string;
    go_version: string;
    os: string;
    arch: string;
  };
  runtime: {
    started_at: string;
    uptime_sec: number;
    goroutines: number;
    num_cpu: number;
  };
  config: {
    public_url: string;
    tracker: string;
    tracker_authed: boolean;
    resolver_available: boolean;
    poll_interval_sec: number;
    email_ingest_enabled: boolean;
    email_ingest_address?: string;
    llm_configured: boolean;
    llm_provider: string;
    llm_model: string;
    mail_configured: boolean;
    dev_auth_bypass: boolean;
    auth_github: boolean;
    auth_google: boolean;
  };
}

export interface ResolveFlightInput {
  ident: string;
  /** YYYY-MM-DD in UTC. */
  date: string;
}

export interface ResolvedFlight {
  ident: string;
  scheduled_out: string;
  scheduled_in: string;
  origin_iata: string;
  origin_lat: number;
  origin_lon: number;
  /** IANA timezone of the origin airport, empty if unknown. */
  origin_tz?: string;
  dest_iata: string;
  dest_lat: number;
  dest_lon: number;
  /** IANA timezone of the destination airport, empty if unknown. */
  dest_tz?: string;
  icao24: string;
  notes: string;
}

// The legacy single-flight model was retired in the trip-planning cut-over.
// The `Flight` shape and its `FlightStatus` survive because the Statistics
// dialog's flown/upcoming rollup (state/stats.ts) still reads /api/flights.
export type FlightStatus =
  | 'Scheduled'
  | 'Boarding'
  | 'Departed'
  | 'Enroute'
  | 'Arrived'
  | 'Cancelled'
  | 'Diverted'
  | string;

export interface Flight {
  id: number;
  ident: string;
  icao24?: string;
  scheduled_out: string;
  scheduled_in: string;
  estimated_out?: string;
  estimated_in?: string;
  actual_out?: string;
  actual_in?: string;
  origin_iata: string;
  origin_lat?: number;
  origin_lon?: number;
  /** IANA timezone of the origin airport; used to render scheduled_out
   * in the departure airport's local time. Empty when the IATA is unknown. */
  origin_tz?: string;
  dest_iata: string;
  dest_lat?: number;
  dest_lon?: number;
  /** IANA timezone of the destination airport; used to render scheduled_in
   * and estimated_in in the arrival airport's local time. Empty when the
   * IATA is unknown. */
  dest_tz?: string;
  status: FlightStatus;
  notes: string;
  last_polled_at?: string;
  created_by?: number;
  passenger_ids: number[];
  /** When true, every authenticated user can see the flight, regardless
   * of passenger / share-list membership. */
  is_public: boolean;
  /** Users explicitly granted view access (non-passengers, non-creator). */
  shared_user_ids: number[];
  latest_position?: Position;
  /** Recent positions in order (oldest → newest), for the flown-track line. */
  track?: Position[];
}

export interface InviteUserInput {
  username: string;
  name?: string;
  is_superuser?: boolean;
}

export interface UpdateUserInput {
  name?: string;
  is_superuser?: boolean;
  is_active?: boolean;
}

export type FriendshipStatus = 'pending' | 'accepted';

/** Direction is "" (empty) for accepted edges; the API leaves the field off
 * for those rows, so the optional/empty mix is intentional. */
export type FriendshipDirection = 'incoming' | 'outgoing' | '';

export interface Friendship {
  /** The other user in the pair. Absent (omitted on the wire) for outgoing
   *  pending invites — the inviter must not learn whether the target email
   *  belongs to a registered Aerly user. Present otherwise. */
  friend_id?: number;
  /** Inviter-typed email. Present only for outgoing pending invites. */
  email?: string;
  status: FriendshipStatus;
  direction?: FriendshipDirection;
  requested_at: string;
  accepted_at?: string;
}

export interface InviteFriendInput {
  email: string;
  message?: string;
}

export interface Notifications {
  /** Count of friendship rows where the viewer is the recipient and
   *  status is still 'pending'. */
  friend_requests_pending: number;
  /** Count of the viewer's unread flight alerts (in-app inbox). */
  unread_alerts: number;
  /** Count of unread share notifications (trips/plans shared with the viewer). */
  unread_shares: number;
}

export interface AcceptFriendTokenResult {
  /** Populated when the token resolved to a freshly-accepted row. */
  friendship?: Friendship;
  /** True when the pending row was already gone (already accepted,
   *  cancelled by the inviter, etc.). Mutually exclusive with
   *  `friendship`. */
  already?: boolean;
}

// ---------------------------------------------------------------------------
// Trip-planning redesign (Wave 0b scaffold).
//
// These types mirror the LOCKED backend DTO contract (spec §5.3). Field names
// match the JSON wire shape exactly — snake_case as the Go DTOs serialize it,
// same convention as the existing `Flight` type above. The backend agent (0a)
// builds to the same contract; any drift here must be flagged loudly.
// ---------------------------------------------------------------------------

/** The viewer's role on a trip, governing what they can edit. */
export type TripRole = 'owner' | 'editor' | 'viewer';

/** Role granted by an "always share with" default. 'viewer'/'editor' make the
 * person a trip member; 'passenger' adds them as a trip-level passenger (a
 * traveller on every plan). */
export type AutoShareRole = 'viewer' | 'editor' | 'passenger';

/** One "always share with" default: every new trip the user creates is
 * automatically shared with `user_id` at the given role. */
export interface AutoShare {
  user_id: number;
  role: AutoShareRole;
}

/** The kind of thing a plan (and each of its parts) represents. Selects which
 * per-type detail object is populated on a `PlanPart`. */
export type PlanType =
  | 'flight'
  | 'train'
  | 'hotel'
  | 'ground'
  | 'dining'
  | 'excursion'
  | 'ice_cream';

/** Lifecycle status of a single plan part. */
export type PlanPartStatus = 'planned' | 'confirmed' | 'cancelled';

/** How a plan's visibility is scoped within its trip.
 * - `everyone`: all trip members see it.
 * - `hidden_from`: all members except `user_ids` see it.
 * - `only_visible_to`: only `user_ids` (plus owner/passengers) see it. */
export type PlanVisibilityMode = 'everyone' | 'hidden_from' | 'only_visible_to';

export interface TripMember {
  user_id: number;
  role: string;
}

export interface Trip {
  id: number;
  name: string;
  destination: string;
  /** YYYY-MM-DD; absent when the trip has no fixed start. */
  starts_on?: string;
  /** YYYY-MM-DD; absent when the trip has no fixed end. */
  ends_on?: string;
  /** Role granted to all accepted friends: 'viewer', 'editor', or '' (disabled). */
  share_all_friends_role?: 'viewer' | 'editor';
  /** YYYY-MM-DD span inferred from the trip's parts (list payload), used to show
   * dates when starts_on/ends_on aren't set. */
  effective_start?: string;
  effective_end?: string;
  created_by?: number;
  /** The viewer's role on this trip. */
  my_role: TripRole;
  /** True when the viewer is a passenger on a plan in this trip (travelling on
   * it), as opposed to merely a shared viewer. Files the trip under "My trips"
   * and badges it (issue #19). */
  viewer_is_passenger: boolean;
  members: TripMember[];
  /** User ids added as trip-level passengers: travellers on the whole trip
   * (a passenger on every plan), distinct from members merely shared the
   * trip (issue #20). */
  passenger_ids: number[];
  tags: string[];
  /** Main country as a lowercase ISO 3166-1 alpha-2 code, for the card flag.
   * Absent while underived; "zz" means derived-but-unknown (no flag shown). */
  country_code?: string;
  /** Viewer's trip-level upcoming-plan reminder opt-in (#11): true when a row
   * exists; reminder_lead_hours is the lead time (default 24). */
  reminder_opted_in: boolean;
  reminder_lead_hours: number;
  created_at: string;
  updated_at: string;
}

/** A registered iCal feed on a trip ("external plans" source). Managed from the
 * Edit trip dialog; the cached events come from getTripExternalEvents. */
export interface TripFeed {
  id: number;
  trip_id: number;
  url: string;
  /** Optional friendly label; blank falls back to the host in the UI. */
  name: string;
  /** ISO timestamp of the last successful/attempted fetch; absent until first. */
  last_fetched_at?: string;
  /** Last fetch/parse error, surfaced on the Edit dialog. Absent when healthy. */
  last_error?: string;
}

/** A read-only event pulled from a trip's iCal feed. Kept separate from Plan:
 * external events never join the plan model (no split/link/sharing/map), they
 * only render as their own tiles behind the "Show external plans" toggle. */
export interface ExternalEvent {
  id: number;
  feed_id: number;
  /** Owning feed's display name, for the per-feed tile label/colour. */
  feed_name?: string;
  title: string;
  location?: string;
  description?: string;
  /** ISO datetime. */
  starts_at: string;
  ends_at?: string;
  /** IANA zone of the wall-clock time, for local display; absent for UTC. */
  start_tz?: string;
  all_day: boolean;
}

export interface PlanVisibility {
  mode: PlanVisibilityMode;
  user_ids: number[];
}

export interface FlightDetail {
  ident: string;
  icao24?: string;
  callsign: string;
  scheduled_out: string;
  scheduled_in: string;
  estimated_out?: string;
  estimated_in?: string;
  actual_out?: string;
  actual_in?: string;
  origin_iata: string;
  dest_iata: string;
  flight_status: string;
  origin_gate?: string;
  dest_gate?: string;
  origin_terminal?: string;
  dest_terminal?: string;
  /** Aircraft model, e.g. "Boeing 777-300ER". Empty until an airframe is assigned. */
  aircraft_type?: string;
  /** Arrival baggage belt/carousel. Empty until published near arrival. */
  dest_baggage_belt?: string;
  /** True when the route came from the flight-data provider. The origin/dest
   * IATA are editable only when this is false (a manually-entered flight the
   * provider can't track), so a manual route is never clobbered by a re-resolve. */
  resolved: boolean;
  last_polled_at?: string;
  latest_position?: Position;
  /** Recent positions in order (oldest → newest), for the flown-track line. */
  track?: Position[];
}

export interface HotelDetail {
  property_name: string;
  address: string;
  phone: string;
  room_type: string;
  guests?: number;
  /** Property's standard check-in time of day (HH:MM), if known. */
  standard_checkin?: string;
  /** Property's standard check-out time of day (HH:MM), if known. */
  standard_checkout?: string;
  /** Smart-suggested check-in derived from the surrounding plan (§10). */
  checkin_suggested?: string;
  /** Smart-suggested check-out derived from the surrounding plan (§10). */
  checkout_suggested?: string;
}

export interface TrainDetail {
  operator: string;
  service_no: string;
  coach: string;
  seat: string;
  class: string;
  platform: string;
}

export interface GroundDetail {
  provider: string;
  phone: string;
  vehicle: string;
  driver: string;
  pax?: number;
}

export interface DiningDetail {
  party_size?: number;
  reservation_name: string;
  phone: string;
}

export interface ExcursionDetail {
  provider: string;
  ticket_count?: number;
}

/** An ice cream stop: a 0–5 star rating for the find and a free-text note of
 * what was ordered. */
export interface IceCreamDetail {
  /** 0–5 stars. */
  rating: number;
  what_ordered: string;
}

export interface PlanPart {
  id: number;
  plan_id: number;
  type: PlanType;
  seq: number;
  starts_at: string;
  ends_at?: string;
  /** IANA timezone for `starts_at`. */
  start_tz: string;
  /** IANA timezone for `ends_at`. */
  end_tz: string;
  start_label: string;
  start_lat?: number;
  start_lon?: number;
  start_address: string;
  end_label: string;
  end_lat?: number;
  end_lon?: number;
  end_address: string;
  status: PlanPartStatus;
  /** Derived COALESCE(actual_*, estimated_*, scheduled_*) used to sort/group
   * every part type uniformly on the timeline. */
  effective_at: string;
  /** Set on the new part of a rebooking; points at the part it replaces. */
  supersedes_id?: number;
  /** When set, the part has been tidied away and drops off the timeline. */
  dismissed_at?: string;
  /** Exactly one of these is populated, selected by `type`. */
  flight?: FlightDetail;
  hotel?: HotelDetail;
  train?: TrainDetail;
  ground?: GroundDetail;
  dining?: DiningDetail;
  excursion?: ExcursionDetail;
  ice_cream?: IceCreamDetail;
  /** The owning plan's title (the user-facing name of the booking), copied onto
   * the part so the map marker/list can show it. Absent when unknown. */
  title?: string;
  /** Who added the plan + who's on it, so the map can show whose plan it is.
   * Populated on the tracker and trip-detail payloads. */
  owner?: User;
  passengers?: User[];
  /** Who the booking is with (airline, operator…), copied from the plan so the
   * map row can show it. Absent when unknown. */
  supplier_name?: string;
  /** Coordinates manually pinned by the user — a geocoder-proof override.
   * Absent (false) when the coordinates are geocoded from the address. */
  start_coords_pinned?: boolean;
  end_coords_pinned?: boolean;
  /** User id of the owner of the containing trip. The map hashes it to a
   * per-person colour so each person's trips share a hue (issue #13). Absent
   * (or 0) when unknown. */
  trip_owner_id?: number;
}

export interface Plan {
  id: number;
  trip_id: number;
  type: PlanType;
  title: string;
  confirmation_ref: string;
  /** e-ticket / ticket number for the booking, '' when unknown (issue #22). */
  ticket_number: string;
  notes: string;
  source: string;
  /** Booking total; absent when unknown. Pair with cost_currency (issue #22). */
  cost_amount?: number;
  /** ISO 4217 currency code for cost_amount, '' when unknown. */
  cost_currency: string;
  /** Who the booking is with (airline, hotel, operator, agent…), '' when unknown.
   * Part of the supplier contact block shown consistently on every plan type. */
  supplier_name: string;
  /** Supplier contact email for this booking, '' when unknown. */
  contact_email: string;
  /** Supplier contact phone for this booking, '' when unknown. */
  contact_phone: string;
  /** Supplier booking/management URL, '' when unknown. Rendered as an
   * open-in-new-tab link in view mode. */
  website: string;
  created_by?: number;
  /** When true, all accepted friends have access to this plan. */
  share_all_friends: boolean;
  passenger_ids: number[];
  visibility: PlanVisibility;
  /** Whether the requesting viewer has opted in to this plan's change alerts
   * (a per-viewer projection of plan_alert_optin). Drives PlanAlertToggle. */
  alert_opted_in: boolean;
  /** Viewer's per-plan reminder override (#11): "inherit" uses the trip
   * setting, "on"/"off" force it. reminder_lead_hours is the override's lead. */
  reminder_override: 'inherit' | 'on' | 'off';
  reminder_lead_hours: number;
  parts: PlanPart[];
  /** Uploaded file attachments (issue #91); always present, [] when none. */
  attachments: Attachment[];
  created_at: string;
  updated_at: string;
}

/** A file attached to a plan (issue #91). Metadata only — fetch the bytes from
 * GET /api/attachments/{id}. */
export interface Attachment {
  id: number;
  plan_id: number;
  filename: string;
  content_type: string;
  size_bytes: number;
  uploaded_by?: number;
  created_at: string;
}

/** A single trackable part as surfaced by the tracker convergence view. */
export interface TrackerPart {
  plan_part_id: number;
  plan_id: number;
  trip_id: number;
  owner_id?: number;
  title: string;
  status: string;
  effective_at: string;
  ident: string;
  dest_iata: string;
  latest_position?: Position;
  last_polled_at?: string;
  /** Recent positions in order (oldest → newest), for the flown-track line. */
  track?: Position[];
}

/** The tracker payload: flight convergence parts plus in-window venue markers. */
/** The unified tracker payload: every mappable, visible part in the window, as
 * full PlanParts (flights carry their track + latest position). */
export interface TrackerResponse {
  parts: PlanPart[];
}

/** A tag autocomplete candidate from /api/tags/suggest. */
export interface TagSuggestion {
  label: string;
}

// --- Inputs -----------------------------------------------------------------

export interface CreateTripInput {
  name: string;
  destination?: string;
  starts_on?: string;
  ends_on?: string;
}

export interface UpdateTripInput {
  name?: string;
  destination?: string;
  starts_on?: string;
  ends_on?: string;
}

export interface AddTripMemberInput {
  user_id: number;
  role: TripRole;
}

/** Input body for the share-by-email endpoint: pre-shares a trip to an address
 * that may not yet belong to an Aerly user. */
export interface ShareByEmailTripInput {
  email: string;
  role: 'viewer' | 'editor';
}

export interface CreatePlanInput {
  type: PlanType;
  title: string;
  confirmation_ref?: string;
  ticket_number?: string;
  notes?: string;
  cost_amount?: number;
  cost_currency?: string;
  supplier_name?: string;
  contact_email?: string;
  contact_phone?: string;
  website?: string;
  passenger_ids?: number[];
  visibility?: PlanVisibility;
  parts: PlanPartInput[];
}

export interface UpdatePlanInput {
  title?: string;
  confirmation_ref?: string;
  ticket_number?: string;
  notes?: string;
  cost_amount?: number;
  cost_currency?: string;
  supplier_name?: string;
  contact_email?: string;
  contact_phone?: string;
  website?: string;
}

/** A part as supplied when creating/editing a plan. */
export interface PlanPartInput {
  type: PlanType;
  seq?: number;
  starts_at: string;
  ends_at?: string;
  start_tz?: string;
  end_tz?: string;
  start_label?: string;
  start_lat?: number;
  start_lon?: number;
  start_address?: string;
  end_label?: string;
  end_lat?: number;
  end_lon?: number;
  end_address?: string;
  status?: PlanPartStatus;
  flight?: Partial<FlightDetail>;
  hotel?: Partial<HotelDetail>;
  train?: Partial<TrainDetail>;
  ground?: Partial<GroundDetail>;
  dining?: Partial<DiningDetail>;
  excursion?: Partial<ExcursionDetail>;
  ice_cream?: Partial<IceCreamDetail>;
}

export interface UpdatePlanPartInput {
  starts_at?: string;
  ends_at?: string;
  start_tz?: string;
  end_tz?: string;
  start_label?: string;
  start_lat?: number;
  start_lon?: number;
  start_address?: string;
  end_label?: string;
  end_lat?: number;
  end_lon?: number;
  end_address?: string;
  status?: PlanPartStatus;
  flight?: Partial<FlightDetail>;
  hotel?: Partial<HotelDetail>;
  train?: Partial<TrainDetail>;
  ground?: Partial<GroundDetail>;
  dining?: Partial<DiningDetail>;
  excursion?: Partial<ExcursionDetail>;
  ice_cream?: Partial<IceCreamDetail>;
  /** Pin/unpin a manual coordinate override so the geocoder leaves it alone.
   * Send true alongside start_lat/start_lon to pin; false to revert to auto. */
  start_coords_pinned?: boolean;
  end_coords_pinned?: boolean;
}

export interface MovePlanInput {
  trip_id: number;
}

/** Fold the given plans into a primary plan as one multi-part booking (#12). */
export interface LinkPlansInput {
  plan_ids: number[];
}

/** Source channel for an ingest request. */
export type IngestSource = 'paste' | 'upload' | 'email';

export interface IngestInput {
  /** Pasted text (Manual paste tab). */
  text?: string;
  source?: IngestSource;
  /** An uploaded document (e.g. a PDF ticket). When present the client sends
   * multipart/form-data so the backend extractor's binary/PDF path runs; absent
   * keeps the JSON/text path. */
  file?: File;
}

/** A plan proposed by the ingest pipeline, awaiting confirmation. */
export interface ProposedPlan {
  type: PlanType;
  title: string;
  confirmation_ref: string;
  ticket_number: string;
  notes: string;
  cost_amount?: number;
  cost_currency: string;
  supplier_name: string;
  contact_email: string;
  contact_phone: string;
  website: string;
  /** 0..1 extraction confidence; low values are flagged in the confirm step. */
  confidence: number;
  parts: PlanPart[];
  /** Set when this proposal would supersede an existing part (rebooking). */
  supersedes_part_id?: number;
}

export interface IngestResult {
  proposals: ProposedPlan[];
}

/** Result of importing a whole .ics (POST /api/trips/import). A TripIt export
 * yields one trip; a Kayak feed yields several, so `trips` lists them all
 * (created or reused) and `trip` is the first. `added`/`skipped` are totals
 * across every trip: plans added vs skipped as already-imported. */
export interface ImportResult {
  trip: Trip;
  trips: Trip[];
  added: number;
  skipped: number;
}

/** A confirmed/edited proposal sent back to /ingest/confirm. */
export interface ConfirmPlanInput {
  type: PlanType;
  title: string;
  confirmation_ref?: string;
  ticket_number?: string;
  notes?: string;
  cost_amount?: number;
  cost_currency?: string;
  supplier_name?: string;
  contact_email?: string;
  contact_phone?: string;
  website?: string;
  passenger_ids?: number[];
  visibility?: PlanVisibility;
  parts: PlanPartInput[];
  supersedes_part_id?: number;
}

export interface IngestConfirmInput {
  plans: ConfirmPlanInput[];
}

/** Scope of an iCal calendar feed token. */
export type CalendarScope = 'me' | 'trip' | 'plan';

export interface CalendarToken {
  scope: CalendarScope;
  /** Trip/plan id this token's feed is pinned to; 0 for the `me` scope. Tokens
   * are keyed per (scope, resource_id), so each trip/plan feed is independently
   * revocable. */
  resource_id: number;
  token: string;
  /** Ready-to-use feed URL. */
  url: string;
  created_at: string;
}

/** Generic in-app inbox item returned by GET /api/alerts.
 * Replaces the narrower FlightAlert list shape on the REST endpoint; the SSE
 * alert.created payload still uses FlightAlert. */
export interface NotificationItem {
  id: number;
  /** Backing table the item came from: 'flight' (flight_alerts, incl.
   * reminders) or 'notification' (shares). Selects the delete endpoint. */
  source: 'flight' | 'notification';
  kind: string;
  actor_id?: number;
  trip_id?: number;
  plan_id?: number;
  plan_part_id?: number;
  message: string;
  created_at: string;
  read_at?: string;
}

/** Input body for the notify-shares endpoints. */
export interface NotifySharesInput {
  user_ids: number[];
  emails: string[];
}

/** A persisted in-app flight-change alert (inbox item / alert.created payload). */
export interface FlightAlert {
  id: number;
  plan_part_id: number;
  plan_id: number;
  trip_id: number;
  ident: string;
  kind: string; // delayed|cancelled|diverted|gate
  status: string;
  message: string;
  created_at: string;
  read_at?: string;
}

export interface AlertPrefs {
  in_app: boolean;
  email: boolean;
  /** Suppress flight changes below this many minutes of delay. */
  min_delay_min: number;
}

export interface UpdateAlertPrefsInput {
  in_app?: boolean;
  email?: boolean;
  min_delay_min?: number;
}

// --- Web Push (PWA push notifications) ---

/** GET /api/push/vapid-key response. public_key is present only when enabled. */
export interface VapidKey {
  enabled: boolean;
  public_key?: string;
}

/** POST body for registering a device, mirroring PushSubscription.toJSON(). */
export interface PushSubscriptionInput {
  endpoint: string;
  keys: { p256dh: string; auth: string };
}

/** The notification kinds a user can independently toggle for push. */
export type PushKind = 'alert' | 'share';

/** GET/PATCH /api/push/prefs response: each known kind mapped to on/off. */
export interface PushPrefs {
  kinds: Record<PushKind, boolean>;
}
