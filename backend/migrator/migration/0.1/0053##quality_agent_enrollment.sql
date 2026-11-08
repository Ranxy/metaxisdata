-- Automatic enrollment: the runner stops treating agent_runner_databases as a
-- creation gate and starts deriving a queue from it. One enrollment row records
-- the desired state of every monitorable object in scope; a session is created
-- lazily when the row is admitted. Sessions also gain a source (person or queue)
-- and a mode (monitor or triage).

ALTER TABLE quality_agent_session
    ADD COLUMN IF NOT EXISTS source INT2 NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS mode INT2 NOT NULL DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_quality_agent_session_source
    ON quality_agent_session(workspace_id, source, created_at DESC);
COMMENT ON COLUMN quality_agent_session.source IS 'Stored as QualityAgentSessionSource (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_session.mode IS 'Stored as QualityAgentSessionMode (proto/store/store/data_quality.proto)';

CREATE TABLE IF NOT EXISTS quality_agent_enrollment (
    id BIGSERIAL PRIMARY KEY,
    -- API-facing identity: workspaces/{workspace}/qualityAgentEnrollments/{resource_id}.
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    target_guid TEXT COLLATE "C" NOT NULL,
    target_type INT2 NOT NULL,
    -- instances/{instance}/databases/{database}.
    database_ref TEXT NOT NULL,
    -- 1 PENDING, 2 ACTIVE, 3 DORMANT, 4 EXCLUDED, 5 OUT_OF_SCOPE, 6 GONE
    -- (QualityAgentEnrollmentState).
    state INT2 NOT NULL DEFAULT 1,
    -- 1 MONITOR, 2 TRIAGE (QualityAgentSessionMode): what admission will create.
    mode INT2 NOT NULL DEFAULT 1,
    -- 0..1000; larger is admitted first.
    priority INT4 NOT NULL DEFAULT 0,
    -- Human-readable signals behind the score, for example "rules,failing".
    priority_reason TEXT NOT NULL DEFAULT '',
    -- Why the row is not progressing: a budget, a person or a missing dependency.
    blocked_reason TEXT NOT NULL DEFAULT '',
    session_id INT8 REFERENCES quality_agent_session(id) ON DELETE SET NULL,
    triage_count INT4 NOT NULL DEFAULT 0,
    last_triaged_at TIMESTAMPTZ,
    -- When a dormant row is due for another look; NULL while pending or active.
    next_review_at TIMESTAMPTZ,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    excluded_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    excluded_at TIMESTAMPTZ,
    revision INT4 NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_quality_agent_enrollment_target UNIQUE (workspace_id, target_guid)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_enrollment_resource
    ON quality_agent_enrollment(resource_id);
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_admit
    ON quality_agent_enrollment(workspace_id, state, priority DESC, target_guid);
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_database
    ON quality_agent_enrollment(database_ref, state);
CREATE INDEX IF NOT EXISTS idx_quality_agent_enrollment_review
    ON quality_agent_enrollment(state, next_review_at);
COMMENT ON TABLE quality_agent_enrollment IS 'One target''s place in the quality agent runner''s queue: desired state, priority and session link';
COMMENT ON COLUMN quality_agent_enrollment.state IS 'Stored as QualityAgentEnrollmentState (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_enrollment.mode IS 'Stored as QualityAgentSessionMode (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_agent_enrollment_id_seq RESTART WITH 101;
