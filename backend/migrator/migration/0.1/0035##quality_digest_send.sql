-- One row per weekly data quality digest period the runner has finished. The
-- unique period key is what makes a second tick in the schedule window skip a
-- period it already handled; it does not make enqueue atomic with this insert.
-- The runner enqueues first and records the period second, so a crash between
-- the two re-enqueues the same period with the same deterministic event id,
-- which the delivery unique index absorbs (a repeated enqueue is harmless), or
-- leaves the period unrecorded so the next tick retries it. A period row therefore
-- means "handled", not "delivered exactly once".
CREATE TABLE IF NOT EXISTS quality_digest_send (
    id BIGSERIAL PRIMARY KEY,
    period_key TEXT COLLATE "C" NOT NULL,
    -- How many outbound channels the digest was queued for. Zero records a week
    -- that had no channel subscribed, which is still a finished period.
    channel_count INT4 NOT NULL DEFAULT 0,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_digest_send_period ON quality_digest_send(period_key);

COMMENT ON TABLE quality_digest_send IS 'Weekly data quality digest periods the runner has handled';
COMMENT ON COLUMN quality_digest_send.period_key IS 'ISO week key in the digest schedule time zone, for example 2026-W44';
COMMENT ON COLUMN quality_digest_send.channel_count IS 'Outbound channels the digest was queued for; 0 means none subscribed';
