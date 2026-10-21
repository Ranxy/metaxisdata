-- One rule check is now the user-facing execution; the former per-run result
-- table becomes it. The feature is unreleased, so the two-table batch/result
-- model is replaced in place: a renamed table keeps any developer rows, which
-- the added state column marks COMPLETED (they all are, by construction).

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = current_schema() AND table_name = 'quality_rule_result'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = current_schema() AND table_name = 'quality_execution'
    ) THEN
        ALTER TABLE quality_rule_result RENAME TO quality_execution;
        ALTER INDEX IF EXISTS idx_quality_rule_result_run RENAME TO idx_quality_execution_run;
        ALTER INDEX IF EXISTS idx_quality_rule_result_rule_time RENAME TO idx_quality_execution_rule_time;
        ALTER INDEX IF EXISTS idx_quality_rule_result_target_time RENAME TO idx_quality_execution_target_time;
        ALTER INDEX IF EXISTS idx_quality_rule_result_started_at RENAME TO idx_quality_execution_started_at;
        ALTER SEQUENCE IF EXISTS quality_rule_result_id_seq RENAME TO quality_execution_id_seq;
    END IF;
END $$;

-- The chain-replay gate applies every historical incremental on top of
-- LATEST.sql: 0024 still creates the old table, so drop whatever it recreated.
-- On an upgrade the table was renamed above and this is a no-op.
DROP TABLE IF EXISTS quality_rule_result;

-- The renamed table kept the old constraint names; a fresh install gets the
-- execution names from LATEST.sql, so rename them here to converge the paths.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON c.conrelid = t.oid
        WHERE t.relname = 'quality_execution' AND c.conname = 'quality_rule_result_pkey'
    ) THEN
        ALTER TABLE quality_execution RENAME CONSTRAINT quality_rule_result_pkey TO quality_execution_pkey;
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON c.conrelid = t.oid
        WHERE t.relname = 'quality_execution' AND c.conname = 'quality_rule_result_run_id_fkey'
    ) THEN
        ALTER TABLE quality_execution RENAME CONSTRAINT quality_rule_result_run_id_fkey TO quality_execution_run_id_fkey;
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON c.conrelid = t.oid
        WHERE t.relname = 'quality_execution' AND c.conname = 'quality_rule_result_rule_id_fkey'
    ) THEN
        ALTER TABLE quality_execution RENAME CONSTRAINT quality_rule_result_rule_id_fkey TO quality_execution_rule_id_fkey;
    END IF;
END $$;

-- The old index was ascending; the execution list reads newest first. Dropping
-- and recreating is idempotent: a chain replay just rebuilds the same DESC index.
DROP INDEX IF EXISTS idx_quality_execution_started_at;
CREATE INDEX IF NOT EXISTS idx_quality_execution_started_at ON quality_execution(started_at DESC);

-- The target snapshot, the instance, the trigger and the initiator make one
-- execution readable without joining its batch or its rule revision.
ALTER TABLE quality_execution ADD COLUMN IF NOT EXISTS target_path JSONB NOT NULL DEFAULT '{}';
ALTER TABLE quality_execution ADD COLUMN IF NOT EXISTS instance_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE quality_execution ADD COLUMN IF NOT EXISTS trigger INT2 NOT NULL DEFAULT 0;
ALTER TABLE quality_execution ADD COLUMN IF NOT EXISTS initiator_id INT4;
-- A planned execution has no verdict and no finish time yet.
ALTER TABLE quality_execution ADD COLUMN IF NOT EXISTS state INT2 NOT NULL DEFAULT 3;
ALTER TABLE quality_execution ALTER COLUMN status SET DEFAULT 0;
ALTER TABLE quality_execution ALTER COLUMN finished_at DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_quality_execution_state ON quality_execution(state);
CREATE INDEX IF NOT EXISTS idx_quality_execution_trigger ON quality_execution(trigger);

-- An issue now points at the execution that showed the problem, not at the
-- internal batch; a batch has no user-facing identity anymore.
ALTER TABLE quality_issue ADD COLUMN IF NOT EXISTS first_seen_execution_id BIGINT;
ALTER TABLE quality_issue ADD COLUMN IF NOT EXISTS last_seen_execution_id BIGINT;
ALTER TABLE quality_issue DROP COLUMN IF EXISTS first_seen_run_id;
ALTER TABLE quality_issue DROP COLUMN IF EXISTS last_seen_run_id;

COMMENT ON COLUMN quality_execution.target_path IS 'Stored as QualityTargetPath (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_execution.thresholds IS 'Stored as QualityThresholds (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_execution.sample IS 'Stored as QualityFailureSamples (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_run.counts IS 'Stored as QualityStatusCounts (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_execution.state IS 'Lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 ABANDONED';
COMMENT ON COLUMN quality_run.status IS 'Internal dispatch batch lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 ABANDONED';
