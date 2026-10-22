-- quality_suggestion_job is one background AI drafting job. The job fans out over
-- its targets, one LLM analysis per target, and leaves the queued suggestions for
-- human review. It is durable on purpose: the job outlives the browser tab that
-- submitted it, and clients poll it for progress.
CREATE TABLE IF NOT EXISTS quality_suggestion_job (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    -- 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 FAILED, 5 CANCELED.
    status INT2 NOT NULL DEFAULT 1,
    intent TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    targets JSONB NOT NULL DEFAULT '{}',
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    created_by INT4,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_job_resource_id ON quality_suggestion_job(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_job_status_id ON quality_suggestion_job(status, id);
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_job_created_at ON quality_suggestion_job(created_at DESC);
COMMENT ON TABLE quality_suggestion_job IS 'Background AI drafting jobs; each fans out over its targets and queues quality_suggestion rows';
COMMENT ON COLUMN quality_suggestion_job.status IS 'Lifecycle: 1 PENDING, 2 RUNNING, 3 COMPLETED, 4 FAILED, 5 CANCELED';
COMMENT ON COLUMN quality_suggestion_job.targets IS 'Stored as QualitySuggestionJobTargets (proto/store/store/data_quality.proto)';

-- Match LATEST.sql: a fresh install seeds the sequence at 101, and an upgraded
-- deployment must not start job ids at 1 (migrator/AGENTS.md keeps the two files
-- producing the same schema).
ALTER SEQUENCE quality_suggestion_job_id_seq RESTART WITH 101;
