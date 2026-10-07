-- notification is one in-app message for one user: the outcome of a sync
-- operation they asked for, or an ingestion failure the workspace
-- administrators need to see. It is personal data, so unlike every other table
-- its reads are scoped by recipient_id rather than by the workspace, which is
-- also why it carries no workspace column of its own.
--
-- The message body is stored as protojson of proto/store Notification. The
-- server keeps no prose in it: the client renders the localized text from the
-- detail and its own catalogs, so a message reads in the user's language and an
-- unknown type degrades to a generic sentence instead of an empty row.
CREATE TABLE IF NOT EXISTS notification (
    id BIGSERIAL PRIMARY KEY,
    recipient_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- NULL means unread.
    read_at TIMESTAMPTZ,
    -- Deduplication key of a background notification, empty when the message is
    -- not deduplicated. Format: <event>:<target>:<bucket>, where the bucket is a
    -- timestamp aligned to the event's suppression window.
    dedupe_key TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'
);

-- The inbox lists one user's messages newest first.
CREATE INDEX IF NOT EXISTS idx_notification_recipient_created_at
    ON notification(recipient_id, created_at DESC, id DESC);

-- The unread badge counts without scanning read rows.
CREATE INDEX IF NOT EXISTS idx_notification_recipient_unread
    ON notification(recipient_id) WHERE read_at IS NULL;

-- Suppresses a repeated background notification within its window. Enforced
-- here rather than in process memory so that replicas and restarts share it.
CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_dedupe
    ON notification(recipient_id, dedupe_key) WHERE dedupe_key <> '';

-- Notification messages are kept forever, the same deliberate choice audit_log
-- made: the ledger of what the system told a user is worth more than the disk it
-- costs, and nothing here prunes them.
COMMENT ON COLUMN notification.payload IS 'Stored as Notification (proto/store/store/notification.proto)';
