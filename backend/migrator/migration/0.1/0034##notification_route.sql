-- A quiet-hours hold is distinguishable from retry backoff, so a schedule change
-- can release exactly the rows it is holding instead of waiting for the old
-- window's end.
ALTER TABLE notification_delivery
    ADD COLUMN IF NOT EXISTS quiet_deferred BOOLEAN NOT NULL DEFAULT FALSE;

-- Owner/team routing rules for outbound notifications. A route selects extra
-- channels for the events whose in-app audience matches it, so a team can watch
-- the rules it owns without receiving the whole workspace. The target channels
-- are stored as resource ids in the payload; a deleted channel is ignored at
-- enqueue time rather than cascading the rule away.
CREATE TABLE IF NOT EXISTS notification_route (
    id SERIAL PRIMARY KEY,
    resource_id TEXT NOT NULL,
    -- Stored as NotificationRoute (proto/store/store/notification.proto)
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_notification_route_resource_id UNIQUE (resource_id)
);

COMMENT ON TABLE notification_route IS 'Owner/team routing rules for outbound notifications';
COMMENT ON COLUMN notification_route.payload IS 'Stored as NotificationRoute (proto/store/store/notification.proto)';
