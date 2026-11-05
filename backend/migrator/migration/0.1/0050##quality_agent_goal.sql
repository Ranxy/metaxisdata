-- A quality agent goal is one durable objective of the built-in runner: a target to
-- keep trustworthy, one event to investigate, or a person's request. The four durable
-- phases are active, paused, blocked (waiting for a person/window/dependency) and
-- complete; every mutation bumps revision so a stale writer is rejected. The step
-- table is the append-only journal behind the goal, with the tokens each step spent so
-- the workspace's hourly budget is a single indexed sum.
CREATE TABLE IF NOT EXISTS quality_agent_goal (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    workspace_id TEXT COLLATE "C" NOT NULL,
    -- Stored as QualityAgentGoalKind (proto/store/store/data_quality.proto)
    kind INT2 NOT NULL,
    -- Stored as QualityAgentGoalTrigger (proto/store/store/data_quality.proto)
    trigger INT2 NOT NULL DEFAULT 0,
    trigger_ref TEXT NOT NULL DEFAULT '',
    target_guid TEXT COLLATE "C" NOT NULL DEFAULT '',
    target_type INT2 NOT NULL DEFAULT 0,
    database_ref TEXT NOT NULL DEFAULT '',
    agent_id INT4 NOT NULL REFERENCES quality_agent(id) ON DELETE CASCADE,
    objective TEXT NOT NULL DEFAULT '',
    -- Stored as QualityAgentGoalPhase (proto/store/store/data_quality.proto)
    phase INT2 NOT NULL DEFAULT 1,
    blocked_code TEXT NOT NULL DEFAULT '',
    blocked_reason TEXT NOT NULL DEFAULT '',
    revision INT4 NOT NULL DEFAULT 1,
    -- Stored as QualityAgentGoalCheckpoint (proto/store/store/data_quality.proto)
    checkpoint JSONB NOT NULL DEFAULT '{}',
    rounds_started INT4 NOT NULL DEFAULT 0,
    max_rounds INT4 NOT NULL DEFAULT 256,
    tokens_used INT8 NOT NULL DEFAULT 0,
    next_wake_at TIMESTAMPTZ,
    lease_until TIMESTAMPTZ,
    restart_count INT4 NOT NULL DEFAULT 0,
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    cleared_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_goal_resource
    ON quality_agent_goal(resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_goal_maintain
    ON quality_agent_goal(workspace_id, target_guid)
    WHERE kind = 1 AND cleared_at IS NULL AND phase <> 4;
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_goal_event
    ON quality_agent_goal(workspace_id, "trigger", trigger_ref)
    WHERE kind = 2 AND cleared_at IS NULL AND phase <> 4;
CREATE INDEX IF NOT EXISTS idx_quality_agent_goal_due
    ON quality_agent_goal(phase, next_wake_at) WHERE cleared_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quality_agent_goal_workspace
    ON quality_agent_goal(workspace_id, phase);
CREATE INDEX IF NOT EXISTS idx_quality_agent_goal_database
    ON quality_agent_goal(database_ref) WHERE cleared_at IS NULL;
COMMENT ON TABLE quality_agent_goal IS 'Durable objectives of the built-in quality agent runner';
COMMENT ON COLUMN quality_agent_goal.kind IS 'Stored as QualityAgentGoalKind (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_goal.trigger IS 'Stored as QualityAgentGoalTrigger (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_goal.phase IS 'Stored as QualityAgentGoalPhase (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_goal.checkpoint IS 'Stored as QualityAgentGoalCheckpoint (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_agent_goal_id_seq RESTART WITH 101;

CREATE TABLE IF NOT EXISTS quality_agent_goal_step (
    id BIGSERIAL PRIMARY KEY,
    goal_id INT8 NOT NULL REFERENCES quality_agent_goal(id) ON DELETE CASCADE,
    workspace_id TEXT COLLATE "C" NOT NULL,
    seq INT4 NOT NULL,
    -- Stored as QualityAgentGoalStepKind (proto/store/store/data_quality.proto)
    kind INT2 NOT NULL,
    -- Stored as QualityAgentGoalStepPayload (proto/store/store/data_quality.proto)
    payload JSONB NOT NULL DEFAULT '{}',
    tokens INT4 NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_goal_step_seq
    ON quality_agent_goal_step(goal_id, seq);
CREATE INDEX IF NOT EXISTS idx_quality_agent_goal_step_workspace
    ON quality_agent_goal_step(workspace_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_goal_step_idempotency
    ON quality_agent_goal_step(goal_id, idempotency_key) WHERE idempotency_key <> '';
COMMENT ON TABLE quality_agent_goal_step IS 'Append-only journal of one quality agent goal';
COMMENT ON COLUMN quality_agent_goal_step.kind IS 'Stored as QualityAgentGoalStepKind (proto/store/store/data_quality.proto)';
COMMENT ON COLUMN quality_agent_goal_step.payload IS 'Stored as QualityAgentGoalStepPayload (proto/store/store/data_quality.proto)';
ALTER SEQUENCE quality_agent_goal_step_id_seq RESTART WITH 101;
