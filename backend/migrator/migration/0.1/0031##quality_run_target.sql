-- A dispatch batch now belongs to exactly one target: its checks that became due
-- in one scheduler tick, or a manual selection grouped by target. The target
-- identity and its display path are stored on the batch so the read model and
-- the target history no longer have to aggregate executions to know what the
-- batch was about. The data quality module has never shipped, so there is no
-- historical run to backfill; development data is cleared instead.
ALTER TABLE quality_run
    ADD COLUMN IF NOT EXISTS target_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS target_type INT2 NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS target_path JSONB NOT NULL DEFAULT '{}';

-- LATEST.sql declares the identity columns without defaults; drop the temporary
-- upgrade defaults so both schema paths converge.
ALTER TABLE quality_run
    ALTER COLUMN target_guid DROP DEFAULT,
    ALTER COLUMN target_type DROP DEFAULT;

CREATE INDEX IF NOT EXISTS idx_quality_run_target_started
    ON quality_run(target_guid, target_type, started_at DESC);
