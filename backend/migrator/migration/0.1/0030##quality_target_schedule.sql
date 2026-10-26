-- Scheduling moves from the rule to the target: a target has one timer, and all
-- of its enabled checks run together when it comes due. The data quality module
-- has never shipped, so there is no schedule state to backfill; existing
-- development databases are cleared instead of migrated row by row.
CREATE TABLE IF NOT EXISTS quality_target_schedule (
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- 15 minutes..7 days. The API validates the range; the runner keeps a minimum
    -- backstop for a malformed row.
    schedule_interval_seconds INT4 NOT NULL,
    -- When the scheduler next considers the target due; NULL means its checks are
    -- all paused or archived.
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (target_guid, target_type)
);
CREATE INDEX IF NOT EXISTS idx_quality_target_schedule_due ON quality_target_schedule(next_run_at)
    WHERE next_run_at IS NOT NULL;

DROP INDEX IF EXISTS idx_quality_rule_due;
ALTER TABLE quality_rule
    DROP COLUMN IF EXISTS next_run_at,
    DROP COLUMN IF EXISTS schedule_interval_seconds;
