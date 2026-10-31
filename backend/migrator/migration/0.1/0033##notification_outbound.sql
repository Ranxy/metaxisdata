-- Outbound notification delivery. A channel is an administrator-configured
-- destination (a webhook endpoint today); a delivery is one queued attempt to
-- post one notification event to one channel. They are separate from the
-- notification table, which holds one personal inbox row per recipient: the
-- outbox is workspace-level, so a single event reaches a channel once no matter
-- how many inbox rows it produced.
CREATE TABLE IF NOT EXISTS notification_channel (
    id SERIAL PRIMARY KEY,
    resource_id TEXT NOT NULL,
    -- Stored as NotificationChannel (proto/store/store/notification.proto)
    payload JSONB NOT NULL DEFAULT '{}',
    -- The v1: AES-256-GCM ciphertext of NotificationChannelSecret
    -- (proto/store/store/notification.proto). Write-only: no read returns it.
    secret TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_notification_channel_resource_id UNIQUE (resource_id)
);

CREATE TABLE IF NOT EXISTS notification_delivery (
    id BIGSERIAL PRIMARY KEY,
    -- One Send call is one event; every inbox row it wrote shares this id, so a
    -- channel cannot receive the same event twice.
    event_id TEXT NOT NULL,
    -- The notification dedupe key of the event, kept for diagnosis only.
    event_key TEXT NOT NULL DEFAULT '',
    channel_id INT4 NOT NULL REFERENCES notification_channel(id) ON DELETE CASCADE,
    -- 1 PENDING, 2 SENDING, 3 SENT, 4 DEAD (NotificationDeliveryState).
    state INT4 NOT NULL,
    attempts INT4 NOT NULL DEFAULT 0,
    -- Also the lease deadline while SENDING: an expired lease is reclaimed.
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NOT NULL DEFAULT '',
    -- Stored as NotificationDeliveryEvent (proto/store/store/notification.proto)
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    CONSTRAINT uk_notification_delivery_event_channel UNIQUE (event_id, channel_id)
);
CREATE INDEX IF NOT EXISTS idx_notification_delivery_due
    ON notification_delivery(next_attempt_at) WHERE state IN (1, 2);
CREATE INDEX IF NOT EXISTS idx_notification_delivery_channel_created
    ON notification_delivery(channel_id, created_at DESC, id DESC);

COMMENT ON TABLE notification_channel IS 'Administrator-configured outbound notification destinations';
COMMENT ON COLUMN notification_channel.payload IS 'Stored as NotificationChannel (proto/store/store/notification.proto)';
COMMENT ON COLUMN notification_channel.secret IS 'Encrypted NotificationChannelSecret (v1: AES-256-GCM); never returned by a read';
COMMENT ON TABLE notification_delivery IS 'Queued and attempted outbound deliveries, one per (event, channel)';
COMMENT ON COLUMN notification_delivery.payload IS 'Stored as NotificationDeliveryEvent (proto/store/store/notification.proto)';
COMMENT ON COLUMN notification_delivery.next_attempt_at IS 'Next automatic attempt, or the SENDING lease deadline';
