-- Recreate the passenger⇒viewer trigger (mirror of 0010_trip_core).
CREATE FUNCTION plan_passenger_ensure_member() RETURNS trigger AS $$
BEGIN
    INSERT INTO trip_members (trip_id, user_id, role)
    SELECT p.trip_id, NEW.user_id, 'viewer'
    FROM plans p
    WHERE p.id = NEW.plan_id
    ON CONFLICT (trip_id, user_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER plan_passengers_ensure_member
    AFTER INSERT ON plan_passengers
    FOR EACH ROW
    EXECUTE FUNCTION plan_passenger_ensure_member();

DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS pending_shares;
ALTER TABLE plans DROP COLUMN IF EXISTS share_all_friends;
ALTER TABLE trips DROP COLUMN IF EXISTS share_all_friends_role;
