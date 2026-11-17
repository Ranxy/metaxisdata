-- Multi-agent isolation: an enrollment, a session and a suggestion now belong to
-- one quality agent. A target can be covered by several agents (the settings page
-- warns about the duplicate cost), so the unique keys move from the workspace to
-- the agent. The data quality feature has never shipped; existing development rows
-- are reset rather than backfilled.

-- Enrollment: add the owning agent. The old unique constraint on
-- (workspace_id,target_guid) becomes (agent_id,target_guid).
ALTER TABLE quality_agent_enrollment
    ADD COLUMN IF NOT EXISTS agent_id INT4;
DELETE FROM quality_agent_enrollment;
ALTER TABLE quality_agent_enrollment
    ALTER COLUMN agent_id SET NOT NULL;
ALTER TABLE quality_agent_enrollment
    DROP CONSTRAINT IF EXISTS uk_quality_agent_enrollment_target;
ALTER TABLE quality_agent_enrollment
    DROP CONSTRAINT IF EXISTS uk_quality_agent_enrollment_target_agent;
ALTER TABLE quality_agent_enrollment
    ADD CONSTRAINT uk_quality_agent_enrollment_target_agent UNIQUE (agent_id, target_guid);
ALTER TABLE quality_agent_enrollment
    DROP CONSTRAINT IF EXISTS quality_agent_enrollment_agent_fk;
ALTER TABLE quality_agent_enrollment
    ADD CONSTRAINT quality_agent_enrollment_agent_fk
    FOREIGN KEY (agent_id) REFERENCES quality_agent(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_agent_admit
    ON quality_agent_enrollment(agent_id, state, priority DESC, target_guid);
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_agent_database
    ON quality_agent_enrollment(agent_id, database_ref);

-- Session: a per-target session is unique per agent, not per workspace. kind
-- separates the target work thread from the agent's front-office conversation.
ALTER TABLE quality_agent_session
    ADD COLUMN IF NOT EXISTS kind INT2 NOT NULL DEFAULT 1;
ALTER TABLE quality_agent_session
    ALTER COLUMN target_guid DROP NOT NULL;
ALTER TABLE quality_agent_session
    ALTER COLUMN target_guid SET DEFAULT '';
ALTER TABLE quality_agent_session
    DROP CONSTRAINT IF EXISTS quality_agent_session_kind_check;
ALTER TABLE quality_agent_session
    ADD CONSTRAINT quality_agent_session_kind_check
    CHECK (kind IN (1, 2) AND (kind = 1 OR target_guid = ''));
DROP INDEX IF EXISTS idx_quality_agent_session_target;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_target
    ON quality_agent_session(agent_id, target_guid)
    WHERE archived_at IS NULL AND kind = 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_session_conversation
    ON quality_agent_session(agent_id)
    WHERE archived_at IS NULL AND kind = 2;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_agent_due
    ON quality_agent_session(agent_id, state, next_wake_at)
    WHERE archived_at IS NULL;
COMMENT ON COLUMN quality_agent_session.kind IS '1 TARGET work thread, 2 CONVERSATION front office';

-- Transcript: the human who sent an ASK is recorded on the row.
ALTER TABLE quality_agent_message
    ADD COLUMN IF NOT EXISTS created_by INT4 REFERENCES principal(id) ON DELETE SET NULL;

-- Suggestions: an agent's proposals belong to that agent. Two agents covering the
-- same target each keep their own pending draft; a refresh may only touch its own
-- agent's row. Human/other proposals keep the old workspace-wide identity.
ALTER TABLE quality_suggestion
    ADD COLUMN IF NOT EXISTS agent_id INT4 REFERENCES quality_agent(id) ON DELETE SET NULL;
DROP INDEX IF EXISTS idx_quality_suggestion_pending_signature;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_pending_signature_agent
    ON quality_suggestion(agent_id, target_guid, target_type, signature)
    WHERE status = 1 AND agent_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_suggestion_pending_signature
    ON quality_suggestion(target_guid, target_type, signature)
    WHERE status = 1 AND agent_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_suggestion_agent_status
    ON quality_suggestion(agent_id, status, created_at DESC);
COMMENT ON COLUMN quality_suggestion.agent_id IS 'The proposing quality agent; NULL for human/external proposals';
