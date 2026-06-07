-- Surface (and alert on) the arrival baggage belt (flight-tiles-aircraft-gate
-- follow-up).
--
-- AeroDataBox returns the arrival baggage belt/carousel on the arrival movement
-- for many airports. Like gate, it is a live value that changes and is worth
-- alerting on, so the poller writes it overwrite-when-non-empty each tick and a
-- change to a new non-empty belt fires an in-app + email alert. Nullable;
-- NULL means "unknown / not yet resolved". Belt is arrival-only (there is no
-- departure equivalent), so a single dest column.
ALTER TABLE flight_details ADD COLUMN dest_baggage_belt TEXT;
