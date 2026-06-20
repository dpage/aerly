-- Trip-scoped iCal feed subscriptions (dynamic "external" plans).
--
-- A trip may register one or more iCalendar feed URLs (e.g. a conference's
-- published schedule). The server fetches and parses them periodically and
-- caches the resulting events in trip_feed_events. The events render as
-- read-only "external plan" tiles on the trip, gated behind a per-viewer
-- "Show external plans" toggle. Visibility is inherited wholesale from the
-- trip — there is no per-feed or per-event sharing.
CREATE TABLE trip_feeds (
    id              BIGSERIAL PRIMARY KEY,
    trip_id         BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    url             TEXT   NOT NULL,
    -- Optional friendly label; falls back to the feed's own X-WR-CALNAME, then
    -- the host, in the UI when blank.
    name            TEXT   NOT NULL DEFAULT '',
    -- Conditional-GET validators echoed back on the next poll so an unchanged
    -- feed costs a cheap 304 rather than a full re-parse.
    etag            TEXT   NOT NULL DEFAULT '',
    last_modified   TEXT   NOT NULL DEFAULT '',
    last_fetched_at TIMESTAMPTZ,
    -- Last fetch/parse error, surfaced on the Edit trip dialog. '' = healthy.
    last_error      TEXT   NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX trip_feeds_trip_idx ON trip_feeds (trip_id);

-- Materialised events parsed from a feed. Replaced wholesale on each refresh,
-- so there's no per-row update path — the feed is the source of truth.
CREATE TABLE trip_feed_events (
    id          BIGSERIAL PRIMARY KEY,
    feed_id     BIGINT NOT NULL REFERENCES trip_feeds(id) ON DELETE CASCADE,
    uid         TEXT   NOT NULL DEFAULT '',
    summary     TEXT   NOT NULL DEFAULT '',
    description TEXT   NOT NULL DEFAULT '',
    location    TEXT   NOT NULL DEFAULT '',
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ,
    -- IANA zone of the event's wall-clock time, for local-time display. '' when
    -- the feed gave a UTC or floating value.
    start_tz    TEXT   NOT NULL DEFAULT '',
    -- A date-only (VALUE=DATE) event, shown without a time of day.
    all_day     BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX trip_feed_events_feed_idx ON trip_feed_events (feed_id);
