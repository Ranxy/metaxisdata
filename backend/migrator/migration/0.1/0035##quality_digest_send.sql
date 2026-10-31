-- One row per weekly data quality digest the runner has queued. The period key
-- is the ISO week in the digest schedule's time zone, so a tick that runs twice,
-- a restart in the middle of the schedule hour, or a catch-up pass after a short
-- outage cannot queue the same week's digest to the outbound channels twice.
CREATE TABLE IF NOT EXISTS quality_digest_send (
    id BIGSERIAL PRIMARY KEY,
    period_key TEXT COLLATE "C" NOT NULL,
    -- How many outbound channels the digest was queued for. Zero records a week
    -- that had no channel subscribed, which is still a finished period.
    channel_count INT4 NOT NULL DEFAULT 0,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_digest_send_period ON quality_digest_send(period_key);

COMMENT ON TABLE quality_digest_send IS 'Weekly data quality digest periods already queued';
COMMENT ON COLUMN quality_digest_send.period_key IS 'ISO week key in the digest schedule time zone, for example 2026-W44';
COMMENT ON COLUMN quality_digest_send.channel_count IS 'Outbound channels the digest was queued for; 0 means none subscribed';
