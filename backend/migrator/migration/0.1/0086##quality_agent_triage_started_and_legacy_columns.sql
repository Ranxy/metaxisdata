-- The triage rate counts starts, not any wake on a session that once triaged.
ALTER TABLE quality_agent_enrollment
    ADD COLUMN IF NOT EXISTS triage_started_at TIMESTAMPTZ;
COMMENT ON COLUMN quality_agent_enrollment.triage_started_at IS 'When the target last entered TRIAGE; the hourly triage rate';

-- 0083 was edited after some development databases had already recorded it; those
-- databases never re-ran the later ADD COLUMNs. Reapply them here idempotently instead
-- of editing a migration that a released version already recorded.
ALTER TABLE quality_agent_session
    ADD COLUMN IF NOT EXISTS manual_resume_at TIMESTAMPTZ;
ALTER TABLE quality_agent_enrollment
    ADD COLUMN IF NOT EXISTS participation INT2
    CHECK (participation IS NULL OR participation BETWEEN 1 AND 3);
ALTER TABLE quality_agent_attention
    ADD COLUMN IF NOT EXISTS case_resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS suggested_patch JSONB;

-- One active ask per (agent, dedupe key) is one OPEN ask. An ANSWERED ask must not
-- block a later question about the same target after the owner has already answered.
DROP INDEX IF EXISTS idx_quality_agent_ask_dedupe;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_ask_dedupe
    ON quality_agent_ask(agent_id, dedupe_key)
    WHERE dedupe_key <> '' AND status = 1;
